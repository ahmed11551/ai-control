package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var ticketReportMonths = []string{
	"", "января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

func ticketTransportLabel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "air", "самолет", "самолёт":
		return "Самолёт"
	case "rail", "поезд":
		return "Поезд"
	case "bus", "автобус":
		return "Автобус"
	default:
		return strings.TrimSpace(raw)
	}
}

func ticketMoscow(t time.Time) time.Time {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*3600)
	}
	return t.In(loc)
}

func ticketDaypart(t time.Time) string {
	t = ticketMoscow(t)
	mins := t.Hour()*60 + t.Minute()
	if mins == 0 {
		return ""
	}
	if mins >= 21*60 || mins < 6*60 {
		return "ночь"
	}
	if mins < 12*60 {
		return "утро"
	}
	if mins < 21*60 {
		return "вечер"
	}
	return ""
}

func formatTicketWhen(t time.Time) string {
	t = ticketMoscow(t)
	month := ""
	if int(t.Month()) < len(ticketReportMonths) {
		month = ticketReportMonths[t.Month()]
	}
	date := t.Format("2") + " " + month + " " + t.Format("2006")
	if part := ticketDaypart(t); part != "" {
		return date + ", " + part
	}
	return date
}

// ticketReportText — текст диалога. Пустые рейс, вагон и место не попадают в сообщение.
func ticketReportText(from, to, transport, flight, wagon, seat string, depart, arrive *time.Time) string {
	lines := []string{"Билет оформлен", "", strings.TrimSpace(from) + " → " + strings.TrimSpace(to)}
	if label := ticketTransportLabel(transport); label != "" {
		lines = append(lines, label)
	}
	if v := strings.TrimSpace(flight); v != "" {
		lines = append(lines, "Рейс: "+v)
	}
	if v := strings.TrimSpace(wagon); v != "" {
		lines = append(lines, "Вагон: "+v)
	}
	if v := strings.TrimSpace(seat); v != "" {
		lines = append(lines, "Место: "+v)
	}
	if depart != nil {
		lines = append(lines, "Выезд: "+formatTicketWhen(*depart))
	}
	if arrive != nil {
		lines = append(lines, "Прибытие: "+formatTicketWhen(*arrive))
	}
	return strings.Join(lines, "\n")
}

func parseTicketWhen(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	tm, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &tm
}

// reportTicketToManager сохраняет билет и пишет ответственному в диалог.
// Стадию и владельца сделки не меняет. Обычный PUT /ticket для этого не годится:
// сохранение рейса логистом само переводит сделку на «Куплен билет».
func (a *App) reportTicketToManager(w http.ResponseWriter, r *http.Request) {
	if !canBrowseDealDocs(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "отчёт отправляет логист"})
		return
	}
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	a.ensureLogisticsSchema(r.Context())
	a.ensureStaffDialogsSchema(r.Context())

	var in struct {
		Transport     string `json:"transport"`
		FromCity      string `json:"from_city"`
		ToCity        string `json:"to_city"`
		FlightOrTrain string `json:"flight_or_train"`
		DepartAt      string `json:"depart_at"`
		ArriveAt      string `json:"arrive_at"`
		Wagon         string `json:"wagon"`
		Seat          string `json:"seat"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	from := strings.TrimSpace(in.FromCity)
	to := strings.TrimSpace(in.ToCity)
	transport := strings.TrimSpace(in.Transport)
	depart := parseTicketWhen(in.DepartAt)
	arrive := parseTicketWhen(in.ArriveAt)
	if from == "" || to == "" || transport == "" || depart == nil {
		writeJSON(w, 400, map[string]string{"error": "заполните откуда, куда, транспорт и выезд"})
		return
	}

	var ownerID string
	if err := a.db.QueryRow(r.Context(), `
		SELECT coalesce(owner_id::text,'') FROM deals WHERE id=$1::uuid`, id).Scan(&ownerID); err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if strings.TrimSpace(ownerID) == "" {
		writeJSON(w, 400, map[string]string{"error": "у сделки нет ответственного менеджера"})
		return
	}

	flight := strings.TrimSpace(in.FlightOrTrain)
	wagon := strings.TrimSpace(in.Wagon)
	seat := strings.TrimSpace(in.Seat)
	body := ticketReportText(from, to, transport, flight, wagon, seat, depart, arrive)
	route := from + " → " + to

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось сохранить"})
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(), `
		INSERT INTO deal_tickets (deal_id, transport, from_city, to_city, flight_or_train, depart_at, arrive_at, route, wagon, seat, updated_by, updated_at)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,nullif($11,'')::uuid, now())
		ON CONFLICT (deal_id) DO UPDATE SET
			transport=EXCLUDED.transport,
			from_city=EXCLUDED.from_city,
			to_city=EXCLUDED.to_city,
			flight_or_train=EXCLUDED.flight_or_train,
			depart_at=EXCLUDED.depart_at,
			arrive_at=EXCLUDED.arrive_at,
			route=EXCLUDED.route,
			wagon=EXCLUDED.wagon,
			seat=EXCLUDED.seat,
			updated_by=EXCLUDED.updated_by,
			updated_at=now()`,
		id, transport, from, to, flight, depart, arrive, route, wagon, seat, actorID(r)); err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось сохранить билет"})
		return
	}

	var dialogID string
	_ = tx.QueryRow(r.Context(), `
		SELECT id::text FROM staff_dialogs
		WHERE deal_id=$1::uuid AND status='open'
		LIMIT 1`, id).Scan(&dialogID)
	created := false
	if dialogID == "" {
		var leadID string
		_ = tx.QueryRow(r.Context(), `SELECT coalesce(lead_id::text,'') FROM deals WHERE id=$1::uuid`, id).Scan(&leadID)
		err = tx.QueryRow(r.Context(), `
			INSERT INTO staff_dialogs (deal_id, lead_id, opened_by, status, last_body, last_at)
			VALUES ($1::uuid, nullif($2,'')::uuid, $3::uuid, 'open', '', now())
			RETURNING id::text`, id, leadID, actorID(r)).Scan(&dialogID)
		if err != nil || dialogID == "" {
			writeJSON(w, 400, map[string]string{"error": "не удалось открыть диалог"})
			return
		}
		created = true
	}

	var msgID string
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO staff_dialog_messages (dialog_id, author_id, body)
		VALUES ($1::uuid, $2::uuid, $3)
		RETURNING id::text`, dialogID, actorID(r), body).Scan(&msgID); err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось отправить"})
		return
	}
	preview := body
	if len([]rune(preview)) > 180 {
		preview = string([]rune(preview)[:180])
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE staff_dialogs SET last_body=$2, last_at=now() WHERE id=$1::uuid`, dialogID, preview); err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось отправить"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось отправить"})
		return
	}

	a.logDealLeadAct(r.Context(), id, actorID(r), "ticket_reported", map[string]any{
		"transport": transport,
	})
	if a.hub != nil {
		if created {
			a.hub.Broadcast("staff_dialog.updated", map[string]any{"id": dialogID, "deal_id": id, "action": "opened"})
		}
		a.hub.Broadcast("staff_dialog.updated", map[string]any{"id": dialogID, "action": "message", "message_id": msgID})
		if isLogist(r) {
			a.notifyStaffDialogMessage(r.Context(), dialogID, actorID(r), preview)
		} else {
			a.notifyTicketReportOwner(r.Context(), dialogID, actorID(r), preview)
		}
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "text": body, "dialog_id": dialogID,
		"ticket": a.loadDealTicketMap(r.Context(), id, true),
	})
}

// notifyTicketReportOwner — админ в обычном диалоге будит логистов, а отчёт должен дойти владельцу сделки.
func (a *App) notifyTicketReportOwner(ctx context.Context, dialogID, authorID, preview string) {
	if a.hub == nil || dialogID == "" || authorID == "" {
		return
	}
	var authorName, dealTitle, dealID, leadID, ownerID string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(u.full_name,'')
		FROM users u WHERE u.id=$1::uuid`, authorID).Scan(&authorName)
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(nullif(c.full_name,''), d.title, 'Кандидат'),
		       d.id::text, coalesce(sd.lead_id::text,''), coalesce(d.owner_id::text,'')
		FROM staff_dialogs sd
		JOIN deals d ON d.id=sd.deal_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		WHERE sd.id=$1::uuid`, dialogID).Scan(&dealTitle, &dealID, &leadID, &ownerID)
	if ownerID == "" || ownerID == authorID {
		return
	}
	var ok bool
	_ = a.db.QueryRow(ctx, `
		SELECT true
		FROM users u
		JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid
		  AND coalesce(u.is_active,true)=true
		  AND lower(r.name) IN ('manager','sales','rop','admin')`, ownerID).Scan(&ok)
	if !ok {
		return
	}
	title := "Билет оформлен"
	if authorName != "" {
		title = "Билет оформлен: " + authorName
	}
	body := dealTitle
	if preview != "" {
		body = dealTitle + " — " + preview
		if len([]rune(body)) > 200 {
			body = string([]rune(body)[:200])
		}
	}
	payload := mustJSON(map[string]any{
		"kind":      "staff_dialog",
		"dialog_id": dialogID,
		"deal_id":   dealID,
		"lead_id":   leadID,
		"author_id": authorID,
	})
	tag, err := a.db.Exec(ctx, `
		UPDATE notifications
		SET title=$3, body=$4, payload=$5::jsonb, is_read=false, created_at=now()
		WHERE id = (
		  SELECT id FROM notifications
		  WHERE user_id=$1::uuid AND is_read=false
		    AND channel='staff_dialog'
		    AND payload->>'dialog_id'=$2
		  ORDER BY created_at DESC LIMIT 1
		)`, ownerID, dialogID, title, body, payload)
	if err == nil && tag.RowsAffected() > 0 {
		a.hub.Broadcast("notification", map[string]any{
			"user_id": ownerID, "kind": "staff_dialog", "dialog_id": dialogID,
			"title": title, "body": body,
		})
		return
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO notifications (user_id, channel, title, body, payload)
		VALUES ($1::uuid, 'staff_dialog', $2, $3, $4::jsonb)`,
		ownerID, title, body, payload)
	a.hub.Broadcast("notification", map[string]any{
		"user_id": ownerID, "kind": "staff_dialog", "dialog_id": dialogID,
		"title": title, "body": body,
	})
}
