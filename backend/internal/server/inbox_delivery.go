package server

import (
	"context"
	"encoding/json"
	"strings"
)

// normalizeWappiDeliveryStatus — статусы Wappi MAX: pending, delivered, read, undelivered, error…
func normalizeWappiDeliveryStatus(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, " ", "_")
	switch s {
	case "pending", "queued":
		return "pending"
	case "sent":
		return "sent"
	case "delivered", "delivery":
		return "delivered"
	case "read", "seen", "viewed":
		return "read"
	case "undelivered", "error", "failed", "temporary_ban", "temporaryban", "ban":
		return "failed"
	default:
		return ""
	}
}

func deliveryStatusRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "failed":
		return 0
	case "pending":
		return 1
	case "sent":
		return 2
	case "delivered":
		return 3
	case "read":
		return 4
	default:
		return -1
	}
}

func canUpgradeDeliveryStatus(from, to string) bool {
	to = strings.ToLower(strings.TrimSpace(to))
	from = strings.ToLower(strings.TrimSpace(from))
	if to == "" {
		return false
	}
	if to == "failed" {
		return from == "" || from == "pending" || from == "sent"
	}
	return deliveryStatusRank(to) > deliveryStatusRank(from)
}

func scalarKeys(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := firstScalar(m[k]); v != "" {
			return v
		}
		for rk, rv := range m {
			if strings.EqualFold(rk, k) {
				if v := firstScalar(rv); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

// applyWappiDeliveryStatuses обрабатывает wh_type=delivery_status из вебхука Wappi.
func (a *App) applyWappiDeliveryStatuses(ctx context.Context, raw map[string]any) int {
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
		if !strings.Contains(wh, "delivery") {
			continue
		}
		msgID := strings.TrimSpace(scalarKeys(m, "id", "message_id", "messageId"))
		status := normalizeWappiDeliveryStatus(scalarKeys(m, "status", "delivery_status", "deliveryStatus"))
		if msgID == "" || status == "" {
			continue
		}
		if a.updateInboxDeliveryByExtID(ctx, msgID, status) {
			n++
		}
	}
	return n
}

func (a *App) updateInboxDeliveryByExtID(ctx context.Context, wappiMsgID, status string) bool {
	wappiMsgID = strings.TrimSpace(wappiMsgID)
	status = strings.TrimSpace(status)
	if wappiMsgID == "" || status == "" {
		return false
	}
	extExact := []string{
		"max:" + wappiMsgID,
		"whatsapp:" + wappiMsgID,
		"telegram:" + wappiMsgID,
		wappiMsgID,
	}
	var id, threadID, cur string
	err := a.db.QueryRow(ctx, `
		SELECT id::text, thread_id::text, coalesce(delivery_status,'')
		FROM inbox_messages
		WHERE direction='outbound'
		  AND (
		    external_id = ANY($1::text[])
		    OR external_id LIKE '%:' || $2
		  )
		ORDER BY created_at DESC
		LIMIT 1`, extExact, wappiMsgID).Scan(&id, &threadID, &cur)
	if err != nil || id == "" {
		return false
	}
	if !canUpgradeDeliveryStatus(cur, status) {
		return false
	}
	_, err = a.db.Exec(ctx, `
		UPDATE inbox_messages SET
			delivery_status = $2,
			delivered_at = CASE
				WHEN $2 IN ('delivered','read') AND delivered_at IS NULL THEN now()
				ELSE delivered_at
			END,
			read_at = CASE
				WHEN $2 = 'read' AND read_at IS NULL THEN now()
				ELSE read_at
			END
		WHERE id = $1::uuid`, id, status)
	if err != nil {
		return false
	}
	if a.hub != nil {
		a.hub.Broadcast("inbox.updated", map[string]any{
			"message_id":      id,
			"thread_id":       threadID,
			"delivery_status": status,
		})
	}
	return true
}

func (a *App) setInboxMessageDelivery(ctx context.Context, msgID, status, wappiExtID, channel string) {
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return
	}
	status = strings.TrimSpace(status)
	ext := ""
	if wid := strings.TrimSpace(wappiExtID); wid != "" {
		ch := normChannel(channel)
		if ch == "" {
			ch = "max"
		}
		if strings.Contains(wid, ":") {
			ext = wid
		} else {
			ext = ch + ":" + wid
		}
	}
	if ext != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_messages SET
				external_id = COALESCE(NULLIF(external_id,''), $2),
				delivery_status = CASE
					WHEN coalesce(delivery_status,'') IN ('delivered','read') THEN delivery_status
					WHEN $3 = 'failed' AND coalesce(delivery_status,'') IN ('delivered','read') THEN delivery_status
					ELSE $3
				END
			WHERE id = $1::uuid`, msgID, ext, status)
		return
	}
	if status == "" {
		return
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE inbox_messages SET delivery_status = CASE
			WHEN coalesce(delivery_status,'') IN ('delivered','read') THEN delivery_status
			ELSE $2
		END
		WHERE id = $1::uuid`, msgID, status)
}

// extractWappiMessageID достаёт id сообщения из ответа sync send.
func extractWappiMessageID(body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	for _, k := range []string{"message_id", "messageId", "id"} {
		if v := strings.TrimSpace(firstScalar(m[k])); v != "" && v != "done" {
			return v
		}
	}
	if nested, ok := m["messages"].(map[string]any); ok {
		if v := strings.TrimSpace(scalarKeys(nested, "id", "message_id")); v != "" {
			return v
		}
	}
	return ""
}
