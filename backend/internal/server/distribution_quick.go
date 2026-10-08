package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"unicode"
)

type quickCred struct {
	FullName  string `json:"full_name"`
	Login     string `json:"login"`
	Password  string `json:"password"`
	Role      string `json:"role"`
	RoleLabel string `json:"role_label"`
	ID        string `json:"id"`
}

func genShortPassword(n int) string {
	if n < 6 {
		n = 6
	}
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			b.WriteByte(alphabet[i%len(alphabet)])
			continue
		}
		b.WriteByte(alphabet[idx.Int64()])
	}
	return b.String()
}

// shortLoginBase — короткий логин из ФИО: ivanov.i (не длинные транслиты всего ФИО).
func shortLoginBase(fullName string) string {
	base := suggestLoginFromName(fullName)
	if base == "" {
		base = "user"
	}
	runes := []rune(base)
	if len(runes) > 14 {
		base = string(runes[:14])
		base = strings.Trim(base, "._-")
	}
	if base == "" {
		return "user"
	}
	return base
}

func (a *App) uniqueLoginReserved(ctx context.Context, base string, reserved map[string]bool) string {
	base = sanitizeLogin(base)
	if base == "" {
		base = "user"
	}
	runes := []rune(base)
	if len(runes) > 14 {
		base = string(runes[:14])
		base = strings.Trim(base, "._-")
		if base == "" {
			base = "user"
		}
	}
	cand := base
	for i := 2; i < 1000; i++ {
		low := strings.ToLower(cand)
		if !reserved[low] && !a.loginTaken(ctx, cand, "") {
			reserved[low] = true
			return cand
		}
		cand = fmt.Sprintf("%s%d", base, i)
	}
	cand = fmt.Sprintf("%s%d", base, len(reserved)+1)
	reserved[strings.ToLower(cand)] = true
	return cand
}

func cleanPersonName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func looksLikeName(s string) bool {
	s = cleanPersonName(s)
	if s == "" || len([]rune(s)) < 2 {
		return false
	}
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 2
}

// postQuickDepartment — отдел + РОП + менеджеры только по ФИО, логины/пароли генерим сами.
func (a *App) postQuickDepartment(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	var in struct {
		Name       string   `json:"name"`
		ROPName    string   `json:"rop_name"`
		Managers   []string `json:"managers"`
		Share      float64  `json:"share"`
		DailyLimit int      `json:"daily_limit"`
		Pool       string   `json:"pool"`
		MskTower   bool     `json:"msk_tower"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	deptName := cleanPersonName(in.Name)
	ropName := cleanPersonName(in.ROPName)
	if deptName == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите название отдела"})
		return
	}
	if !looksLikeName(ropName) {
		writeJSON(w, 400, map[string]string{"error": "укажите ФИО РОПа"})
		return
	}
	var managers []string
	seenNames := map[string]bool{}
	for _, m := range in.Managers {
		n := cleanPersonName(m)
		if !looksLikeName(n) {
			continue
		}
		key := strings.ToLower(n)
		if seenNames[key] {
			continue
		}
		seenNames[key] = true
		managers = append(managers, n)
	}

	share := in.Share
	if share < 0 {
		share = 0
	}
	if share > 100 {
		share = 100
	}
	share = float64(int(share*10+0.5)) / 10
	lim := in.DailyLimit
	if lim <= 0 {
		lim = 200
	}
	pool := normPool(in.Pool)

	if share > 0 {
		var used float64
		_ = a.db.QueryRow(r.Context(), `
			SELECT coalesce(sum(share),0)::float8 FROM distribution_quotas WHERE pool=$1`, pool).Scan(&used)
		remain := float64(int((100-used)*10+0.5)) / 10
		if remain < 0 {
			remain = 0
		}
		if share > remain+0.05 {
			writeJSON(w, 400, map[string]string{
				"error": fmt.Sprintf("в пуле осталось только %.1f%% — уменьшите долю", remain),
			})
			return
		}
	}

	ctx := r.Context()
	reserved := map[string]bool{}
	ropLogin := a.uniqueLoginReserved(ctx, shortLoginBase(ropName), reserved)
	ropPass := genShortPassword(8)

	type pendingUser struct {
		fullName, role, login, pass string
	}
	pending := []pendingUser{{fullName: ropName, role: "rop", login: ropLogin, pass: ropPass}}
	for _, m := range managers {
		pending = append(pending, pendingUser{
			fullName: m,
			role:     "manager",
			login:    a.uniqueLoginReserved(ctx, shortLoginBase(m), reserved),
			pass:     genShortPassword(8),
		})
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось начать создание"})
		return
	}
	defer tx.Rollback(ctx)

	var deptID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO departments (name) VALUES ($1) RETURNING id::text`, deptName).Scan(&deptID); err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось создать отдел"})
		return
	}

	creds := make([]quickCred, 0, len(pending))
	var ropID string
	for _, p := range pending {
		hash, err := bcryptHash(p.pass)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "ошибка пароля"})
			return
		}
		var roleID string
		if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE name=$1`, p.role).Scan(&roleID); err != nil || roleID == "" {
			writeJSON(w, 400, map[string]string{"error": "роль не найдена: " + p.role})
			return
		}
		var uid string
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, login, password_hash, full_name, role_id, can_call, department_id, is_active, msk_tower)
			VALUES (NULL, $1, $2, $3, $4::uuid, true, $5::uuid, true, $6)
			RETURNING id::text`,
			p.login, hash, p.fullName, roleID, deptID, p.role == "rop" && in.MskTower).Scan(&uid); err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось создать пользователя «" + p.fullName + "»: " + err.Error()})
			return
		}
		if p.role == "rop" {
			ropID = uid
		}
		creds = append(creds, quickCred{
			ID: uid, FullName: p.fullName, Login: p.login, Password: p.pass,
			Role: p.role, RoleLabel: fmtRoleLabel(p.role),
		})
	}

	if ropID != "" {
		if _, err := tx.Exec(ctx, `UPDATE departments SET head_user_id=$2::uuid WHERE id=$1::uuid`, deptID, ropID); err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось назначить РОПа"})
			return
		}
	}

	for _, p := range []string{"depts", "skorozvon"} {
		s, l := 0.0, 0
		if p == pool {
			s, l = share, lim
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO distribution_quotas (department_id, pool, share, daily_limit)
			VALUES ($1::uuid,$2,$3,$4)
			ON CONFLICT (department_id, pool) DO UPDATE SET
			  share=EXCLUDED.share, daily_limit=EXCLUDED.daily_limit`,
			deptID, p, s, l); err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить квоты"})
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось сохранить"})
		return
	}

	var txt strings.Builder
	txt.WriteString("Отдел: " + deptName + "\n")
	txt.WriteString("--------------------------------\n")
	for _, c := range creds {
		txt.WriteString(fmt.Sprintf("%s | %s | %s | %s\n", c.RoleLabel, c.FullName, c.Login, c.Password))
	}

	writeJSON(w, 201, map[string]any{
		"ok":              true,
		"department_id":   deptID,
		"department_name": deptName,
		"credentials":     creds,
		"txt":             txt.String(),
		"filename":        "otdel-" + sanitizeLogin(deptName) + ".txt",
	})
}
