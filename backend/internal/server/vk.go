package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func (a *App) vkLeadVerify(w http.ResponseWriter, r *http.Request) {
	// VK sometimes checks webhook with GET
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) vkWebhook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if !a.checkVKSign(r, body) {
		writeJSON(w, 401, map[string]string{"error": "bad signature"})
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO inbound_webhooks (source, payload) VALUES ('vk_ads', $1::jsonb)`, string(body))
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	go a.ingestVKPayload(payload)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) vkLeadWebhook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if a.cfg.VKWebhookSecret != "" && r.URL.Query().Get("secret") != a.cfg.VKWebhookSecret && !a.checkVKSign(r, body) {
		writeJSON(w, 401, map[string]string{"error": "bad secret"})
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO inbound_webhooks (source, payload) VALUES ('vk_ads_leads', $1::jsonb)`, string(body))
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	id, err := a.ingestVKLead(r.Context(), payload)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"lead_id": id})
}

func (a *App) checkVKSign(r *http.Request, body []byte) bool {
	sig := r.Header.Get("X-Vk-Signature")
	if sig == "" || a.cfg.VKWebhookSecret == "" {
		return a.cfg.VKWebhookSecret == "" || r.URL.Query().Get("secret") == a.cfg.VKWebhookSecret
	}
	mac := hmac.New(sha256.New, []byte(a.cfg.VKWebhookSecret))
	mac.Write(body)
	expect := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expect), []byte(strings.ToLower(sig)))
}

func (a *App) ingestVKPayload(payload map[string]any) {
	ctx := context.Background()
	// Унифицируем формы VK Ads / lead ads
	if leads, ok := payload["leads"].([]any); ok {
		for _, x := range leads {
			if m, ok := x.(map[string]any); ok {
				_, _ = a.ingestVKLead(ctx, m)
			}
		}
		return
	}
	_, _ = a.ingestVKLead(ctx, payload)
}

func (a *App) ingestVKLead(ctx context.Context, p map[string]any) (string, error) {
	name := firstString(p, "name", "full_name", "user_name")
	phone := firstString(p, "phone", "user_phone")
	email := firstString(p, "email", "user_email")
	campaign := firstString(p, "campaign_id", "vk_ads_campaign_id", "ad_campaign_id")
	click := firstString(p, "click_id", "rclid", "vk_click_id")
	leadExt := firstString(p, "lead_id", "id")
	if name == "" {
		name = strings.TrimSpace(phone + " " + email)
	}
	title := firstString(p, "title")
	if title == "" {
		title = "VK Ads: " + nz(name, phone)
	}
	if leadExt != "" {
		var byExt string
		_ = a.db.QueryRow(ctx, `SELECT id::text FROM leads WHERE source='vk_ads' AND vk_lead_id=$1`, leadExt).Scan(&byExt)
		if byExt != "" {
			return byExt, nil
		}
	}
	if phone != "" {
		if dup := a.existingVKAdsLeadByPhone(ctx, phone); dup != "" {
			return dup, nil
		}
	}
	var contactID string
	if phone != "" {
		_ = a.db.QueryRow(ctx, `SELECT id::text FROM contacts WHERE regexp_replace(coalesce(phone,''),'[^0-9]','','g') LIKE '%'||$1 LIMIT 1`, normalizePhone(phone)).Scan(&contactID)
	}
	if contactID == "" {
		_ = a.db.QueryRow(ctx, `INSERT INTO contacts (full_name, phone, email, source) VALUES ($1,$2,$3,'vk_ads') RETURNING id::text`,
			nz(name, "VK Ads lead"), phone, email).Scan(&contactID)
	}
	in := map[string]any{
		"title":              title,
		"source":             "vk_ads",
		"vk_ads_campaign_id": campaign,
		"vk_click_id":        click,
		"vk_lead_id":         leadExt,
		"contact_id":         contactID,
		"phone":              phone,
	}
	id, err := a.insertLead(ctx, in, "")
	if err != nil {
		// Гонка по unique (source, vk_lead_id) — вернём уже существующий.
		if leadExt != "" {
			var byExt string
			_ = a.db.QueryRow(ctx, `SELECT id::text FROM leads WHERE source='vk_ads' AND vk_lead_id=$1`, leadExt).Scan(&byExt)
			if byExt != "" {
				return byExt, nil
			}
		}
		return "", err
	}
	_, _ = a.db.Exec(ctx, `INSERT INTO notifications (user_id, channel, title, body, payload)
		SELECT id, 'in_app', 'Новый лид VK Ads', $1, $2::jsonb FROM users WHERE is_active LIMIT 5`,
		in["title"], mustJSON(p))
	a.hub.Broadcast("vk.lead", map[string]any{"lead_id": id, "campaign": campaign})
	return id, nil
}

func (a *App) enqueueOfflineConversion(ctx context.Context, dealID string) {
	var amount float64
	var campaign, click *string
	err := a.db.QueryRow(ctx, `SELECT coalesce(amount,0), vk_ads_campaign_id, vk_click_id FROM deals WHERE id=$1`, dealID).
		Scan(&amount, &campaign, &click)
	if err != nil {
		return
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO vk_ads_offline_conversions (deal_id, vk_ads_campaign_id, vk_click_id, conversion_value, event_name, status)
		VALUES ($1::uuid, $2, $3, $4, 'purchase', 'pending')`,
		dealID, deref(campaign), deref(click), amount)
}

func (a *App) vkFlushLoop() {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for range t.C {
		_, _ = a.flushVK(context.Background())
	}
}

func (a *App) flushVKNow(w http.ResponseWriter, r *http.Request) {
	n, err := a.flushVK(r.Context())
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"sent": n})
}

func (a *App) flushVK(ctx context.Context) (int, error) {
	if a.cfg.VKToken == "" || a.cfg.VKAccountID == "" {
		return 0, nil
	}
	rows, err := a.db.Query(ctx, `SELECT id::text, coalesce(vk_click_id,''), coalesce(conversion_value,0), event_name, created_at
		FROM vk_ads_offline_conversions WHERE status='pending' LIMIT 100`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type conv struct {
		id, click, event string
		value            float64
		at               time.Time
	}
	var list []conv
	for rows.Next() {
		var c conv
		if err := rows.Scan(&c.id, &c.click, &c.value, &c.event, &c.at); err == nil {
			list = append(list, c)
		}
	}
	if len(list) == 0 {
		return 0, nil
	}
	// VK Ads offline conversions: POST https://ads.vk.ru/api/v2/offline_conversions/upload.json
	body := map[string]any{"conversions": []any{}}
	cs := []any{}
	for _, c := range list {
		item := map[string]any{
			"conversion_id":    c.id,
			"conversion_event": c.event,
			"conversion_value": c.value,
			"conversion_time":  c.at.Unix(),
		}
		if c.click != "" {
			item["click_id"] = c.click
		}
		cs = append(cs, item)
	}
	body["conversions"] = cs
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("https://ads.vk.ru/api/v2/offline_conversions/upload.json?account_id=%s", a.cfg.VKAccountID),
		bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.VKToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	for _, c := range list {
		if ok {
			_, _ = a.db.Exec(ctx, `UPDATE vk_ads_offline_conversions SET status='sent', sent_at=now(), last_error=NULL WHERE id=$1`, c.id)
		} else {
			_, _ = a.db.Exec(ctx, `UPDATE vk_ads_offline_conversions SET status='failed', last_error=$2 WHERE id=$1`, c.id, string(rb))
		}
	}
	if !ok {
		log.Println("VK offline conversions:", resp.Status, string(rb))
		return 0, fmt.Errorf("vk ads http %s", resp.Status)
	}
	return len(list), nil
}

func (a *App) listVKCampaigns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.queryMaps(r.Context(), `SELECT id, name, budget::text, status, spend::text, synced_at FROM vk_ads_campaigns ORDER BY name`))
}

func (a *App) syncVKCampaigns(w http.ResponseWriter, r *http.Request) {
	if a.cfg.VKToken == "" {
		writeJSON(w, 400, map[string]string{"error": "VK_ADS_TOKEN empty"})
		return
	}
	url := fmt.Sprintf("https://ads.vk.ru/api/v2/ad_plans.json?account_id=%s&limit=50", a.cfg.VKAccountID)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+a.cfg.VKToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(b, &parsed)
	items, _ := parsed["items"].([]any)
	n := 0
	for _, it := range items {
		m, _ := it.(map[string]any)
		id := firstString(m, "id")
		name := firstString(m, "name")
		if id == "" {
			continue
		}
		_, _ = a.db.Exec(r.Context(), `
			INSERT INTO vk_ads_campaigns (id, name, status, synced_at) VALUES ($1,$2,$3,now())
			ON CONFLICT (id) DO UPDATE SET name=excluded.name, status=excluded.status, synced_at=now()`,
			id, name, firstString(m, "status"))
		n++
	}
	writeJSON(w, 200, map[string]any{"synced": n, "raw_status": resp.Status})
}

func (a *App) reportVK(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "выгрузки только для администратора"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"leads": a.queryMaps(r.Context(), `SELECT coalesce(vk_ads_campaign_id,'(none)') AS campaign, count(*) AS leads FROM leads WHERE source='vk_ads' GROUP BY 1`),
		"won": a.queryMaps(r.Context(), `
			SELECT coalesce(d.vk_ads_campaign_id,'(none)') AS campaign, count(*) AS deals, coalesce(sum(d.amount),0)::text AS revenue
			FROM deals d JOIN pipeline_stages s ON s.id=d.stage_id
			WHERE s.is_won GROUP BY 1`),
		"conversions": a.queryMaps(r.Context(), `SELECT status, count(*) AS n, coalesce(sum(conversion_value),0)::text AS value FROM vk_ads_offline_conversions GROUP BY status`),
	})
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m, k); s != "" {
			return s
		}
	}
	if fields, ok := m["form_data"].([]any); ok {
		for _, f := range fields {
			fm, _ := f.(map[string]any)
			q := strings.ToLower(str(fm, "question") + str(fm, "name"))
			ans := str(fm, "answer")
			if ans == "" {
				ans = str(fm, "value")
			}
			if strings.Contains(q, "phone") || strings.Contains(q, "телефон") {
				return ans
			}
		}
	}
	return ""
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return "{}"
	}
	return string(b)
}
