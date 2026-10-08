package server

import (
	"context"
	"strings"
)

// pushUserNotification — запись + WS. Если dedupeField/Value заданы — обновляем
// существующее непрочитанное вместо спама.
func (a *App) pushUserNotification(ctx context.Context, userID, channel, title, body string, payload map[string]any, dedupeField, dedupeValue string) {
	userID = strings.TrimSpace(userID)
	channel = strings.TrimSpace(channel)
	if userID == "" || channel == "" || title == "" {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["kind"]; !ok {
		payload["kind"] = channel
	}
	raw := mustJSON(payload)
	kind, _ := payload["kind"].(string)
	if kind == "" {
		kind = channel
	}

	updated := false
	if dedupeField != "" && dedupeValue != "" {
		tag, err := a.db.Exec(ctx, `
			UPDATE notifications
			SET title=$3, body=$4, payload=$5::jsonb, is_read=false, created_at=now()
			WHERE id = (
			  SELECT id FROM notifications
			  WHERE user_id=$1::uuid AND is_read=false
			    AND channel=$6
			    AND payload->>$7=$2
			  ORDER BY created_at DESC LIMIT 1
			)`, userID, dedupeValue, title, body, raw, channel, dedupeField)
		updated = err == nil && tag.RowsAffected() > 0
	}
	if !updated {
		_, _ = a.db.Exec(ctx, `
			INSERT INTO notifications (user_id, channel, title, body, payload)
			VALUES ($1::uuid, $2, $3, $4, $5::jsonb)`,
			userID, channel, title, body, raw)
	}

	msg := map[string]any{
		"user_id": userID, "kind": kind, "title": title, "body": body, "channel": channel,
	}
	for k, v := range payload {
		if s, ok := v.(string); ok && s != "" {
			msg[k] = s
		}
	}
	a.hub.Broadcast("notification", msg)
}

// entityOwnerID — владелец лида или сделки (только ему шлём missed/MAX).
func (a *App) entityOwnerID(ctx context.Context, entityType, entityID string) (ownerID, leadID, dealID string) {
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return "", "", ""
	}
	switch entityType {
	case "lead":
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(owner_id::text,'')
			FROM leads
			WHERE id=$1::uuid AND coalesce(is_active,true)=true`, entityID).Scan(&ownerID)
		return ownerID, entityID, ""
	case "deal":
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(owner_id::text,''), coalesce(lead_id::text,'')
			FROM deals WHERE id=$1::uuid`, entityID).Scan(&ownerID, &leadID)
		return ownerID, leadID, entityID
	default:
		return "", "", ""
	}
}

func (a *App) notifyMissedCall(ctx context.Context, _ /*softphoneUser*/, entityType, entityID, peer, callID string) {
	ownerID, leadID, dealID := a.entityOwnerID(ctx, entityType, entityID)
	if ownerID == "" {
		// Нет привязки к чужой/бесхозной карточке — не спамим softphone-юзеру чужие пропущенные.
		return
	}
	title := "Пропущенный звонок"
	body := strings.TrimSpace(peer)
	payload := map[string]any{
		"kind":        "missed_call",
		"entity_type": entityType,
		"entity_id":   entityID,
		"peer":        peer,
		"call_id":     callID,
		"lead_id":     leadID,
		"deal_id":     dealID,
	}
	a.pushUserNotification(ctx, ownerID, "missed_call", title, body, payload, "call_id", callID)
}

func (a *App) notifyInboxMaxInbound(ctx context.Context, threadID, channel, preview, titleHint string) {
	if threadID == "" || !strings.EqualFold(channel, "max") {
		return
	}
	var dealID, contactID, phone, threadTitle string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(deal_id::text,''), coalesce(contact_id::text,''),
		       coalesce(phone,''), coalesce(title,'')
		FROM inbox_threads WHERE id=$1::uuid`, threadID).Scan(&dealID, &contactID, &phone, &threadTitle)

	var leadID, ownerID string
	// 1) Сделка треда → только её владелец.
	if dealID != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(owner_id::text,''), coalesce(lead_id::text,'')
			FROM deals WHERE id=$1::uuid`, dealID).Scan(&ownerID, &leadID)
	}
	// 2) Лид по контакту треда — только с owner_id, свежий.
	if ownerID == "" && contactID != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(owner_id::text,''), id::text
			FROM leads
			WHERE contact_id=$1::uuid
			  AND coalesce(is_active,true)=true
			  AND owner_id IS NOT NULL
			ORDER BY created_at DESC LIMIT 1`, contactID).Scan(&ownerID, &leadID)
	}
	// 3) По телефону — только лид с владельцем (не «чужой» без owner).
	if ownerID == "" && phone != "" {
		digits := maxNormPhone(phone)
		if len(digits) >= 7 {
			_ = a.db.QueryRow(ctx, `
				SELECT coalesce(l.owner_id::text,''), l.id::text
				FROM leads l
				LEFT JOIN contacts c ON c.id=l.contact_id
				WHERE coalesce(l.is_active,true)=true
				  AND l.owner_id IS NOT NULL
				  AND length(regexp_replace(coalesce(c.phone,''),'[^0-9]','','g')) >= 7
				  AND right(regexp_replace(coalesce(c.phone,''),'[^0-9]','','g'), 10)
				      = right($1, 10)
				ORDER BY l.created_at DESC LIMIT 1`, digits).Scan(&ownerID, &leadID)
		}
	}
	if ownerID == "" {
		return
	}

	who := strings.TrimSpace(titleHint)
	if who == "" {
		who = strings.TrimSpace(threadTitle)
	}
	if who == "" {
		who = phone
	}
	if who == "" {
		who = "Клиент"
	}
	title := "Сообщение в MAX"
	body := who
	if preview != "" {
		body = who + " — " + preview
		if len([]rune(body)) > 200 {
			body = string([]rune(body)[:200])
		}
	}
	payload := map[string]any{
		"kind":      "inbox_max",
		"thread_id": threadID,
		"lead_id":   leadID,
		"deal_id":   dealID,
	}
	a.pushUserNotification(ctx, ownerID, "inbox_max", title, body, payload, "thread_id", threadID)
}

func humanizeMessengerErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "недоступен"):
		return "сервис MAX (Wappi) временно недоступен"
	case strings.Contains(s, "нет профиля"):
		return "не привязана MAX-линия менеджера"
	case strings.Contains(s, "нет получателя"):
		return "нет чата/телефона кандидата"
	default:
		if len(s) > 120 {
			return s[:120]
		}
		return s
	}
}

// notifyTicketUploaded — логист загрузил файл билета → только владельцу сделки.
func (a *App) notifyTicketUploaded(ctx context.Context, dealID, filename, actorID string) {
	ownerID, leadID, _ := a.entityOwnerID(ctx, "deal", dealID)
	if ownerID == "" || ownerID == actorID {
		return
	}
	var titleDeal string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(nullif(c.full_name,''), d.title, 'Кандидат')
		FROM deals d
		LEFT JOIN contacts c ON c.id=d.contact_id
		WHERE d.id=$1::uuid`, dealID).Scan(&titleDeal)
	body := strings.TrimSpace(titleDeal)
	if filename != "" {
		if body != "" {
			body += " — " + filename
		} else {
			body = filename
		}
	}
	a.pushUserNotification(ctx, ownerID, "ticket_file", "Загружен билет", body, map[string]any{
		"kind":     "ticket_file",
		"deal_id":  dealID,
		"lead_id":  leadID,
		"filename": filename,
	}, "deal_id", dealID)
}
