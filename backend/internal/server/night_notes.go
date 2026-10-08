package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// nightNoteWriter — кто пишет в «Информацию для ночного логиста».
// Логист, ночной логист и админ. Менеджер карточки — её владелец.
// РОП — руководитель отдела этого владельца, не РОП другого отдела.
func nightNoteWriter(role, actorID, ownerID, headID string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "logist", "night_logist":
		return true
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	if actorID == strings.TrimSpace(ownerID) {
		return true
	}
	headID = strings.TrimSpace(headID)
	return headID != "" && actorID == headID
}

func (a *App) dealNightNoteParties(ctx context.Context, dealID string) (ownerID, headID string) {
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(d.owner_id::text,''), coalesce(dep.head_user_id::text,'')
		FROM deals d
		LEFT JOIN users ou ON ou.id=d.owner_id
		LEFT JOIN departments dep ON dep.id=ou.department_id
		WHERE d.id=$1::uuid`, dealID).Scan(&ownerID, &headID)
	return ownerID, headID
}

func (a *App) canWriteNightNote(ctx context.Context, r *http.Request, dealID string) bool {
	if !a.canSeeDeal(ctx, a.crmScope(r), dealID) {
		return false
	}
	ownerID, headID := a.dealNightNoteParties(ctx, dealID)
	return nightNoteWriter(roleName(r), actorID(r), ownerID, headID)
}

func (a *App) listNightNotes(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.ensureLogisticsSchema(r.Context())
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if !a.canWriteNightNote(r.Context(), r, id) {
		writeJSON(w, 200, map[string]any{"allowed": false, "items": []any{}})
		return
	}
	items := a.queryMaps(r.Context(), `
		SELECT n.id::text, n.body, n.created_at,
		       coalesce(u.full_name,'') AS author
		FROM deal_night_notes n
		LEFT JOIN users u ON u.id=n.author_id
		WHERE n.deal_id=$1::uuid
		ORDER BY n.created_at ASC`, id)
	writeJSON(w, 200, map[string]any{
		"allowed": true,
		"items":   items,
		"brief":   a.nightNoteBrief(r.Context(), id),
	})
}

// nightNoteBrief — шапка окна ночного логиста из карточки и заполненного билета.
func (a *App) nightNoteBrief(ctx context.Context, dealID string) map[string]any {
	var recruiter, candidate, rop, fromCity, toCity, transport string
	var depart, arrive *time.Time
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(nullif(b.recruiter,''), ou.full_name, ''),
		       coalesce(nullif(b.candidate,''), nullif(c.full_name,''), nullif(l.title,''), nullif(d.title,''), ''),
		       coalesce(
		         nullif(b.rop,''),
		         nullif(coalesce(rop.full_name,''), ''),
		         CASE WHEN lower(coalesce(orole.name,''))='rop' THEN coalesce(ou.full_name,'') ELSE '' END,
		         ''
		       ),
		       coalesce(t.from_city,''), coalesce(t.to_city,''), coalesce(t.transport,''),
		       t.depart_at, t.arrive_at
		FROM deals d
		LEFT JOIN users ou ON ou.id=d.owner_id
		LEFT JOIN roles orole ON orole.id=ou.role_id
		LEFT JOIN departments dep ON dep.id=ou.department_id
		LEFT JOIN users rop ON rop.id=dep.head_user_id
		LEFT JOIN leads l ON l.id=d.lead_id
		LEFT JOIN contacts c ON c.id=coalesce(d.contact_id, l.contact_id)
		LEFT JOIN deal_tickets t ON t.deal_id=d.id
		LEFT JOIN deal_night_briefs b ON b.deal_id=d.id
		WHERE d.id=$1::uuid`, dealID).Scan(
		&recruiter, &candidate, &rop, &fromCity, &toCity, &transport, &depart, &arrive)
	out := map[string]any{
		"recruiter": recruiter,
		"candidate": candidate,
		"rop":       rop,
		"from_city": fromCity,
		"to_city":   toCity,
		"transport": transport,
	}
	if err != nil {
		return out
	}
	if depart != nil {
		out["depart_at"] = depart.UTC().Format(time.RFC3339)
	}
	if arrive != nil {
		out["arrive_at"] = arrive.UTC().Format(time.RFC3339)
	}
	return out
}

func clipNightText(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}

// nightBriefNameOverride — своё имя в справке. Совпадение с карточкой не замораживает его.
func nightBriefNameOverride(edited, live string) string {
	edited = clipNightText(edited, 160)
	if edited == strings.TrimSpace(live) {
		return ""
	}
	return edited
}

func nightBriefTransport(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "—":
		return "", true
	case "air", "avia", "авиа", "самолет", "самолёт":
		return "самолет", true
	case "rail", "train", "жд", "поезд":
		return "поезд", true
	case "bus", "автобус":
		return "автобус", true
	default:
		return "", false
	}
}

func nightBriefTime(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return t.UTC(), nil
}

func (a *App) nightBriefLiveNames(ctx context.Context, dealID string) (recruiter, candidate, rop string) {
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(ou.full_name,''),
		       coalesce(nullif(c.full_name,''), nullif(l.title,''), nullif(d.title,''), ''),
		       coalesce(
		         nullif(coalesce(rop.full_name,''), ''),
		         CASE WHEN lower(coalesce(orole.name,''))='rop' THEN coalesce(ou.full_name,'') ELSE '' END,
		         ''
		       )
		FROM deals d
		LEFT JOIN users ou ON ou.id=d.owner_id
		LEFT JOIN roles orole ON orole.id=ou.role_id
		LEFT JOIN departments dep ON dep.id=ou.department_id
		LEFT JOIN users rop ON rop.id=dep.head_user_id
		LEFT JOIN leads l ON l.id=d.lead_id
		LEFT JOIN contacts c ON c.id=coalesce(d.contact_id, l.contact_id)
		WHERE d.id=$1::uuid`, dealID).Scan(&recruiter, &candidate, &rop)
	return recruiter, candidate, rop
}

func (a *App) putNightBrief(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.ensureLogisticsSchema(r.Context())
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if rejectVisualOnlyChange(w, a.dealVisualOnly(r.Context(), id)) {
		return
	}
	if !a.canWriteNightNote(r.Context(), r, id) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	var in struct {
		Recruiter string `json:"recruiter"`
		Candidate string `json:"candidate"`
		ROP       string `json:"rop"`
		FromCity  string `json:"from_city"`
		ToCity    string `json:"to_city"`
		DepartAt  string `json:"depart_at"`
		ArriveAt  string `json:"arrive_at"`
		Transport string `json:"transport"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	transport, ok := nightBriefTransport(in.Transport)
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "неизвестный транспорт"})
		return
	}
	depart, err := nightBriefTime(in.DepartAt)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "время отправления"})
		return
	}
	arrive, err := nightBriefTime(in.ArriveAt)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "время прибытия"})
		return
	}
	fromCity := clipNightText(in.FromCity, 120)
	toCity := clipNightText(in.ToCity, 120)
	liveRec, liveCand, liveRop := a.nightBriefLiveNames(r.Context(), id)
	route := ""
	if fromCity != "" && toCity != "" {
		route = fromCity + " → " + toCity
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO deal_night_briefs (deal_id, recruiter, candidate, rop, updated_by, updated_at)
		VALUES ($1::uuid,$2,$3,$4,nullif($5,'')::uuid, now())
		ON CONFLICT (deal_id) DO UPDATE SET
			recruiter=EXCLUDED.recruiter,
			candidate=EXCLUDED.candidate,
			rop=EXCLUDED.rop,
			updated_by=EXCLUDED.updated_by,
			updated_at=now()`,
		id,
		nightBriefNameOverride(in.Recruiter, liveRec),
		nightBriefNameOverride(in.Candidate, liveCand),
		nightBriefNameOverride(in.ROP, liveRop),
		actorID(r)); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO deal_tickets (deal_id, transport, from_city, to_city, depart_at, arrive_at, route, updated_by, updated_at)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,nullif($8,'')::uuid, now())
		ON CONFLICT (deal_id) DO UPDATE SET
			transport=EXCLUDED.transport,
			from_city=EXCLUDED.from_city,
			to_city=EXCLUDED.to_city,
			depart_at=EXCLUDED.depart_at,
			arrive_at=EXCLUDED.arrive_at,
			route=CASE WHEN EXCLUDED.route <> '' THEN EXCLUDED.route ELSE deal_tickets.route END,
			updated_by=EXCLUDED.updated_by,
			updated_at=now()`,
		id, transport, fromCity, toCity, depart, arrive, route, actorID(r)); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "brief": a.nightNoteBrief(r.Context(), id)})
}

func (a *App) addNightNote(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.ensureLogisticsSchema(r.Context())
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if rejectVisualOnlyChange(w, a.dealVisualOnly(r.Context(), id)) {
		return
	}
	if !a.canWriteNightNote(r.Context(), r, id) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	_ = readJSON(r, &in)
	body := strings.TrimSpace(in.Body)
	if body == "" {
		writeJSON(w, 400, map[string]string{"error": "пустой комментарий"})
		return
	}
	if len([]rune(body)) > 8000 {
		body = string([]rune(body)[:8000])
	}
	var noteID string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO deal_night_notes (deal_id, author_id, body)
		VALUES ($1::uuid, nullif($2,'')::uuid, $3)
		RETURNING id::text`, id, actorID(r), body).Scan(&noteID)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	items := a.queryMaps(r.Context(), `
		SELECT n.id::text, n.body, n.created_at,
		       coalesce(u.full_name,'') AS author
		FROM deal_night_notes n
		LEFT JOIN users u ON u.id=n.author_id
		WHERE n.id=$1::uuid`, noteID)
	if len(items) == 0 {
		writeJSON(w, 201, map[string]any{"id": noteID, "body": body})
		return
	}
	writeJSON(w, 201, items[0])
}
