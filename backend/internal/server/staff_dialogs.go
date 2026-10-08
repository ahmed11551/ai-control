package server

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (a *App) ensureStaffDialogsSchema(ctx context.Context) {
	a.staffDialogsOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS staff_dialogs (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
				lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
				opened_by UUID REFERENCES users(id) ON DELETE SET NULL,
				status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
				closed_by UUID REFERENCES users(id) ON DELETE SET NULL,
				closed_at TIMESTAMPTZ,
				last_body TEXT NOT NULL DEFAULT '',
				last_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				created_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`)
		_, _ = a.db.Exec(ctx, `
			CREATE UNIQUE INDEX IF NOT EXISTS staff_dialogs_one_open_per_deal
			ON staff_dialogs (deal_id) WHERE status = 'open'`)
		_, _ = a.db.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS staff_dialogs_status_last_idx
			ON staff_dialogs (status, last_at DESC)`)
		_, _ = a.db.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS staff_dialogs_deal_idx ON staff_dialogs (deal_id)`)
		_, _ = a.db.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS staff_dialogs_lead_idx ON staff_dialogs (lead_id)`)
		_, _ = a.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS staff_dialog_messages (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				dialog_id UUID NOT NULL REFERENCES staff_dialogs(id) ON DELETE CASCADE,
				author_id UUID REFERENCES users(id) ON DELETE SET NULL,
				body TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`)
		_, _ = a.db.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS staff_dialog_messages_dialog_idx
			ON staff_dialog_messages (dialog_id, created_at ASC)`)
		_, _ = a.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS staff_dialog_titles (
				dialog_id UUID NOT NULL REFERENCES staff_dialogs(id) ON DELETE CASCADE,
				user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				title TEXT NOT NULL DEFAULT '',
				updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				PRIMARY KEY (dialog_id, user_id)
			)`)
		_, _ = a.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS staff_dialog_message_files (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				message_id UUID NOT NULL REFERENCES staff_dialog_messages(id) ON DELETE CASCADE,
				sort_order INT NOT NULL DEFAULT 0,
				kind TEXT NOT NULL DEFAULT 'file',
				filename TEXT NOT NULL DEFAULT '',
				mime TEXT NOT NULL DEFAULT '',
				size_bytes BIGINT NOT NULL DEFAULT 0,
				storage_key TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`)
		_, _ = a.db.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS staff_dialog_message_files_msg
			ON staff_dialog_message_files (message_id, sort_order)`)
	})
}

func staffDialogRoleOK(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "logist", "night_logist", "manager", "sales", "rop":
		return true
	default:
		return false
	}
}

// staffDialogSeesAll — admin и логист видят все диалоги (чтение + участие).
func staffDialogSeesAll(r *http.Request) bool {
	return isAdmin(r) || isLogist(r)
}

func staffDialogAllowsMessage(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "open")
}

func (a *App) canAccessStaffDialog(ctx context.Context, r *http.Request, dialogID string) bool {
	if staffDialogSeesAll(r) {
		return true
	}
	uid := actorID(r)
	if uid == "" || dialogID == "" {
		return false
	}
	s := a.crmScope(r)
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM staff_dialogs sd
			JOIN deals d ON d.id=sd.deal_id
			WHERE sd.id=$1::uuid
			  AND (
				d.owner_id=$2::uuid
				OR d.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
			  )
			LIMIT 1`, dialogID, uid, s.DeptID).Scan(&n)
		return err == nil
	}
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM staff_dialogs sd
		JOIN deals d ON d.id=sd.deal_id
		WHERE sd.id=$1::uuid AND d.owner_id=$2::uuid
		LIMIT 1`, dialogID, uid).Scan(&n)
	return err == nil
}

func (a *App) canOpenStaffDialogForDeal(ctx context.Context, r *http.Request, dealID string) bool {
	if !a.canSeeDeal(ctx, a.crmScope(r), dealID) {
		return false
	}
	if isAdmin(r) || isLogist(r) {
		return true
	}
	if isManager(r) {
		var n int
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM deals WHERE id=$1::uuid AND owner_id=$2::uuid LIMIT 1`,
			dealID, actorID(r)).Scan(&n)
		return err == nil
	}
	if isROP(r) {
		s := a.crmScope(r)
		if s.DeptID == "" {
			return false
		}
		var n int
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM deals d
			WHERE d.id=$1::uuid
			  AND (
				d.owner_id=$2::uuid
				OR d.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
			  )
			LIMIT 1`, dealID, actorID(r), s.DeptID).Scan(&n)
		return err == nil
	}
	return false
}

func (a *App) staffDialogCandidateMap(ctx context.Context, dealID string) map[string]any {
	rows := a.queryMaps(ctx, `
		SELECT d.id::text AS deal_id, d.title,
		       coalesce(nullif(d.lead_id::text,''),
		         (SELECT l.id::text FROM leads l WHERE l.converted_deal_id=d.id LIMIT 1), '') AS lead_id,
		       d.owner_id::text AS owner_id,
		       coalesce(u.full_name,'') AS owner,
		       coalesce(dep.name,'') AS owner_department,
		       coalesce(s.name,'') AS stage,
		       coalesce(nullif(s.short_name,''), s.name,'') AS stage_short,
		       coalesce(c.full_name,'') AS contact,
		       coalesce(c.phone,'') AS contact_phone
		FROM deals d
		LEFT JOIN users u ON u.id=d.owner_id
		LEFT JOIN departments dep ON dep.id=u.department_id
		LEFT JOIN pipeline_stages s ON s.id=d.stage_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		WHERE d.id=$1::uuid`, dealID)
	if len(rows) == 0 {
		return map[string]any{}
	}
	return rows[0]
}

func (a *App) staffDialogRow(ctx context.Context, id string) map[string]any {
	return a.staffDialogRowForUser(ctx, id, "")
}

func (a *App) staffDialogRowForUser(ctx context.Context, id, userID string) map[string]any {
	rows := a.queryMaps(ctx, `
		SELECT sd.id::text, sd.deal_id::text, coalesce(sd.lead_id::text,'') AS lead_id,
		       sd.status, coalesce(sd.opened_by::text,'') AS opened_by,
		       coalesce(op.full_name,'') AS opened_by_name,
		       coalesce(sd.closed_by::text,'') AS closed_by,
		       coalesce(cl.full_name,'') AS closed_by_name,
		       sd.closed_at, sd.last_body, sd.last_at, sd.created_at,
		       coalesce(d.title,'') AS deal_title,
		       coalesce(c.full_name,'') AS contact,
		       coalesce(u.full_name,'') AS owner_name,
		       coalesce(t.title,'') AS custom_title
		FROM staff_dialogs sd
		JOIN deals d ON d.id=sd.deal_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN users u ON u.id=d.owner_id
		LEFT JOIN users op ON op.id=sd.opened_by
		LEFT JOIN users cl ON cl.id=sd.closed_by
		LEFT JOIN staff_dialog_titles t ON t.dialog_id=sd.id AND t.user_id=nullif($2,'')::uuid
		WHERE sd.id=$1::uuid`, id, userID)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func (a *App) staffDialogMessages(ctx context.Context, dialogID string) []map[string]any {
	rows := a.queryMaps(ctx, `
		SELECT m.id::text, m.body, m.created_at,
		       coalesce(m.author_id::text,'') AS author_id,
		       coalesce(u.full_name,'') AS author,
		       coalesce(r.name,'') AS author_role,
		       (SELECT count(*)::int FROM staff_dialog_message_files f WHERE f.message_id=m.id) AS media_count,
		       coalesce((
		         SELECT f.kind FROM staff_dialog_message_files f
		         WHERE f.message_id=m.id ORDER BY f.sort_order, f.created_at LIMIT 1
		       ), '') AS media_kind,
		       coalesce((
		         SELECT f.filename FROM staff_dialog_message_files f
		         WHERE f.message_id=m.id ORDER BY f.sort_order, f.created_at LIMIT 1
		       ), '') AS media_filename
		FROM staff_dialog_messages m
		LEFT JOIN users u ON u.id=m.author_id
		LEFT JOIN roles r ON r.id=u.role_id
		WHERE m.dialog_id=$1::uuid
		ORDER BY m.created_at ASC, m.id ASC
		LIMIT 500`, dialogID)
	if rows == nil {
		return []map[string]any{}
	}
	for _, row := range rows {
		n := 0
		switch v := row["media_count"].(type) {
		case int32:
			n = int(v)
		case int64:
			n = int(v)
		case int:
			n = v
		case float64:
			n = int(v)
		}
		if n > 0 {
			row["has_media"] = true
			row["media_download"] = true
			if strings.TrimSpace(fmt.Sprint(row["body"])) == "" {
				kind := strings.TrimSpace(fmt.Sprint(row["media_kind"]))
				row["body"] = mediaPlaceholder(kind)
			}
		}
	}
	return rows
}

func (a *App) listStaffDialogs(w http.ResponseWriter, r *http.Request) {
	if !staffDialogRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureStaffDialogsSchema(r.Context())
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	me := next(actorID(r))
	// Без сообщений не показываем — иначе куча пустых чатов.
	// Исключение: пустой диалог, который я только что открыл (пишу первое сообщение).
	where := []string{fmt.Sprintf(`(
		EXISTS (SELECT 1 FROM staff_dialog_messages m WHERE m.dialog_id=sd.id)
		OR (
			sd.opened_by=%s::uuid
			AND coalesce(sd.last_body,'')=''
			AND sd.created_at > now() - interval '24 hours'
		)
	)`, me)}
	if status == "open" || status == "closed" {
		where = append(where, "sd.status="+next(status))
	}
	if isLogist(r) || isAdmin(r) {
		// логист и админ видят все непустые диалоги (один чат на кандидата с менеджером)
	} else if isROP(r) {
		s := a.crmScope(r)
		uid := next(actorID(r))
		if s.DeptID != "" {
			dept := next(s.DeptID)
			where = append(where, fmt.Sprintf(`(
				d.owner_id=%s::uuid
				OR d.owner_id IN (SELECT id FROM users WHERE department_id=%s::uuid)
			)`, uid, dept))
		} else {
			where = append(where, "d.owner_id="+uid+"::uuid")
		}
	} else if isManager(r) {
		where = append(where, "d.owner_id="+next(actorID(r))+"::uuid")
	} else {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	items := a.queryMaps(r.Context(), `
		SELECT sd.id::text, sd.deal_id::text, coalesce(sd.lead_id::text,'') AS lead_id,
		       sd.status, sd.last_body,
		       coalesce(
		         (SELECT max(m.created_at) FROM staff_dialog_messages m WHERE m.dialog_id=sd.id),
		         sd.last_at
		       ) AS last_at,
		       sd.created_at,
		       coalesce(nullif(c.full_name,''), d.title, '') AS deal_title,
		       coalesce(d.title,'') AS deal_title_raw,
		       coalesce(c.full_name,'') AS contact,
		       coalesce(u.full_name,'') AS owner_name,
		       coalesce(op.full_name,'') AS opened_by_name,
		       coalesce(t.title,'') AS custom_title,
		       (SELECT count(*)::int FROM staff_dialog_messages m WHERE m.dialog_id=sd.id) AS message_count
		FROM staff_dialogs sd
		JOIN deals d ON d.id=sd.deal_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN users u ON u.id=d.owner_id
		LEFT JOIN users op ON op.id=sd.opened_by
		LEFT JOIN staff_dialog_titles t ON t.dialog_id=sd.id AND t.user_id=`+me+`::uuid`+clause+`
		ORDER BY last_at DESC, sd.created_at DESC
		LIMIT 200`, args...)
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (a *App) listStaffDialogCandidates(w http.ResponseWriter, r *http.Request) {
	if !staffDialogRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureStaffDialogsSchema(r.Context())
	a.ensureLogisticsSchema(r.Context())
	a.ensureRecruitFunnels(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	where := []string{"s.is_lost=false"}
	if isLogist(r) {
		// Логист выбирает только кандидатов из своей воронки / очереди.
		where = append(where, `(
			coalesce(d.logistics_needed,false)=true
			OR `+sqlDealOnManagerHandoffStage("d")+`
			OR EXISTS (
				SELECT 1 FROM pipelines p
				WHERE p.id=d.pipeline_id AND coalesce(p.bitrix_id,'')=`+next(escortPipelineKey)+`
			)
		)`)
	} else if isAdmin(r) {
		where = append(where, `(
			coalesce(d.logistics_needed,false)=true
			OR EXISTS (
				SELECT 1 FROM pipelines p
				WHERE p.id=d.pipeline_id AND coalesce(p.bitrix_id,'')=`+next(escortPipelineKey)+`
			)
			OR EXISTS (
				SELECT 1 FROM pipelines p
				WHERE p.id=d.pipeline_id AND coalesce(p.bitrix_id,'')=`+next(hirePipelineKey)+`
			)
		)`)
	} else if isROP(r) {
		s := a.crmScope(r)
		uid := next(actorID(r))
		if s.DeptID != "" {
			dept := next(s.DeptID)
			where = append(where, fmt.Sprintf(`(
				d.owner_id=%s::uuid
				OR d.owner_id IN (SELECT id FROM users WHERE department_id=%s::uuid)
			)`, uid, dept))
		} else {
			where = append(where, "d.owner_id="+uid+"::uuid")
		}
	} else if isManager(r) {
		where = append(where, "d.owner_id="+next(actorID(r))+"::uuid")
	} else {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	if q != "" {
		p := next("%" + q + "%")
		where = append(where, "(d.title ILIKE "+p+" OR coalesce(c.full_name,'') ILIKE "+p+")")
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	items := a.queryMaps(r.Context(), `
		SELECT d.id::text AS deal_id, d.title,
		       coalesce(nullif(d.lead_id::text,''),
		         (SELECT l.id::text FROM leads l WHERE l.converted_deal_id=d.id LIMIT 1), '') AS lead_id,
		       coalesce(u.full_name,'') AS owner,
		       coalesce(s.name,'') AS stage,
		       coalesce(nullif(s.short_name,''), s.name,'') AS stage_short,
		       coalesce(c.full_name,'') AS contact,
		       EXISTS (
		         SELECT 1 FROM staff_dialogs sd
		         WHERE sd.deal_id=d.id AND sd.status='open'
		       ) AS has_open_dialog
		FROM deals d
		JOIN pipeline_stages s ON s.id=d.stage_id
		LEFT JOIN users u ON u.id=d.owner_id
		LEFT JOIN contacts c ON c.id=d.contact_id`+clause+`
		ORDER BY d.created_at DESC
		LIMIT 80`, args...)
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (a *App) createStaffDialog(w http.ResponseWriter, r *http.Request) {
	if !staffDialogRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureStaffDialogsSchema(r.Context())
	var in struct {
		DealID string `json:"deal_id"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.DealID) == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите кандидата (deal_id)"})
		return
	}
	dealID := strings.TrimSpace(in.DealID)
	if !a.canOpenStaffDialogForDeal(r.Context(), r, dealID) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	var existing string
	_ = a.db.QueryRow(r.Context(), `
		SELECT id::text FROM staff_dialogs
		WHERE deal_id=$1::uuid AND status='open'
		LIMIT 1`, dealID).Scan(&existing)
	if existing != "" {
		row := a.staffDialogRowForUser(r.Context(), existing, actorID(r))
		writeJSON(w, 200, map[string]any{
			"ok": true, "created": false, "dialog": row,
			"candidate": a.staffDialogCandidateMap(r.Context(), dealID),
			"messages":  a.staffDialogMessages(r.Context(), existing),
		})
		return
	}
	cand := a.staffDialogCandidateMap(r.Context(), dealID)
	leadID := strings.TrimSpace(fmt.Sprint(cand["lead_id"]))
	if leadID == "<nil>" {
		leadID = ""
	}
	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO staff_dialogs (deal_id, lead_id, opened_by, status, last_body, last_at)
		VALUES ($1::uuid, nullif($2,'')::uuid, $3::uuid, 'open', '', now())
		RETURNING id::text`, dealID, leadID, actorID(r)).Scan(&id)
	if err != nil {
		// гонка: кто-то уже открыл
		_ = a.db.QueryRow(r.Context(), `
			SELECT id::text FROM staff_dialogs
			WHERE deal_id=$1::uuid AND status='open' LIMIT 1`, dealID).Scan(&id)
		if id == "" {
			writeJSON(w, 400, map[string]string{"error": "не удалось открыть диалог"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok": true, "created": false, "dialog": a.staffDialogRowForUser(r.Context(), id, actorID(r)),
			"candidate": a.staffDialogCandidateMap(r.Context(), dealID),
			"messages":  a.staffDialogMessages(r.Context(), id),
		})
		return
	}
	a.hub.Broadcast("staff_dialog.updated", map[string]any{"id": id, "deal_id": dealID, "action": "opened"})
	writeJSON(w, 201, map[string]any{
		"ok": true, "created": true, "dialog": a.staffDialogRowForUser(r.Context(), id, actorID(r)),
		"candidate": cand,
		"messages":  []map[string]any{},
	})
}

func (a *App) getStaffDialog(w http.ResponseWriter, r *http.Request) {
	if !staffDialogRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureStaffDialogsSchema(r.Context())
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canAccessStaffDialog(r.Context(), r, id)) {
		return
	}
	row := a.staffDialogRowForUser(r.Context(), id, actorID(r))
	if row == nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	dealID := fmt.Sprint(row["deal_id"])
	a.markStaffDialogNotificationsRead(r.Context(), actorID(r), id)
	writeJSON(w, 200, map[string]any{
		"dialog":    row,
		"candidate": a.staffDialogCandidateMap(r.Context(), dealID),
		"messages":  a.staffDialogMessages(r.Context(), id),
	})
}

func (a *App) renameStaffDialog(w http.ResponseWriter, r *http.Request) {
	if !isLogist(r) && !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "переименовать может только логист"})
		return
	}
	a.ensureStaffDialogsSchema(r.Context())
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canAccessStaffDialog(r.Context(), r, id)) {
		return
	}
	var in struct {
		Title string `json:"title"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "неверный JSON"})
		return
	}
	title := strings.TrimSpace(in.Title)
	// Схлопываем пробелы и ограничиваем длину.
	title = strings.Join(strings.Fields(title), " ")
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:120])
	}
	uid := actorID(r)
	if uid == "" {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if title == "" {
		_, _ = a.db.Exec(r.Context(), `
			DELETE FROM staff_dialog_titles WHERE dialog_id=$1::uuid AND user_id=$2::uuid`, id, uid)
	} else {
		_, err := a.db.Exec(r.Context(), `
			INSERT INTO staff_dialog_titles (dialog_id, user_id, title, updated_at)
			VALUES ($1::uuid, $2::uuid, $3, now())
			ON CONFLICT (dialog_id, user_id) DO UPDATE
			SET title=EXCLUDED.title, updated_at=now()`, id, uid, title)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить название"})
			return
		}
	}
	row := a.staffDialogRowForUser(r.Context(), id, uid)
	writeJSON(w, 200, map[string]any{"ok": true, "dialog": row})
}

func (a *App) postStaffDialogMessage(w http.ResponseWriter, r *http.Request) {
	if !staffDialogRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureStaffDialogsSchema(r.Context())
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canAccessStaffDialog(r.Context(), r, id)) {
		return
	}
	var status string
	_ = a.db.QueryRow(r.Context(), `SELECT status FROM staff_dialogs WHERE id=$1::uuid`, id).Scan(&status)
	if !staffDialogAllowsMessage(status) {
		writeJSON(w, 400, map[string]string{"error": "диалог завершён — писать нельзя"})
		return
	}

	type outFile struct {
		bytes []byte
		name  string
		mime  string
		kind  string
		key   string
	}
	var bodyText string
	var outFiles []outFile

	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось прочитать файл"})
			return
		}
		bodyText = strings.TrimSpace(r.FormValue("body"))
		var hdrs []*multipart.FileHeader
		if r.MultipartForm != nil {
			hdrs = append(hdrs, r.MultipartForm.File["file"]...)
			hdrs = append(hdrs, r.MultipartForm.File["files"]...)
			hdrs = append(hdrs, r.MultipartForm.File["file[]"]...)
		}
		seen := map[string]bool{}
		for _, hdr := range hdrs {
			if hdr == nil {
				continue
			}
			dupKey := hdr.Filename + ":" + strconv.FormatInt(hdr.Size, 10)
			if seen[dupKey] {
				continue
			}
			seen[dupKey] = true
			f, err := hdr.Open()
			if err != nil {
				continue
			}
			raw, rerr := io.ReadAll(io.LimitReader(f, 32<<20))
			_ = f.Close()
			if rerr != nil || len(raw) == 0 {
				writeJSON(w, 400, map[string]string{"error": "пустой файл"})
				return
			}
			name := sanitizeMediaFilename(hdr.Filename)
			mime := strings.TrimSpace(hdr.Header.Get("Content-Type"))
			if mime == "" {
				mime = "application/octet-stream"
			}
			kind := mediaKindFromContentType(mime, name)
			if kind == "" {
				kind = "file"
			}
			outFiles = append(outFiles, outFile{bytes: raw, name: name, mime: mime, kind: kind})
		}
	} else {
		var in struct {
			Body string `json:"body"`
		}
		_ = readJSON(r, &in)
		bodyText = strings.TrimSpace(in.Body)
	}

	if bodyText == "" && len(outFiles) == 0 {
		writeJSON(w, 400, map[string]string{"error": "пустой текст"})
		return
	}
	if a.gateManagerOutbound(w, r, "staff_dialog", id, bodyText, true) {
		return
	}
	if len([]rune(bodyText)) > 8000 {
		bodyText = string([]rune(bodyText)[:8000])
	}

	displayBody := bodyText
	if displayBody == "" && len(outFiles) > 0 {
		if len(outFiles) == 1 {
			displayBody = mediaPlaceholder(outFiles[0].kind)
			if outFiles[0].name != "" && outFiles[0].name != "file.bin" {
				displayBody = displayBody + ": " + outFiles[0].name
			}
		} else {
			displayBody = fmt.Sprintf("%d вложения", len(outFiles))
		}
	}

	if len(outFiles) > 0 {
		dir := filepath.Join(a.staffDialogMediaRoot(), id)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			writeJSON(w, 500, map[string]string{"error": "не удалось сохранить файл"})
			return
		}
		for i := range outFiles {
			mediaKey := fmt.Sprintf("%s/out-%d-%d-%s", id, time.Now().UnixNano(), i, outFiles[i].name)
			abs, err := a.staffMediaAbsPath(mediaKey)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": "некорректный путь файла"})
				return
			}
			if err := os.WriteFile(abs, outFiles[i].bytes, 0o640); err != nil {
				writeJSON(w, 500, map[string]string{"error": "не удалось сохранить файл"})
				return
			}
			outFiles[i].key = mediaKey
		}
	}

	var msgID string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO staff_dialog_messages (dialog_id, author_id, body)
		VALUES ($1::uuid, $2::uuid, $3)
		RETURNING id::text`, id, actorID(r), displayBody).Scan(&msgID)
	if err != nil {
		for _, f := range outFiles {
			if abs, aerr := a.staffMediaAbsPath(f.key); aerr == nil {
				_ = os.Remove(abs)
			}
		}
		writeJSON(w, 400, map[string]string{"error": "не удалось отправить"})
		return
	}

	for i, f := range outFiles {
		_, err := a.db.Exec(r.Context(), `
			INSERT INTO staff_dialog_message_files
				(message_id, sort_order, kind, filename, mime, size_bytes, storage_key)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)`,
			msgID, i, f.kind, f.name, f.mime, len(f.bytes), f.key)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "не удалось сохранить вложение"})
			return
		}
	}

	preview := displayBody
	if len([]rune(preview)) > 180 {
		preview = string([]rune(preview)[:180])
	}
	_, _ = a.db.Exec(r.Context(), `
		UPDATE staff_dialogs SET last_body=$2, last_at=now() WHERE id=$1::uuid`, id, preview)
	a.notifyStaffDialogMessage(r.Context(), id, actorID(r), preview)
	a.hub.Broadcast("staff_dialog.updated", map[string]any{"id": id, "action": "message", "message_id": msgID})
	writeJSON(w, 201, map[string]any{
		"ok": true, "id": msgID,
		"messages": a.staffDialogMessages(r.Context(), id),
		"dialog":   a.staffDialogRowForUser(r.Context(), id, actorID(r)),
	})
}

// notifyStaffDialogMessage — логисту о сообщении менеджера и менеджеру о сообщении логиста.
func (a *App) notifyStaffDialogMessage(ctx context.Context, dialogID, authorID, preview string) {
	if dialogID == "" || authorID == "" {
		return
	}
	var authorRole, authorName string
	_ = a.db.QueryRow(ctx, `
		SELECT lower(coalesce(r.name,'')), coalesce(u.full_name,'')
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid`, authorID).Scan(&authorRole, &authorName)

	var dealTitle, dealID, leadID, ownerID string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(nullif(c.full_name,''), d.title, 'Кандидат'),
		       d.id::text, coalesce(sd.lead_id::text,''), coalesce(d.owner_id::text,'')
		FROM staff_dialogs sd
		JOIN deals d ON d.id=sd.deal_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		WHERE sd.id=$1::uuid`, dialogID).Scan(&dealTitle, &dealID, &leadID, &ownerID)

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

	var recipientIDs []string
	var title string
	switch authorRole {
	case "manager", "sales", "rop", "admin":
		title = "Сообщение от менеджера"
		if authorName != "" {
			title = "Сообщение: " + authorName
		}
		rows := a.queryMaps(ctx, `
			SELECT u.id::text
			FROM users u
			JOIN roles r ON r.id=u.role_id
			WHERE lower(r.name) IN ('logist','night_logist')
			  AND coalesce(u.is_active,true)=true
			  AND u.id <> $1::uuid`, authorID)
		for _, row := range rows {
			if uid, _ := row["id"].(string); uid != "" {
				recipientIDs = append(recipientIDs, uid)
			}
		}
	case "logist", "night_logist":
		title = "Сообщение от логиста"
		if authorName != "" {
			title = "Сообщение: " + authorName
		}
		// Владелец кандидата (менеджер) — кто ведёт диалог с логистом.
		if ownerID != "" && ownerID != authorID {
			var ok bool
			_ = a.db.QueryRow(ctx, `
				SELECT true
				FROM users u
				JOIN roles r ON r.id=u.role_id
				WHERE u.id=$1::uuid
				  AND coalesce(u.is_active,true)=true
				  AND lower(r.name) IN ('manager','sales','rop','admin')`, ownerID).Scan(&ok)
			if ok {
				recipientIDs = append(recipientIDs, ownerID)
			}
		}
	default:
		return
	}

	for _, uid := range recipientIDs {
		tag, err := a.db.Exec(ctx, `
			UPDATE notifications
			SET title=$3, body=$4, payload=$5::jsonb, is_read=false, created_at=now()
			WHERE id = (
			  SELECT id FROM notifications
			  WHERE user_id=$1::uuid AND is_read=false
			    AND channel='staff_dialog'
			    AND payload->>'dialog_id'=$2
			  ORDER BY created_at DESC LIMIT 1
			)`, uid, dialogID, title, body, payload)
		if err == nil && tag.RowsAffected() > 0 {
			a.hub.Broadcast("notification", map[string]any{
				"user_id": uid, "kind": "staff_dialog", "dialog_id": dialogID,
				"title": title, "body": body,
			})
			continue
		}
		_, _ = a.db.Exec(ctx, `
			INSERT INTO notifications (user_id, channel, title, body, payload)
			VALUES ($1::uuid, 'staff_dialog', $2, $3, $4::jsonb)`,
			uid, title, body, payload)
		a.hub.Broadcast("notification", map[string]any{
			"user_id": uid, "kind": "staff_dialog", "dialog_id": dialogID,
			"title": title, "body": body,
		})
	}
}

func (a *App) markStaffDialogNotificationsRead(ctx context.Context, userID, dialogID string) {
	if userID == "" || dialogID == "" {
		return
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE notifications SET is_read=true
		WHERE user_id=$1::uuid AND is_read=false
		  AND channel='staff_dialog'
		  AND payload->>'dialog_id'=$2`, userID, dialogID)
}

func (a *App) closeStaffDialog(w http.ResponseWriter, r *http.Request) {
	if !staffDialogRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureStaffDialogsSchema(r.Context())
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canAccessStaffDialog(r.Context(), r, id)) {
		return
	}
	tag, err := a.db.Exec(r.Context(), `
		UPDATE staff_dialogs
		SET status='closed', closed_by=$2::uuid, closed_at=now(), last_at=now()
		WHERE id=$1::uuid AND status='open'`, id, actorID(r))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось завершить"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, 400, map[string]string{"error": "диалог уже завершён"})
		return
	}
	a.hub.Broadcast("staff_dialog.updated", map[string]any{"id": id, "action": "closed"})
	writeJSON(w, 200, map[string]any{"ok": true, "dialog": a.staffDialogRowForUser(r.Context(), id, actorID(r))})
}
