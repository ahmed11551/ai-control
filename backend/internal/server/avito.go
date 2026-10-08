package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var avitoHTTP = &http.Client{Timeout: 20 * time.Second}

var avitoTok struct {
	mu  sync.Mutex
	tok string
	exp time.Time
}

type avitoApply struct {
	ID, Name, Phone, Vacancy string
}

func extractAvitoIDs(raw any) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	if s, ok := raw.(string); ok {
		add(s)
		return out
	}
	walkIDs(raw, add, 0)
	return out
}

func walkIDs(v any, add func(string), depth int) {
	if depth > 6 || v == nil {
		return
	}
	switch t := v.(type) {
	case string:
		if depth > 0 {
			add(t)
		}
	case []any:
		for _, item := range t {
			switch x := item.(type) {
			case string:
				add(x)
			default:
				walkIDs(x, add, depth+1)
			}
		}
	case map[string]any:
		for _, k := range []string{"id", "application_id", "apply_id", "ids"} {
			if x, ok := t[k]; ok {
				if k == "ids" {
					walkIDs(x, add, depth+1)
					continue
				}
				if s := firstScalar(x); s != "" {
					add(s)
				}
			}
		}
		for _, k := range []string{"applications", "applies", "items", "data", "payload", "value"} {
			if x, ok := t[k]; ok {
				walkIDs(x, add, depth+1)
			}
		}
	}
}

func avitoLeadTitle(name, vacancy, appID string) string {
	name = strings.TrimSpace(name)
	vacancy = strings.TrimSpace(vacancy)
	switch {
	case name != "" && vacancy != "":
		return name + " · Avito " + vacancy
	case name != "":
		return name + " · Avito"
	case vacancy != "":
		return "Avito · " + vacancy
	case appID != "":
		return "Avito отклик " + appID
	default:
		return "Avito отклик"
	}
}

func parseAvitoApply(m map[string]any) avitoApply {
	out := avitoApply{ID: firstScalar(m["id"])}
	out.Vacancy = firstScalar(m["vacancy_id"])
	app, _ := asMap(m["applicant"])
	if app != nil {
		out.Name = firstScalar(app["name"])
		data := asMapOr(app["data"], app)
		if out.Name == "" {
			out.Name = firstScalar(data["name"])
		}
		if out.Name == "" {
			fn := asMapOr(data["full_name"], nil)
			if fn != nil {
				out.Name = strings.TrimSpace(strings.Join([]string{
					firstScalar(fn["last_name"]),
					firstScalar(fn["first_name"]),
					firstScalar(fn["patronymic"]),
				}, " "))
			}
		}
	}
	contacts := asMapOr(m["contacts"], nil)
	if contacts != nil {
		phones, _ := contacts["phones"].([]any)
		for _, p := range phones {
			pm := asMapOr(p, nil)
			if pm == nil {
				continue
			}
			if s := firstScalar(pm["value"]); s != "" {
				out.Phone = s
				if strings.EqualFold(firstScalar(pm["status"]), "received") {
					break
				}
			}
		}
	}
	return out
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asMapOr(v any, fallback map[string]any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return fallback
}

func (a *App) avitoApplicationsHook(w http.ResponseWriter, r *http.Request) {
	if !a.checkInboxHook(r) {
		writeJSON(w, 403, map[string]string{"error": "секрет вебхука не совпал"})
		return
	}
	a.handleAvitoApplications(w, r)
}

func (a *App) handleAvitoApplications(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var raw any
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &raw); err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось прочитать json"})
			return
		}
	}
	ids := extractAvitoIDs(raw)
	if len(ids) == 0 {
		writeJSON(w, 200, map[string]any{"ok": true, "accepted": 0, "note": "нет id откликов"})
		return
	}
	if len(ids) > 100 {
		ids = ids[:100]
	}

	details := map[string]avitoApply{}
	enriched := false
	if a.cfg.AvitoReady() {
		if fetched, err := a.fetchAvitoApplies(r.Context(), ids); err == nil {
			details = fetched
			enriched = true
		}
	}

	accepted, dups := 0, 0
	items := make([]map[string]any, 0, len(ids))
	for _, appID := range ids {
		if existing := a.existingAvitoLead(r.Context(), appID); existing != "" {
			dups++
			items = append(items, map[string]any{"id": appID, "lead_id": existing, "duplicate": true})
			continue
		}
		info := details[appID]
		info.ID = appID
		leadID, err := a.ingestAvitoApply(r.Context(), info)
		if err != nil {
			items = append(items, map[string]any{"id": appID, "error": "не удалось создать лид"})
			continue
		}
		accepted++
		items = append(items, map[string]any{"id": appID, "lead_id": leadID, "duplicate": false})
	}
	writeJSON(w, 201, map[string]any{
		"ok": true, "accepted": accepted, "duplicates": dups, "enriched": enriched, "items": items,
	})
}

func (a *App) existingAvitoLead(ctx context.Context, appID string) string {
	var id string
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM leads WHERE source='avito' AND vk_lead_id=$1`, appID).Scan(&id)
	return id
}

func (a *App) ingestAvitoApply(ctx context.Context, info avitoApply) (string, error) {
	contactID := a.ensureAvitoContact(ctx, info.Name, info.Phone)
	in := map[string]any{
		"title":      avitoLeadTitle(info.Name, info.Vacancy, info.ID),
		"source":     "avito",
		"vk_lead_id": info.ID,
		"contact_id": contactID,
	}
	if info.Vacancy != "" {
		in["vk_click_id"] = info.Vacancy
	}
	id, err := a.insertLead(ctx, in, "")
	if err != nil {
		return "", err
	}
	var owner string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1`, id).Scan(&owner)
	if owner == "" {
		pool := a.poolForSource(ctx, "avito")
		if pool == "" {
			pool = "depts"
		}
		_, _ = a.distributeOne(ctx, "", id, pool, "")
	}
	return id, nil
}

func (a *App) ensureAvitoContact(ctx context.Context, name, phone string) string {
	n := normalizePhone(phone)
	if n == "" {
		return ""
	}
	var id string
	if err := a.db.QueryRow(ctx, `
		SELECT id::text FROM contacts
		WHERE regexp_replace(coalesce(phone,''),'[^0-9]','','g') LIKE '%'||$1
		LIMIT 1`, n).Scan(&id); err == nil {
		return id
	}
	label := strings.TrimSpace(name)
	if label == "" {
		label = phone
	}
	_ = a.db.QueryRow(ctx, `
		INSERT INTO contacts (full_name, phone, source) VALUES ($1,$2,'avito') RETURNING id::text`,
		label, phone).Scan(&id)
	return id
}

func (a *App) fetchAvitoApplies(ctx context.Context, ids []string) (map[string]avitoApply, error) {
	tok, err := a.avitoToken(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{"ids": ids})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.avito.ru/job/v1/applications/get_by_ids", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	res, err := avitoHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("avito applications %d", res.StatusCode)
	}
	var out struct {
		Applies []map[string]any `json:"applies"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	got := map[string]avitoApply{}
	for _, row := range out.Applies {
		info := parseAvitoApply(row)
		if info.ID != "" {
			got[info.ID] = info
		}
	}
	return got, nil
}

func (a *App) avitoToken(ctx context.Context) (string, error) {
	avitoTok.mu.Lock()
	if avitoTok.tok != "" && time.Now().Before(avitoTok.exp) {
		tok := avitoTok.tok
		avitoTok.mu.Unlock()
		return tok, nil
	}
	avitoTok.mu.Unlock()

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {a.cfg.AvitoClientID},
		"client_secret": {a.cfg.AvitoClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.avito.ru/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := avitoHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("avito oauth %d", res.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("avito oauth: нет токена")
	}
	ttl := out.ExpiresIn
	if ttl <= 0 {
		ttl = 3000
	}
	avitoTok.mu.Lock()
	avitoTok.tok = out.AccessToken
	avitoTok.exp = time.Now().Add(time.Duration(ttl-60) * time.Second)
	avitoTok.mu.Unlock()
	return out.AccessToken, nil
}
