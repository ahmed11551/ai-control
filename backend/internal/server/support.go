package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func supportRoleOK(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "rop", "manager", "sales":
		return true
	default:
		return false
	}
}

func (a *App) ensureSupportSchema(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS support_threads (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
			last_body TEXT NOT NULL DEFAULT '',
			last_at TIMESTAMPTZ,
			admin_unread INT NOT NULL DEFAULT 0,
			user_unread INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS support_messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			thread_id UUID NOT NULL REFERENCES support_threads(id) ON DELETE CASCADE,
			author_id UUID REFERENCES users(id) ON DELETE SET NULL,
			body TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE INDEX IF NOT EXISTS support_messages_thread_idx
			ON support_messages (thread_id, created_at ASC);
		CREATE TABLE IF NOT EXISTS support_message_files (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			message_id UUID NOT NULL REFERENCES support_messages(id) ON DELETE CASCADE,
			filename TEXT NOT NULL DEFAULT '',
			mime TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'file',
			size_bytes INT NOT NULL DEFAULT 0,
			storage_key TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
}

func (a *App) supportMediaRoot() string {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	return filepath.Join(root, "data", "support-files")
}

func (a *App) supportMediaAbs(storageKey string) (string, error) {
	root, err := filepath.Abs(a.supportMediaRoot())
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Join(a.supportMediaRoot(), filepath.FromSlash(storageKey)))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("bad path")
	}
	return abs, nil
}

type supportFile struct {
	name, mime, kind, key string
	bytes                 []byte
}

func (a *App) supportMessages(ctx context.Context, threadID, actor string) []map[string]any {
	rows, err := a.db.Query(ctx, `
		SELECT m.id::text, coalesce(m.body,''), m.created_at,
		       coalesce(m.author_id::text,''), coalesce(u.full_name,'')
		FROM support_messages m
		LEFT JOIN users u ON u.id=m.author_id
		WHERE m.thread_id=$1::uuid
		ORDER BY m.created_at ASC, m.id`, threadID)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	ids := []string{}
	for rows.Next() {
		var id, body, authorID, author string
		var at time.Time
		if rows.Scan(&id, &body, &at, &authorID, &author) != nil {
			continue
		}
		ids = append(ids, id)
		out = append(out, map[string]any{
			"id": id, "body": body, "created_at": at,
			"author_id": authorID, "author": author,
			"mine":  authorID != "" && authorID == actor,
			"files": []map[string]any{},
		})
	}
	if len(ids) == 0 {
		return out
	}
	frows, err := a.db.Query(ctx, `
		SELECT f.message_id::text, f.id::text, f.filename, f.mime, f.kind
		FROM support_message_files f
		JOIN support_messages m ON m.id=f.message_id
		WHERE m.thread_id=$1::uuid
		ORDER BY f.created_at`, threadID)
	if err != nil {
		return out
	}
	defer frows.Close()
	by := map[string][]map[string]any{}
	for frows.Next() {
		var mid, id, name, mime, kind string
		if frows.Scan(&mid, &id, &name, &mime, &kind) != nil {
			continue
		}
		by[mid] = append(by[mid], map[string]any{
			"id": id, "name": name, "mime": mime, "kind": kind,
		})
	}
	for _, msg := range out {
		id, _ := msg["id"].(string)
		if files := by[id]; len(files) > 0 {
			msg["files"] = files
		}
	}
	return out
}

func (a *App) getSupportSummary(w http.ResponseWriter, r *http.Request) {
	if !supportRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	ctx := r.Context()
	a.ensureSupportSchema(ctx)
	var n int
	if isAdmin(r) {
		_ = a.db.QueryRow(ctx, `SELECT coalesce(sum(admin_unread),0) FROM support_threads`).Scan(&n)
	} else {
		_ = a.db.QueryRow(ctx, `SELECT coalesce(user_unread,0) FROM support_threads WHERE user_id=$1::uuid`, actorID(r)).Scan(&n)
	}
	writeJSON(w, 200, map[string]any{"unread": n})
}

func (a *App) getSupport(w http.ResponseWriter, r *http.Request) {
	if !supportRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	ctx := r.Context()
	a.ensureSupportSchema(ctx)
	if isAdmin(r) {
		rows, err := a.db.Query(ctx, `
			SELECT t.id::text, t.user_id::text, coalesce(u.full_name,''),
			       coalesce(t.last_body,''), t.last_at, t.admin_unread
			FROM support_threads t
			JOIN users u ON u.id=t.user_id
			WHERE t.last_at IS NOT NULL
			ORDER BY t.last_at DESC`)
		items := []map[string]any{}
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, userID, name, body string
				var unread int
				var at *time.Time
				if rows.Scan(&id, &userID, &name, &body, &at, &unread) != nil {
					continue
				}
				items = append(items, map[string]any{
					"id": id, "user_id": userID, "name": name,
					"last_body": body, "last_at": at, "unread": unread,
				})
			}
		}
		writeJSON(w, 200, map[string]any{"threads": items})
		return
	}
	var threadID string
	err := a.db.QueryRow(ctx, `SELECT id::text FROM support_threads WHERE user_id=$1::uuid`, actorID(r)).Scan(&threadID)
	if err != nil || threadID == "" {
		writeJSON(w, 200, map[string]any{"thread": nil, "messages": []any{}})
		return
	}
	_, _ = a.db.Exec(ctx, `UPDATE support_threads SET user_unread=0 WHERE id=$1::uuid`, threadID)
	writeJSON(w, 200, map[string]any{
		"thread":   map[string]any{"id": threadID},
		"messages": a.supportMessages(ctx, threadID, actorID(r)),
	})
}

func (a *App) getSupportThread(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	ctx := r.Context()
	a.ensureSupportSchema(ctx)
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var userID, name string
	err := a.db.QueryRow(ctx, `
		SELECT t.user_id::text, coalesce(u.full_name,'')
		FROM support_threads t
		JOIN users u ON u.id=t.user_id
		WHERE t.id=$1::uuid`, id).Scan(&userID, &name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "диалог не найден"})
		return
	}
	_, _ = a.db.Exec(ctx, `UPDATE support_threads SET admin_unread=0 WHERE id=$1::uuid`, id)
	writeJSON(w, 200, map[string]any{
		"thread":   map[string]any{"id": id, "user_id": userID, "name": name},
		"messages": a.supportMessages(ctx, id, actorID(r)),
	})
}

func (a *App) postSupportMessage(w http.ResponseWriter, r *http.Request) {
	if !supportRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	ctx := r.Context()
	a.ensureSupportSchema(ctx)
	bodyText := ""
	requested := ""
	var files []supportFile
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeJSON(w, 400, map[string]string{"error": "файл не прочитался"})
			return
		}
		bodyText = strings.TrimSpace(r.FormValue("body"))
		requested = strings.TrimSpace(r.FormValue("thread_id"))
		headers := r.MultipartForm.File["file"]
		if len(headers) > 8 {
			writeJSON(w, 400, map[string]string{"error": "не больше 8 файлов"})
			return
		}
		for _, hdr := range headers {
			if hdr == nil {
				continue
			}
			f, err := hdr.Open()
			if err != nil {
				continue
			}
			raw, rerr := io.ReadAll(io.LimitReader(f, 20<<20))
			_ = f.Close()
			if rerr != nil || len(raw) == 0 {
				writeJSON(w, 400, map[string]string{"error": "пустой файл"})
				return
			}
			if hdr.Size > 20<<20 {
				writeJSON(w, 400, map[string]string{"error": "файл больше 20 МБ"})
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
			files = append(files, supportFile{name: name, mime: mime, kind: kind, bytes: raw})
		}
	} else {
		var in struct {
			Body     string `json:"body"`
			ThreadID string `json:"thread_id"`
		}
		_ = readJSON(r, &in)
		bodyText = strings.TrimSpace(in.Body)
		requested = strings.TrimSpace(in.ThreadID)
	}
	if bodyText == "" && len(files) == 0 {
		writeJSON(w, 400, map[string]string{"error": "напишите текст или прикрепите файл"})
		return
	}
	if len([]rune(bodyText)) > 8000 {
		bodyText = string([]rune(bodyText)[:8000])
	}
	if a.gateManagerOutbound(w, r, "support", "", bodyText, true) {
		return
	}
	actor := actorID(r)
	threadID := ""
	if isAdmin(r) {
		if requested == "" {
			writeJSON(w, 400, map[string]string{"error": "выберите диалог"})
			return
		}
		if a.db.QueryRow(ctx, `SELECT id::text FROM support_threads WHERE id=$1::uuid`, requested).Scan(&threadID) != nil || threadID == "" {
			writeJSON(w, 404, map[string]string{"error": "диалог не найден"})
			return
		}
	} else {
		_ = a.db.QueryRow(ctx, `SELECT id::text FROM support_threads WHERE user_id=$1::uuid`, actor).Scan(&threadID)
		if threadID == "" {
			if a.db.QueryRow(ctx, `
				INSERT INTO support_threads (user_id) VALUES ($1::uuid)
				ON CONFLICT (user_id) DO UPDATE SET user_id=EXCLUDED.user_id
				RETURNING id::text`, actor).Scan(&threadID) != nil || threadID == "" {
				writeJSON(w, 500, map[string]string{"error": "не удалось открыть диалог"})
				return
			}
		}
	}

	display := bodyText
	if display == "" && len(files) > 0 {
		if len(files) == 1 {
			display = files[0].name
		} else {
			display = fmt.Sprintf("%d вложения", len(files))
		}
	}
	if len(files) > 0 {
		dir := filepath.Join(a.supportMediaRoot(), threadID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			writeJSON(w, 500, map[string]string{"error": "не удалось сохранить файл"})
			return
		}
		for i := range files {
			key := fmt.Sprintf("%s/%d-%d-%s", threadID, time.Now().UnixNano(), i, files[i].name)
			abs, err := a.supportMediaAbs(key)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": "некорректный путь файла"})
				return
			}
			if err := os.WriteFile(abs, files[i].bytes, 0o640); err != nil {
				writeJSON(w, 500, map[string]string{"error": "не удалось сохранить файл"})
				return
			}
			files[i].key = key
		}
	}
	var msgID string
	err := a.db.QueryRow(ctx, `
		INSERT INTO support_messages (thread_id, author_id, body)
		VALUES ($1::uuid, $2::uuid, $3)
		RETURNING id::text`, threadID, actor, display).Scan(&msgID)
	if err != nil {
		for _, f := range files {
			if abs, aerr := a.supportMediaAbs(f.key); aerr == nil {
				_ = os.Remove(abs)
			}
		}
		writeJSON(w, 400, map[string]string{"error": "не удалось отправить"})
		return
	}
	for _, f := range files {
		_, err := a.db.Exec(ctx, `
			INSERT INTO support_message_files (message_id, filename, mime, kind, size_bytes, storage_key)
			VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
			msgID, f.name, f.mime, f.kind, len(f.bytes), f.key)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "не удалось сохранить вложение"})
			return
		}
	}
	preview := display
	if len([]rune(preview)) > 180 {
		preview = string([]rune(preview)[:180])
	}
	if isAdmin(r) {
		_, _ = a.db.Exec(ctx, `
			UPDATE support_threads
			SET last_body=$2, last_at=now(), user_unread=user_unread+1, admin_unread=0
			WHERE id=$1::uuid`, threadID, preview)
	} else {
		_, _ = a.db.Exec(ctx, `
			UPDATE support_threads
			SET last_body=$2, last_at=now(), admin_unread=admin_unread+1, user_unread=0
			WHERE id=$1::uuid`, threadID, preview)
	}
	a.hub.Broadcast("support.updated", map[string]any{"thread_id": threadID, "user_id": actor})
	writeJSON(w, 201, map[string]any{
		"ok": true, "thread_id": threadID,
		"messages": a.supportMessages(ctx, threadID, actor),
	})
}

func (a *App) getSupportFile(w http.ResponseWriter, r *http.Request) {
	if !supportRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	ctx := r.Context()
	a.ensureSupportSchema(ctx)
	fileID := strings.TrimSpace(chi.URLParam(r, "id"))
	var threadUser, key, name, mime, kind string
	err := a.db.QueryRow(ctx, `
		SELECT t.user_id::text, f.storage_key, f.filename, f.mime, f.kind
		FROM support_message_files f
		JOIN support_messages m ON m.id=f.message_id
		JOIN support_threads t ON t.id=m.thread_id
		WHERE f.id=$1::uuid`, fileID).Scan(&threadUser, &key, &name, &mime, &kind)
	if err != nil || key == "" {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	if !isAdmin(r) && threadUser != actorID(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	abs, err := a.supportMediaAbs(key)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	disp := "attachment"
	if kind == "image" {
		disp = "inline"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disp, name))
	http.ServeFile(w, r, abs)
}
