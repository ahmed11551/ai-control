package server

import (
	"net/http"
)

func (a *App) telephonyHome(w http.ResponseWriter, r *http.Request) {
	// Вкладку «Пользователи» убрали с UI — список не отдаём.
	users := []map[string]any{}
	args := []any{}
	callWhere := ""
	if !isAdmin(r) {
		uid := actorID(r)
		if uid == "" {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		args = append(args, uid)
		callWhere = ` WHERE c.user_id=$1::uuid`
	}
	calls := a.queryMaps(r.Context(), `
		SELECT c.id::text, c.direction, c.from_number, c.to_number, c.duration_sec,
		       c.disposition, c.entity_type, c.entity_id::text, c.started_at,
		       coalesce(u.full_name,'') AS user_name
		FROM calls c
		LEFT JOIN users u ON u.id=c.user_id`+callWhere+`
		ORDER BY c.started_at DESC
		LIMIT 150`, args...)
	if calls == nil {
		calls = []map[string]any{}
	}
	live := []map[string]string{}
	if a.ami != nil {
		live = a.ami.Snapshot()
	}
	var withInner int
	if isAdmin(r) {
		_ = a.db.QueryRow(r.Context(), `SELECT count(*) FROM users WHERE is_active AND nullif(phone_extension,'') IS NOT NULL`).Scan(&withInner)
	} else {
		_ = a.db.QueryRow(r.Context(), `
			SELECT count(*) FROM users
			WHERE id=$1::uuid AND is_active AND nullif(phone_extension,'') IS NOT NULL`, actorID(r)).Scan(&withInner)
	}
	pbx := a.pbx.status()
	a.writePhones(w, r, 200, map[string]any{
		"users":     users,
		"calls":     calls,
		"live":      live,
		"ami":       a.cfg.AMIReady(),
		"pbx":       pbx,
		"withInner": withInner,
	})
}
