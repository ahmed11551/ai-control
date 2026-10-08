package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var nightReqOnce sync.Once

func (a *App) ensureNightLogistRequests(ctx context.Context) {
	if a == nil || a.db == nil {
		return
	}
	nightReqOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS night_logist_requests (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				deal_id UUID NOT NULL UNIQUE REFERENCES deals(id) ON DELETE CASCADE,
				sent_by UUID,
				sent_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`)
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE night_logist_requests
				ADD COLUMN IF NOT EXISTS done_at TIMESTAMPTZ,
				ADD COLUMN IF NOT EXISTS done_by UUID,
				ADD COLUMN IF NOT EXISTS skip_at TIMESTAMPTZ,
				ADD COLUMN IF NOT EXISTS skip_by UUID,
				ADD COLUMN IF NOT EXISTS skip_reason TEXT,
				ADD COLUMN IF NOT EXISTS first_call_at TIMESTAMPTZ,
				ADD COLUMN IF NOT EXISTS self_note TEXT NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS self_note_at TIMESTAMPTZ`)
	})
}

func moscowLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*3600)
	}
	return loc
}

// parseNightFirstCall читает дату и время первого звонка как московские.
func parseNightFirstCall(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	loc := moscowLocation()
	var at time.Time
	var err error
	switch {
	case len(raw) == len("2006-01-02T15:04"):
		at, err = time.ParseInLocation("2006-01-02T15:04", raw, loc)
	case len(raw) >= len("2006-01-02T15:04:05") && raw[10] == 'T':
		at, err = time.ParseInLocation("2006-01-02T15:04:05", raw[:19], loc)
	case len(raw) >= len("2006-01-02 15:04") && raw[10] == ' ':
		at, err = time.ParseInLocation("2006-01-02 15:04", raw[:16], loc)
	default:
		return time.Time{}, false
	}
	if err != nil || at.Year() < 2024 || at.Year() > 2036 {
		return time.Time{}, false
	}
	return at, true
}

const nightRequestFrom = `
	FROM night_logist_requests n
	JOIN deals d ON d.id = n.deal_id
	LEFT JOIN contacts c ON c.id = d.contact_id
	LEFT JOIN LATERAL (
	  SELECT city, contact_id, title
	  FROM leads
	  WHERE id = d.lead_id OR converted_deal_id = d.id
	  ORDER BY (id = d.lead_id) DESC
	  LIMIT 1
	) l ON true
	LEFT JOIN contacts lc ON lc.id = l.contact_id
	LEFT JOIN pipeline_stages s ON s.id = d.stage_id
	LEFT JOIN users owner ON owner.id = d.owner_id
	LEFT JOIN departments dep ON dep.id = owner.department_id
	LEFT JOIN users u ON u.id = n.sent_by`

func nightRequestWhere(deptID, q, when, from, to, done, call string) (string, []any) {
	args := []any{}
	where := []string{"TRUE"}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	switch cleanDeptFilter(deptID) {
	case "none":
		where = append(where, "owner.department_id IS NULL")
	case "":
	default:
		where = append(where, "owner.department_id = "+next(cleanDeptFilter(deptID))+"::uuid")
	}
	q = strings.TrimSpace(q)
	if q != "" {
		pat := next(likePattern(q))
		name := `(coalesce(nullif(btrim(c.full_name), ''), nullif(btrim(lc.full_name), ''), nullif(btrim(d.title), ''), '') ILIKE ` + pat +
			` OR coalesce(n.self_note, '') ILIKE ` + pat + `)`
		digits := digitsOnly(q)
		if len(digits) >= 3 {
			phone := `regexp_replace(coalesce(c.phone, ''), '\D', '', 'g') LIKE ` + next("%"+digits+"%") +
				` OR regexp_replace(coalesce(lc.phone, ''), '\D', '', 'g') LIKE ` + next("%"+digits+"%")
			where = append(where, "("+name+" OR "+phone+")")
		} else {
			where = append(where, name)
		}
	}
	sentDate := "(timezone('Europe/Moscow', n.sent_at))::date"
	switch when {
	case "today":
		where = append(where, sentDate+" = (timezone('Europe/Moscow', now()))::date")
	case "week":
		where = append(where, sentDate+" >= (timezone('Europe/Moscow', now()))::date - 6 AND "+sentDate+" <= (timezone('Europe/Moscow', now()))::date")
	case "past":
		where = append(where, sentDate+" < (timezone('Europe/Moscow', now()))::date")
	case "range":
		if fromDate, ok := parseISODate(from); ok {
			where = append(where, sentDate+" >= "+next(fromDate.Format("2006-01-02"))+"::date")
		}
		if toDate, ok := parseISODate(to); ok {
			where = append(where, sentDate+" <= "+next(toDate.Format("2006-01-02"))+"::date")
		}
	}
	switch done {
	case "yes":
		where = append(where, "n.done_at IS NOT NULL")
	case "wait", "no":
		where = append(where, "n.done_at IS NULL AND n.skip_at IS NULL")
	case "skip":
		where = append(where, "n.skip_at IS NOT NULL")
	}
	switch call {
	case "soon":
		where = append(where, "n.done_at IS NULL AND n.skip_at IS NULL AND n.first_call_at IS NOT NULL AND (timezone('Europe/Moscow', n.first_call_at))::date <= (timezone('Europe/Moscow', now()))::date")
	case "overdue":
		where = append(where, "n.done_at IS NULL AND n.skip_at IS NULL AND n.first_call_at < now()")
	}
	return strings.Join(where, " AND "), args
}

func nightListQuery(r *http.Request) (q, dept, when, from, to, done, call string) {
	q = strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) > 80 {
		q = string([]rune(q)[:80])
	}
	dept = cleanDeptFilter(r.URL.Query().Get("dept"))
	when = strings.TrimSpace(r.URL.Query().Get("when"))
	switch when {
	case "today", "week", "past", "range":
	default:
		when = ""
	}
	from = strings.TrimSpace(r.URL.Query().Get("from"))
	to = strings.TrimSpace(r.URL.Query().Get("to"))
	done = strings.TrimSpace(r.URL.Query().Get("done"))
	switch done {
	case "yes", "wait", "skip":
	case "no":
		done = "wait"
	default:
		done = ""
	}
	call = strings.TrimSpace(r.URL.Query().Get("call"))
	switch call {
	case "soon", "overdue":
	default:
		call = ""
	}
	return q, dept, when, from, to, done, call
}

func canSendToNightLogist(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "manager", "sales", "rop", "admin":
		return true
	default:
		return false
	}
}

func (a *App) getNightLogistSend(w http.ResponseWriter, r *http.Request) {
	if !canSendToNightLogist(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureNightLogistRequests(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !a.canSeeDeal(r.Context(), a.crmScope(r), id) {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	var sentAt, callAt string
	err := a.db.QueryRow(r.Context(), `
		SELECT to_char(timezone('Europe/Moscow', sent_at), 'YYYY-MM-DD HH24:MI'),
		       coalesce(to_char(timezone('Europe/Moscow', first_call_at), 'YYYY-MM-DD HH24:MI'), '')
		FROM night_logist_requests WHERE deal_id=$1::uuid`, id).Scan(&sentAt, &callAt)
	if err != nil {
		writeJSON(w, 200, map[string]any{"sent": false})
		return
	}
	writeJSON(w, 200, map[string]any{"sent": true, "sent_at": sentAt, "first_call_at": callAt})
}

func (a *App) postNightLogistSend(w http.ResponseWriter, r *http.Request) {
	if !canSendToNightLogist(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureNightLogistRequests(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !a.canSeeDeal(r.Context(), a.crmScope(r), id) {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	var body struct {
		FirstCallAt string `json:"first_call_at"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "укажите дату и время первого звонка"})
		return
	}
	callAt, ok := parseNightFirstCall(body.FirstCallAt)
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "укажите дату и время первого звонка"})
		return
	}
	callLabel := callAt.In(moscowLocation()).Format("2006-01-02 15:04")
	tag, err := a.db.Exec(r.Context(), `
		INSERT INTO night_logist_requests (deal_id, sent_by, first_call_at)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (deal_id) DO NOTHING`, id, actorID(r), callAt)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось отправить"})
		return
	}
	already := tag.RowsAffected() == 0
	if !already {
		a.logAct(r.Context(), "deal", id, actorID(r), "sent_to_night_logist", map[string]any{
			"first_call_at": callLabel,
		})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "already": already, "first_call_at": callLabel})
}

func canSeeNightRequests(r *http.Request) bool {
	return roleName(r) == "night_logist" || isAdmin(r)
}

// previewNightRequests — временный локальный просмотр без входа.
// На сервере маршрута нет, пока процесс не запущен с LOCAL_NIGHT_PREVIEW=1.
func (a *App) previewNightRequests(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("LOCAL_NIGHT_PREVIEW") != "1" || !loopbackRequest(r) {
		http.NotFound(w, r)
		return
	}
	var adminID string
	err := a.db.QueryRow(r.Context(), `
		SELECT u.id::text
		FROM users u
		JOIN roles ro ON ro.id = u.role_id
		WHERE ro.name = 'admin' AND u.is_active
		ORDER BY u.created_at
		LIMIT 1`).Scan(&adminID)
	if err != nil || strings.TrimSpace(adminID) == "" {
		writeJSON(w, 404, map[string]string{"error": "недоступно"})
		return
	}
	ctx := context.WithValue(r.Context(), ctxUserID, adminID)
	ctx = context.WithValue(ctx, ctxRole, "admin")
	a.listNightLogistRequests(w, r.WithContext(ctx))
}

func loopbackRequest(r *http.Request) bool {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *App) listNightLogistRequests(w http.ResponseWriter, r *http.Request) {
	if !canSeeNightRequests(r) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureNightLogistRequests(r.Context())
	a.ensureLogisticsSchema(r.Context())
	q, dept, when, from, to, done, call := nightListQuery(r)
	listWhere, listArgs := nightRequestWhere(dept, q, when, from, to, done, call)
	whenWhere, whenArgs := nightRequestWhere(dept, q, "", "", "", done, call)
	doneWhere, doneArgs := nightRequestWhere(dept, q, when, from, to, "", "")
	deptWhere, deptArgs := nightRequestWhere("", q, when, from, to, done, call)
	callWhere, callArgs := nightRequestWhere(dept, q, when, from, to, "", "")

	var today, week, past, allWhen int
	err := a.db.QueryRow(r.Context(), `
		SELECT count(*) FILTER (WHERE (timezone('Europe/Moscow', n.sent_at))::date = (timezone('Europe/Moscow', now()))::date)::int,
		       count(*) FILTER (WHERE (timezone('Europe/Moscow', n.sent_at))::date >= (timezone('Europe/Moscow', now()))::date - 6
		                           AND (timezone('Europe/Moscow', n.sent_at))::date <= (timezone('Europe/Moscow', now()))::date)::int,
		       count(*) FILTER (WHERE (timezone('Europe/Moscow', n.sent_at))::date < (timezone('Europe/Moscow', now()))::date)::int,
		       count(*)::int
		`+nightRequestFrom+`
		WHERE `+whenWhere, whenArgs...).Scan(&today, &week, &past, &allWhen)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось загрузить"})
		return
	}
	var doneAll, doneYes, doneWait, doneSkip int
	err = a.db.QueryRow(r.Context(), `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE n.done_at IS NOT NULL)::int,
		       count(*) FILTER (WHERE n.done_at IS NULL AND n.skip_at IS NULL)::int,
		       count(*) FILTER (WHERE n.skip_at IS NOT NULL)::int
		`+nightRequestFrom+`
		WHERE `+doneWhere, doneArgs...).Scan(&doneAll, &doneYes, &doneWait, &doneSkip)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось загрузить"})
		return
	}
	var callSoon, callOverdue int
	err = a.db.QueryRow(r.Context(), `
		SELECT count(*) FILTER (
		         WHERE n.done_at IS NULL AND n.skip_at IS NULL AND n.first_call_at IS NOT NULL
		           AND (timezone('Europe/Moscow', n.first_call_at))::date <= (timezone('Europe/Moscow', now()))::date
		       )::int,
		       count(*) FILTER (
		         WHERE n.done_at IS NULL AND n.skip_at IS NULL AND n.first_call_at < now()
		       )::int
		`+nightRequestFrom+`
		WHERE `+callWhere, callArgs...).Scan(&callSoon, &callOverdue)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось загрузить"})
		return
	}
	var deptAll int
	if err = a.db.QueryRow(r.Context(), `SELECT count(*)::int `+nightRequestFrom+` WHERE `+deptWhere, deptArgs...).Scan(&deptAll); err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось загрузить"})
		return
	}
	depts := a.queryMaps(r.Context(), `
		SELECT dep.id::text AS id, dep.name AS name, count(*)::int AS count
		`+nightRequestFrom+`
		WHERE `+deptWhere+` AND dep.id IS NOT NULL
		GROUP BY dep.id, dep.name
		HAVING count(*) > 0
		ORDER BY count(*) DESC, dep.name`, deptArgs...)
	if depts == nil {
		depts = []map[string]any{}
	}
	var total int
	err = a.db.QueryRow(r.Context(), `SELECT count(*)::int `+nightRequestFrom+` WHERE `+listWhere, listArgs...).Scan(&total)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось загрузить"})
		return
	}
	rows := a.queryMaps(r.Context(), `
		SELECT n.id::text, n.deal_id::text AS deal_id,
		       coalesce(nullif(btrim(c.full_name), ''), nullif(btrim(lc.full_name), ''), nullif(btrim(d.title), ''), 'Клиент') AS full_name,
		       coalesce(nullif(btrim(c.phone), ''), nullif(btrim(lc.phone), ''), '') AS phone,
		       coalesce(nullif(btrim(c.position), ''), nullif(btrim(lc.position), ''), '') AS position,
		       coalesce(nullif(btrim(l.city), ''), '') AS city,
		       coalesce(s.name, '') AS stage,
		       coalesce(u.full_name, '') AS sent_by_name,
		       coalesce(dep.name, '') AS department,
		       to_char(timezone('Europe/Moscow', n.sent_at), 'YYYY-MM-DD HH24:MI') AS sent_at,
		       coalesce(to_char(timezone('Europe/Moscow', n.first_call_at), 'YYYY-MM-DD HH24:MI'), '') AS first_call_at,
		       (n.done_at IS NULL AND n.skip_at IS NULL AND n.first_call_at IS NOT NULL AND n.first_call_at < now()) AS first_call_due,
		       coalesce(to_char(timezone('Europe/Moscow', n.done_at), 'YYYY-MM-DD HH24:MI'), '') AS done_at,
		       coalesce(to_char(timezone('Europe/Moscow', n.skip_at), 'YYYY-MM-DD HH24:MI'), '') AS skip_at,
		       coalesce(n.skip_reason, '') AS skip_reason,
		       coalesce(n.self_note, '') AS self_note,
		       coalesce(to_char(timezone('Europe/Moscow', n.self_note_at), 'YYYY-MM-DD HH24:MI'), '') AS self_note_at,
		       coalesce(nullif(b.recruiter,''), owner.full_name, '') AS brief_recruiter,
		       coalesce(nullif(b.candidate,''), nullif(c.full_name,''), nullif(lc.full_name,''), nullif(l.title,''), nullif(d.title,''), '') AS brief_candidate,
		       coalesce(
		         nullif(b.rop,''),
		         nullif(coalesce(ropuser.full_name,''), ''),
		         CASE WHEN lower(coalesce(orole.name,''))='rop' THEN coalesce(owner.full_name,'') ELSE '' END,
		         ''
		       ) AS brief_rop,
		       coalesce(t.from_city,'') AS from_city,
		       coalesce(t.to_city,'') AS to_city,
		       coalesce(t.transport,'') AS transport,
		       t.depart_at,
		       t.arrive_at,
		       coalesce((
		         SELECT json_agg(json_build_object(
		           'id', nn.id::text,
		           'body', nn.body,
		           'author', coalesce(au.full_name, ''),
		           'created_at', to_char(timezone('Europe/Moscow', nn.created_at), 'YYYY-MM-DD HH24:MI')
		         ) ORDER BY nn.created_at)
		         FROM deal_night_notes nn
		         LEFT JOIN users au ON au.id = nn.author_id
		         WHERE nn.deal_id = d.id
		       ), '[]'::json)::text AS notes
		`+nightRequestFrom+`
		LEFT JOIN roles orole ON orole.id = owner.role_id
		LEFT JOIN users ropuser ON ropuser.id = dep.head_user_id
		LEFT JOIN deal_night_briefs b ON b.deal_id = d.id
		LEFT JOIN deal_tickets t ON t.deal_id = d.id
		WHERE `+listWhere+`
		ORDER BY CASE WHEN n.done_at IS NOT NULL THEN 2 WHEN n.skip_at IS NOT NULL THEN 1 ELSE 0 END,
		         CASE WHEN n.done_at IS NULL AND n.skip_at IS NULL THEN n.first_call_at END ASC NULLS LAST,
		         n.sent_at DESC
		LIMIT 300`, listArgs...)
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{
		"items":       rows,
		"total":       total,
		"when_counts": map[string]int{"today": today, "week": week, "past": past, "all": allWhen},
		"done_counts": map[string]int{"all": doneAll, "yes": doneYes, "wait": doneWait, "skip": doneSkip},
		"call_counts": map[string]int{"soon": callSoon, "overdue": callOverdue},
		"dept_all":    deptAll,
		"dept_counts": depts,
	})
}

func (a *App) putNightLogistDone(w http.ResponseWriter, r *http.Request) {
	if !canSeeNightRequests(r) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureNightLogistRequests(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var body struct {
		Done   *bool  `json:"done"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный запрос"})
		return
	}
	status := strings.ToLower(strings.TrimSpace(body.Status))
	switch status {
	case "done", "skip", "wait":
	case "":
		if body.Done == nil {
			writeJSON(w, 400, map[string]string{"error": "некорректный запрос"})
			return
		}
		if *body.Done {
			status = "done"
		} else {
			status = "wait"
		}
	default:
		writeJSON(w, 400, map[string]string{"error": "некорректный запрос"})
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if status == "skip" {
		if reason == "" {
			writeJSON(w, 400, map[string]string{"error": "напишите причину"})
			return
		}
		if len([]rune(reason)) > 500 {
			reason = string([]rune(reason)[:500])
		}
	}
	var dealID, doneAt, skipAt, savedReason string
	err := a.db.QueryRow(r.Context(), `
		UPDATE night_logist_requests
		SET done_at = CASE WHEN $2 = 'done' THEN now() ELSE NULL END,
		    done_by = CASE WHEN $2 = 'done' THEN $3::uuid ELSE NULL END,
		    skip_at = CASE WHEN $2 = 'skip' THEN now() ELSE NULL END,
		    skip_by = CASE WHEN $2 = 'skip' THEN $3::uuid ELSE NULL END,
		    skip_reason = CASE WHEN $2 = 'skip' THEN $4 ELSE '' END
		WHERE id = $1::uuid
		RETURNING deal_id::text,
		          coalesce(to_char(timezone('Europe/Moscow', done_at), 'YYYY-MM-DD HH24:MI'), ''),
		          coalesce(to_char(timezone('Europe/Moscow', skip_at), 'YYYY-MM-DD HH24:MI'), ''),
		          coalesce(skip_reason, '')`,
		id, status, actorID(r), reason).Scan(&dealID, &doneAt, &skipAt, &savedReason)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	a.logAct(r.Context(), "deal", dealID, actorID(r), "night_request_done", map[string]any{
		"status": status,
		"reason": savedReason,
	})
	writeJSON(w, 200, map[string]any{
		"ok":          true,
		"status":      status,
		"done_at":     doneAt,
		"skip_at":     skipAt,
		"skip_reason": savedReason,
	})
}

func clipNightSelfNote(raw string) string {
	note := strings.TrimSpace(strings.ReplaceAll(raw, "\x00", ""))
	if len([]rune(note)) > 1000 {
		note = string([]rune(note)[:1000])
	}
	return note
}

func (a *App) putNightLogistNote(w http.ResponseWriter, r *http.Request) {
	if !canSeeNightRequests(r) {
		writeJSON(w, 403, map[string]string{"error": "недоступно"})
		return
	}
	a.ensureNightLogistRequests(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var body struct {
		Note string `json:"note"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный запрос"})
		return
	}
	note := clipNightSelfNote(body.Note)
	var saved, at string
	err := a.db.QueryRow(r.Context(), `
		UPDATE night_logist_requests
		SET self_note = $2,
		    self_note_at = CASE WHEN $2 = '' THEN NULL ELSE now() END
		WHERE id = $1::uuid
		RETURNING coalesce(self_note, ''),
		          coalesce(to_char(timezone('Europe/Moscow', self_note_at), 'YYYY-MM-DD HH24:MI'), '')`,
		id, note).Scan(&saved, &at)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "note": saved, "note_at": at})
}

