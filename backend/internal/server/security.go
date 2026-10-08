package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const extensionQuarantineDays = 14

func randomSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}

func (a *App) extensionBlocked(ctx context.Context, ext string) bool {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return false
	}
	var until time.Time
	err := a.db.QueryRow(ctx, `SELECT until_at FROM extension_quarantine WHERE extension=$1 AND until_at > now()`, ext).Scan(&until)
	return err == nil
}

func (a *App) offboardEmployee(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор может увольнять"})
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" || id == actorID(r) {
		writeJSON(w, 400, map[string]string{"error": "нельзя уволить свою учётку"})
		return
	}
	var name, role, ext, work, dept string
	var wasAdmin bool
	err := a.db.QueryRow(r.Context(), `
		SELECT u.full_name, coalesce(r.name,''), coalesce(u.phone_extension,''), coalesce(u.work_phone,''),
		       coalesce(u.department_id::text,'')
		FROM users u LEFT JOIN roles r ON r.id=u.role_id WHERE u.id=$1`, id).
		Scan(&name, &role, &ext, &work, &dept)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}
	wasAdmin = role == "admin"
	if wasAdmin {
		var n int
		_ = a.db.QueryRow(r.Context(), `
			SELECT count(*) FROM users u JOIN roles r ON r.id=u.role_id
			WHERE r.name='admin' AND u.is_active`).Scan(&n)
		if n <= 1 {
			writeJSON(w, 400, map[string]string{"error": "нельзя уволить последнего администратора"})
			return
		}
	}

	hash, err := bcryptHash(randomSecret(24))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось сбросить пароль"})
		return
	}
	_, err = a.db.Exec(r.Context(), `
		UPDATE users SET
			is_active=false,
			can_call=false,
			phone_extension=NULL,
			password_hash=$2,
			token_version=coalesce(token_version,1)+1
		WHERE id=$1`, id, hash)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось отключить учётку"})
		return
	}
	if ext != "" {
		_, _ = a.db.Exec(r.Context(), `
			INSERT INTO extension_quarantine (extension, user_id, released_at, until_at)
			VALUES ($1,$2,now(), now() + interval '14 days')
			ON CONFLICT (extension) DO UPDATE SET
				user_id=EXCLUDED.user_id, released_at=now(), until_at=EXCLUDED.until_at`,
			ext, id)
	}
	_, _ = a.db.Exec(r.Context(), `UPDATE departments SET head_user_id=NULL WHERE head_user_id=$1`, id)
	a.deactivateEmployeeTelephony(r.Context(), id)
	personal, _ := a.db.Exec(r.Context(), `
		UPDATE inbox_lines SET is_active=false, owner_user_id=NULL
		WHERE owner_user_id=$1 AND kind='personal'`, id)
	_, _ = a.db.Exec(r.Context(), `
		UPDATE inbox_lines SET owner_user_id=NULL
		WHERE owner_user_id=$1 AND kind <> 'personal'`, id)
	paused := personal.RowsAffected()

	payload := map[string]any{
		"extension": ext, "work_phone": work, "department_id": dept,
		"paused_personal_lines": paused, "was_admin": wasAdmin,
	}
	a.logAct(r.Context(), "user", id, actorID(r), "offboard", payload)

	checklist := []string{
		"Забрать SIM и рабочий телефон",
		"Сменить пароль или токен Wappi и отвязать личный профиль",
		"Проверить канал Wappi, если линия была на этом человеке",
		"Снять внутренний номер в Телфин или UIS",
		"Выйти из Avito, если вход был личный",
	}
	if wasAdmin {
		checklist = append(checklist, "Сменить INBOX_WEBHOOK_SECRET и CALL_WEBHOOK_SECRET в локальном .env и перезапустить API. Значения сюда не вставляйте.")
	}
	writeJSON(w, 200, map[string]any{
		"ok":              true,
		"name":            name,
		"extension":       ext,
		"work_phone":      work,
		"quarantine":      ext != "",
		"quarantine_days": extensionQuarantineDays,
		"paused_lines":    paused,
		"rotate_hooks":    wasAdmin,
		"checklist":       checklist,
	})
}

func (a *App) numberRegistry(ctx context.Context, admin bool) map[string]any {
	people := a.queryMaps(ctx, `
		SELECT u.id::text, u.full_name, coalesce(d.name,'') AS department, u.is_active,
		       coalesce(u.phone_extension,'') AS extension,
		       coalesce(u.work_phone,'') AS work_phone,
		       (SELECT count(*) FROM inbox_lines l WHERE l.owner_user_id=u.id) AS lines
		FROM users u
		LEFT JOIN departments d ON d.id=u.department_id
		WHERE nullif(u.phone_extension,'') IS NOT NULL OR nullif(u.work_phone,'') IS NOT NULL
		   OR EXISTS (SELECT 1 FROM inbox_lines l WHERE l.owner_user_id=u.id)
		ORDER BY u.full_name`)
	if people == nil {
		people = []map[string]any{}
	}
	lineSQL := `
		SELECT l.id::text, l.provider, l.channel, l.name, l.kind, l.is_active,
		       l.owner_user_id::text AS owner_id, coalesce(u.full_name,'') AS owner,
		       l.operator_label`
	if admin {
		lineSQL += `, l.external_ref`
	} else {
		lineSQL += `, '' AS external_ref`
	}
	lineSQL += `
		FROM inbox_lines l
		LEFT JOIN users u ON u.id=l.owner_user_id
		ORDER BY l.kind, l.channel, l.name`
	lines := a.queryMaps(ctx, lineSQL)
	if lines == nil {
		lines = []map[string]any{}
	}
	qtn := a.queryMaps(ctx, `
		SELECT q.extension, q.until_at, q.released_at, coalesce(u.full_name,'') AS last_owner
		FROM extension_quarantine q
		LEFT JOIN users u ON u.id=q.user_id
		WHERE q.until_at > now()
		ORDER BY q.until_at`)
	if qtn == nil {
		qtn = []map[string]any{}
	}
	log := []map[string]any{}
	if admin {
		log = a.queryMaps(ctx, `
			SELECT a.created_at, a.payload, coalesce(u.full_name,'') AS actor,
			       coalesce(t.full_name,'') AS target
			FROM activity_log a
			LEFT JOIN users u ON u.id=a.actor_id
			LEFT JOIN users t ON t.id=a.entity_id
			WHERE a.entity_type='user' AND a.action='offboard'
			ORDER BY a.created_at DESC LIMIT 20`)
		if log == nil {
			log = []map[string]any{}
		}
	}
	return map[string]any{
		"people": people, "lines": lines, "quarantine": qtn, "log": log,
		"quarantine_days": extensionQuarantineDays,
	}
}
