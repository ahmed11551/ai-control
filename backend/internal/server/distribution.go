package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

type distPool struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var distPools = []distPool{
	{ID: "depts", Label: "По отделам"},
	{ID: "skorozvon", Label: "Скорозвон"},
}

type distCand struct {
	DeptID string
	Share  float64
	Today  int
	Limit  int
	HeadID string
}

func normPool(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "skorozvon", "скорозвон":
		return "skorozvon"
	default:
		return "depts"
	}
}

func moscowDay(now time.Time) string {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	return now.In(loc).Format("2006-01-02")
}

// pickDistribution — дневные лимиты (не %).
//
//	У кандидата Limit = план на день, Today = уже выдано сегодня (МСК).
//	1) Только room (Today < Limit) — жёсткий кап, без soft overflow.
//	2) Выбираем минимальный fill = Today/Limit (пропорциональный catch-up).
//	3) Ничья: больший Limit → меньший Today → HeadID → DeptID.
//	4) Все на лимите — стоп (лид не трогаем, не в банк).
//	Limit=0 — не участвует. Legacy: только % без дневных лимитов.
func pickDistribution(cands []distCand) (distCand, error) {
	if got, ok := pickDailyQuota(cands, true); ok {
		return got, nil
	}
	hasDaily := false
	for _, c := range cands {
		if c.Limit > 0 {
			hasDaily = true
			break
		}
	}
	if hasDaily {
		return distCand{}, fmt.Errorf("дневные лимиты исчерпаны")
	}
	// Legacy fallback: только % без дневного лимита (пока админ не проставил лиды/день).
	legacy := make([]distCand, 0, len(cands))
	for _, c := range cands {
		if c.Share <= 0 {
			continue
		}
		// фиктивный лимит = share*10, чтобы catch-up по % работал пропорционально доле
		lim := int(c.Share * 10)
		if lim < 1 {
			lim = 1
		}
		legacy = append(legacy, distCand{
			DeptID: c.DeptID, Share: c.Share, Today: c.Today, Limit: lim, HeadID: c.HeadID,
		})
	}
	if got, ok := pickDailyQuota(legacy, true); ok {
		for _, c := range cands {
			if c.DeptID == got.DeptID && c.HeadID == got.HeadID {
				return c, nil
			}
		}
		return got, nil
	}
	return distCand{}, fmt.Errorf("нет отделов с дневным лимитом")
}

func pickDailyQuota(cands []distCand, respectRoom bool) (distCand, bool) {
	type scored struct {
		c    distCand
		fill float64
	}
	var ok []scored
	for _, c := range cands {
		if c.Limit <= 0 {
			continue
		}
		if respectRoom && c.Today >= c.Limit {
			continue
		}
		fill := float64(c.Today) / float64(c.Limit)
		ok = append(ok, scored{c: c, fill: fill})
	}
	if len(ok) == 0 {
		return distCand{}, false
	}
	best := ok[0]
	for i := 1; i < len(ok); i++ {
		s := ok[i]
		if dailyQuotaLess(s.c, s.fill, best.c, best.fill) {
			best = s
		}
	}
	return best.c, true
}

// dailyQuotaLess — true, если a должен получить следующий лид раньше b.
func dailyQuotaLess(a distCand, aFill float64, b distCand, bFill float64) bool {
	if aFill < bFill-1e-12 {
		return true
	}
	if aFill > bFill+1e-12 {
		return false
	}
	if a.Limit != b.Limit {
		return a.Limit > b.Limit
	}
	if a.Today != b.Today {
		return a.Today < b.Today
	}
	if a.HeadID != b.HeadID {
		return a.HeadID < b.HeadID
	}
	return a.DeptID < b.DeptID
}

// distEffShare оставлен для старых тестов/утилит; hot path использует Limit.
func distEffShare(c distCand) float64 {
	if c.Limit > 0 {
		return float64(c.Limit)
	}
	if c.Share > 0 {
		return c.Share
	}
	return 0
}

func equalShares(n int) []float64 {
	if n <= 0 {
		return nil
	}
	base := math.Floor((100.0/float64(n))*10) / 10
	out := make([]float64, n)
	sum := 0.0
	for i := range out {
		out[i] = base
		sum += base
	}
	out[0] = math.Round((out[0]+(100-sum))*10) / 10
	return out
}

func distSeedHint(name string) (pools []string, limit int) {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "мск 2") || strings.Contains(n, "мск2"):
		return []string{"depts"}, 0
	case strings.Contains(n, "екатеринбург"):
		return []string{"depts", "skorozvon"}, 150
	case strings.Contains(n, "фонтанк"):
		return []string{"depts"}, 200
	case strings.Contains(n, "чернышев"):
		return []string{"depts"}, 0
	case strings.Contains(n, "ильи"):
		return []string{"depts"}, 0
	case strings.Contains(n, "тай"), strings.Contains(n, "пирогов"),
		strings.Contains(n, "башня"), strings.Contains(n, "богдан"):
		return nil, 0
	case n == "москва" || strings.HasPrefix(n, "москва "):
		return nil, 0
	default:
		return nil, 0
	}
}

func (a *App) seedDistribution(ctx context.Context) {
	var n int
	if err := a.db.QueryRow(ctx, `SELECT count(*) FROM distribution_quotas`).Scan(&n); err != nil || n > 0 {
		return
	}
	deps := a.queryMaps(ctx, `SELECT id::text, name FROM departments ORDER BY name`)
	byPool := map[string][]string{}
	limits := map[string]int{}
	for _, d := range deps {
		id := fmt.Sprint(d["id"])
		name := fmt.Sprint(d["name"])
		pools, lim := distSeedHint(name)
		if lim > 0 {
			limits[id] = lim
		}
		for _, p := range pools {
			byPool[p] = append(byPool[p], id)
		}
	}
	for pool, ids := range byPool {
		shares := equalShares(len(ids))
		for i, id := range ids {
			_, _ = a.db.Exec(ctx, `
				INSERT INTO distribution_quotas (department_id, pool, share, daily_limit)
				VALUES ($1::uuid,$2,$3,$4)
				ON CONFLICT DO NOTHING`, id, pool, shares[i], limits[id])
		}
	}
}

func (a *App) listDistribution(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	_ = a.backfillMissingDeals(r.Context(), 500)
	a.seedDistribution(r.Context())
	a.ensureDeptAccessColumn(r.Context())
	day := moscowDay(time.Now())
	deps := a.queryMaps(r.Context(), `
		SELECT d.id::text, d.name, d.head_user_id::text AS head_id,
		       coalesce(h.full_name,'') AS head_name,
		       coalesce(d.access_enabled, true) AS access_enabled,
		       (SELECT count(*)::int FROM users u
		         WHERE u.is_active AND u.department_id=d.id) AS people,
		       (SELECT count(*)::int FROM leads l
		         JOIN users ou ON ou.id=l.owner_id
		         WHERE ou.department_id=d.id
		           AND coalesce(l.status,'') <> 'rejected') AS assigned
		FROM departments d
		LEFT JOIN users h ON h.id=d.head_user_id
		ORDER BY d.name`)
	if deps == nil {
		deps = []map[string]any{}
	}
	users := a.queryMaps(r.Context(), `
		SELECT u.id::text, u.full_name, u.department_id::text AS department_id,
		       coalesce(r.name,'') AS role
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.is_active
		ORDER BY u.full_name`)
	if users == nil {
		users = []map[string]any{}
	}
	rops := make([]map[string]any, 0)
	for _, u := range users {
		if strings.EqualFold(fmt.Sprint(u["role"]), "rop") {
			rops = append(rops, u)
		}
	}
	quotas := a.queryMaps(r.Context(), `
		SELECT q.department_id::text, q.pool, q.share::float8 AS share, q.daily_limit,
		       coalesce(c.assigned,0)::int AS today
		FROM distribution_quotas q
		LEFT JOIN distribution_counters c ON c.department_id=q.department_id AND c.day=$1::date`, day)
	if quotas == nil {
		quotas = []map[string]any{}
	}
	logRows := a.queryMaps(r.Context(), `
		SELECT l.id::text, l.pool, l.created_at,
		       coalesce(d.name,'') AS department,
		       coalesce(u.full_name,'') AS owner,
		       coalesce(dl.title,'') AS deal,
		       l.deal_id::text, l.lead_id::text
		FROM distribution_log l
		LEFT JOIN departments d ON d.id=l.department_id
		LEFT JOIN users u ON u.id=l.owner_id
		LEFT JOIN deals dl ON dl.id=l.deal_id
		ORDER BY l.created_at DESC LIMIT 40`)
	if logRows == nil {
		logRows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{
		"pools":       distPools,
		"departments": deps,
		"users":       users,
		"rops":        rops,
		"quotas":      quotas,
		"log":         logRows,
		"today":       day,
		"reset":       "00:00 Europe/Moscow",
		"limit_note":  "лимит общий на все пулы за сутки",
		"mode":        "catch_up",
		"mode_note":   "Заявка идёт РОПу отдела, который сегодня сильнее всего отстаёт от своей доли. Счётчики обнуляются в полночь по Москве.",
		"auto":        a.distAutoOn(r.Context()),
		"sources":     a.listDistSources(r.Context()),
		"hook":        a.cfg.InboxWebhookSecret != "",
		"avito":       a.cfg.AvitoReady(),
		"bank":        a.bankLeadCount(r.Context()),
	})
}

func (a *App) saveDistribution(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	var in struct {
		Quotas []struct {
			DepartmentID string  `json:"department_id"`
			Pool         string  `json:"pool"`
			Share        float64 `json:"share"`
			DailyLimit   int     `json:"daily_limit"`
		} `json:"quotas"`
		Heads []struct {
			DepartmentID string `json:"department_id"`
			HeadID       string `json:"head_id"`
		} `json:"heads"`
		Auto    *bool `json:"auto"`
		Sources []struct {
			Source string `json:"source"`
			Pool   string `json:"pool"`
		} `json:"sources"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	// Модель: только дневной лимит (share в БД пишем 0 — для совместимости схемы).
	for _, h := range in.Heads {
		hid := strings.TrimSpace(h.HeadID)
		if h.DepartmentID == "" || hid == "" {
			continue
		}
		if !a.userHasRole(r.Context(), hid, "rop") {
			writeJSON(w, 400, map[string]string{"error": "РОПом отдела может быть только пользователь с ролью РОП"})
			return
		}
	}
	for _, q := range in.Quotas {
		if q.DepartmentID == "" {
			continue
		}
		lim := q.DailyLimit
		if lim < 0 {
			lim = 0
		}
		_, err := a.db.Exec(r.Context(), `
			INSERT INTO distribution_quotas (department_id, pool, share, daily_limit)
			VALUES ($1::uuid,$2,0,$3)
			ON CONFLICT (department_id, pool) DO UPDATE SET share=0, daily_limit=EXCLUDED.daily_limit`,
			q.DepartmentID, normPool(q.Pool), lim)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить доли"})
			return
		}
	}
	for _, h := range in.Heads {
		if h.DepartmentID == "" {
			continue
		}
		_, err := a.db.Exec(r.Context(), `UPDATE departments SET head_user_id=nullif($2,'')::uuid WHERE id=$1`, h.DepartmentID, h.HeadID)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить РОП"})
			return
		}
		if h.HeadID != "" {
			_, _ = a.db.Exec(r.Context(), `
				UPDATE users SET department_id=$1::uuid
				WHERE id=$2::uuid AND (department_id IS NULL OR department_id<>$1::uuid)`,
				h.DepartmentID, h.HeadID)
		}
	}
	if in.Auto != nil {
		val := "0"
		if *in.Auto {
			val = "1"
		}
		_, _ = a.db.Exec(r.Context(), `
			INSERT INTO distribution_settings (key, value) VALUES ('auto', $1)
			ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, val)
	}
	if in.Sources != nil {
		_, _ = a.db.Exec(r.Context(), `DELETE FROM distribution_sources`)
		for _, s := range in.Sources {
			src := strings.ToLower(strings.TrimSpace(s.Source))
			if src == "" || src == "manual" {
				continue
			}
			_, _ = a.db.Exec(r.Context(), `
				INSERT INTO distribution_sources (source, pool) VALUES ($1,$2)
				ON CONFLICT (source) DO UPDATE SET pool=EXCLUDED.pool`, src, normPool(s.Pool))
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) createDepartment(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	var in struct {
		Name       string  `json:"name"`
		HeadID     string  `json:"head_id"`
		Share      float64 `json:"share"`
		DailyLimit int     `json:"daily_limit"`
		Pool       string  `json:"pool"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите название отдела"})
		return
	}
	share := in.Share
	if share < 0 {
		share = 0
	}
	if share > 100 {
		share = 100
	}
	share = math.Round(share*10) / 10
	lim := in.DailyLimit
	if lim < 0 {
		lim = 0
	}
	pool := normPool(in.Pool)
	headID := strings.TrimSpace(in.HeadID)
	if headID != "" && !a.userHasRole(r.Context(), headID, "rop") {
		writeJSON(w, 400, map[string]string{"error": "РОПом отдела может быть только пользователь с ролью РОП"})
		return
	}
	// доля нового отдела не должна ломать Σ100% пула
	if share > 0 {
		var used float64
		_ = a.db.QueryRow(r.Context(), `
			SELECT coalesce(sum(share),0)::float8 FROM distribution_quotas WHERE pool=$1`, pool).Scan(&used)
		remain := math.Round((100-used)*10) / 10
		if remain < 0 {
			remain = 0
		}
		if share > remain+0.05 {
			writeJSON(w, 400, map[string]string{
				"error": fmt.Sprintf("в пуле осталось только %.1f%% — уменьшите долю или нажмите «На 100%%» у существующих", remain),
			})
			return
		}
	}

	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO departments (name, head_user_id)
		VALUES ($1, nullif($2,'')::uuid)
		RETURNING id::text`, name, headID).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось создать отдел"})
		return
	}
	if headID != "" {
		_, _ = a.db.Exec(r.Context(), `UPDATE users SET department_id=$1::uuid WHERE id=$2::uuid`, id, headID)
	}
	for _, p := range []string{"depts", "skorozvon"} {
		s := 0.0
		l := 0
		if p == pool {
			s, l = share, lim
		}
		_, _ = a.db.Exec(r.Context(), `
			INSERT INTO distribution_quotas (department_id, pool, share, daily_limit)
			VALUES ($1::uuid,$2,$3,$4)
			ON CONFLICT (department_id, pool) DO UPDATE SET
			  share=CASE WHEN EXCLUDED.share>0 THEN EXCLUDED.share ELSE distribution_quotas.share END,
			  daily_limit=CASE WHEN EXCLUDED.daily_limit>0 THEN EXCLUDED.daily_limit ELSE distribution_quotas.daily_limit END`,
			id, p, s, l)
	}
	writeJSON(w, 201, map[string]any{
		"ok": true, "id": id, "name": name, "head_id": headID, "share": share, "daily_limit": lim, "pool": pool,
	})
}

func (a *App) assignDistribution(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	var in struct {
		DealID string `json:"deal_id"`
		LeadID string `json:"lead_id"`
		Ref    string `json:"ref"`
		Pool   string `json:"pool"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	pool := normPool(in.Pool)
	dealID, leadID := strings.TrimSpace(in.DealID), strings.TrimSpace(in.LeadID)
	ref := strings.TrimSpace(in.Ref)
	if dealID == "" && leadID == "" && ref != "" {
		_ = a.db.QueryRow(r.Context(), `SELECT id::text FROM deals WHERE id::text=$1 OR bitrix_id=$1 LIMIT 1`, ref).Scan(&dealID)
		if dealID == "" {
			_ = a.db.QueryRow(r.Context(), `SELECT id::text FROM leads WHERE id::text=$1 OR bitrix_id=$1 LIMIT 1`, ref).Scan(&leadID)
		}
	}
	if dealID == "" && leadID == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите сделку или лид"})
		return
	}
	res, err := a.distributeOne(r.Context(), dealID, leadID, pool, actorID(r))
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "pool": res.Pool, "department_id": res.DeptID,
		"department": res.DeptName, "owner_id": res.OwnerID, "owner": res.OwnerName,
		"deal_id": res.DealID, "lead_id": res.LeadID,
	})
}

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n := 0
		_, _ = fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

func (a *App) userHasRole(ctx context.Context, userID, want string) bool {
	if userID == "" || want == "" {
		return false
	}
	var name string
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(r.name,'') FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid AND u.is_active`, userID).Scan(&name)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(name), want)
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		var n float64
		_, _ = fmt.Sscanf(t, "%f", &n)
		return n
	default:
		return 0
	}
}
