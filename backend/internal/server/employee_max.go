package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (a *App) canAccessEmployee(ctx context.Context, r *http.Request, targetID string) bool {
	if targetID == "" {
		return false
	}
	if isAdmin(r) || actorID(r) == targetID {
		return true
	}
	if isROP(r) {
		return a.canManageEmployeeTelephony(ctx, r, targetID)
	}
	// Менеджер/логист — только свою карточку
	return actorID(r) == targetID
}

func (a *App) getEmployee(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if !a.canAccessEmployee(ctx, r, id) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}

	var name, email, login, role, ext, work, dept, deptID string
	var active, canCall bool
	err := a.db.QueryRow(ctx, `
		SELECT u.full_name, coalesce(u.email,''), coalesce(u.login,''), coalesce(r.name,''),
		       coalesce(u.phone_extension,''), coalesce(u.work_phone,''),
		       coalesce(d.name,''), coalesce(u.department_id::text,''),
		       u.is_active, u.can_call
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN departments d ON d.id=u.department_id
		WHERE u.id=$1::uuid`, id).Scan(
		&name, &email, &login, &role, &ext, &work, &dept, &deptID, &active, &canCall)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}

	a.ensureMaxSendDown(ctx)
	bound := a.queryMaps(ctx, `
		SELECT l.id::text, coalesce(l.external_ref,'') AS profile_id,
		       coalesce(l.name,'') AS name, coalesce(l.operator_label,'') AS phone,
		       l.is_active, coalesce(l.max_send_down,false) AS send_down,
		       coalesce(l.max_cold_limit,false) AS cold_limit,
		       (SELECT count(*)::int FROM inbox_threads t WHERE t.line_id=l.id) AS threads
		FROM inbox_lines l
		WHERE l.owner_user_id=$1::uuid AND l.channel='max' AND l.provider='wappi'
		ORDER BY l.name`, id)
	if bound == nil {
		bound = []map[string]any{}
	}

	ownedProfiles := map[string]bool{}
	for _, row := range bound {
		if p := strings.TrimSpace(fmt.Sprint(row["profile_id"])); p != "" {
			ownedProfiles[p] = true
		}
	}

	canBind := canBindMax(r)
	available := []empMaxAvail{}
	wappiErr := ""
	if canBind {
		available, wappiErr = a.employeeMaxAvailable(ctx, ownedProfiles)
	} else {
		// Менеджеру не отдаём каталог Wappi и полные номера привязанных аккаунтов.
		for _, row := range bound {
			if phone := strings.TrimSpace(fmt.Sprint(row["phone"])); phone != "" {
				row["phone"] = maskClientPhone(phone)
			}
			if name := strings.TrimSpace(fmt.Sprint(row["name"])); name != "" {
				row["name"] = redactPhonesInText(name)
			}
			row["profile_id"] = ""
		}
	}

	out := map[string]any{
		"user": map[string]any{
			"id":              id,
			"full_name":       name,
			"email":           email,
			"login":           login,
			"role":            role,
			"extension":       ext,
			"phone_extension": ext,
			"work_phone":      work,
			"department":      dept,
			"department_id":   deptID,
			"is_active":       active,
			"can_call":        canCall,
		},
		"max_accounts": bound,
		"available":    available,
		"can_bind_max": canBind,
		"wappi":        canBind && a.cfg.WappiReady(),
	}
	if wappiErr != "" {
		out["wappi_error"] = wappiErr
	}
	if isAdmin(r) {
		out["can_edit_department"] = true
		deps := a.queryMaps(ctx, `
			SELECT d.id::text, d.name,
			       (SELECT count(*) FROM users u WHERE u.department_id=d.id AND u.is_active) AS people
			FROM departments d
			ORDER BY d.name`)
		if deps == nil {
			deps = []map[string]any{}
		}
		out["departments"] = deps
	}
	writeJSON(w, 200, out)
}

// patchEmployee — админ быстро меняет отдел (и при необходимости другие поля профиля).
func (a *App) patchEmployee(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор"})
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "нет id"})
		return
	}
	var in struct {
		DepartmentID *string `json:"department_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	if in.DepartmentID == nil {
		writeJSON(w, 400, map[string]string{"error": "укажите department_id"})
		return
	}
	deptID := strings.TrimSpace(*in.DepartmentID)
	var exists bool
	err := a.db.QueryRow(r.Context(), `SELECT true FROM users WHERE id=$1::uuid`, id).Scan(&exists)
	if err != nil || !exists {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}
	if deptID == "" {
		_, err = a.db.Exec(r.Context(), `UPDATE users SET department_id=NULL WHERE id=$1::uuid`, id)
	} else {
		var ok bool
		if err := a.db.QueryRow(r.Context(), `SELECT true FROM departments WHERE id=$1::uuid`, deptID).Scan(&ok); err != nil || !ok {
			writeJSON(w, 400, map[string]string{"error": "отдел не найден"})
			return
		}
		_, err = a.db.Exec(r.Context(), `UPDATE users SET department_id=$1::uuid WHERE id=$2::uuid`, deptID, id)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	var name, email, login, role, dept, deptOut string
	_ = a.db.QueryRow(r.Context(), `
		SELECT u.full_name, coalesce(u.email,''), coalesce(u.login,''), coalesce(r.name,''),
		       coalesce(d.name,''), coalesce(u.department_id::text,'')
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN departments d ON d.id=u.department_id
		WHERE u.id=$1::uuid`, id).Scan(&name, &email, &login, &role, &dept, &deptOut)
	a.logAct(r.Context(), "user", id, actorID(r), "department", map[string]any{
		"department_id": deptOut, "department": dept,
	})
	writeJSON(w, 200, map[string]any{
		"id": id, "full_name": name, "email": email, "login": login, "role": role,
		"department": dept, "department_id": deptOut,
	})
}

type empMaxAvail struct {
	ProfileID  string `json:"profile_id"`
	LineID     string `json:"line_id,omitempty"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	OwnerID    string `json:"owner_id,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Authorized bool   `json:"authorized"`
	AppStatus  string `json:"app_status"`
	Working    bool   `json:"working"`
}

// chooseMaxProfile — на один номер один профиль: сначала уже включённая линия, затем онлайн, затем стабильный id.
func chooseMaxProfile(prev, next string, prevOnLine, nextOnLine, prevWorking, nextWorking bool) string {
	if prev == "" {
		return next
	}
	if next == "" {
		return prev
	}
	if nextOnLine != prevOnLine {
		if nextOnLine {
			return next
		}
		return prev
	}
	if nextWorking != prevWorking {
		if nextWorking {
			return next
		}
		return prev
	}
	if next < prev {
		return next
	}
	return prev
}

// employeeMaxAvailable — список из Wappi MAX (profile/all/get), обогащённый линиями из БД.
func (a *App) employeeMaxAvailable(ctx context.Context, owned map[string]bool) ([]empMaxAvail, string) {
	type lineMeta struct {
		LineID  string
		OwnerID string
		Owner   string
		Phone   string
	}
	byRef := map[string]lineMeta{}
	byPhone := map[string]string{}
	rows, err := a.db.Query(ctx, `
		SELECT l.id::text, coalesce(l.external_ref,''),
		       coalesce(l.owner_user_id::text,''), coalesce(u.full_name,''),
		       coalesce(l.operator_label,''), coalesce(l.name,'')
		FROM inbox_lines l
		LEFT JOIN users u ON u.id=l.owner_user_id
		WHERE l.channel='max' AND l.provider='wappi' AND l.is_active`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var lineID, ref, ownerID, owner, label, name string
			if rows.Scan(&lineID, &ref, &ownerID, &owner, &label, &name) != nil {
				continue
			}
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			phone := maxNormPhone(label)
			if phone == "" {
				phone = maxNormPhone(name)
			}
			byRef[ref] = lineMeta{LineID: lineID, OwnerID: ownerID, Owner: owner, Phone: phone}
			if phone == "" {
				continue
			}
			prev := byPhone[phone]
			prevOn := prev != ""
			byPhone[phone] = chooseMaxProfile(prev, ref, prevOn, true, true, true)
		}
	}

	out := []empMaxAvail{}
	if !a.cfg.WappiReady() {
		return out, "нет WAPPI_TOKEN"
	}
	profiles, err := a.wappiListProfiles(ctx)
	if err != nil {
		return out, err.Error()
	}
	seen := map[string]bool{}
	byShownPhone := map[string]int{}
	for _, p := range profiles {
		pid := strings.TrimSpace(p.ProfileID)
		if pid == "" {
			continue
		}
		if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
			continue
		}
		if owned[pid] || seen[pid] {
			continue
		}
		seen[pid] = true
		phone := maxDisplayPhone(p.Phone)
		if phone == "" {
			phone = maxDisplayPhone(p.Name)
		}
		phoneKey := maxNormPhone(phone)
		if phoneKey != "" {
			if kept, ok := byPhone[phoneKey]; ok && kept != pid {
				continue
			}
		}
		label := strings.TrimSpace(p.Name)
		if label == "" {
			label = phone
		}
		if label == "" {
			label = "MAX"
		}
		st := strings.ToLower(strings.TrimSpace(p.AppStatus))
		if st == "" {
			st = "unknown"
		}
		working := wappiProfileWorking(p)
		item := empMaxAvail{
			ProfileID:  pid,
			Name:       label,
			Phone:      phone,
			Authorized: p.Authorized,
			AppStatus:  st,
			Working:    working,
		}
		if m, ok := byRef[pid]; ok {
			item.LineID = m.LineID
			item.OwnerID = m.OwnerID
			item.Owner = m.Owner
		}
		if phoneKey == "" {
			out = append(out, item)
			continue
		}
		if i, ok := byShownPhone[phoneKey]; ok {
			prev := out[i]
			keep := chooseMaxProfile(prev.ProfileID, pid, prev.LineID != "", item.LineID != "", prev.Working, working)
			if keep == pid {
				out[i] = item
			}
			continue
		}
		byShownPhone[phoneKey] = len(out)
		out = append(out, item)
	}
	return out, ""
}

// activeMaxLineOtherProfile — номер уже на включённой линии с другим профилем.
func (a *App) activeMaxLineOtherProfile(ctx context.Context, phone, profileID, exceptLineID string) (owner string, taken bool) {
	phone = maxNormPhone(phone)
	profileID = strings.TrimSpace(profileID)
	if phone == "" {
		return "", false
	}
	tail := phone
	if len(tail) > 10 {
		tail = tail[len(tail)-10:]
	}
	var lineID, who string
	err := a.db.QueryRow(ctx, `
		SELECT l.id::text, coalesce(u.full_name,'')
		FROM inbox_lines l
		LEFT JOIN users u ON u.id=l.owner_user_id
		WHERE l.provider='wappi' AND l.channel='max' AND l.is_active
		  AND coalesce(l.external_ref,'') <> $1
		  AND l.id::text <> $2
		  AND (
		    right(regexp_replace(coalesce(l.operator_label,''), '[^0-9]', '', 'g'), 10) = $3
		    OR right(regexp_replace(coalesce(l.name,''), '[^0-9]', '', 'g'), 10) = $3
		  )
		LIMIT 1`, profileID, exceptLineID, tail).Scan(&lineID, &who)
	if err != nil || lineID == "" {
		return "", false
	}
	return who, true
}

// checkEmployeeMaxPhone — Wappi GET /maxapi/sync/contact/check: есть ли номер в MAX.
func (a *App) checkEmployeeMaxPhone(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if !a.canManageEmployeeTelephony(ctx, r, id) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	if !a.cfg.WappiReady() {
		writeJSON(w, 400, map[string]string{"error": "нет WAPPI_TOKEN"})
		return
	}
	var in struct {
		Phone     string `json:"phone"`
		ProfileID string `json:"profile_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	phone := maxNormPhone(in.Phone)
	if phone == "" {
		writeJSON(w, 400, map[string]string{"error": "введите номер телефона"})
		return
	}
	profileID := strings.TrimSpace(in.ProfileID)
	if profileID == "" {
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(external_ref,'') FROM inbox_lines
			WHERE owner_user_id=$1::uuid AND channel='max' AND provider='wappi'
			  AND is_active AND coalesce(external_ref,'')<>''
			ORDER BY name LIMIT 1`, id).Scan(&profileID)
	}
	if profileID == "" {
		profileID = strings.TrimSpace(a.cfg.WappiProfileID)
	}
	if profileID == "" {
		if profiles, err := a.wappiListProfiles(ctx); err == nil {
			for _, p := range profiles {
				if !p.Authorized || strings.TrimSpace(p.ProfileID) == "" {
					continue
				}
				if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
					continue
				}
				profileID = strings.TrimSpace(p.ProfileID)
				break
			}
		}
	}
	if profileID == "" {
		writeJSON(w, 400, map[string]string{"error": "нет профиля MAX для проверки — привяжите аккаунт"})
		return
	}

	u := "https://wappi.pro/maxapi/sync/contact/check?profile_id=" + url.QueryEscape(profileID) +
		"&phone=" + url.QueryEscape(phone)
	body, err := a.wappiJSON(ctx, http.MethodGet, u, nil)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	onMax := false
	switch v := raw["on_max"].(type) {
	case bool:
		onMax = v
	case string:
		onMax = strings.EqualFold(v, "true") || v == "1"
	}
	detail := strings.TrimSpace(fmt.Sprint(raw["detail"]))
	if detail == "<nil>" {
		detail = ""
	}
	out := map[string]any{
		"ok":         true,
		"phone":      phone,
		"on_max":     onMax,
		"detail":     detail,
		"profile_id": profileID,
		"user_id":    raw["user_id"],
		"chat_id":    raw["chat_id"],
	}
	writeJSON(w, 200, out)
}

func (a *App) bindEmployeeMax(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if !canBindMax(r) {
		writeJSON(w, 403, map[string]string{"error": "подключать и отключать MAX может только администратор"})
		return
	}
	var in struct {
		ProfileID string `json:"profile_id"`
		LineID    string `json:"line_id"`
		Name      string `json:"name"`
		Phone     string `json:"phone"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	profileID := strings.TrimSpace(in.ProfileID)
	lineID := strings.TrimSpace(in.LineID)
	name := strings.TrimSpace(in.Name)
	phone := strings.TrimSpace(in.Phone)
	if profileID == "" && lineID == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите аккаунт MAX"})
		return
	}
	if who, taken := a.activeMaxLineOtherProfile(ctx, phone, profileID, lineID); taken {
		msg := "этот номер уже привязан, второй профиль нельзя включить"
		if who != "" {
			msg = "этот номер уже привязан к " + who + ", второй профиль нельзя включить"
		}
		writeJSON(w, 400, map[string]string{"error": msg})
		return
	}

	var exists bool
	if a.db.QueryRow(ctx, `SELECT true FROM users WHERE id=$1::uuid`, id).Scan(&exists) != nil || !exists {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}

	if lineID != "" {
		tag, err := a.db.Exec(ctx, `
			UPDATE inbox_lines SET owner_user_id=$2::uuid, is_active=true,
				external_ref=CASE WHEN nullif($3,'') IS NOT NULL THEN $3 ELSE external_ref END,
				operator_label=CASE WHEN nullif($4,'') IS NOT NULL THEN $4 ELSE operator_label END,
				name=CASE WHEN nullif($5,'') IS NOT NULL THEN $5 ELSE name END
			WHERE id=$1::uuid AND channel='max' AND provider='wappi'`,
			lineID, id, profileID, phone, name)
		if err != nil || tag.RowsAffected() == 0 {
			writeJSON(w, 400, map[string]string{"error": "не удалось привязать линию"})
			return
		}
		a.cutInboxLineHistory(ctx, lineID)
		writeJSON(w, 200, map[string]any{"ok": true, "line_id": lineID})
		return
	}

	var existing string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM inbox_lines
		WHERE provider='wappi' AND channel='max' AND external_ref=$1
		ORDER BY is_active DESC LIMIT 1`, profileID).Scan(&existing)
	if existing != "" {
		_, err := a.db.Exec(ctx, `
			UPDATE inbox_lines SET owner_user_id=$2::uuid, is_active=true,
				operator_label=CASE WHEN nullif($3,'') IS NOT NULL THEN $3 ELSE operator_label END,
				name=CASE WHEN nullif($4,'') IS NOT NULL THEN $4 ELSE name END
			WHERE id=$1::uuid`, existing, id, phone, name)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось привязать аккаунт"})
			return
		}
		a.cutInboxLineHistory(ctx, existing)
		writeJSON(w, 200, map[string]any{"ok": true, "line_id": existing})
		return
	}

	if name == "" {
		name = "MAX"
		if phone != "" {
			name = "MAX · " + phone
		}
	}
	var newID string
	err := a.db.QueryRow(ctx, `
		INSERT INTO inbox_lines (provider, channel, name, kind, operator_label, external_ref, owner_user_id, is_active, history_from)
		VALUES ('wappi','max',$1,'personal',$2,$3,$4::uuid,true,now())
		RETURNING id::text`, name, phone, profileID, id).Scan(&newID)
	if err != nil {
		// history_from может ещё не быть в схеме на старом инстансе
		a.ensureInboxLineHistoryFrom(ctx)
		err = a.db.QueryRow(ctx, `
			INSERT INTO inbox_lines (provider, channel, name, kind, operator_label, external_ref, owner_user_id, is_active)
			VALUES ('wappi','max',$1,'personal',$2,$3,$4::uuid,true)
			RETURNING id::text`, name, phone, profileID, id).Scan(&newID)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось создать линию"})
			return
		}
		a.cutInboxLineHistory(ctx, newID)
	}
	writeJSON(w, 201, map[string]any{"ok": true, "line_id": newID})
}

func (a *App) unbindEmployeeMax(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lineID := chi.URLParam(r, "lineId")
	ctx := r.Context()
	if !canBindMax(r) {
		writeJSON(w, 403, map[string]string{"error": "подключать и отключать MAX может только администратор"})
		return
	}
	tag, err := a.db.Exec(ctx, `
		UPDATE inbox_lines SET owner_user_id=NULL
		WHERE id=$1::uuid AND owner_user_id=$2::uuid AND channel='max' AND provider='wappi'`,
		lineID, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeJSON(w, 400, map[string]string{"error": "не удалось отвязать"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
