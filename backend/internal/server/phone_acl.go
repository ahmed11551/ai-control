package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"unicode"
)

var deptSeePhonesColumnOnce sync.Once

const baurovDeptName = "Отдел Баурова"
const mishaDeptName = "Отдел Миши"
const pwwwwDeptName = "Отдел Pwwww"
const martynovNightLogin = "martynov.night"

func (a *App) enableMskTowerInboxFiles(ctx context.Context) {
	if a == nil || a.db == nil {
		return
	}
	rows, err := a.db.Query(ctx, `SELECT id::text, coalesce(name,'') FROM departments`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) != nil || !isMskTowerDepartmentName(name) {
			continue
		}
		_, _ = a.db.Exec(ctx, `
			UPDATE departments
			SET see_inbox_files=true
			WHERE id=$1::uuid AND coalesce(see_inbox_files, false)=false`, id)
	}
}

func (a *App) userInMskTowerDepartment(ctx context.Context, userID string) bool {
	userID = strings.TrimSpace(userID)
	if a == nil || a.db == nil || userID == "" {
		return false
	}
	var name string
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(d.name,'')
		FROM users u
		JOIN departments d ON d.id = u.department_id
		WHERE u.id=$1::uuid`, userID).Scan(&name)
	return err == nil && isMskTowerDepartmentName(name)
}

func (a *App) mskTowerManagerSeesFiles(ctx context.Context, userID, role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "manager", "sales", "rop":
	default:
		return false
	}
	return a.userInMskTowerDepartment(ctx, userID)
}

func (a *App) ensureDeptSeeClientPhones(ctx context.Context) {
	deptSeePhonesColumnOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE departments
			ADD COLUMN IF NOT EXISTS see_client_phones BOOLEAN NOT NULL DEFAULT false`)
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE departments
			ADD COLUMN IF NOT EXISTS see_inbox_files BOOLEAN NOT NULL DEFAULT false`)
		// Номера: Бауров, Миша и Pwwww. Файлы чата: Миша и МСК Башня.
		// У МСК Башня номера не включаем.
		_, _ = a.db.Exec(ctx, `
			UPDATE departments SET see_client_phones=true
			WHERE name=$1 AND coalesce(see_client_phones,false)=false`, baurovDeptName)
		_, _ = a.db.Exec(ctx, `
			UPDATE departments SET see_client_phones=true
			WHERE name=$1 AND coalesce(see_client_phones,false)=false`, mishaDeptName)
		_, _ = a.db.Exec(ctx, `
			UPDATE departments SET see_client_phones=true
			WHERE name=$1 AND coalesce(see_client_phones,false)=false`, pwwwwDeptName)
		_, _ = a.db.Exec(ctx, `
			UPDATE departments SET see_inbox_files=true
			WHERE name=$1 AND coalesce(see_inbox_files,false)=false`, mishaDeptName)
		a.enableMskTowerInboxFiles(ctx)
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE users
			ADD COLUMN IF NOT EXISTS see_client_phones BOOLEAN NOT NULL DEFAULT false`)
		// Ночной логист — та же должность, что логист, но видит номера клиентов.
		_, _ = a.db.Exec(ctx, `
			INSERT INTO roles (name, permissions)
			VALUES ('night_logist', '{"logistics":true}'::jsonb)
			ON CONFLICT (name) DO NOTHING`)
		_, _ = a.db.Exec(ctx, `
			UPDATE users u
			SET role_id = night.id
			FROM roles night, roles cur
			WHERE night.name = 'night_logist'
			  AND u.login = $1
			  AND cur.id = u.role_id
			  AND cur.name = 'logist'`, martynovNightLogin)
	})
}

// userCanSeeClientPhones — полный номер клиента видит только администратор.
// Остальные роли получают маску независимо от отдела и персональных флагов.
func (a *App) userCanSeeClientPhones(ctx context.Context, userID, role string) bool {
	a.ensureDeptSeeClientPhones(ctx)
	role = strings.ToLower(strings.TrimSpace(role))
	return role == "admin"
}

// seeInboxFilesAllowed — файлы переписки: admin/логист всегда; manager/rop только при флаге отдела.
// Билеты логиста сюда не входят.
func seeInboxFilesAllowed(role string, deptSeeInboxFiles bool) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "logist", "night_logist":
		return true
	case "manager", "sales", "rop":
		return deptSeeInboxFiles
	default:
		return false
	}
}

// userCanViewInboxMedia — содержимое вложений чата видят только администратор
// и логист. Менеджер видит факт вложения, но не его содержимое.
func (a *App) userCanViewInboxMedia(ctx context.Context, userID, role string) bool {
	a.ensureDeptSeeClientPhones(ctx)
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "admin" || isLogistRole(role) {
		return true
	}
	return false
}

func maskClientPhone(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	d := digitsOnly(s)
	if len(d) < 4 {
		return "••••"
	}
	visible := d[len(d)-4:]
	prefix := ""
	if len(d) >= 11 && (d[0] == '7' || d[0] == '8') {
		prefix = "7"
		hidden := len(d) - 1 - 4
		if hidden < 0 {
			hidden = 0
		}
		return prefix + strings.Repeat("•", hidden) + visible
	}
	hidden := len(d) - 4
	return strings.Repeat("•", hidden) + visible
}

func looksLikeClientPhone(s string) bool {
	d := digitsOnly(s)
	return len(d) >= 10 && len(d) <= 15
}

// looksLikeStandalonePhone — значение целиком номер (не JSON/комментарий с цифрами внутри).
// Иначе payload `{"body":"...7900..."}` с 10+ цифрами маскировался целиком → комментарий пропадал.
func looksLikeStandalonePhone(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || !looksLikeClientPhone(s) {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '+' || r == '(' || r == ')' || r == '-' || r == ' ' || r == '\t':
		default:
			return false
		}
	}
	return true
}

// looksLikeUUID — не маскировать id сделок/лидов: в хвосте UUID часто 10–12 цифр подряд.
func looksLikeUUID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

func isProtectedIDKey(lk string) bool {
	if lk == "id" || lk == "uuid" || lk == "guid" {
		return true
	}
	if strings.HasSuffix(lk, "_id") || strings.HasSuffix(lk, "id") && strings.Contains(lk, "_") {
		// plain_id / plainId — это телефон в мессенджере, не CRM id
		if lk == "plain_id" || lk == "plainid" {
			return false
		}
		return true
	}
	return false
}

// redactPhonesInText маскирует номера внутри произвольной строки (ошибки, note).
func redactPhonesInText(s string) string {
	if s == "" || !strings.ContainsFunc(s, unicode.IsDigit) {
		return s
	}
	if looksLikeUUID(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] >= '0' && s[i] <= '9' {
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			run := s[i:j]
			if len(run) >= 10 && len(run) <= 15 {
				b.WriteString(maskClientPhone(run))
			} else {
				b.WriteString(run)
			}
			i = j
			continue
		}
		if s[i] == '+' || s[i] == '(' {
			j := i
			digits := 0
			for j < len(s) {
				c := s[j]
				if c >= '0' && c <= '9' {
					digits++
					j++
					continue
				}
				if c == '+' || c == '(' || c == ')' || c == '-' || c == ' ' {
					j++
					continue
				}
				break
			}
			chunk := s[i:j]
			if digits >= 10 && digits <= 15 && looksLikeClientPhone(chunk) {
				b.WriteString(maskClientPhone(chunk))
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

var phoneFieldKeys = map[string]bool{
	"phone": true, "contact_phone": true, "work_phone": true,
	"from_number": true, "to_number": true, "from": true, "to": true,
	"cli": true, "peer": true, "incoming_peer": true, "caller": true,
	"calleridnum": true, "connectedlinenum": true,
	"plain_id": true, "plainId": true,
}

var phoneTextKeys = map[string]bool{
	"error": true, "note": true, "detail": true, "message": true,
	"title": true, "name": true, "full_name": true, "author_label": true,
}

// commentTextKeys — текст комментариев/заметок: не маскируем (менеджер пишет цифры как есть).
var commentTextKeys = map[string]bool{
	"body": true, "card_note": true, "comment": true,
}

func redactPhoneKeyedString(key, s string) string {
	lk := strings.ToLower(key)
	// Номер нашего аккаунта MAX в мессенджере, не номер клиента.
	if lk == "from_phone" || lk == "operator_label" {
		return s
	}
	if phoneFieldKeys[lk] {
		return maskClientPhone(s)
	}
	// id / *_id / UUID никогда не режем — иначе kanban stage/open ломается (• в URL).
	if isProtectedIDKey(lk) || looksLikeUUID(s) {
		return s
	}
	// Комментарии / card_note — без phone ACL (иначе цифры «съедают» весь текст).
	if commentTextKeys[lk] {
		return s
	}
	// activity payload как JSON-строка: разбираем и маскируем только phone-поля внутри.
	if lk == "payload" {
		trim := strings.TrimSpace(s)
		if len(trim) > 0 && (trim[0] == '{' || trim[0] == '[') {
			var parsed any
			if err := json.Unmarshal([]byte(trim), &parsed); err == nil {
				redacted := redactPhonesValue(parsed)
				if b, err := json.Marshal(redacted); err == nil {
					return string(b)
				}
			}
		}
		return s
	}
	if lk == "external_id" && looksLikeStandalonePhone(s) {
		return maskClientPhone(s)
	}
	if phoneTextKeys[lk] {
		if looksLikeStandalonePhone(s) {
			return "Клиент"
		}
		return redactPhonesInText(s)
	}
	// Прочие поля: маскируем только если значение целиком — номер (без букв/JSON).
	if looksLikeStandalonePhone(s) {
		return maskClientPhone(s)
	}
	return s
}

func redactPhonesValue(v any) any {
	switch t := v.(type) {
	case string:
		return redactPhonesInText(t)
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, val := range t {
			out[k] = redactPhoneKeyedString(k, val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if s, ok := val.(string); ok {
				out[k] = redactPhoneKeyedString(k, s)
				continue
			}
			out[k] = redactPhonesValue(val)
		}
		return out
	case []map[string]string:
		out := make([]map[string]string, len(t))
		for i, row := range t {
			if m, ok := redactPhonesValue(row).(map[string]string); ok {
				out[i] = m
			}
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(t))
		for i, row := range t {
			if m, ok := redactPhonesValue(row).(map[string]any); ok {
				out[i] = m
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, row := range t {
			out[i] = redactPhonesValue(row)
		}
		return out
	default:
		return v
	}
}

// redactAMIEventForWS — в broadcast всегда маскируем номера (у WS нет роли клиента).
func redactAMIEventForWS(ev map[string]string) map[string]string {
	if ev == nil {
		return nil
	}
	if m, ok := redactPhonesValue(ev).(map[string]string); ok {
		return m
	}
	return ev
}

func (a *App) writePhones(w http.ResponseWriter, r *http.Request, status int, payload any) {
	if !a.userCanSeeClientPhones(r.Context(), actorID(r), roleName(r)) {
		payload = redactPhonesValue(payload)
	}
	writeJSON(w, status, payload)
}

// nightLogistCanDialSeen — ночной логист звонит по карточке, которую ему уже показали.
// Дневной логист номер так не получает.
func nightLogistCanDialSeen(role string, visible bool) bool {
	return visible && strings.EqualFold(strings.TrimSpace(role), "night_logist")
}

func (a *App) resolveCallDest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To        string `json:"to"`
		LeadID    string `json:"lead_id"`
		ContactID string `json:"contact_id"`
		DealID    string `json:"deal_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	uid := actorID(r)
	var can bool
	if err := a.db.QueryRow(r.Context(), `SELECT can_call FROM users WHERE id=$1`, uid).Scan(&can); err != nil || !can {
		writeJSON(w, 403, map[string]string{"error": "нет права звонить"})
		return
	}

	phone := strings.TrimSpace(in.To)
	ctx := r.Context()
	scope := a.crmScope(r)

	if phone == "" && in.LeadID != "" {
		if !a.canSeeLead(ctx, scope, in.LeadID) {
			writeJSON(w, 403, map[string]string{"error": "нет доступа к лиду"})
			return
		}
		if !nightLogistCanDialSeen(roleName(r), true) && !a.canDialEntity(ctx, scope, in.LeadID, "", "") {
			writeJSON(w, 403, map[string]string{"error": "номер скрыт — звонок только через скорозвон"})
			return
		}
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(c.phone,'') FROM leads l
			LEFT JOIN contacts c ON c.id=l.contact_id WHERE l.id=$1`, in.LeadID).Scan(&phone)
	}
	if phone == "" && in.DealID != "" {
		if !a.canSeeDeal(ctx, scope, in.DealID) {
			writeJSON(w, 403, map[string]string{"error": "нет доступа к сделке"})
			return
		}
		if !nightLogistCanDialSeen(roleName(r), true) && !a.canDialEntity(ctx, scope, "", in.DealID, "") {
			writeJSON(w, 403, map[string]string{"error": "номер скрыт — звонок только через скорозвон"})
			return
		}
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(c.phone,'') FROM deals d
			LEFT JOIN contacts c ON c.id=d.contact_id WHERE d.id=$1`, in.DealID).Scan(&phone)
	}
	if phone == "" && in.ContactID != "" {
		if !a.contactVisible(ctx, scope, in.ContactID) {
			writeJSON(w, 403, map[string]string{"error": "нет доступа к контакту"})
			return
		}
		if !nightLogistCanDialSeen(roleName(r), true) && !a.canDialEntity(ctx, scope, "", "", in.ContactID) {
			writeJSON(w, 403, map[string]string{"error": "номер скрыт — звонок только через скорозвон"})
			return
		}
		_ = a.db.QueryRow(ctx, `SELECT coalesce(phone,'') FROM contacts WHERE id=$1`, in.ContactID).Scan(&phone)
	}

	to := e164RU(phone)
	if to == "" {
		writeJSON(w, 400, map[string]string{"error": "нет номера для звонка"})
		return
	}
	// Менеджеру не отдаём полный номер в UI-полях — только одноразовый dest для софтфона.
	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"to":     to,
		"masked": maskClientPhone(to),
	})
}

func fmtRoleLabel(role string) string {
	switch normStaffRole(role) {
	case "admin":
		return "Администратор"
	case "rop":
		return "РОП"
	case "manager", "sales":
		return "Менеджер"
	case "logist":
		return "Логист"
	case "night_logist":
		return "Ночной логист"
	case "customer":
		return "Заказчик"
	case "support":
		return "Поддержка"
	case "marketing":
		return "Маркетинг"
	default:
		return role
	}
}
