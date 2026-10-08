package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type maxOpenIn struct {
	Phone     string `json:"phone"`
	LeadID    string `json:"lead_id"`
	ContactID string `json:"contact_id"`
	DealID    string `json:"deal_id"`
	LineID    string `json:"line_id"`
}

func (a *App) openMaxByPhone(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.MaxBotReady() && !a.cfg.WappiReady() {
		writeJSON(w, 400, map[string]string{"error": "подключите Wappi (WAPPI_TOKEN) или MAX_BOT_TOKEN"})
		return
	}
	var in maxOpenIn
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "неверный JSON"})
		return
	}
	ctx := r.Context()
	// UI может прислать маску (7••••••6602 → «76602») — не доверяем, берём из CRM.
	phone := maxNormPhone(in.Phone)
	contactID := strings.TrimSpace(in.ContactID)
	leadID := strings.TrimSpace(in.LeadID)
	dealID := strings.TrimSpace(in.DealID)

	if leadID != "" {
		var cPhone, cID, title string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(c.phone,''), coalesce(l.contact_id::text,''), l.title
			FROM leads l LEFT JOIN contacts c ON c.id=l.contact_id
			WHERE l.id=$1`, leadID).Scan(&cPhone, &cID, &title)
		if phone == "" {
			phone = maxNormPhone(cPhone)
		}
		if contactID == "" {
			contactID = cID
		}
		_ = title
	}
	if dealID != "" && (phone == "" || contactID == "") {
		var cPhone, cID string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(c.phone,''), coalesce(d.contact_id::text,'')
			FROM deals d LEFT JOIN contacts c ON c.id=d.contact_id
			WHERE d.id=$1`, dealID).Scan(&cPhone, &cID)
		if phone == "" {
			phone = maxNormPhone(cPhone)
		}
		if contactID == "" {
			contactID = cID
		}
	}
	if contactID != "" && phone == "" {
		var cPhone string
		_ = a.db.QueryRow(ctx, `SELECT coalesce(phone,'') FROM contacts WHERE id=$1`, contactID).Scan(&cPhone)
		phone = maxNormPhone(cPhone)
	}

	norm := maxNormPhone(phone)
	if norm == "" {
		if dealID == "" && leadID != "" {
			_ = a.db.QueryRow(ctx, `
				SELECT coalesce(
					nullif((SELECT converted_deal_id::text FROM leads WHERE id=$1::uuid), ''),
					(SELECT d.id::text FROM deals d WHERE d.lead_id=$1::uuid ORDER BY d.created_at ASC LIMIT 1),
					''
				)`, leadID).Scan(&dealID)
		}
		if tid, lid := a.findInboxThreadWithMessages(ctx, dealID, contactID, ""); tid != "" {
			a.writePhones(w, r, 200, map[string]any{
				"ok":        true,
				"opened":    true,
				"via":       "wappi",
				"thread_id": tid,
				"line_id":   lid,
			})
			return
		}
		writeJSON(w, 400, map[string]string{"error": "нет телефона — укажите номер у контакта/лида"})
		return
	}

	if dealID == "" && leadID != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(
				nullif((SELECT converted_deal_id::text FROM leads WHERE id=$1::uuid), ''),
				(SELECT d.id::text FROM deals d WHERE d.lead_id=$1::uuid ORDER BY d.created_at ASC LIMIT 1),
				''
			)`, leadID).Scan(&dealID)
	}
	// Явный выбор своего аккаунта открывает тред именно этой линии.
	// Чужой диалог с сообщениями не подменяем и его line_id не переписываем.
	title := "Клиент"
	if leadID != "" {
		_ = a.db.QueryRow(ctx, `SELECT coalesce(nullif(title,''),'Клиент') FROM leads WHERE id=$1::uuid`, leadID).Scan(&title)
	}
	if a.openExplicitMaxLine(w, r, strings.TrimSpace(in.LineID), norm, title, leadID, contactID, dealID) {
		return
	}
	if tid, lid := a.findInboxThreadWithMessages(ctx, dealID, contactID, norm); tid != "" {
		if contactID != "" {
			_, _ = a.db.Exec(ctx, `
				UPDATE inbox_threads SET contact_id=coalesce(contact_id, $2::uuid)
				WHERE id=$1::uuid`, tid, contactID)
		}
		a.writePhones(w, r, 200, map[string]any{
			"ok":        true,
			"opened":    true,
			"via":       "wappi",
			"phone":     norm,
			"thread_id": tid,
			"line_id":   lid,
		})
		return
	}

	// Логист и админ без явного выбора: не создаём пустую оболочку на первом номере из списка.
	if isLogist(r) || isAdmin(r) {
		if dealID == "" && leadID != "" {
			_ = a.db.QueryRow(ctx, `
				SELECT id::text FROM deals WHERE lead_id=$1::uuid
				ORDER BY updated_at DESC NULLS LAST, created_at DESC LIMIT 1`, leadID).Scan(&dealID)
		}
		if tid, lid := a.findInboxThreadWithMessages(ctx, dealID, contactID, norm); tid != "" {
			if contactID != "" {
				_, _ = a.db.Exec(ctx, `
					UPDATE inbox_threads SET contact_id=coalesce(contact_id, $2::uuid)
					WHERE id=$1::uuid`, tid, contactID)
			}
			a.writePhones(w, r, 200, map[string]any{
				"ok":        true,
				"opened":    true,
				"via":       "wappi",
				"phone":     norm,
				"thread_id": tid,
				"line_id":   lid,
			})
			return
		}
	}

	if a.cfg.WappiReady() {
		lineID := a.wappiLineForLead(ctx, leadID, actorID(r), strings.TrimSpace(in.LineID))
		if lineID == "" {
			writeJSON(w, 400, map[string]string{
				"error": "нет привязанного аккаунта MAX — выберите свой номер или попросите РОП привязать",
			})
			return
		}
		threadID, err := a.wappiEnsureThread(ctx, norm, title, lineID)
		if err != nil {
			a.writePhones(w, r, 502, map[string]string{"error": err.Error()})
			return
		}
		if contactID != "" {
			_, _ = a.db.Exec(ctx, `
				UPDATE inbox_threads SET contact_id=coalesce(contact_id, $2::uuid)
				WHERE id=$1::uuid`, threadID, contactID)
		}
		a.writePhones(w, r, 200, map[string]any{
			"ok":        true,
			"opened":    true,
			"via":       "wappi",
			"phone":     norm,
			"thread_id": threadID,
			"line_id":   lineID,
		})
		return
	}

	// Нативный бот: холодной отправки по номеру нет — только invite / существующий тред.
	threadID := a.findMaxThread(ctx, normalizePhone(phone), contactID)
	if threadID != "" {
		if contactID != "" {
			_, _ = a.db.Exec(ctx, `
				UPDATE inbox_threads SET contact_id=coalesce(contact_id, $2::uuid),
					phone=coalesce(nullif(phone,''), nullif($3,''))
				WHERE id=$1`, threadID, contactID, phone)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "opened": true, "via": "max_bot", "thread_id": threadID})
		return
	}

	invite := a.maxInviteLink(leadID, contactID, dealID)
	writeJSON(w, 200, map[string]any{
		"ok":          true,
		"opened":      false,
		"via":         "max_bot",
		"phone":       phone,
		"invite_link": invite,
		"note":        "Бот MAX не пишет по номеру первым. Отправьте ссылку клиенту — после старта диалог появится во Входящих. Либо подключите Wappi (WAPPI_TOKEN).",
	})
}

// keepMaxThreadLine — другой аккаунт не переписывает line_id диалога, где уже идёт переписка.
func keepMaxThreadLine(threadLine, resolved string) bool {
	threadLine = strings.TrimSpace(threadLine)
	resolved = strings.TrimSpace(resolved)
	return threadLine != "" && resolved != "" && threadLine != resolved
}

// openExplicitMaxLine открывает или создаёт тред выбранной линии.
// false — line_id нет или этот аккаунт менеджеру недоступен, тогда остаётся прежний поиск диалога.
func (a *App) openExplicitMaxLine(w http.ResponseWriter, r *http.Request, chosen, phone, title, leadID, contactID, dealID string) bool {
	chosen = strings.TrimSpace(chosen)
	phone = maxNormPhone(phone)
	if chosen == "" || phone == "" || !a.cfg.WappiReady() {
		return false
	}
	ctx := r.Context()
	lineID := a.wappiLineForLead(ctx, leadID, actorID(r), chosen)
	if lineID != chosen {
		return false
	}
	threadID, err := a.wappiEnsureThread(ctx, phone, title, lineID)
	if err != nil {
		a.writePhones(w, r, 502, map[string]string{"error": err.Error()})
		return true
	}
	a.attachThreadCRM(ctx, threadID, contactID, dealID)
	a.writePhones(w, r, 200, map[string]any{
		"ok":        true,
		"opened":    true,
		"via":       "wappi",
		"phone":     phone,
		"thread_id": threadID,
		"line_id":   lineID,
	})
	return true
}

func (a *App) attachThreadCRM(ctx context.Context, threadID, contactID, dealID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	if strings.TrimSpace(contactID) != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_threads SET contact_id=coalesce(contact_id, $2::uuid)
			WHERE id=$1::uuid`, threadID, contactID)
	}
	if strings.TrimSpace(dealID) != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_threads SET deal_id=coalesce(deal_id, $2::uuid)
			WHERE id=$1::uuid`, threadID, dealID)
	}
}

// listMaxLinesForLead — аккаунты MAX для карточки лида + has_inbound (зелёная метка = уже есть переписка).
func (a *App) listMaxLinesForLead(w http.ResponseWriter, r *http.Request) {
	a.syncWappiLineLabels(r.Context())
	a.ensureMaxSendDown(r.Context())
	if !isAdmin(r) {
		a.ensureManagerMaxLine(r.Context(), actorID(r))
	}
	leadID := strings.TrimSpace(r.URL.Query().Get("lead_id"))
	ctx := r.Context()

	var contactID, phone, dealID string
	if leadID != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(l.contact_id::text,''), coalesce(c.phone,''),
			       coalesce(
			         nullif(l.converted_deal_id::text,''),
			         (SELECT d.id::text FROM deals d WHERE d.lead_id=l.id ORDER BY d.created_at ASC LIMIT 1),
			         ''
			       )
			FROM leads l LEFT JOIN contacts c ON c.id=l.contact_id
			WHERE l.id=$1::uuid`, leadID).Scan(&contactID, &phone, &dealID)
		phone = maxNormPhone(phone)
	}

	sql := `
		SELECT l.id::text, l.provider, l.channel, l.name, l.kind, l.operator_label,
		       l.external_ref, l.is_active, l.owner_user_id::text AS owner_id,
		       coalesce(l.max_send_down,false) AS send_down,
		       coalesce(l.max_cold_limit,false) AS cold_limit,
		       coalesce(u.full_name,'') AS owner
		FROM inbox_lines l
		LEFT JOIN users u ON u.id=l.owner_user_id
		WHERE l.is_active AND l.channel='max' AND l.provider='wappi'`
	args := []any{}
	if !canManageStaff(r) {
		sql += ` AND l.owner_user_id=$1::uuid`
		args = append(args, actorID(r))
	}
	sql += ` ORDER BY l.name`
	rows := a.queryMaps(ctx, sql, args...)
	if rows == nil {
		rows = []map[string]any{}
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		lineID := fmt.Sprint(row["id"])
		label := strings.TrimSpace(fmt.Sprint(row["operator_label"]))
		if label == "" {
			label = strings.TrimSpace(fmt.Sprint(row["name"]))
		}
		if label == "" {
			label = "MAX"
		}
		item := map[string]any{
			"id":             lineID,
			"label":          label,
			"phone":          label,
			"name":           row["name"],
			"operator_label": row["operator_label"],
			"channel":        "max",
			"has_inbound":    false,
			"thread_id":      "",
			"send_down":      row["send_down"],
			"cold_limit":     row["cold_limit"],
		}
		if !canManageStaff(r) {
			item["external_ref"] = ""
		}

		if lineID != "" && (phone != "" || contactID != "" || dealID != "") {
			var threadID string
			var hasDialog bool
			_ = a.db.QueryRow(ctx, `
				SELECT t.id::text,
				       EXISTS (
				         SELECT 1 FROM inbox_messages m WHERE m.thread_id=t.id
				       )
				FROM inbox_threads t
				WHERE t.channel='max' AND t.line_id=$1::uuid
				  AND (
				    ($2<>'' AND t.contact_id=$2::uuid)
				    OR ($4<>'' AND t.deal_id=$4::uuid)
				    OR ($3<>'' AND (
				      regexp_replace(coalesce(t.phone,''), '\D', '', 'g') LIKE '%' || $3
				      OR t.external_id = $1::text || ':' || $3
				    ))
				  )
				ORDER BY t.last_at DESC NULLS LAST
				LIMIT 1`, lineID, contactID, phone, dealID).Scan(&threadID, &hasDialog)
			if threadID != "" {
				item["thread_id"] = threadID
				item["has_inbound"] = hasDialog
			}
		}
		out = append(out, item)
	}
	a.writePhones(w, r, 200, map[string]any{"items": out})
}

func (a *App) findMaxThread(ctx context.Context, normPhone, contactID string) string {
	var id string
	if contactID != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_threads
			WHERE channel='max' AND contact_id=$1::uuid
			ORDER BY last_at DESC NULLS LAST LIMIT 1`, contactID).Scan(&id)
		if id != "" {
			return id
		}
	}
	if normPhone != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_threads
			WHERE channel='max' AND (
				regexp_replace(coalesce(phone,''), '\D', '', 'g') LIKE '%' || $1 OR
				regexp_replace(coalesce(external_id,''), '\D', '', 'g') LIKE '%' || $1
			)
			ORDER BY last_at DESC NULLS LAST LIMIT 1`, normPhone).Scan(&id)
	}
	return id
}

// findInboxThreadWithMessages — тред сделки/контакта, где уже есть переписка
// (не пустая оболочка с другого номера MAX). Сначала совпадение по сделке, затем самое свежее сообщение.
func (a *App) findInboxThreadWithMessages(ctx context.Context, dealID, contactID, phone string) (threadID, lineID string) {
	dealID = strings.TrimSpace(dealID)
	contactID = strings.TrimSpace(contactID)
	tail := peerTail(phone)
	if dealID == "" && contactID == "" && tail == "" {
		return "", ""
	}
	_ = a.db.QueryRow(ctx, `
		SELECT t.id::text, coalesce(t.line_id::text,'')
		FROM inbox_threads t
		WHERE EXISTS (
			SELECT 1 FROM inbox_messages m
			WHERE m.thread_id=t.id
			  AND length(trim(coalesce(m.body,''))) > 0
			  AND m.body <> '__DEMO_PASSPORT__'
		)
		AND (
			t.deal_id = nullif($1,'')::uuid
			OR t.contact_id = nullif($2,'')::uuid
			OR ($3 <> '' AND right(regexp_replace(coalesce(t.phone,''), '[^0-9]', '', 'g'), length($3::text)) = $3)
		)
		ORDER BY
			CASE WHEN t.deal_id = nullif($1,'')::uuid THEN 0 ELSE 1 END,
			(SELECT max(m.created_at) FROM inbox_messages m WHERE m.thread_id=t.id) DESC NULLS LAST
		LIMIT 1`, dealID, contactID, tail).Scan(&threadID, &lineID)
	return threadID, lineID
}

func (a *App) maxInviteLink(leadID, contactID, dealID string) string {
	_ = leadID
	_ = contactID
	_ = dealID
	u := strings.TrimPrefix(strings.TrimSpace(a.maxBotUsername), "@")
	if u == "" {
		return ""
	}
	return "https://max.ru/" + u
}
