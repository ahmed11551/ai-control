package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// wappiEnsureWebhook регистрирует URL и типы событий MAX на стороне Wappi
// для всех авторизованных профилей и всех активных линий (не только WAPPI_PROFILE_ID).
func (a *App) wappiEnsureWebhook() {
	if !a.cfg.WappiReady() {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(a.cfg.PublicBaseURL), "/")
	if base == "" {
		log.Println("Wappi MAX: PUBLIC_BASE_URL не задан — webhook не регистрируем")
		return
	}
	secret := strings.TrimSpace(a.cfg.InboxWebhookSecret)
	if secret == "" {
		log.Println("Wappi MAX: INBOX_WEBHOOK_SECRET не задан — webhook не регистрируем")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	ids := map[string]struct{}{}
	if id := strings.TrimSpace(a.cfg.WappiProfileID); id != "" {
		ids[id] = struct{}{}
	}
	for _, row := range a.queryMaps(ctx, `
		SELECT coalesce(external_ref,'') AS profile_id FROM inbox_lines
		WHERE provider='wappi' AND channel='max' AND is_active AND coalesce(external_ref,'')<>''`) {
		if id := strings.TrimSpace(fmt.Sprint(row["profile_id"])); id != "" && id != "<nil>" {
			ids[id] = struct{}{}
		}
	}
	if profiles, err := a.wappiListProfiles(ctx); err == nil {
		for _, p := range profiles {
			if !p.Authorized {
				continue
			}
			if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
				continue
			}
			if id := strings.TrimSpace(p.ProfileID); id != "" {
				ids[id] = struct{}{}
			}
		}
	} else {
		log.Println("Wappi MAX webhook: список профилей:", err)
	}
	if len(ids) == 0 {
		if id, err := a.wappiProfileID(ctx); err == nil && id != "" {
			ids[id] = struct{}{}
		} else if err != nil {
			log.Println("Wappi MAX webhook:", err)
			return
		}
	}
	if len(ids) == 0 {
		log.Println("Wappi MAX webhook: нет profile_id")
		return
	}

	hookURL := base + "/webhooks/inbox/wappi/" + secret
	types := []string{"incoming_message", "outgoing_message_api", "outgoing_message_phone", "delivery_status"}
	okN := 0
	for profileID := range ids {
		setURL := "https://wappi.pro/maxapi/webhook/url/set?profile_id=" + url.QueryEscape(profileID) +
			"&url=" + url.QueryEscape(hookURL)
		if _, err := a.wappiJSON(ctx, http.MethodPost, setURL, map[string]any{}); err != nil {
			log.Println("Wappi MAX webhook url/set", profileID+":", err)
			continue
		}
		typesURL := "https://wappi.pro/maxapi/webhook/types/set?profile_id=" + url.QueryEscape(profileID)
		if _, err := a.wappiJSON(ctx, http.MethodPost, typesURL, types); err != nil {
			log.Println("Wappi MAX webhook types/set", profileID+":", err)
			continue
		}
		okN++
		log.Println("Wappi MAX webhook зарегистрирован:", profileID, "→", hookURL)
	}
	if okN == 0 {
		log.Println("Wappi MAX webhook: не удалось зарегистрировать ни на одном профиле")
	}
}

// wappiOpenByPhone создаёт/открывает диалог MAX во Входящих по номеру (без отправки).
func (a *App) wappiOpenByPhone(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.WappiReady() {
		writeJSON(w, 400, map[string]string{"error": "нет WAPPI_TOKEN"})
		return
	}
	var in struct {
		Phone     string `json:"phone"`
		Name      string `json:"name"`
		LineID    string `json:"line_id"`
		ProfileID string `json:"profile_id"`
		LeadID    string `json:"lead_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "неверный JSON"})
		return
	}
	phone := maxNormPhone(in.Phone)
	leadID := strings.TrimSpace(in.LeadID)
	if phone == "" && leadID != "" {
		var cPhone string
		_ = a.db.QueryRow(r.Context(), `
			SELECT coalesce(c.phone,'')
			FROM leads l LEFT JOIN contacts c ON c.id=l.contact_id
			WHERE l.id=$1::uuid`, leadID).Scan(&cPhone)
		phone = maxNormPhone(cPhone)
	}
	if phone == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите номер"})
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || looksLikeClientPhone(name) || name == phone {
		name = "Клиент"
	}
	if pid := strings.TrimSpace(in.ProfileID); pid != "" && strings.TrimSpace(in.LineID) == "" {
		// выбран профиль из Wappi без линии — найдём/создадим линию только с этим profile_id
		_ = a.db.QueryRow(r.Context(), `
			SELECT id::text FROM inbox_lines
			WHERE provider='wappi' AND channel='max' AND external_ref=$1
			ORDER BY is_active DESC LIMIT 1`, pid).Scan(&in.LineID)
		if in.LineID == "" {
			_ = a.db.QueryRow(r.Context(), `
				INSERT INTO inbox_lines (provider, channel, name, kind, operator_label, external_ref, is_active)
				VALUES ('wappi','max','MAX','personal','',$1,true)
				RETURNING id::text`, pid).Scan(&in.LineID)
		}
	}
	lineID := a.wappiLineForLead(r.Context(), in.LeadID, actorID(r), strings.TrimSpace(in.LineID))
	if lineID == "" {
		writeJSON(w, 400, map[string]string{
			"error": "укажите аккаунт MAX или привяжите номер ответственному",
		})
		return
	}
	threadID, err := a.wappiEnsureThread(r.Context(), phone, name, lineID)
	if err != nil {
		a.writePhones(w, r, 502, map[string]string{"error": err.Error()})
		return
	}
	a.writePhones(w, r, 200, map[string]any{
		"ok": true, "phone": phone, "thread_id": threadID, "line_id": lineID, "via": "wappi",
	})
}

func (a *App) wappiEnsureThread(ctx context.Context, phone, title, preferLineID string) (string, error) {
	phone = maxNormPhone(phone)
	if phone == "" {
		return "", fmt.Errorf("нет телефона")
	}
	if strings.TrimSpace(title) == "" || looksLikeClientPhone(title) {
		title = "Клиент"
	}
	lineID := strings.TrimSpace(preferLineID)
	if lineID == "" {
		return "", fmt.Errorf("нет аккаунта MAX")
	}
	extID := lineID + ":" + phone
	var id string
	err := a.db.QueryRow(ctx, `
		INSERT INTO inbox_threads (channel, external_id, title, phone, line_id, last_body, last_at, unread)
		VALUES ('max', $1, $2, $3, $4::uuid, '', now(), 0)
		ON CONFLICT (channel, external_id) DO UPDATE SET
			title=CASE WHEN coalesce(inbox_threads.title,'')='' THEN EXCLUDED.title ELSE inbox_threads.title END,
			phone=EXCLUDED.phone,
			line_id=EXCLUDED.line_id,
			last_at=now()
		RETURNING id::text`, extID, title, phone, lineID).Scan(&id)
	return id, err
}

// wappiLineForLead — линия MAX: явный line_id (свой) → линия владельца лида → линия актора.
func (a *App) wappiLineForLead(ctx context.Context, leadID, actorID, preferLineID string) string {
	preferLineID = strings.TrimSpace(preferLineID)
	actorID = strings.TrimSpace(actorID)
	if preferLineID != "" {
		var ok bool
		// Свой аккаунт или admin/rop — можно выбрать любую активную линию.
		err := a.db.QueryRow(ctx, `
			SELECT true FROM inbox_lines l
			LEFT JOIN users u ON u.id=$2::uuid
			LEFT JOIN roles r ON r.id=u.role_id
			WHERE l.id=$1::uuid AND l.provider='wappi' AND l.channel='max' AND l.is_active
			  AND coalesce(l.external_ref,'')<>''
			  AND (
				l.owner_user_id=$2::uuid
				OR lower(coalesce(r.name,'')) IN ('admin','rop')
			  )`, preferLineID, actorID).Scan(&ok)
		if err == nil && ok {
			return preferLineID
		}
	}
	owner := ""
	if strings.TrimSpace(leadID) != "" {
		_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1::uuid`, leadID).Scan(&owner)
	}
	for _, uid := range []string{owner, actorID} {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		var id string
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_lines
			WHERE provider='wappi' AND channel='max' AND is_active AND owner_user_id=$1::uuid
			  AND coalesce(external_ref,'')<>''
			ORDER BY name LIMIT 1`, uid).Scan(&id)
		if id != "" {
			return id
		}
	}
	if pid := strings.TrimSpace(a.cfg.WappiProfileID); pid != "" {
		var id string
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_lines
			WHERE provider='wappi' AND channel='max' AND is_active AND external_ref=$1
			LIMIT 1`, pid).Scan(&id)
		if id != "" {
			return id
		}
	}
	return ""
}

func (a *App) wappiLineProfile(ctx context.Context, lineID string) (profileID string) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return ""
	}
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(external_ref,'') FROM inbox_lines
		WHERE id=$1::uuid AND provider='wappi' AND channel='max' AND is_active`, lineID).Scan(&profileID)
	return strings.TrimSpace(profileID)
}

// wappiProfileID — только для webhook/служебных вызовов. Не трогает external_ref линий.
func (a *App) wappiProfileID(ctx context.Context) (string, error) {
	if id := strings.TrimSpace(a.cfg.WappiProfileID); id != "" {
		return id, nil
	}
	if id := a.wappiStoredProfileID(ctx); id != "" {
		return id, nil
	}
	profiles, err := a.wappiListProfiles(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range profiles {
		if p.Authorized && strings.TrimSpace(p.ProfileID) != "" {
			if p.Platform == "" || strings.EqualFold(p.Platform, "max") {
				return strings.TrimSpace(p.ProfileID), nil
			}
		}
	}
	return "", fmt.Errorf("нет авторизованного профиля MAX в Wappi — укажите WAPPI_PROFILE_ID")
}

func (a *App) wappiStoredProfileID(ctx context.Context) string {
	var id string
	_ = a.db.QueryRow(ctx, `
		SELECT external_ref FROM inbox_lines
		WHERE provider='wappi' AND channel='max' AND is_active AND coalesce(external_ref,'')<>''
		ORDER BY name LIMIT 1`).Scan(&id)
	return strings.TrimSpace(id)
}

type wappiProfile struct {
	ProfileID  string `json:"profile_id"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Authorized bool   `json:"authorized"`
	Platform   string `json:"platform"`
	AppStatus  string `json:"app_status"`
}

// wappiProfileWorking — сессия MAX открыта и авторизована.
func wappiProfileWorking(p wappiProfile) bool {
	if !p.Authorized {
		return false
	}
	st := strings.ToLower(strings.TrimSpace(p.AppStatus))
	return st == "" || st == "open"
}

func (a *App) wappiListProfiles(ctx context.Context) ([]wappiProfile, error) {
	body, err := a.wappiJSON(ctx, http.MethodGet, "https://wappi.pro/maxapi/profile/all/get", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Profiles []wappiProfile `json:"profiles"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Wappi profiles: bad json")
	}
	return out.Profiles, nil
}

// syncWappiLineLabels — восстановить правильный profile_id по телефону линии
// (раньше всем дублям прописали один external_ref → слали с одного номера).
func (a *App) syncWappiLineLabels(ctx context.Context) {
	if !a.cfg.WappiReady() {
		return
	}
	if !wappiLabelSyncStart(time.Now()) {
		return
	}
	defer wappiLabelSyncDone()
	profiles, err := a.wappiListProfiles(ctx)
	if err != nil {
		return
	}
	byPhone := map[string]wappiProfile{}
	for _, p := range profiles {
		if strings.TrimSpace(p.ProfileID) == "" {
			continue
		}
		if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
			continue
		}
		ph := maxDisplayPhone(p.Phone)
		if ph == "" {
			continue
		}
		byPhone[ph] = p
	}
	if len(byPhone) == 0 {
		return
	}

	rows, err := a.db.Query(ctx, `
		SELECT id::text, coalesce(external_ref,''), coalesce(operator_label,''), coalesce(name,''),
		       coalesce(owner_user_id::text,'')
		FROM inbox_lines
		WHERE provider='wappi' AND channel='max'`)
	if err != nil {
		return
	}
	defer rows.Close()

	type row struct{ id, ref, op, name, owner string }
	var list []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.ref, &r.op, &r.name, &r.owner) != nil {
			continue
		}
		list = append(list, r)
	}

	// profile → line id (кто уже занял этот профиль)
	used := map[string]string{}

	for _, ln := range list {
		hint := strings.ReplaceAll(ln.name, "(дубль)", "")
		hint = strings.TrimSpace(hint)
		hint = strings.TrimPrefix(hint, "MAX · ")
		hint = strings.TrimSpace(hint)
		// Сначала телефон из имени (до «починки» ярлыка), иначе operator_label.
		phone := maxDisplayPhone(hint)
		if phone == "" {
			phone = maxDisplayPhone(ln.op)
		}
		p, ok := byPhone[phone]
		if !ok {
			continue
		}
		pid := strings.TrimSpace(p.ProfileID)
		realPhone := maxDisplayPhone(p.Phone)
		if realPhone == "" {
			realPhone = phone
		}
		label := "MAX · " + realPhone

		if prev, taken := used[pid]; taken && prev != ln.id {
			// этот профиль уже привязан к другой линии — гасим дубль
			_, _ = a.db.Exec(ctx, `UPDATE inbox_threads SET line_id=$2::uuid WHERE line_id=$1::uuid`, ln.id, prev)
			_, _ = a.db.Exec(ctx, `
				UPDATE inbox_lines SET is_active=false,
					name = CASE WHEN name LIKE '%(дубль)%' THEN name ELSE name || ' (дубль)' END
				WHERE id=$1::uuid`, ln.id)
			continue
		}
		used[pid] = ln.id
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_lines SET
				external_ref=$2,
				operator_label=$3,
				name=$4,
				is_active=true
			WHERE id=$1::uuid`, ln.id, pid, realPhone, label)
	}
}

func (a *App) wappiJSON(ctx context.Context, method, rawURL string, payload any) ([]byte, error) {
	var rdr io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", a.cfg.WappiToken)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cli := &http.Client{Timeout: 20 * time.Second}
	res, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Wappi недоступен")
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 240 {
			msg = msg[:240]
		}
		if msg == "" {
			msg = res.Status
		}
		return nil, fmt.Errorf("Wappi %s: %s", res.Status, msg)
	}
	return body, nil
}

// appendWappiMaxNumbers добавляет авторизованные профили Wappi MAX в список номеров.
func (a *App) appendWappiMaxNumbers(ctx context.Context, out *[]maxNumberItem, seen map[string]bool) {
	if !a.cfg.WappiReady() {
		return
	}
	profiles, err := a.wappiListProfiles(ctx)
	if err != nil {
		return
	}
	for _, p := range profiles {
		if !p.Authorized || strings.TrimSpace(p.ProfileID) == "" {
			continue
		}
		if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
			continue
		}
		if seen[p.ProfileID] {
			continue
		}
		seen[p.ProfileID] = true
		phone := maxDisplayPhone(p.Phone)
		label := strings.TrimSpace(p.Name)
		if label == "" {
			label = "MAX"
		} else if !strings.Contains(strings.ToLower(label), "max") {
			label = "MAX · " + label
		}
		*out = append(*out, maxNumberItem{
			ID: p.ProfileID, ChannelID: p.ProfileID, Phone: phone, Label: label,
		})
	}
}

type maxNumberItem struct {
	ID        string `json:"id"`
	LineID    string `json:"line_id"`
	ChannelID string `json:"channel_id"`
	Phone     string `json:"phone"`
	Label     string `json:"label"`
	Entities  int    `json:"entities"`
}

// listMaxNumbers — профили Wappi MAX + линии для модалки «Написать в MAX».
// Полный каталог Wappi — только admin/РОП. Менеджер видит свои линии, номер аккаунта открыт.
func (a *App) listMaxNumbers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := []maxNumberItem{}
	seen := map[string]bool{}
	manage := canManageStaff(r)

	q := `
		SELECT l.id::text, coalesce(l.external_ref,''), coalesce(l.name,''), coalesce(l.operator_label,''),
		       (SELECT count(*)::int FROM inbox_threads t
		         WHERE t.line_id=l.id
		           AND EXISTS (
		             SELECT 1 FROM inbox_messages m
		             WHERE m.thread_id=t.id
		               AND length(trim(coalesce(m.body,''))) > 0
		               AND m.body <> '__DEMO_PASSPORT__'
		           )
		       )
		FROM inbox_lines l
		WHERE l.channel='max' AND l.is_active AND l.provider='wappi'`
	args := []any{}
	if !manage {
		q += ` AND l.owner_user_id=$1::uuid`
		args = append(args, actorID(r))
	}
	q += ` ORDER BY l.name`

	rows, err := a.db.Query(ctx, q, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, ref, name, opLabel string
			var n int
			if rows.Scan(&id, &ref, &name, &opLabel, &n) != nil {
				continue
			}
			ref = strings.TrimSpace(ref)
			phone := maxDisplayPhone(opLabel)
			if phone == "" {
				phone = maxDisplayPhone(name)
			}
			label := strings.TrimSpace(name)
			if label == "" {
				label = strings.TrimSpace(opLabel)
			}
			if label == "" {
				label = "Wappi MAX"
			}
			if !strings.Contains(strings.ToLower(label), "max") {
				label = "MAX · " + label
			}
			if !manage {
				ref = ""
			}
			if ref != "" {
				seen[ref] = true
			}
			out = append(out, maxNumberItem{
				ID: id, LineID: id, ChannelID: ref, Phone: phone, Label: label, Entities: n,
			})
		}
	}

	if canBindMax(r) {
		a.appendWappiMaxNumbers(ctx, &out, seen)
	}
	writeJSON(w, 200, map[string]any{
		"items": out, "wappi": canBindMax(r) && a.cfg.WappiReady(), "can_manage": canBindMax(r),
	})
}

// maxNormPhone — телефон MAX: строго 7XXXXXXXXXX (11 цифр).
// Маски вроде 7••••••6602 после digitsOnly дают «76602» — их отбрасываем.
func maxNormPhone(s string) string {
	d := digitsOnly(s)
	if len(d) == 11 && d[0] == '8' {
		d = "7" + d[1:]
	}
	if len(d) == 10 {
		d = "7" + d
	}
	if len(d) == 11 && d[0] == '7' {
		return d
	}
	return ""
}

func maxDisplayPhone(s string) string {
	d := maxNormPhone(s)
	if len(d) == 11 && d[0] == '7' {
		return d
	}
	return ""
}
