package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const exolveCLILease = 2 * time.Minute
const exolvePoolEnsureTTL = 15 * time.Minute

var (
	exolvePoolEnsureMu sync.Mutex
	exolvePoolEnsureAt = map[string]time.Time{}
)

type exolveSipRow struct {
	UserID        string
	SipLogin      string
	SipResourceID string
	SipPassword   string
	CarouselAuth  string
	Active        bool
}

func (a *App) exolveSession(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права на телефонию"})
		return
	}
	uid := actorID(r)
	sip, err := a.loadExolveSip(r.Context(), uid)
	if err == nil && sip.SipLogin != "" {
		var poolCount int
		_ = a.db.QueryRow(r.Context(), `
			SELECT count(*) FROM exolve_dids WHERE user_id=$1::uuid AND active`, uid).Scan(&poolCount)
		writeJSON(w, 200, map[string]any{
			"mode":            "pool",
			"sip_login":       sip.SipLogin,
			"sip_password":    sip.SipPassword,
			"sip_resource_id": sip.SipResourceID,
			"pool_count":      poolCount,
			"api_key":         a.cfg.ExolveAPIKey != "",
		})
		return
	}
	login := strings.TrimSpace(a.cfg.ExolveSIPLogin)
	pass := strings.TrimSpace(a.cfg.ExolveSIPPassword)
	if login == "" || pass == "" {
		writeJSON(w, 503, map[string]string{
			"error": "Нет личного SIP в пуле и нет VITE_EXOLVE_SIP_* в .env. Импортируйте SIP менеджера или задайте fallback.",
		})
		return
	}
	writeJSON(w, 200, map[string]any{
		"mode":         "fallback",
		"sip_login":    login,
		"sip_password": pass,
		"pool_count":   0,
		"api_key":      a.cfg.ExolveAPIKey != "",
	})
}

func (a *App) exolvePoolStatus(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права на телефонию"})
		return
	}
	uid := actorID(r)
	sip, sipErr := a.loadExolveSip(r.Context(), uid)
	rows, err := a.db.Query(r.Context(), `
		SELECT id::text, number, forwarding_ok, last_used_at, cli_lease_until
		FROM exolve_dids
		WHERE user_id=$1::uuid AND active
		ORDER BY last_used_at NULLS FIRST, number`, uid)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось загрузить пул"})
		return
	}
	defer rows.Close()

	type item struct {
		ID           string  `json:"id"`
		Number       string  `json:"number"`
		ForwardingOK bool    `json:"forwarding_ok"`
		LastUsedAt   *string `json:"last_used_at,omitempty"`
		LeaseUntil   *string `json:"cli_lease_until,omitempty"`
	}
	list := make([]item, 0)
	leased := ""
	now := time.Now()
	for rows.Next() {
		var id, number string
		var fwd bool
		var last, lease *time.Time
		if rows.Scan(&id, &number, &fwd, &last, &lease) != nil {
			continue
		}
		it := item{ID: id, Number: number, ForwardingOK: fwd}
		if last != nil {
			s := last.UTC().Format(time.RFC3339)
			it.LastUsedAt = &s
		}
		if lease != nil && lease.After(now) {
			s := lease.UTC().Format(time.RFC3339)
			it.LeaseUntil = &s
			if leased == "" {
				leased = number
			}
		}
		list = append(list, it)
	}

	fwdOK := 0
	for _, it := range list {
		if it.ForwardingOK {
			fwdOK++
		}
	}
	out := map[string]any{
		"mode":          "fallback",
		"sip_login":     a.cfg.ExolveSIPLogin,
		"carousel_auth": "",
		"numbers":       list,
		"pool_count":    len(list),
		"forwarding_ok": fwdOK,
		"leased_cli":    leased,
		"api_key":       a.cfg.ExolveAPIKey != "",
		"hint":          "Личный пул не назначен — нажмите «Подтянуть из Exolve» (привяжет SIP из .env и карусель).",
	}
	if sipErr == nil && sip.SipLogin != "" {
		out["mode"] = "pool"
		out["sip_login"] = sip.SipLogin
		out["carousel_auth"] = sip.CarouselAuth
		out["hint"] = ""
		if len(list) == 0 {
			out["hint"] = "Пул пуст — нажмите «Подтянуть из Exolve» (укажите auth карусели 88314…)."
		} else if fwdOK < len(list) {
			out["hint"] = "Часть номеров без переадресации на ваш SIP — нажмите «Проверить входящие»."
		}
	}
	writeJSON(w, 200, out)
}

func (a *App) exolveCLINext(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права на телефонию"})
		return
	}
	uid := actorID(r)
	if a.cfg.ExolveAPIKey == "" {
		writeJSON(w, 503, map[string]string{"error": "Задайте EXOLVE_API_KEY для карусели CLI"})
		return
	}

	_, _ = a.ensureExolvePoolReady(r.Context(), uid)

	sip, err := a.loadExolveSip(r.Context(), uid)
	if err != nil || sip.SipLogin == "" {
		writeJSON(w, 200, map[string]any{
			"mode": "fallback",
			"cli":  "",
			"ok":   true,
		})
		return
	}

	cli, err := a.reserveExolveCLI(r.Context(), uid, sip)
	if err != nil {
		status := 502
		msg := err.Error()
		if strings.Contains(msg, "занят") || strings.Contains(msg, "пуст") || strings.Contains(msg, "свободного") {
			status = 409
		}
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":   true,
		"mode": "pool",
		"cli":  cli,
		"sip":  sip.SipLogin,
	})
}

// exolvePoolEnsure — авто: SIP + CLI из .env + CF (без ручного Sync).
func (a *App) exolvePoolEnsure(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права на телефонию"})
		return
	}
	uid := actorID(r)
	poolCount, err := a.ensureExolvePoolReady(r.Context(), uid)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	sip, _ := a.loadExolveSip(r.Context(), uid)
	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"pool_count": poolCount,
		"sip":        sip.SipLogin,
		"mode":       "pool",
	})
}

func (a *App) exolveCLIRelease(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права на телефонию"})
		return
	}
	uid := actorID(r)
	var in struct {
		CLI string `json:"cli"`
	}
	_ = readJSON(r, &in)
	cli := normalizeExolveDID(in.CLI)

	ctx := r.Context()
	if cli != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE exolve_dids SET cli_lease_until=NULL, updated_at=now()
			WHERE user_id=$1::uuid AND number=$2 AND active`, uid, cli)
	} else {
		_, _ = a.db.Exec(ctx, `
			UPDATE exolve_dids SET cli_lease_until=NULL, updated_at=now()
			WHERE user_id=$1::uuid AND active AND cli_lease_until IS NOT NULL`, uid)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) exolvePoolProvision(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права на телефонию"})
		return
	}
	if a.cfg.ExolveAPIKey == "" {
		writeJSON(w, 503, map[string]string{"error": "Задайте EXOLVE_API_KEY"})
		return
	}
	uid := actorID(r)
	sip, err := a.ensureExolveSipForUser(r.Context(), uid)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	okN, failN, errs := a.provisionExolvePool(r.Context(), uid, sip.SipLogin, false)
	writeJSON(w, 200, map[string]any{
		"ok":         failN == 0,
		"sip":        sip.SipLogin,
		"ok_count":   okN,
		"fail_count": failN,
		"errors":     errs,
		"message":    fmt.Sprintf("Входящие: %d ок, %d ошибок → SIP %s", okN, failN, sip.SipLogin),
	})
}

// exolvePoolSync pulls DIDs from Exolve for the manager's SIP (carousel + CLI + CF).
func (a *App) exolvePoolSync(w http.ResponseWriter, r *http.Request) {
	if !a.canUseExolveInbound(r) {
		writeJSON(w, 403, map[string]string{"error": "нет права на телефонию"})
		return
	}
	if a.cfg.ExolveAPIKey == "" {
		writeJSON(w, 503, map[string]string{"error": "Задайте EXOLVE_API_KEY"})
		return
	}
	uid := actorID(r)
	var in struct {
		CarouselAuth string `json:"carousel_auth"`
	}
	_ = readJSON(r, &in)
	carouselAuth := strings.TrimSpace(in.CarouselAuth)

	sip, err := a.ensureExolveSipForUser(r.Context(), uid)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if carouselAuth != "" {
		sip.CarouselAuth = carouselAuth
		_, _ = a.db.Exec(r.Context(), `
			UPDATE exolve_sips SET carousel_auth=$2, updated_at=now() WHERE user_id=$1::uuid`,
			uid, carouselAuth)
	}

	numbers, sources, err := a.fetchExolveNumbersForSIP(r.Context(), sip)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	imported := 0
	var errs []string
	for _, n := range numbers {
		if !a.isCLIEligibleDID(r.Context(), n) {
			continue
		}
		if err := a.upsertExolveDID(r.Context(), uid, n); err != nil {
			errs = append(errs, n+": "+err.Error())
			continue
		}
		imported++
	}

	var poolCount int
	_ = a.db.QueryRow(r.Context(), `
		SELECT count(*) FROM exolve_dids WHERE user_id=$1::uuid AND active`, uid).Scan(&poolCount)

	hint := ""
	if imported == 0 {
		hint = "Карусель не вернула номера. Проверьте auth_number карусели в ЛК Exolve (например 88314…) и EXOLVE_API_KEY."
	}

	writeJSON(w, 200, map[string]any{
		"ok":            true,
		"sip":           sip.SipLogin,
		"carousel_auth": sip.CarouselAuth,
		"imported":      imported,
		"pool_count":    poolCount,
		"sources":       sources,
		"errors":        errs,
		"hint":          hint,
		"message": fmt.Sprintf(
			"Из Exolve подтянуто %d номеров (SIP %s, карусель %s) — в пуле %d",
			imported, sip.SipLogin, nz(sip.CarouselAuth, "—"), poolCount,
		),
	})
}

func (a *App) exolveAdminImport(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только админ"})
		return
	}
	var in struct {
		Managers []struct {
			UserID      string   `json:"user_id"`
			Email       string   `json:"email"`
			SipLogin    string   `json:"sip_login"`
			SipPassword string   `json:"sip_password"`
			Numbers     []string `json:"numbers"`
		} `json:"managers"`
	}
	if err := readJSON(r, &in); err != nil || len(in.Managers) == 0 {
		writeJSON(w, 400, map[string]string{"error": "нужен JSON { managers: [{ email|user_id, sip_login, sip_password, numbers[] }] }"})
		return
	}

	type mgrResult struct {
		Email    string   `json:"email,omitempty"`
		UserID   string   `json:"user_id"`
		SipLogin string   `json:"sip_login"`
		Imported int      `json:"imported"`
		Errors   []string `json:"errors,omitempty"`
	}
	results := make([]mgrResult, 0, len(in.Managers))
	ctx := r.Context()

	for _, m := range in.Managers {
		res := mgrResult{Email: strings.TrimSpace(m.Email)}
		uid := strings.TrimSpace(m.UserID)
		if uid == "" && res.Email != "" {
			_ = a.db.QueryRow(ctx, `SELECT id::text FROM users WHERE lower(email)=lower($1)`, res.Email).Scan(&uid)
		}
		if uid == "" {
			res.Errors = append(res.Errors, "пользователь не найден")
			results = append(results, res)
			continue
		}
		res.UserID = uid

		login := strings.TrimSpace(m.SipLogin)
		pass := strings.TrimSpace(m.SipPassword)
		if login == "" || pass == "" {
			res.Errors = append(res.Errors, "нужны sip_login и sip_password")
			results = append(results, res)
			continue
		}

		resourceID := ""
		if a.cfg.ExolveAPIKey != "" {
			if rid, _, _, err := a.exolveSIPContext(ctx, login); err == nil {
				resourceID = rid
			} else {
				res.Errors = append(res.Errors, "SIP в Exolve: "+err.Error())
			}
		}

		_, err := a.db.Exec(ctx, `
			INSERT INTO exolve_sips (user_id, sip_login, sip_resource_id, sip_password, active, updated_at)
			VALUES ($1::uuid, $2, $3, $4, true, now())
			ON CONFLICT (user_id) DO UPDATE SET
				sip_login=EXCLUDED.sip_login,
				sip_resource_id=CASE WHEN EXCLUDED.sip_resource_id<>'' THEN EXCLUDED.sip_resource_id ELSE exolve_sips.sip_resource_id END,
				sip_password=EXCLUDED.sip_password,
				active=true,
				updated_at=now()`,
			uid, login, resourceID, pass)
		if err != nil {
			res.Errors = append(res.Errors, "SIP: "+err.Error())
			results = append(results, res)
			continue
		}
		res.SipLogin = login

		for _, raw := range m.Numbers {
			n := normalizeExolveDID(raw)
			if n == "" || strings.HasPrefix(n, "88314") {
				if strings.TrimSpace(raw) != "" {
					res.Errors = append(res.Errors, "пропуск: "+raw)
				}
				continue
			}
			if a.autodialBridgeDIDSet(ctx)[n] {
				res.Errors = append(res.Errors, n+": номер скорозвона — не в CLI менеджера")
				continue
			}
			if err := a.upsertExolveDID(ctx, uid, n); err != nil {
				res.Errors = append(res.Errors, n+": "+err.Error())
				continue
			}
			res.Imported++
		}
		results = append(results, res)
	}

	writeJSON(w, 200, map[string]any{"ok": true, "results": results})
}

func (a *App) replaceExolveDIDs(ctx context.Context, userID string, numbers []string) {
	norm := make([]string, 0, len(numbers))
	seen := map[string]bool{}
	for _, n := range numbers {
		n = strings.TrimSpace(n)
		n = strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		norm = append(norm, n)
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE exolve_dids SET active=false, updated_at=now()
		WHERE user_id=$1::uuid AND active`, userID)
	for _, n := range norm {
		_ = a.upsertExolveDID(ctx, userID, n)
	}
}

func (a *App) upsertExolveDID(ctx context.Context, userID, number string) error {
	number = normalizeExolveDID(number)
	if number == "" {
		return fmt.Errorf("пустой номер")
	}
	// Связующие скорозвона не кладём в CLI-пул менеджера.
	if a.autodialBridgeDIDSet(ctx)[number] {
		return fmt.Errorf("номер скорозвона — не для CLI")
	}
	// Reassign if another user owns this active number
	_, _ = a.db.Exec(ctx, `
		UPDATE exolve_dids SET active=false, updated_at=now()
		WHERE number=$1 AND active AND user_id<>$2::uuid`, number, userID)

	tag, err := a.db.Exec(ctx, `
		UPDATE exolve_dids SET user_id=$2::uuid, active=true, updated_at=now()
		WHERE number=$1`, number, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = a.db.Exec(ctx, `
		INSERT INTO exolve_dids (number, user_id, active, updated_at)
		VALUES ($1, $2::uuid, true, now())`, number, userID)
	return err
}

// seedCLINumsForUser кладёт EXOLVE_CLI_NUMBERS в личный пул (обычные исходящие).
func (a *App) seedCLINumsForUser(ctx context.Context, userID string) {
	raw := strings.TrimSpace(a.cfg.ExolveCLINums)
	if raw == "" {
		return
	}
	for _, part := range strings.Split(raw, ",") {
		n := normalizeExolveDID(part)
		if n == "" || !a.isCLIEligibleDID(ctx, n) {
			continue
		}
		_ = a.upsertExolveDID(ctx, userID, n)
	}
}

func (a *App) poolEnsureStale(userID string) bool {
	exolvePoolEnsureMu.Lock()
	defer exolvePoolEnsureMu.Unlock()
	at, ok := exolvePoolEnsureAt[userID]
	return !ok || time.Since(at) > exolvePoolEnsureTTL
}

func (a *App) markExolvePoolEnsured(userID string) {
	exolvePoolEnsureMu.Lock()
	exolvePoolEnsureAt[userID] = time.Now()
	exolvePoolEnsureMu.Unlock()
}

// ensureExolvePoolReady — без ручного Sync: SIP, CLI из env, CF на личные номера.
func (a *App) ensureExolvePoolReady(ctx context.Context, userID string) (poolCount int, err error) {
	sip, err := a.ensureExolveSipForUser(ctx, userID)
	if err != nil {
		return 0, err
	}

	a.seedCLINumsForUser(ctx, userID)

	_ = a.db.QueryRow(ctx, `
		SELECT count(*) FROM exolve_dids WHERE user_id=$1::uuid AND active`, userID).Scan(&poolCount)

	// Подтянуть eligible номера из Exolve, если пул пуст или давно не синкали.
	if a.cfg.ExolveAPIKey != "" && (poolCount == 0 || a.poolEnsureStale(userID)) {
		if numbers, _, ferr := a.fetchExolveNumbersForSIP(ctx, sip); ferr == nil {
			for _, n := range numbers {
				if !a.isCLIEligibleDID(ctx, n) {
					continue
				}
				_ = a.upsertExolveDID(ctx, userID, n)
			}
		}
		a.markExolvePoolEnsured(userID)
		_ = a.db.QueryRow(ctx, `
			SELECT count(*) FROM exolve_dids WHERE user_id=$1::uuid AND active`, userID).Scan(&poolCount)
	}

	var needCF int
	_ = a.db.QueryRow(ctx, `
		SELECT count(*) FROM exolve_dids
		WHERE user_id=$1::uuid AND active AND NOT forwarding_ok`, userID).Scan(&needCF)
	if needCF > 0 && a.cfg.ExolveAPIKey != "" {
		_, _, _ = a.provisionExolvePool(ctx, userID, sip.SipLogin, true)
	}

	_ = a.db.QueryRow(ctx, `
		SELECT count(*) FROM exolve_dids WHERE user_id=$1::uuid AND active`, userID).Scan(&poolCount)
	return poolCount, nil
}

func (a *App) loadExolveSip(ctx context.Context, userID string) (exolveSipRow, error) {
	var s exolveSipRow
	err := a.db.QueryRow(ctx, `
		SELECT user_id::text, sip_login, coalesce(sip_resource_id,''), sip_password,
			coalesce(carousel_auth,''), active
		FROM exolve_sips WHERE user_id=$1::uuid AND active`, userID).
		Scan(&s.UserID, &s.SipLogin, &s.SipResourceID, &s.SipPassword, &s.CarouselAuth, &s.Active)
	if err != nil {
		return exolveSipRow{}, err
	}
	return s, nil
}

// ensureExolveSipForUser returns personal SIP or binds fallback .env SIP to this user (single-SIP setup).
func (a *App) ensureExolveSipForUser(ctx context.Context, userID string) (exolveSipRow, error) {
	if sip, err := a.loadExolveSip(ctx, userID); err == nil && sip.SipLogin != "" {
		_ = a.ensureSipResourceID(ctx, &sip)
		return sip, nil
	}
	login := strings.TrimSpace(a.cfg.ExolveSIPLogin)
	pass := strings.TrimSpace(a.cfg.ExolveSIPPassword)
	if login == "" || pass == "" {
		return exolveSipRow{}, fmt.Errorf("нет SIP: задайте VITE_EXOLVE_SIP_* или импортируйте SIP менеджера")
	}
	resourceID := ""
	if rid, _, _, err := a.exolveSIPContext(ctx, login); err == nil {
		resourceID = rid
	}
	_, err := a.db.Exec(ctx, `
		INSERT INTO exolve_sips (user_id, sip_login, sip_resource_id, sip_password, active, updated_at)
		VALUES ($1::uuid, $2, $3, $4, true, now())
		ON CONFLICT (user_id) DO UPDATE SET
			sip_login=EXCLUDED.sip_login,
			sip_resource_id=CASE WHEN EXCLUDED.sip_resource_id<>'' THEN EXCLUDED.sip_resource_id ELSE exolve_sips.sip_resource_id END,
			sip_password=EXCLUDED.sip_password,
			active=true,
			updated_at=now()`,
		userID, login, resourceID, pass)
	if err != nil {
		return exolveSipRow{}, fmt.Errorf("не удалось привязать SIP: %w", err)
	}
	return a.loadExolveSip(ctx, userID)
}

func (a *App) fetchExolveNumbersForSIP(ctx context.Context, sip exolveSipRow) (numbers []string, sources map[string]string, err error) {
	sources = map[string]string{}
	seen := map[string]bool{}
	add := func(raw, source string) {
		n := normalizeExolveDID(raw)
		if n == "" || strings.HasPrefix(n, "88314") || seen[n] {
			return
		}
		seen[n] = true
		numbers = append(numbers, n)
		sources[n] = source
	}

	if err := a.ensureSipResourceID(ctx, &sip); err == nil {
		_, cli, forwarded, e := a.exolveSIPContext(ctx, sip.SipLogin)
		if e == nil {
			add(cli, "sip_cli")
			for _, f := range forwarded {
				add(f, "sip_forwarding")
			}
		}
	}

	// Exolve native carousel: auth_number may differ from softphone SIP
	authKeys := []string{}
	if strings.TrimSpace(sip.CarouselAuth) != "" {
		authKeys = append(authKeys, strings.TrimSpace(sip.CarouselAuth))
	}
	if sip.SipLogin != "" {
		authKeys = append(authKeys, sip.SipLogin)
	}
	for _, key := range authKeys {
		if pool, e := a.exolveCarouselNumbers(ctx, key); e == nil {
			for _, n := range pool {
				add(n, "exolve_carousel")
			}
		}
	}

	return numbers, sources, nil
}

func (a *App) exolveCarouselNumbers(ctx context.Context, sipLogin string) ([]string, error) {
	var raw struct {
		Carousel []struct {
			AuthNumber string          `json:"auth_number"`
			NumberPool json.RawMessage `json:"number_pool"`
			Regional   *struct {
				Default []string `json:"default_number_pool"`
				Regions map[string]struct {
					Numbers []string `json:"numbers"`
				} `json:"regions"`
			} `json:"regional_number_pool"`
		} `json:"carousel"`
	}
	body := map[string]any{
		"auth_number": sipLogin,
		"limit":       50,
		"offset":      0,
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/carousel/v1/GetList", body, &raw); err != nil {
		raw.Carousel = nil
		if err2 := a.exolvePOST(ctx, "https://api.exolve.ru/carousel/v1/GetList", map[string]any{
			"limit":  50,
			"offset": 0,
		}, &raw); err2 != nil {
			return nil, err
		}
	}
	out := make([]string, 0)
	for _, c := range raw.Carousel {
		auth := strings.TrimSpace(c.AuthNumber)
		if auth != "" && auth != sipLogin && normalizeExolveDID(auth) != normalizeExolveDID(sipLogin) {
			continue
		}
		out = append(out, parseExolveNumberPool(c.NumberPool)...)
		if c.Regional != nil {
			out = append(out, c.Regional.Default...)
			for _, reg := range c.Regional.Regions {
				out = append(out, reg.Numbers...)
			}
		}
	}
	return out, nil
}

// parseExolveNumberPool accepts both []string and {"numbers":[]string} shapes from Exolve.
func parseExolveNumberPool(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var asList []string
	if json.Unmarshal(raw, &asList) == nil {
		return asList
	}
	var asObj struct {
		Numbers []string `json:"numbers"`
	}
	if json.Unmarshal(raw, &asObj) == nil {
		return asObj.Numbers
	}
	return nil
}

func (a *App) ensureSipResourceID(ctx context.Context, sip *exolveSipRow) error {
	if strings.TrimSpace(sip.SipResourceID) != "" {
		return nil
	}
	if a.cfg.ExolveAPIKey == "" {
		return fmt.Errorf("нет sip_resource_id и EXOLVE_API_KEY")
	}
	rid, _, _, err := a.exolveSIPContext(ctx, sip.SipLogin)
	if err != nil {
		return err
	}
	sip.SipResourceID = rid
	_, _ = a.db.Exec(ctx, `
		UPDATE exolve_sips SET sip_resource_id=$2, updated_at=now() WHERE user_id=$1::uuid`,
		sip.UserID, rid)
	return nil
}

func (a *App) reserveExolveCLI(ctx context.Context, userID string, sip exolveSipRow) (string, error) {
	if err := a.ensureSipResourceID(ctx, &sip); err != nil {
		return "", fmt.Errorf("SIP resource: %w", err)
	}

	exclude := a.autodialBridgeDIDs(ctx)
	if exclude == nil {
		exclude = []string{}
	}
	allowList := []string{}
	if allow := a.cliAllowlistSet(); allow != nil {
		seen := map[string]bool{}
		for n := range allow {
			nn := normalizeExolveDID(n)
			if nn == "" || seen[nn] {
				continue
			}
			seen[nn] = true
			allowList = append(allowList, nn)
		}
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("не удалось начать транзакцию")
	}
	defer tx.Rollback(ctx)

	var id, number string
	err = tx.QueryRow(ctx, `
		SELECT id::text, number FROM exolve_dids
		WHERE user_id=$1::uuid AND active
		  AND (cli_lease_until IS NULL OR cli_lease_until < now())
		  AND regexp_replace(number,'[^0-9]','','g') <> ALL($2::text[])
		  AND (
		    cardinality($3::text[]) = 0
		    OR regexp_replace(number,'[^0-9]','','g') = ANY($3::text[])
		  )
		ORDER BY last_used_at NULLS FIRST, number
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, userID, exclude, allowList).Scan(&id, &number)
	if err == pgx.ErrNoRows {
		var nActive, nLeased, nEligible int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM exolve_dids WHERE user_id=$1::uuid AND active`, userID).Scan(&nActive)
		_ = tx.QueryRow(ctx, `
			SELECT count(*) FROM exolve_dids
			WHERE user_id=$1::uuid AND active AND cli_lease_until IS NOT NULL AND cli_lease_until >= now()`, userID).Scan(&nLeased)
		_ = tx.QueryRow(ctx, `
			SELECT count(*) FROM exolve_dids
			WHERE user_id=$1::uuid AND active
			  AND regexp_replace(number,'[^0-9]','','g') <> ALL($2::text[])
			  AND (
			    cardinality($3::text[]) = 0
			    OR regexp_replace(number,'[^0-9]','','g') = ANY($3::text[])
			  )`, userID, exclude, allowList).Scan(&nEligible)
		if nActive == 0 || nEligible == 0 {
			return "", fmt.Errorf("пул CLI пуст — назначьте личные номера (не скорозвон)")
		}
		if nLeased > 0 {
			return "", fmt.Errorf("исходящий уже занят (lease) — завершите текущий звонок")
		}
		return "", fmt.Errorf("нет свободного номера в пуле")
	}
	if err != nil {
		return "", fmt.Errorf("выбор CLI: %w", err)
	}

	leaseUntil := time.Now().Add(exolveCLILease)
	if _, err := tx.Exec(ctx, `
		UPDATE exolve_dids
		SET last_used_at=now(), cli_lease_until=$2, updated_at=now()
		WHERE id=$1::uuid`, id, leaseUntil); err != nil {
		return "", fmt.Errorf("не удалось закрепить CLI")
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("не удалось сохранить CLI")
	}

	if err := a.exolveSetDisplayNumber(ctx, sip.SipResourceID, number); err != nil {
		_, _ = a.db.Exec(ctx, `
			UPDATE exolve_dids SET cli_lease_until=NULL, updated_at=now() WHERE id=$1::uuid`, id)
		return "", fmt.Errorf("SetDisplayNumber: %w", err)
	}
	return number, nil
}

func (a *App) provisionExolvePool(ctx context.Context, userID, sipLogin string, onlyMissing bool) (okN, failN int, errs []string) {
	q := `
		SELECT id::text, number FROM exolve_dids WHERE user_id=$1::uuid AND active ORDER BY number`
	if onlyMissing {
		q = `
		SELECT id::text, number FROM exolve_dids
		WHERE user_id=$1::uuid AND active AND NOT forwarding_ok ORDER BY number`
	}
	rows, err := a.db.Query(ctx, q, userID)
	if err != nil {
		return 0, 1, []string{err.Error()}
	}
	defer rows.Close()

	type pair struct{ id, number string }
	var items []pair
	for rows.Next() {
		var id, number string
		if rows.Scan(&id, &number) == nil {
			if !a.isCLIEligibleDID(ctx, number) {
				continue
			}
			items = append(items, pair{id, number})
		}
	}

	for _, it := range items {
		if err := a.exolveSetCallForwarding(ctx, it.number, sipLogin); err != nil {
			failN++
			errs = append(errs, it.number+": "+err.Error())
			_, _ = a.db.Exec(ctx, `UPDATE exolve_dids SET forwarding_ok=false, updated_at=now() WHERE id=$1::uuid`, it.id)
			continue
		}
		okN++
		_, _ = a.db.Exec(ctx, `UPDATE exolve_dids SET forwarding_ok=true, updated_at=now() WHERE id=$1::uuid`, it.id)
	}
	return okN, failN, errs
}
