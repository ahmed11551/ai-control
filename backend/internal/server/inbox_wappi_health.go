package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Статусы профиля из вебхуков Wappi (authorization_status / application_status).
const (
	wappiStatusOnline         = "online"
	wappiStatusOffline        = "offline"
	wappiStatusSpamRestricted = "account_spam_restricted"
	wappiStatusCheckFloodWait = "check_phone_floodwait"
	wappiDeliveryTemporaryBan = "temporary ban"
)

type wappiProfileHealth struct {
	ProfileID         string `json:"profile_id"`
	Phone             string `json:"phone"`
	Name              string `json:"name"`
	Authorized        bool   `json:"authorized"`
	AppStatus         string `json:"app_status"`
	PaymentExpiredAt  string `json:"payment_expired_at,omitempty"`
	PaymentExpired    bool   `json:"payment_expired"`
	MessageCount      int    `json:"message_count"`
	WorkedDays        int    `json:"worked_days"`
	LastWebhookStatus string `json:"last_webhook_status,omitempty"`
	LastWebhookReason string `json:"last_webhook_reason,omitempty"`
	LastWebhookAt     string `json:"last_webhook_at,omitempty"`
	Health            string `json:"health"` // ok | offline | unauthorized | spam | floodwait | payment | unknown
	Label             string `json:"label"`
	Detail            string `json:"detail,omitempty"`
}

func classifyWappiHealth(authorized bool, appStatus, paymentExpiredAt, webhookStatus string, now time.Time) (health, label, detail string) {
	wh := strings.ToLower(strings.TrimSpace(webhookStatus))
	switch wh {
	case wappiStatusSpamRestricted:
		return "spam", "бан / спам-ограничение", "ACCOUNT_SPAM_RESTRICTED — MAX ограничил аккаунт"
	case wappiStatusCheckFloodWait:
		return "floodwait", "лимит проверок номеров", "CHECK_PHONE_FLOODWAIT — слишком много contact/check"
	}
	if !authorized {
		return "unauthorized", "не авторизован", "Нужно заново войти в MAX в Wappi"
	}
	st := strings.ToLower(strings.TrimSpace(appStatus))
	if st == "close" || st == "offline" || wh == wappiStatusOffline {
		return "offline", "офлайн", "Сессия MAX закрыта"
	}
	if pay := strings.TrimSpace(paymentExpiredAt); pay != "" {
		if t, err := time.Parse(time.RFC3339, pay); err == nil && t.Before(now) {
			return "payment", "оплата истекла", "payment_expired_at в прошлом"
		}
		// иногда без Z
		if t, err := time.Parse("2006-01-02T15:04:05", strings.TrimSuffix(pay, "Z")); err == nil && t.Before(now) {
			return "payment", "оплата истекла", "payment_expired_at в прошлом"
		}
	}
	if st == "" || st == "open" || wh == wappiStatusOnline || wh == "" {
		return "ok", "ок", ""
	}
	return "unknown", "неизвестно", "app_status=" + appStatus
}

func (a *App) wappiGetStatus(ctx context.Context, profileID string) (map[string]any, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return nil, fmt.Errorf("нет profile_id")
	}
	u := "https://wappi.pro/maxapi/sync/get/status?profile_id=" + url.QueryEscape(profileID)
	body, err := a.wappiJSON(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("bad status json")
	}
	return raw, nil
}

func (a *App) lastWappiAuthWebhook(ctx context.Context, profileID string) (status, reason, at string) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return "", "", ""
	}
	var payload []byte
	var created time.Time
	err := a.db.QueryRow(ctx, `
		SELECT payload, created_at FROM inbound_webhooks
		WHERE source='wappi'
		  AND (
		    payload->>'profile_id'=$1
		    OR payload->'messages'->0->>'profile_id'=$1
		    OR payload::text LIKE '%' || $1 || '%'
		  )
		  AND (
		    lower(coalesce(payload->>'wh_type','')) IN ('authorization_status','application_status')
		    OR lower(coalesce(payload->'messages'->0->>'wh_type','')) IN ('authorization_status','application_status')
		    OR payload::text ILIKE '%ACCOUNT_SPAM_RESTRICTED%'
		    OR payload::text ILIKE '%CHECK_PHONE_FLOODWAIT%'
		  )
		ORDER BY created_at DESC
		LIMIT 1`, profileID).Scan(&payload, &created)
	if err != nil || len(payload) == 0 {
		return "", "", ""
	}
	var raw map[string]any
	_ = json.Unmarshal(payload, &raw)
	flat := flattenInbox(raw)
	if msgs, ok := raw["messages"].([]any); ok && len(msgs) > 0 {
		if m0, ok := msgs[0].(map[string]any); ok {
			flat = flattenInbox(m0)
		}
	}
	status = strings.TrimSpace(scalarKeys(flat, "status"))
	reason = strings.TrimSpace(scalarKeys(flat, "reason"))
	at = created.UTC().Format(time.RFC3339)
	return status, reason, at
}

func (a *App) buildWappiProfileHealth(ctx context.Context, p wappiProfile) wappiProfileHealth {
	now := time.Now().UTC()
	h := wappiProfileHealth{
		ProfileID:    strings.TrimSpace(p.ProfileID),
		Phone:        maxDisplayPhone(p.Phone),
		Name:         strings.TrimSpace(p.Name),
		Authorized:   p.Authorized,
		AppStatus:    strings.TrimSpace(p.AppStatus),
		MessageCount: 0,
	}
	if st, err := a.wappiGetStatus(ctx, h.ProfileID); err == nil {
		if v, ok := st["authorized"].(bool); ok {
			h.Authorized = v
		}
		if s, ok := st["app_status"].(string); ok {
			h.AppStatus = s
		}
		if s, ok := st["phone"].(string); ok && maxDisplayPhone(s) != "" {
			h.Phone = maxDisplayPhone(s)
		}
		if s, ok := st["name"].(string); ok && strings.TrimSpace(s) != "" {
			h.Name = strings.TrimSpace(s)
		}
		if s, ok := st["payment_expired_at"].(string); ok {
			h.PaymentExpiredAt = s
		}
		switch v := st["message_count"].(type) {
		case float64:
			h.MessageCount = int(v)
		case int:
			h.MessageCount = v
		}
		switch v := st["worked_days"].(type) {
		case float64:
			h.WorkedDays = int(v)
		case int:
			h.WorkedDays = v
		}
	}
	whStatus, whReason, whAt := a.lastWappiAuthWebhook(ctx, h.ProfileID)
	h.LastWebhookStatus = whStatus
	h.LastWebhookReason = whReason
	h.LastWebhookAt = whAt

	health, label, detail := classifyWappiHealth(h.Authorized, h.AppStatus, h.PaymentExpiredAt, whStatus, now)
	h.Health = health
	h.Label = label
	h.Detail = detail
	h.PaymentExpired = health == "payment"
	return h
}

// getMaxAccountsHealth — проверка наших MAX-номеров: бан / floodwait / офлайн / оплата.
func (a *App) getMaxAccountsHealth(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) && !isROP(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	if !a.cfg.WappiReady() {
		writeJSON(w, 400, map[string]string{"error": "нет WAPPI_TOKEN"})
		return
	}
	ctx := r.Context()
	profiles, err := a.wappiListProfiles(ctx)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	items := make([]wappiProfileHealth, 0, len(profiles))
	counts := map[string]int{}
	for _, p := range profiles {
		if strings.TrimSpace(p.ProfileID) == "" {
			continue
		}
		if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
			continue
		}
		h := a.buildWappiProfileHealth(ctx, p)
		items = append(items, h)
		counts[h.Health]++
	}
	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"items":  items,
		"counts": counts,
		"total":  len(items),
	})
}

// applyWappiAuthStatuses — wh_type=authorization_status|application_status (бан / floodwait / online).
func (a *App) applyWappiAuthStatuses(ctx context.Context, raw map[string]any) int {
	var items []map[string]any
	switch m := raw["messages"].(type) {
	case []any:
		for _, item := range m {
			if row, ok := item.(map[string]any); ok {
				items = append(items, flattenInbox(row))
			}
		}
	case map[string]any:
		items = append(items, flattenInbox(m))
	default:
		items = append(items, flattenInbox(raw))
	}
	n := 0
	for _, m := range items {
		wh := strings.ToLower(strings.TrimSpace(scalarKeys(m, "wh_type", "type")))
		if wh != "authorization_status" && wh != "application_status" {
			continue
		}
		profileID := strings.TrimSpace(scalarKeys(m, "profile_id"))
		status := strings.TrimSpace(scalarKeys(m, "status"))
		if profileID == "" || status == "" {
			continue
		}
		phone := maxDisplayPhone(scalarKeys(m, "phone"))
		reason := strings.TrimSpace(scalarKeys(m, "reason"))
		log.Printf("Wappi profile status profile=%s phone=%s status=%s reason=%s", profileID, phone, status, reason)
		n++
		if a.hub != nil {
			a.hub.Broadcast("max.profile_status", map[string]any{
				"profile_id": profileID,
				"phone":      phone,
				"status":     status,
				"reason":     reason,
			})
		}
	}
	return n
}
