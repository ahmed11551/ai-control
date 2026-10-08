package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Доска MAX для администратора: один телефон — одна строка.
// Чужой привязанный номер не выглядит свободным, даже если у него второй профиль.

type maxBoardLine struct {
	Ref       string
	Phone     string
	OwnerID   string
	OwnerName string
	SendDown  bool
	Cold      bool
}

type maxPhoneRow struct {
	Phone        string `json:"phone"`
	OwnerID      string `json:"owner_id"`
	OwnerName    string `json:"owner_name"`
	Bound        bool   `json:"bound"`
	Session      string `json:"session"`
	SendDown     bool   `json:"send_down"`
	ColdLimit    bool   `json:"cold_limit"`
	Check        string `json:"check,omitempty"`
	CanNew       bool   `json:"can_new"`
	Bucket       string `json:"bucket"`
	Profiles     int    `json:"profiles"`
	ExtraProfile bool   `json:"extra_profile"`
}

type maxPersonRow struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Role       string         `json:"role"`
	HasWorking bool           `json:"has_working"`
	Numbers    []maxPersonNum `json:"numbers"`
}

type maxPersonNum struct {
	Phone string `json:"phone"`
	Label string `json:"label"`
}

func lookupMaxCheck(checks map[string]string, profileID string) string {
	if len(checks) == 0 {
		return ""
	}
	id := strings.ToLower(strings.TrimSpace(profileID))
	if id == "" {
		return ""
	}
	if v := checks[id]; v != "" {
		return v
	}
	if len(id) >= 13 {
		if v := checks[id[:13]]; v != "" {
			return v
		}
	}
	for key, v := range checks {
		if profileRefMatch(key, id) && v != "" {
			return v
		}
	}
	return ""
}

func phoneGroupCheck(profiles []wappiProfile, lines []maxBoardLine, checks map[string]string) string {
	boundRefs := []string{}
	for _, ln := range lines {
		if strings.TrimSpace(ln.OwnerID) != "" && strings.TrimSpace(ln.Ref) != "" {
			boundRefs = append(boundRefs, ln.Ref)
		}
	}
	use := []wappiProfile{}
	if len(boundRefs) > 0 {
		for _, p := range profiles {
			for _, ref := range boundRefs {
				if profileRefMatch(p.ProfileID, ref) {
					use = append(use, p)
					break
				}
			}
		}
	} else {
		use = profiles
	}
	got := []string{}
	for _, p := range use {
		if c := lookupMaxCheck(checks, p.ProfileID); c != "" {
			got = append(got, c)
		}
	}
	if len(boundRefs) > 0 && len(got) == 0 {
		return ""
	}
	for _, c := range got {
		if c == "ok" {
			return "ok"
		}
	}
	for _, c := range got {
		if c == "no" {
			return "no"
		}
	}
	for _, c := range got {
		if c == "unauthorized" {
			return "unauthorized"
		}
	}
	return ""
}

func buildMaxPhoneRows(profiles []wappiProfile, lines []maxBoardLine, checks map[string]string) []maxPhoneRow {
	type slot struct {
		phone     string
		profiles  []wappiProfile
		lines     []maxBoardLine
		orderHint string
	}
	groups := map[string]*slot{}
	var order []string

	touch := func(key, display string) *slot {
		if key == "" {
			return nil
		}
		if g, ok := groups[key]; ok {
			if g.phone == "" && display != "" {
				g.phone = display
			}
			return g
		}
		g := &slot{phone: display, orderHint: display}
		groups[key] = g
		order = append(order, key)
		return g
	}

	for _, p := range profiles {
		if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
			continue
		}
		ph := maxNormPhone(p.Phone)
		if ph == "" {
			ph = maxNormPhone(p.Name)
		}
		key := phoneKey10(ph)
		if key == "" {
			key = "id:" + strings.ToLower(strings.TrimSpace(p.ProfileID))
		}
		g := touch(key, ph)
		if g == nil {
			continue
		}
		g.profiles = append(g.profiles, p)
	}

	for _, ln := range lines {
		ph := maxNormPhone(ln.Phone)
		key := phoneKey10(ph)
		matched := ""
		if key != "" {
			if _, ok := groups[key]; ok {
				matched = key
			}
		}
		if matched == "" && strings.TrimSpace(ln.Ref) != "" {
			for k, g := range groups {
				for _, p := range g.profiles {
					if profileRefMatch(p.ProfileID, ln.Ref) {
						matched = k
						break
					}
				}
				if matched != "" {
					break
				}
			}
		}
		if matched == "" {
			if key == "" {
				key = "ref:" + strings.ToLower(strings.TrimSpace(ln.Ref))
			}
			matched = key
		}
		g := touch(matched, ph)
		if g == nil {
			continue
		}
		if g.phone == "" {
			g.phone = ph
		}
		g.lines = append(g.lines, ln)
	}

	rows := make([]maxPhoneRow, 0, len(order))
	for _, key := range order {
		g := groups[key]
		row := maxPhoneRow{Phone: g.phone, Profiles: len(g.profiles)}
		live := false
		seenProfile := len(g.profiles) > 0
		for _, p := range g.profiles {
			if wappiProfileWorking(p) {
				live = true
				break
			}
		}
		for _, ln := range g.lines {
			if strings.TrimSpace(ln.OwnerID) != "" {
				row.Bound = true
				if row.OwnerID == "" {
					row.OwnerID = ln.OwnerID
					row.OwnerName = ln.OwnerName
				}
			}
			if ln.SendDown {
				row.SendDown = true
			}
			if ln.Cold {
				row.ColdLimit = true
			}
		}
		row.ExtraProfile = row.Bound && len(g.profiles) > 1
		switch {
		case !seenProfile:
			row.Session = "unknown"
		case live:
			row.Session = "live"
		default:
			row.Session = "down"
		}
		row.Check = phoneGroupCheck(g.profiles, g.lines, checks)
		switch row.Check {
		case "ok":
			row.SendDown = false
			row.ColdLimit = false
			row.Session = "live"
		case "no":
			row.SendDown = false
			row.ColdLimit = true
			row.Session = "live"
		case "unauthorized":
			row.SendDown = true
			row.ColdLimit = false
			row.Session = "down"
		}
		row.CanNew = row.Bound && row.Session == "live" && !row.SendDown && !row.ColdLimit
		switch {
		case row.ColdLimit && row.Session == "live" && !row.SendDown:
			row.Bucket = "cold"
		case row.SendDown || row.Session == "down":
			row.Bucket = "down"
		case row.CanNew:
			row.Bucket = "bound_ok"
		case !row.Bound && row.Session == "live":
			row.Bucket = "free"
		default:
			row.Bucket = "other"
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Phone == rows[j].Phone {
			return rows[i].OwnerName < rows[j].OwnerName
		}
		if rows[i].Phone == "" {
			return false
		}
		if rows[j].Phone == "" {
			return true
		}
		return rows[i].Phone < rows[j].Phone
	})
	return rows
}

func maxPersonNumbers(rows []maxPhoneRow, userID string) (nums []maxPersonNum, hasWorking bool) {
	for _, row := range rows {
		if row.OwnerID != userID {
			continue
		}
		label := "новым можно"
		switch {
		case row.SendDown || row.Session == "down":
			label = "не работает"
		case row.ColdLimit:
			label = "лимит на новых"
		case row.Session != "live":
			label = "сессия неизвестна"
		}
		if row.CanNew {
			hasWorking = true
		}
		nums = append(nums, maxPersonNum{Phone: row.Phone, Label: label})
	}
	if nums == nil {
		nums = []maxPersonNum{}
	}
	return nums, hasWorking
}

func (a *App) getMaxAccountsBoard(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	ctx := r.Context()
	a.ensureMaxSendDown(ctx)

	dbLines := []maxBoardLine{}
	lrows, err := a.db.Query(ctx, `
		SELECT coalesce(l.external_ref,''), coalesce(l.operator_label,''), coalesce(l.name,''),
		       coalesce(l.owner_user_id::text,''), coalesce(u.full_name,''),
		       coalesce(l.max_send_down,false), coalesce(l.max_cold_limit,false)
		FROM inbox_lines l
		LEFT JOIN users u ON u.id=l.owner_user_id
		WHERE l.provider='wappi' AND l.channel='max'`)
	if err == nil {
		defer lrows.Close()
		for lrows.Next() {
			var ref, label, name, ownerID, ownerName string
			var sendDown, cold bool
			if lrows.Scan(&ref, &label, &name, &ownerID, &ownerName, &sendDown, &cold) != nil {
				continue
			}
			ph := maxNormPhone(label)
			if ph == "" {
				ph = maxNormPhone(name)
			}
			dbLines = append(dbLines, maxBoardLine{
				Ref: ref, Phone: ph, OwnerID: ownerID, OwnerName: ownerName,
				SendDown: sendDown, Cold: cold,
			})
		}
	}

	wappiErr := ""
	var profiles []wappiProfile
	if a.cfg.WappiReady() {
		profiles, err = a.wappiListProfiles(ctx)
		if err != nil {
			wappiErr = "список номеров сейчас недоступен"
			profiles = nil
		}
	} else {
		wappiErr = "нет доступа к списку номеров"
	}

	phones := buildMaxPhoneRows(profiles, dbLines, a.loadMaxProfileChecks(ctx))
	counts := map[string]int{"all": len(phones)}
	for _, row := range phones {
		counts[row.Bucket]++
	}

	type userRow struct {
		ID, Name, Role string
	}
	var users []userRow
	urows, err := a.db.Query(ctx, `
		SELECT u.id::text, coalesce(u.full_name,''), lower(coalesce(r.name,''))
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.is_active
		  AND (
		    lower(coalesce(r.name,'')) IN ('manager','sales','rop')
		    OR EXISTS (
		      SELECT 1 FROM inbox_lines l
		      WHERE l.owner_user_id=u.id AND l.provider='wappi' AND l.channel='max'
		    )
		  )
		ORDER BY u.full_name`)
	if err == nil {
		defer urows.Close()
		for urows.Next() {
			var u userRow
			if urows.Scan(&u.ID, &u.Name, &u.Role) != nil {
				continue
			}
			users = append(users, u)
		}
	}

	people := make([]maxPersonRow, 0, len(users))
	gap := 0
	for _, u := range users {
		nums, ok := maxPersonNumbers(phones, u.ID)
		if !ok {
			gap++
		}
		people = append(people, maxPersonRow{
			ID: u.ID, Name: u.Name, Role: u.Role, HasWorking: ok, Numbers: nums,
		})
	}
	counts["people_gap"] = gap

	targets := make([]map[string]string, 0, len(profiles))
	for _, p := range profiles {
		if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
			continue
		}
		id := strings.TrimSpace(p.ProfileID)
		if id == "" {
			continue
		}
		ph := maxNormPhone(p.Phone)
		if ph == "" {
			ph = maxNormPhone(p.Name)
		}
		targets = append(targets, map[string]string{"profile_id": id, "phone": ph})
	}

	writeJSON(w, 200, map[string]any{
		"phones":      phones,
		"people":      people,
		"counts":      counts,
		"targets":     targets,
		"wappi_error": wappiErr,
	})
}

func classifyMaxContactCheck(statusErr string, raw map[string]any) string {
	blob := strings.ToLower(statusErr)
	if raw != nil {
		blob += " " + strings.ToLower(fmt.Sprint(raw["detail"])) + " " + strings.ToLower(fmt.Sprint(raw["status"]))
	}
	switch {
	case strings.Contains(blob, "flood"), strings.Contains(blob, "слишком много"):
		return "flood"
	case strings.Contains(blob, "unauthorized"),
		strings.Contains(blob, "not authorized"),
		strings.Contains(blob, "не авторизован"),
		strings.Contains(blob, "forbidden"):
		return "unauthorized"
	}
	if strings.TrimSpace(statusErr) != "" {
		return "error"
	}
	on := false
	switch v := raw["on_max"].(type) {
	case bool:
		on = v
	case string:
		on = strings.EqualFold(v, "true") || v == "1"
	}
	if on {
		return "ok"
	}
	return "no"
}

func (a *App) ensureMaxProfileChecks(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS max_profile_checks (
			profile_id TEXT PRIMARY KEY,
			sender_phone TEXT NOT NULL DEFAULT '',
			probe_phone TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			checked_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
}

func (a *App) loadMaxProfileChecks(ctx context.Context) map[string]string {
	a.ensureMaxProfileChecks(ctx)
	out := map[string]string{}
	rows, err := a.db.Query(ctx, `SELECT profile_id, result FROM max_profile_checks WHERE result <> ''`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, result string
		if rows.Scan(&id, &result) != nil {
			continue
		}
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		out[id] = result
		if len(id) >= 13 {
			out[id[:13]] = result
		}
	}
	return out
}

func phoneCheckState(profiles []wappiProfile, checks map[string]string, phone string) (hasOK, hasBad, hasAny bool) {
	key := phoneKey10(phone)
	if key == "" {
		return false, false, false
	}
	for _, p := range profiles {
		ph := maxNormPhone(p.Phone)
		if ph == "" {
			ph = maxNormPhone(p.Name)
		}
		if phoneKey10(ph) != key {
			continue
		}
		switch lookupMaxCheck(checks, p.ProfileID) {
		case "":
			continue
		case "ok":
			hasOK = true
			hasAny = true
		case "no", "unauthorized":
			hasBad = true
			hasAny = true
		default:
			hasAny = true
		}
	}
	return hasOK, hasBad, hasAny
}

func (a *App) postMaxAccountCheck(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
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
	probe := maxNormPhone(in.Phone)
	profileID := strings.TrimSpace(in.ProfileID)
	if probe == "" || profileID == "" {
		writeJSON(w, 400, map[string]string{"error": "нужны номер и профиль"})
		return
	}
	if !a.cfg.WappiReady() {
		writeJSON(w, 400, map[string]string{"error": "нет доступа к MAX"})
		return
	}
	ctx := r.Context()
	a.ensureMaxProfileChecks(ctx)
	u := "https://wappi.pro/maxapi/sync/contact/check?profile_id=" + url.QueryEscape(profileID) +
		"&phone=" + url.QueryEscape(probe)
	body, err := a.wappiJSON(ctx, http.MethodGet, u, nil)
	statusErr := ""
	raw := map[string]any{}
	if err != nil {
		statusErr = err.Error()
	} else {
		_ = json.Unmarshal(body, &raw)
	}
	result := classifyMaxContactCheck(statusErr, raw)
	if result == "flood" {
		writeJSON(w, 429, map[string]any{"ok": false, "result": "flood", "error": "лимит проверок, остановитесь и повторите позже"})
		return
	}
	if result == "error" {
		writeJSON(w, 400, map[string]any{"ok": false, "result": "error", "error": "проверка не прошла"})
		return
	}
	var sender string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(nullif(operator_label,''), name, '')
		FROM inbox_lines
		WHERE provider='wappi' AND channel='max'
		  AND left(coalesce(external_ref,''), 13)=left($1, 13)
		LIMIT 1`, profileID).Scan(&sender)
	_, _ = a.db.Exec(ctx, `
		INSERT INTO max_profile_checks (profile_id, sender_phone, probe_phone, result, checked_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (profile_id) DO UPDATE
		SET sender_phone=EXCLUDED.sender_phone, probe_phone=EXCLUDED.probe_phone,
		    result=EXCLUDED.result, checked_at=now()`,
		profileID, maxNormPhone(sender), probe, result)
	switch result {
	case "ok":
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_lines SET max_send_down=false, max_cold_limit=false
			WHERE provider='wappi' AND channel='max' AND left(coalesce(external_ref,''), 13)=left($1, 13)`, profileID)
	case "no":
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_lines SET max_send_down=false, max_cold_limit=true
			WHERE provider='wappi' AND channel='max' AND left(coalesce(external_ref,''), 13)=left($1, 13)`, profileID)
	case "unauthorized":
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_lines SET max_send_down=true, max_cold_limit=false
			WHERE provider='wappi' AND channel='max' AND left(coalesce(external_ref,''), 13)=left($1, 13)`, profileID)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "result": result, "profile_id": profileID})
}

func (a *App) postMaxAccountGrant(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	var in struct {
		UserID string `json:"user_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	userID := strings.TrimSpace(in.UserID)
	var exists bool
	if a.db.QueryRow(r.Context(), `SELECT true FROM users WHERE id=$1::uuid AND is_active`, userID).Scan(&exists) != nil || !exists {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}
	lineID, phone, ok := a.grantFreeWorkingMax(r.Context(), userID, true)
	if !ok || phone == "" {
		writeJSON(w, 400, map[string]string{"error": "свободного рабочего номера нет"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "line_id": lineID, "phone": phone})
}
