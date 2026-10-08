package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// canManageEmployeeTelephony — admin любой; РОП — только менеджеров своего отдела.
func (a *App) canManageEmployeeTelephony(ctx context.Context, r *http.Request, targetID string) bool {
	if targetID == "" {
		return false
	}
	if isAdmin(r) {
		return true
	}
	if !isROP(r) {
		return false
	}
	var role, dept string
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(r.name,''), coalesce(u.department_id::text,'')
		FROM users u LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid AND u.is_active`, targetID).Scan(&role, &dept)
	if err != nil {
		return false
	}
	role = strings.ToLower(role)
	if role != "manager" && role != "sales" {
		return false
	}
	var own string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(department_id::text,'') FROM users WHERE id=$1`, actorID(r)).Scan(&own)
	if own == "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM departments WHERE head_user_id=$1::uuid LIMIT 1`, actorID(r)).Scan(&own)
	}
	if own == "" {
		return false
	}
	return dept == own
}

func (a *App) getEmployeeTelephony(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !a.canManageEmployeeTelephony(r.Context(), r, id) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	var name, email, role string
	err := a.db.QueryRow(r.Context(), `
		SELECT u.full_name, coalesce(u.email,''), coalesce(r.name,'')
		FROM users u LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid`, id).Scan(&name, &email, &role)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}

	exolve := map[string]any{"bound": false, "sip_login": "", "sip_resource_id": "", "numbers": []string{}}
	sip, err := a.loadExolveSip(r.Context(), id)
	if err == nil {
		nums := a.queryMaps(r.Context(), `
			SELECT number FROM exolve_dids WHERE user_id=$1::uuid AND active ORDER BY number`, id)
		list := make([]string, 0, len(nums))
		for _, n := range nums {
			list = append(list, fmt.Sprint(n["number"]))
		}
		exolve = map[string]any{
			"bound":           true,
			"sip_login":       sip.SipLogin,
			"sip_resource_id": sip.SipResourceID,
			"has_password":    sip.SipPassword != "",
			"numbers":         list,
			"numbers_text":    strings.Join(list, "\n"),
		}
	}

	voxRows := a.queryMaps(r.Context(), `
		SELECT id::text, coalesce(label,'') AS label, username, is_primary, active,
		       (password <> '') AS has_password, created_at
		FROM vox_accounts
		WHERE user_id=$1::uuid AND active
		ORDER BY is_primary DESC, created_at ASC`, id)
	if voxRows == nil {
		voxRows = []map[string]any{}
	}

	writeJSON(w, 200, map[string]any{
		"user":   map[string]any{"id": id, "full_name": name, "email": email, "role": role},
		"exolve": exolve,
		"vox":    voxRows,
	})
}

func (a *App) putEmployeeExolve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !a.canManageEmployeeTelephony(r.Context(), r, id) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	var in struct {
		SipLogin    string   `json:"sip_login"`
		SipPassword string   `json:"sip_password"`
		Numbers     []string `json:"numbers"`
		NumbersText string   `json:"numbers_text"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	login := strings.TrimSpace(in.SipLogin)
	pass := strings.TrimSpace(in.SipPassword)
	if login == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите SIP login Exolve"})
		return
	}
	if pass == "" {
		if old, err := a.loadExolveSip(r.Context(), id); err == nil {
			pass = old.SipPassword
		}
	}
	if pass == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите SIP пароль"})
		return
	}
	rid := ""
	if resource, _, _, err := a.exolveSIPContext(r.Context(), login); err == nil {
		rid = resource
	}
	_, err := a.db.Exec(r.Context(), `
		INSERT INTO exolve_sips (user_id, sip_login, sip_resource_id, sip_password, active, updated_at)
		VALUES ($1::uuid,$2,$3,$4,true,now())
		ON CONFLICT (user_id) DO UPDATE SET
			sip_login=EXCLUDED.sip_login,
			sip_resource_id=CASE WHEN EXCLUDED.sip_resource_id<>'' THEN EXCLUDED.sip_resource_id ELSE exolve_sips.sip_resource_id END,
			sip_password=EXCLUDED.sip_password,
			active=true,
			updated_at=now()`,
		id, login, rid, pass)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось сохранить SIP: " + err.Error()})
		return
	}

	nums := in.Numbers
	if len(nums) == 0 {
		for _, line := range strings.Split(in.NumbersText, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				nums = append(nums, line)
			}
		}
	}
	a.replaceExolveDIDs(r.Context(), id, nums)
	a.getEmployeeTelephony(w, r)
}

func (a *App) deleteEmployeeExolve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !a.canManageEmployeeTelephony(r.Context(), r, id) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	_, _ = a.db.Exec(r.Context(), `
		UPDATE exolve_dids SET active=false, updated_at=now() WHERE user_id=$1::uuid`, id)
	_, _ = a.db.Exec(r.Context(), `
		UPDATE exolve_sips SET active=false, updated_at=now() WHERE user_id=$1::uuid`, id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) createEmployeeVox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !a.canManageEmployeeTelephony(r.Context(), r, id) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	var in struct {
		Label     string `json:"label"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		IsPrimary bool   `json:"is_primary"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	user := strings.TrimSpace(in.Username)
	pass := strings.TrimSpace(in.Password)
	if user == "" || pass == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите username и пароль Vox"})
		return
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		label = shortVoxLabel(user)
	}
	var count int
	_ = a.db.QueryRow(r.Context(), `
		SELECT count(*) FROM vox_accounts WHERE user_id=$1::uuid AND active`, id).Scan(&count)
	primary := in.IsPrimary || count == 0
	if primary {
		_, _ = a.db.Exec(r.Context(), `
			UPDATE vox_accounts SET is_primary=false, updated_at=now()
			WHERE user_id=$1::uuid AND active`, id)
	}
	var vid string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO vox_accounts (user_id, label, username, password, is_primary, active)
		VALUES ($1::uuid,$2,$3,$4,$5,true)
		RETURNING id::text`, id, label, user, pass, primary).Scan(&vid)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось добавить Vox: " + err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{
		"id": vid, "label": label, "username": user, "is_primary": primary, "has_password": true,
	})
}

func (a *App) patchEmployeeVox(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "id")
	vid := chi.URLParam(r, "vid")
	if !a.canManageEmployeeTelephony(r.Context(), r, uid) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	var in struct {
		Label     *string `json:"label"`
		Username  *string `json:"username"`
		Password  *string `json:"password"`
		IsPrimary *bool   `json:"is_primary"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	var owner string
	err := a.db.QueryRow(r.Context(), `
		SELECT user_id::text FROM vox_accounts WHERE id=$1::uuid AND active`, vid).Scan(&owner)
	if err != nil || owner != uid {
		writeJSON(w, 404, map[string]string{"error": "аккаунт не найден"})
		return
	}
	if in.Label != nil {
		_, _ = a.db.Exec(r.Context(), `
			UPDATE vox_accounts SET label=$2, updated_at=now() WHERE id=$1::uuid`, vid, strings.TrimSpace(*in.Label))
	}
	if in.Username != nil {
		u := strings.TrimSpace(*in.Username)
		if u == "" {
			writeJSON(w, 400, map[string]string{"error": "username пустой"})
			return
		}
		_, err = a.db.Exec(r.Context(), `
			UPDATE vox_accounts SET username=$2, updated_at=now() WHERE id=$1::uuid`, vid, u)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось обновить username"})
			return
		}
	}
	if in.Password != nil {
		p := strings.TrimSpace(*in.Password)
		if p != "" {
			_, _ = a.db.Exec(r.Context(), `
				UPDATE vox_accounts SET password=$2, updated_at=now() WHERE id=$1::uuid`, vid, p)
		}
	}
	if in.IsPrimary != nil && *in.IsPrimary {
		_, _ = a.db.Exec(r.Context(), `
			UPDATE vox_accounts SET is_primary=false, updated_at=now()
			WHERE user_id=$1::uuid AND active AND id<>$2::uuid`, uid, vid)
		_, _ = a.db.Exec(r.Context(), `
			UPDATE vox_accounts SET is_primary=true, updated_at=now() WHERE id=$1::uuid`, vid)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) deleteEmployeeVox(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "id")
	vid := chi.URLParam(r, "vid")
	if !a.canManageEmployeeTelephony(r.Context(), r, uid) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	tag, err := a.db.Exec(r.Context(), `
		UPDATE vox_accounts SET active=false, is_primary=false, updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND active`, vid, uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeJSON(w, 404, map[string]string{"error": "аккаунт не найден"})
		return
	}
	// если удалили primary — назначить другой
	var n int
	_ = a.db.QueryRow(r.Context(), `
		SELECT count(*) FROM vox_accounts WHERE user_id=$1::uuid AND active AND is_primary`, uid).Scan(&n)
	if n == 0 {
		_, _ = a.db.Exec(r.Context(), `
			UPDATE vox_accounts SET is_primary=true, updated_at=now()
			WHERE id=(
				SELECT id FROM vox_accounts WHERE user_id=$1::uuid AND active
				ORDER BY created_at ASC LIMIT 1
			)`, uid)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) voxSession(w http.ResponseWriter, r *http.Request) {
	uid := actorID(r)
	var user, pass, label string
	var primary bool
	err := a.db.QueryRow(r.Context(), `
		SELECT username, password, coalesce(label,''), is_primary
		FROM vox_accounts
		WHERE user_id=$1::uuid AND active
		ORDER BY is_primary DESC, created_at ASC
		LIMIT 1`, uid).Scan(&user, &pass, &label, &primary)
	if err == nil && user != "" && pass != "" {
		writeJSON(w, 200, map[string]any{
			"mode":       "bound",
			"username":   user,
			"password":   pass,
			"label":      label,
			"is_primary": primary,
		})
		return
	}
	// Ночной логист звонит только со своего Vox, не с общего из .env.
	if roleName(r) == "night_logist" {
		writeJSON(w, 404, map[string]string{"error": "нет отдельного Vox — привяжите аккаунт в карточке сотрудника"})
		return
	}
	// fallback на .env
	envUser := strings.TrimSpace(a.cfg.VoxUsername)
	envPass := strings.TrimSpace(a.cfg.VoxPassword)
	if envUser == "" || envPass == "" {
		writeJSON(w, 404, map[string]string{"error": "нет Vox-аккаунта — привяжите в Сотрудниках или задайте VITE_VOX_*"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"mode":     "fallback",
		"username": envUser,
		"password": envPass,
		"label":    "env",
	})
}

func shortVoxLabel(username string) string {
	u := strings.TrimSpace(username)
	if i := strings.Index(u, "@"); i > 0 {
		return u[:i]
	}
	if len(u) > 24 {
		return u[:24]
	}
	return u
}

func (a *App) deactivateEmployeeTelephony(ctx context.Context, userID string) {
	_, _ = a.db.Exec(ctx, `UPDATE exolve_sips SET active=false, updated_at=now() WHERE user_id=$1::uuid`, userID)
	_, _ = a.db.Exec(ctx, `UPDATE exolve_dids SET active=false, updated_at=now() WHERE user_id=$1::uuid`, userID)
	_, _ = a.db.Exec(ctx, `UPDATE vox_accounts SET active=false, is_primary=false, updated_at=now() WHERE user_id=$1::uuid`, userID)
}
