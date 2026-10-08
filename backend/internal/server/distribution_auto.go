package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type distResult struct {
	Pool, DeptID, DeptName, OwnerID, OwnerName, DealID, LeadID string
}

func inferPoolFromSource(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	switch {
	case s == "" || s == "manual" || s == "other" || s == "referral":
		return ""
	case strings.Contains(s, "skorozvon") || strings.Contains(s, "скорозвон"):
		return "skorozvon"
	case strings.Contains(s, "bars") || strings.Contains(s, "барс"):
		return "depts"
	default:
		return "depts"
	}
}

func (a *App) distAutoOn(ctx context.Context) bool {
	var v string
	_ = a.db.QueryRow(ctx, `SELECT value FROM distribution_settings WHERE key='auto'`).Scan(&v)
	return v != "0"
}

func (a *App) listDistSources(ctx context.Context) []map[string]any {
	items := a.queryMaps(ctx, `SELECT source, pool FROM distribution_sources ORDER BY source`)
	if items == nil {
		return []map[string]any{}
	}
	return items
}

func (a *App) poolForSource(ctx context.Context, source string) string {
	src := strings.ToLower(strings.TrimSpace(source))
	if src == "" || src == "manual" {
		return ""
	}
	var pool string
	_ = a.db.QueryRow(ctx, `SELECT pool FROM distribution_sources WHERE source=$1`, src).Scan(&pool)
	if pool != "" {
		return normPool(pool)
	}
	return inferPoolFromSource(src)
}

func (a *App) maybeAutoDistribute(ctx context.Context, dealID, leadID, source, actor string) {
	if !a.distAutoOn(ctx) {
		return
	}
	pool := a.poolForSource(ctx, source)
	if pool == "" {
		return
	}
	_, _ = a.distributeOne(ctx, dealID, leadID, pool, actor)
}

func (a *App) distributeOne(ctx context.Context, dealID, leadID, pool, actor string) (distResult, error) {
	pool = normPool(pool)
	day := moscowDay(time.Now())
	a.ensureDeptAccessColumn(ctx)
	rows := a.queryMaps(ctx, `
		SELECT q.department_id::text, q.share::float8 AS share, q.daily_limit,
		       coalesce(c.assigned,0)::int AS today,
		       coalesce(d.head_user_id::text,'') AS head_id
		FROM distribution_quotas q
		JOIN departments d ON d.id=q.department_id
		LEFT JOIN distribution_counters c ON c.department_id=q.department_id AND c.day=$2::date
		WHERE q.pool=$1 AND coalesce(d.access_enabled, true)=true`, pool, day)
	var cands []distCand
	for _, row := range rows {
		cands = append(cands, distCand{
			DeptID: fmt.Sprint(row["department_id"]),
			Share:  asFloat(row["share"]),
			Today:  asInt(row["today"]),
			Limit:  asInt(row["daily_limit"]),
			HeadID: fmt.Sprint(row["head_id"]),
		})
	}
	picked, err := pickDistribution(cands)
	if err != nil {
		return distResult{}, err
	}
	owner := strings.TrimSpace(picked.HeadID)
	if owner == "" || owner == "<nil>" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM users
			WHERE is_active AND department_id=$1
			ORDER BY full_name LIMIT 1`, picked.DeptID).Scan(&owner)
	}
	if owner == "" {
		return distResult{}, fmt.Errorf("в отделе нет РОП и нет сотрудников")
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return distResult{}, fmt.Errorf("не удалось начать назначение")
	}
	defer tx.Rollback(ctx)

	// Сначала забираем только бесхозный лид в «Новый лид» — не трогаем назначенных и другие стадии.
	if leadID != "" {
		tag, err := tx.Exec(ctx, `
			UPDATE leads SET owner_id=$2::uuid, updated_at=now()
			WHERE id=$1::uuid
			  AND owner_id IS NULL
			  AND (`+sqlLeadOnNewApplicationStage("leads")+`)`, leadID, owner)
		if err != nil {
			return distResult{}, fmt.Errorf("не удалось назначить лид")
		}
		if tag.RowsAffected() == 0 {
			return distResult{}, fmt.Errorf("лид уже назначен или не в колонке «Новый лид»")
		}
		// сделка могла появиться раньше с пустым owner — подтянуть по lead_id
		_, _ = tx.Exec(ctx, `
			UPDATE deals SET owner_id=$2::uuid, updated_at=now()
			WHERE owner_id IS NULL AND (
				id=nullif($3,'')::uuid
				OR lead_id=$1::uuid
				OR id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
			)`, leadID, owner, dealID)
	} else if dealID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE deals SET owner_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, dealID, owner); err != nil {
			return distResult{}, fmt.Errorf("не удалось назначить сделку")
		}
	}
	var newCnt int
	err = tx.QueryRow(ctx, `
		WITH upsert AS (
			INSERT INTO distribution_counters (department_id, day, assigned)
			VALUES ($1::uuid,$2::date,1)
			ON CONFLICT (department_id, day) DO UPDATE
			  SET assigned = distribution_counters.assigned + 1
			WHERE distribution_counters.assigned < $3
			RETURNING assigned
		)
		SELECT assigned FROM upsert`, picked.DeptID, day, picked.Limit).Scan(&newCnt)
	if err != nil {
		return distResult{}, fmt.Errorf("дневные лимиты исчерпаны")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_log (pool, department_id, owner_id, deal_id, lead_id)
		VALUES ($1,$2::uuid,$3::uuid,nullif($4,'')::uuid,nullif($5,'')::uuid)`,
		pool, picked.DeptID, owner, dealID, leadID); err != nil {
		return distResult{}, fmt.Errorf("не удалось записать журнал")
	}
	if err := tx.Commit(ctx); err != nil {
		return distResult{}, fmt.Errorf("не удалось сохранить назначение")
	}
	var deptName, ownerName string
	_ = a.db.QueryRow(ctx, `SELECT name FROM departments WHERE id=$1`, picked.DeptID).Scan(&deptName)
	_ = a.db.QueryRow(ctx, `SELECT full_name FROM users WHERE id=$1`, owner).Scan(&ownerName)
	ent, eid := "deal", dealID
	if dealID == "" {
		ent, eid = "lead", leadID
	}
	payload := map[string]any{
		"pool": pool, "department": deptName, "owner": ownerName, "stage": "rop",
	}
	if ent == "deal" {
		a.logDealLeadAct(ctx, eid, actor, "distributed", payload)
	} else {
		a.logAct(ctx, ent, eid, actor, "distributed", payload)
	}
	res := distResult{
		Pool: pool, DeptID: picked.DeptID, DeptName: deptName,
		OwnerID: owner, OwnerName: ownerName, DealID: dealID, LeadID: leadID,
	}
	// Этап 2: менеджеру, только если автораздача включена и есть свободный дневной план.
	// Иначе заявка остаётся у РОПа. Запасного «отдай любому менеджеру» нет.
	if mgr, ok := a.distributeToManagers(ctx, picked.DeptID, dealID, leadID, pool, actor); ok {
		res.OwnerID = mgr.OwnerID
		res.OwnerName = mgr.OwnerName
	}
	res.DealID = a.finalizeDistributedAssignment(ctx, res.LeadID, res.DealID, res.OwnerID)
	return res, nil
}

func (a *App) leadsHook(w http.ResponseWriter, r *http.Request) {
	if !a.checkInboxHook(r) {
		writeJSON(w, 403, map[string]string{"error": "секрет вебхука не совпал"})
		return
	}
	if strings.EqualFold(chi.URLParam(r, "provider"), "avito") {
		a.handleAvitoApplications(w, r)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	raw := map[string]any{}
	_ = json.Unmarshal(body, &raw)
	title := firstScalar(raw["title"])
	if title == "" {
		title = firstScalar(raw["name"])
	}
	if title == "" {
		title = "Заявка " + time.Now().Format("02.01 15:04")
	}
	source := strings.ToLower(firstScalar(raw["source"]))
	if source == "" {
		source = strings.ToLower(chi.URLParam(r, "provider"))
	}
	if source == "" || source == "leads" {
		source = "site"
	}
	in := map[string]any{"title": title, "source": source}
	id, err := a.insertLead(r.Context(), in, "")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось создать лид"})
		return
	}
	var owner string
	_ = a.db.QueryRow(r.Context(), `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1`, id).Scan(&owner)
	if owner != "" {
		writeJSON(w, 201, map[string]any{"ok": true, "lead_id": id, "distributed": true})
		return
	}
	pool := firstScalar(raw["pool"])
	if pool == "" {
		pool = a.poolForSource(r.Context(), source)
	}
	if pool == "" {
		pool = "depts"
	}
	res, err := a.distributeOne(r.Context(), "", id, pool, "")
	if err != nil {
		// даже без раздачи — карточка в канбане (если есть owner) или сделка без owner для админа
		var own string
		_ = a.db.QueryRow(r.Context(), `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1`, id).Scan(&own)
		a.finalizeDistributedAssignment(r.Context(), id, "", own)
		writeJSON(w, 201, map[string]any{"ok": true, "lead_id": id, "distributed": false, "note": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{
		"ok": true, "lead_id": id, "distributed": true,
		"department": res.DeptName, "owner": res.OwnerName, "pool": res.Pool, "deal_id": res.DealID,
	})
}
