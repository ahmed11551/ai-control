package server

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

const deptAccessDeniedMsg = "Отдел временно отключён администратором"

var deptAccessColumnOnce sync.Once

func (a *App) ensureDeptAccessColumn(ctx context.Context) {
	deptAccessColumnOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE departments
			ADD COLUMN IF NOT EXISTS access_enabled BOOLEAN NOT NULL DEFAULT true`)
	})
}

// deptLoginBlocked — чистая логика: блокировать ли вход/API из‑за отдела.
// hasDept=false (нет отдела / не нашли) → не блокируем.
// Админов и пустой userID не трогаем. Другие отделы сюда не попадают.
func deptLoginBlocked(role, userID string, hasDept, accessEnabled bool) bool {
	if strings.EqualFold(strings.TrimSpace(role), "admin") || strings.TrimSpace(userID) == "" {
		return false
	}
	if !hasDept {
		return false
	}
	return !accessEnabled
}

// userDeptAccessDenied — true, если сотрудник в отделе с access_enabled=false.
func (a *App) userDeptAccessDenied(ctx context.Context, userID, role string) bool {
	if strings.EqualFold(strings.TrimSpace(role), "admin") || strings.TrimSpace(userID) == "" {
		return false
	}
	a.ensureDeptAccessColumn(ctx)
	var enabled bool
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(d.access_enabled, true)
		FROM users u
		JOIN departments d ON d.id=u.department_id
		WHERE u.id=$1::uuid`, userID).Scan(&enabled)
	if err != nil {
		return deptLoginBlocked(role, userID, false, true)
	}
	return deptLoginBlocked(role, userID, true, enabled)
}

func (a *App) deptAccessEnabled(ctx context.Context, deptID string) bool {
	if strings.TrimSpace(deptID) == "" {
		return true
	}
	a.ensureDeptAccessColumn(ctx)
	var enabled bool
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(access_enabled, true) FROM departments WHERE id=$1::uuid`, deptID).Scan(&enabled)
	if err != nil {
		return true
	}
	return enabled
}

// postDepartmentAccess — вкл/выкл вход в CRM для всех сотрудников отдела.
// Данные и аккаунты не удаляются.
func (a *App) postDepartmentAccess(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "id"})
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &in); err != nil || in.Enabled == nil {
		writeJSON(w, 400, map[string]string{"error": "укажите enabled: true|false"})
		return
	}
	a.ensureDeptAccessColumn(r.Context())
	tag, err := a.db.Exec(r.Context(), `
		UPDATE departments SET access_enabled=$2 WHERE id=$1::uuid`, id, *in.Enabled)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось обновить доступ"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, 404, map[string]string{"error": "отдел не найден"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":             true,
		"id":             id,
		"access_enabled": *in.Enabled,
	})
}
