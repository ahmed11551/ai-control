package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (a *App) passportTextRequestAllowed(ctx context.Context, managerID string) bool {
	a.ensureSecuritySchema(ctx)
	var allowed bool
	_ = a.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM passport_text_approvals
			WHERE manager_id=$1::uuid AND revoked_at IS NULL AND expires_at > now()
		)`, strings.TrimSpace(managerID)).Scan(&allowed)
	return allowed
}

func (a *App) grantPassportTextApproval(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "разрешение выдаёт только администратор"})
		return
	}
	managerID := strings.TrimSpace(chi.URLParam(r, "id"))
	var role string
	if err := a.db.QueryRow(r.Context(), `SELECT coalesce(r.name,'') FROM users u JOIN roles r ON r.id=u.role_id WHERE u.id=$1::uuid`, managerID).Scan(&role); err != nil {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}
	if role != "manager" && role != "sales" {
		writeJSON(w, 400, map[string]string{"error": "разрешение доступно только менеджеру"})
		return
	}
	var in struct {
		Reason string `json:"reason"`
		Hours  int    `json:"hours"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный JSON"})
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите основание согласования"})
		return
	}
	if in.Hours <= 0 {
		in.Hours = 24
	}
	if in.Hours > 24 {
		in.Hours = 24
	}
	var id string
	expires := time.Now().Add(time.Duration(in.Hours) * time.Hour)
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO passport_text_approvals (manager_id, approved_by, reason, expires_at)
		VALUES ($1::uuid,$2::uuid,$3,$4) RETURNING id::text`, managerID, actorID(r), in.Reason, expires).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось сохранить разрешение"})
		return
	}
	a.logAct(r.Context(), "user", managerID, actorID(r), "passport_text_approval_granted", map[string]any{"approval_id": id, "expires_at": expires, "reason": in.Reason})
	writeJSON(w, 201, map[string]any{"ok": true, "approval_id": id, "expires_at": expires})
}

func (a *App) revokePassportTextApproval(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "разрешение отзывает только администратор"})
		return
	}
	id := chi.URLParam(r, "id")
	_, err := a.db.Exec(r.Context(), `UPDATE passport_text_approvals SET revoked_at=now() WHERE id=$1::uuid AND revoked_at IS NULL`, id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "разрешение не найдено"})
		return
	}
	a.logAct(r.Context(), "user", id, actorID(r), "passport_text_approval_revoked", nil)
	writeJSON(w, 200, map[string]any{"ok": true})
}
