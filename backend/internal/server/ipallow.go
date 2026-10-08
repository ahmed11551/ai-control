package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
)

const officeIPDeniedMsg = "Доступ только из офиса"

func (a *App) ensureOfficeAccess(ctx context.Context) {
	officeAccessOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS office_access (
				id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
				enabled BOOLEAN NOT NULL DEFAULT false
			)`)
		_, _ = a.db.Exec(ctx, `INSERT INTO office_access (id, enabled) VALUES (1, false) ON CONFLICT (id) DO NOTHING`)
		_, _ = a.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS office_access_ips (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				cidr TEXT NOT NULL,
				note TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`)
		_, _ = a.db.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS office_access_ips_cidr_idx ON office_access_ips (cidr)`)
	})
}

var officeAccessOnce sync.Once

// clientIP — адрес клиента. За nginx берём X-Real-IP ($remote_addr), не X-Forwarded-For.
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return stripIP(ip)
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return stripIP(r.RemoteAddr)
	}
	return stripIP(host)
}

func stripIP(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]")
	if i := strings.Index(s, "%"); i >= 0 {
		s = s[:i]
	}
	return s
}

func normalizeOfficeCIDR(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	if strings.Contains(s, "/") {
		ip, n, err := net.ParseCIDR(s)
		if err != nil || ip == nil {
			return "", false
		}
		return n.String(), true
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return "", false
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String(), true
	}
	return ip.String(), true
}

func ipInOfficeList(ip string, cidrs []string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.Contains(c, "/") {
			_, n, err := net.ParseCIDR(c)
			if err == nil && n.Contains(parsed) {
				return true
			}
			continue
		}
		one := net.ParseIP(c)
		if one != nil && one.Equal(parsed) {
			return true
		}
	}
	return false
}

// officeIPExempt — админ, логист, ночной логист и заказчик заходят с любого адреса.
func officeIPExempt(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "logist", "night_logist", "customer":
		return true
	default:
		return false
	}
}

// officeIPDenied — true, только если ограничение включено, список не пуст и роль не исключена.
// Ошибка схемы или выключенный режим не блокируют вход.
func (a *App) officeIPDenied(ctx context.Context, r *http.Request, role string) bool {
	if officeIPExempt(role) {
		return false
	}
	a.ensureOfficeAccess(ctx)
	var enabled bool
	if err := a.db.QueryRow(ctx, `SELECT enabled FROM office_access WHERE id=1`).Scan(&enabled); err != nil || !enabled {
		return false
	}
	rows, err := a.db.Query(ctx, `SELECT cidr FROM office_access_ips`)
	if err != nil {
		return false
	}
	defer rows.Close()
	var cidrs []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil && strings.TrimSpace(c) != "" {
			cidrs = append(cidrs, c)
		}
	}
	if len(cidrs) == 0 {
		return false
	}
	return !ipInOfficeList(clientIP(r), cidrs)
}

func (a *App) getOfficeAccess(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор"})
		return
	}
	a.ensureOfficeAccess(r.Context())
	var enabled bool
	_ = a.db.QueryRow(r.Context(), `SELECT enabled FROM office_access WHERE id=1`).Scan(&enabled)
	items := a.queryMaps(r.Context(), `
		SELECT id::text, cidr, note, created_at
		FROM office_access_ips ORDER BY created_at ASC`)
	writeJSON(w, 200, map[string]any{
		"enabled": enabled,
		"items":   items,
		"your_ip": clientIP(r),
	})
}

func (a *App) putOfficeAccess(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор"})
		return
	}
	a.ensureOfficeAccess(r.Context())
	var in struct {
		Enabled bool `json:"enabled"`
		Items   []struct {
			CIDR string `json:"cidr"`
			Note string `json:"note"`
		} `json:"items"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	type row struct{ cidr, note string }
	seen := map[string]bool{}
	var rows []row
	for _, it := range in.Items {
		cidr, ok := normalizeOfficeCIDR(it.CIDR)
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "некорректный IP: " + strings.TrimSpace(it.CIDR)})
			return
		}
		if seen[cidr] {
			continue
		}
		seen[cidr] = true
		note := strings.TrimSpace(it.Note)
		if len(note) > 120 {
			note = note[:120]
		}
		rows = append(rows, row{cidr, note})
	}
	if in.Enabled && len(rows) == 0 {
		writeJSON(w, 400, map[string]string{"error": "добавьте хотя бы один IP перед включением"})
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE office_access SET enabled=$1 WHERE id=1`, in.Enabled); err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM office_access_ips`); err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	for _, row := range rows {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO office_access_ips (cidr, note) VALUES ($1,$2)`, row.cidr, row.note); err != nil {
			writeJSON(w, 500, map[string]string{"error": "db"})
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	a.getOfficeAccess(w, r)
}
