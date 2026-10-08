package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgecrm/server/internal/config"
	"github.com/go-chi/chi/v5"
)

type pbxHub struct {
	cfg  config.Config
	http *http.Client
	mu   sync.Mutex
	tok  string
	exp  time.Time
	ext  map[string]int
}

func newPBX(cfg config.Config) *pbxHub {
	return &pbxHub{
		cfg:  cfg,
		http: &http.Client{Timeout: 20 * time.Second},
		ext:  map[string]int{},
	}
}

func (p *pbxHub) status() map[string]any {
	return map[string]any{
		"telfin":  p.cfg.TelfinReady(),
		"uis":     p.cfg.UISReady(),
		"ami":     p.cfg.AMIReady(),
		"hook":    p.cfg.CallWebhookSecret != "",
		"active":  p.activeName(),
		"missing": p.missingHint(),
	}
}

func (p *pbxHub) dialName() string {
	want := p.cfg.CallProvider
	if want == "telfin" && p.cfg.TelfinReady() {
		return "telfin"
	}
	if want == "uis" && p.cfg.UISReady() {
		return "uis"
	}
	if want == "ami" && p.cfg.AMIReady() {
		return "ami"
	}
	if p.cfg.TelfinReady() {
		return "telfin"
	}
	if p.cfg.UISReady() {
		return "uis"
	}
	if p.cfg.AMIReady() {
		return "ami"
	}
	return ""
}

func (p *pbxHub) activeName() string {
	if n := p.dialName(); n != "" {
		return n
	}
	if p.cfg.CallWebhookSecret != "" {
		return "webhook"
	}
	return ""
}

func (p *pbxHub) missingHint() string {
	if p.activeName() != "" {
		return ""
	}
	if p.cfg.CallWebhookSecret != "" {
		return ""
	}
	return "В .env задайте CALL_WEBHOOK_SECRET — журнал звонков заработает без ключей МТС. Исходящие из карточки — когда дадут кабинет АТС. Ключи в чат не кладите."
}

func (p *pbxHub) ready() bool { return p.activeName() != "" }

func (a *App) originate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To        string `json:"to"`
		DealID    string `json:"deal_id"`
		LeadID    string `json:"lead_id"`
		TaskID    string `json:"task_id"`
		ContactID string `json:"contact_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	uid := actorID(r)
	var can bool
	var ext, destPolicy string
	if err := a.db.QueryRow(r.Context(), `SELECT can_call, coalesce(phone_extension,''), allowed_call_dest FROM users WHERE id=$1`, uid).
		Scan(&can, &ext, &destPolicy); err != nil || !can || ext == "" {
		writeJSON(w, 403, map[string]string{"error": "нет права звонить или не задан внутренний номер"})
		return
	}
	to := e164RU(in.To)
	if to == "" && (in.LeadID != "" || in.ContactID != "" || in.DealID != "") {
		// Менеджер звонит по сущности без видимого номера.
		var phone string
		scope := a.crmScope(r)
		ctx := r.Context()
		if in.LeadID != "" && a.canSeeLead(ctx, scope, in.LeadID) {
			_ = a.db.QueryRow(ctx, `
				SELECT coalesce(c.phone,'') FROM leads l LEFT JOIN contacts c ON c.id=l.contact_id
				WHERE l.id=$1`, in.LeadID).Scan(&phone)
		} else if in.DealID != "" && a.canSeeDeal(ctx, scope, in.DealID) {
			_ = a.db.QueryRow(ctx, `
				SELECT coalesce(c.phone,'') FROM deals d LEFT JOIN contacts c ON c.id=d.contact_id
				WHERE d.id=$1`, in.DealID).Scan(&phone)
		} else if in.ContactID != "" && a.contactVisible(ctx, scope, in.ContactID) {
			_ = a.db.QueryRow(ctx, `SELECT coalesce(phone,'') FROM contacts WHERE id=$1`, in.ContactID).Scan(&phone)
		}
		to = e164RU(phone)
	}
	if to == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите номер"})
		return
	}
	if destPolicy == "internal" && len(digitsOnly(to)) > 4 {
		writeJSON(w, 403, map[string]string{"error": "внешние звонки для этой роли запрещены"})
		return
	}
	provider := a.pbx.dialName()
	if provider == "" {
		writeJSON(w, 503, map[string]string{"error": "исходящие ещё без кабинета АТС. Входящие уже пишутся в журнал, если задан CALL_WEBHOOK_SECRET"})
		return
	}
	var err error
	switch provider {
	case "telfin":
		err = a.pbx.telfinCallback(r.Context(), ext, to)
	case "uis":
		err = a.pbx.uisCall(r.Context(), ext, to)
	case "ami":
		if a.ami == nil {
			err = fmt.Errorf("AMI выключена")
			break
		}
		err = a.ami.Originate("PJSIP/"+ext, to, ext, map[string]string{
			"CRM_USER_ID": uid, "DEAL_ID": in.DealID, "LEAD_ID": in.LeadID,
		})
	}
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": provider + ": " + err.Error()})
		return
	}
	ent, eid := "contact", in.ContactID
	if in.DealID != "" {
		ent, eid = "deal", in.DealID
	} else if in.LeadID != "" {
		ent, eid = "lead", in.LeadID
	} else if in.TaskID != "" {
		ent, eid = "task", in.TaskID
	}
	a.logAct(r.Context(), ent, eid, uid, "originate", map[string]any{"to": in.To, "provider": provider})
	_, _ = a.upsertCall(r.Context(), callEvent{
		UID:       "orig:" + provider + ":" + uid + ":" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Provider:  provider,
		Direction: "outbound",
		From:      ext,
		To:        to,
		UserID:    uid,
		Entity:    ent,
		EntityID:  eid,
		Disp:      "ORIGINATED",
	})
	writeJSON(w, 202, map[string]any{"ok": true, "provider": provider, "extension": ext, "to": to})
}

func (p *pbxHub) telfinToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.tok != "" && time.Now().Before(p.exp) {
		tok := p.tok
		p.mu.Unlock()
		return tok, nil
	}
	p.mu.Unlock()
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {p.cfg.TelfinAppID},
		"client_secret": {p.cfg.TelfinAppSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://apiproxy.telphin.ru/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := p.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("oauth %d", res.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("oauth: нет токена")
	}
	ttl := out.ExpiresIn
	if ttl <= 0 {
		ttl = 3000
	}
	p.mu.Lock()
	p.tok = out.AccessToken
	p.exp = time.Now().Add(time.Duration(ttl-60) * time.Second)
	p.mu.Unlock()
	return out.AccessToken, nil
}

func (p *pbxHub) telfinGet(ctx context.Context, path string, dest any) error {
	tok, err := p.telfinToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://apiproxy.telphin.ru/api/ver1.0"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("telfin %s: %d", path, res.StatusCode)
	}
	return json.Unmarshal(body, dest)
}

func (p *pbxHub) telfinExtID(ctx context.Context, name string) (int, error) {
	name = strings.TrimSpace(name)
	p.mu.Lock()
	if id, ok := p.ext[name]; ok {
		p.mu.Unlock()
		return id, nil
	}
	p.mu.Unlock()
	var user map[string]any
	if err := p.telfinGet(ctx, "/user/", &user); err != nil {
		return 0, err
	}
	clientID := firstInt(user, "client_id", "id")
	if clientID == 0 {
		return 0, fmt.Errorf("не удалось получить client_id Телфин")
	}
	var list []map[string]any
	if err := p.telfinGet(ctx, fmt.Sprintf("/client/%d/extension/", clientID), &list); err != nil {
		return 0, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, row := range list {
		n := strings.TrimSpace(fmt.Sprint(row["name"]))
		id := firstInt(row, "id")
		if n != "" && id > 0 {
			p.ext[n] = id
		}
	}
	if id, ok := p.ext[name]; ok {
		return id, nil
	}
	return 0, fmt.Errorf("внутренний %s не найден в Телфин", name)
}

func (p *pbxHub) telfinCallback(ctx context.Context, ext, dest string) error {
	id, err := p.telfinExtID(ctx, ext)
	if err != nil {
		return err
	}
	tok, err := p.telfinToken(ctx)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"dst_num": dest,
		"src_num": []string{ext},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("https://apiproxy.telphin.ru/api/ver1.0/extension/%d/callback/", id),
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	res, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("callback %d", res.StatusCode)
	}
	var wr struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &wr)
	if wr.Error != "" {
		return fmt.Errorf("%s", wr.Error)
	}
	return nil
}

func (p *pbxHub) uisCall(ctx context.Context, ext, dest string) error {
	params := map[string]any{
		"access_token": p.cfg.UISToken,
		"first_call":   "operator",
		"contact":      dest,
		"operator":     ext,
	}
	if p.cfg.UISVirtualPhone != "" {
		params["virtual_phone_number"] = p.cfg.UISVirtualPhone
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "crm",
		"method":  "start.informer_call",
		"params":  params,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://callapi.uiscom.ru/v4.0", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("uis %d", res.StatusCode)
	}
	var wr struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &wr)
	if wr.Error != nil && wr.Error.Message != "" {
		return fmt.Errorf("%s", wr.Error.Message)
	}
	return nil
}

type callEvent struct {
	UID, Provider, Direction, From, To, UserID, Entity, EntityID, Disp string
	Duration                                                           int
}

func (a *App) upsertCall(ctx context.Context, ev callEvent) (string, error) {
	if ev.UID == "" {
		ev.UID = ev.Provider + ":" + ev.From + ":" + ev.To + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if ev.Direction == "" {
		ev.Direction = "unknown"
	}
	if ev.UserID == "" {
		ev.UserID = a.userByExt(ctx, ev.From)
		if ev.UserID == "" {
			ev.UserID = a.userByExt(ctx, ev.To)
		}
	}
	if ev.Entity == "" {
		ev.Entity, ev.EntityID = a.matchPhone(ctx, ev.From, ev.To)
	}
	var id string
	err := a.db.QueryRow(ctx, `SELECT id::text FROM calls WHERE asterisk_uniqueid=$1 LIMIT 1`, ev.UID).Scan(&id)
	if err == nil {
		_, err = a.db.Exec(ctx, `
			UPDATE calls SET duration_sec=$2, disposition=coalesce(nullif($3,''), disposition),
				ended_at=CASE WHEN $2>0 THEN now() ELSE ended_at END, provider=coalesce(provider, $4),
				entity_type=coalesce(nullif(entity_type,''), nullif($5,'')),
				entity_id=coalesce(entity_id, nullif($6,'')::uuid)
			WHERE asterisk_uniqueid=$1`, ev.UID, ev.Duration, ev.Disp, ev.Provider, ev.Entity, ev.EntityID)
		return id, err
	}
	err = a.db.QueryRow(ctx, `
		INSERT INTO calls (asterisk_uniqueid, direction, from_number, to_number, user_id, entity_type, entity_id,
			duration_sec, disposition, provider, started_at)
		VALUES ($1,$2,$3,$4,nullif($5,'')::uuid,nullif($6,''),nullif($7,'')::uuid,$8,nullif($9,''),nullif($10,''), now())
		RETURNING id::text`,
		ev.UID, ev.Direction, ev.From, ev.To, ev.UserID, ev.Entity, ev.EntityID, ev.Duration, ev.Disp, ev.Provider).Scan(&id)
	return id, err
}

func (a *App) telephonyHook(w http.ResponseWriter, r *http.Request) {
	if !a.checkCallHook(r) {
		writeJSON(w, 403, map[string]string{"error": "секрет вебхука не совпал"})
		return
	}
	provider := strings.ToLower(chi.URLParam(r, "provider"))
	body, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	raw := map[string]any{}
	_ = json.Unmarshal(body, &raw)
	if len(raw) == 0 {
		_ = r.ParseForm()
		for k, v := range r.Form {
			if len(v) > 0 {
				raw[k] = v[0]
			}
		}
	}
	if provider == "" {
		provider = "voximplant"
	}
	// Запись Voximplant (сценарий → URL) — привязать к звонку softphone.
	if recURL := recordingURLFromHook(raw); recURL != "" &&
		(provider == "voximplant" || provider == "vox" || hookLooksLikeVoxRecording(raw)) {
		dur := atoi(firstScalar(flattenHook(raw)["duration"]))
		ok := a.attachVoxRecordingRef(r.Context(), recURL, hookPeerPhones(raw), dur)
		writeJSON(w, 200, map[string]any{"ok": true, "recording": ok})
		return
	}
	ev := parseCallHook(provider, raw)
	id, dealID, err := a.recordCall(r.Context(), ev)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось записать звонок"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "call_id": id, "deal_id": dealID})
}

func (a *App) softphoneEvent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UID         string `json:"uid"`
		Provider    string `json:"provider"`
		Direction   string `json:"direction"`
		Peer        string `json:"peer"`
		Phase       string `json:"phase"`
		Duration    int    `json:"duration_sec"`
		Disposition string `json:"disposition"`
		CLI         string `json:"cli"`
		LeadID      string `json:"lead_id"`
		DealID      string `json:"deal_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный JSON"})
		return
	}
	ev, err := buildSoftphoneEvent(in.UID, in.Provider, in.Direction, in.Peer, in.Phase, in.Disposition, in.Duration, actorID(r))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if cli := normalizeExolveDID(in.CLI); cli != "" && ev.Direction == "outbound" && ev.From == "" {
		ev.From = cli
	}
	// Входящий: приоритет — карточка по номеру peer (колбэк не должен висеть на lastLead другой сделки).
	// Исходящий: доверяем lead_id/deal_id с клиента (звонок из открытой карточки).
	if ev.Direction == "inbound" {
		if ent, eid := a.matchPhoneForUser(r.Context(), ev.From, ev.To, actorID(r)); eid != "" {
			ev.Entity, ev.EntityID = ent, eid
		} else if leadID := strings.TrimSpace(in.LeadID); leadID != "" {
			ev.Entity, ev.EntityID = "lead", leadID
		} else if dealID := strings.TrimSpace(in.DealID); dealID != "" {
			ev.Entity, ev.EntityID = "deal", dealID
		}
	} else if leadID := strings.TrimSpace(in.LeadID); leadID != "" {
		ev.Entity, ev.EntityID = "lead", leadID
	} else if dealID := strings.TrimSpace(in.DealID); dealID != "" {
		ev.Entity, ev.EntityID = "deal", dealID
	}
	id, dealID, err := a.recordCall(r.Context(), ev)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось записать звонок"})
		return
	}
	// если lead/deal не пришли в end — берём уже привязанную карточку
	if ev.Entity == "" || ev.EntityID == "" {
		var ent, eid string
		_ = a.db.QueryRow(r.Context(), `
			SELECT coalesce(entity_type,''), coalesce(entity_id::text,'')
			FROM calls WHERE id=$1::uuid`, id).Scan(&ent, &eid)
		if eid != "" {
			ev.Entity, ev.EntityID = ent, eid
		}
	}
	if strings.EqualFold(strings.TrimSpace(in.Phase), "end") && ev.Entity != "" && ev.EntityID != "" {
		peer := ev.To
		if ev.Direction == "inbound" {
			peer = ev.From
		}
		title := "Исходящий звонок"
		if ev.Direction == "inbound" {
			title = "Входящий звонок"
		}
		payload := map[string]any{
			"title":       title,
			"direction":   ev.Direction,
			"peer":        peer,
			"duration":    ev.Duration,
			"disposition": ev.Disp,
			"provider":    ev.Provider,
			"call_id":     id,
		}
		var actID string
		_ = a.db.QueryRow(r.Context(), `
			SELECT id::text FROM activity_log
			WHERE entity_type=$1 AND entity_id=$2::uuid AND action='call'
			  AND payload->>'call_id'=$3
			ORDER BY created_at DESC LIMIT 1`,
			ev.Entity, ev.EntityID, id).Scan(&actID)
		if actID != "" {
			b, _ := json.Marshal(payload)
			_, _ = a.db.Exec(r.Context(), `UPDATE activity_log SET payload=$2::jsonb WHERE id=$1::uuid`, actID, string(b))
		} else {
			a.logAct(r.Context(), ev.Entity, ev.EntityID, actorID(r), "call", payload)
		}
		disp := strings.ToLower(strings.TrimSpace(ev.Disp))
		disp = strings.ReplaceAll(disp, " ", "_")
		disp = strings.ReplaceAll(disp, "-", "_")
		missed := ev.Direction == "inbound" && (disp == "missed" || disp == "no_answer" ||
			disp == "busy" || disp == "canceled" || disp == "cancelled" || disp == "rejected")
		if missed {
			uid := actorID(r)
			a.ensureMissedCallAttention(r.Context(), ev.Entity, ev.EntityID, uid, peer)
			// Уведомление только владельцу лида/сделки (не любому, кто был на softphone).
			a.notifyMissedCall(r.Context(), uid, ev.Entity, ev.EntityID, peer, id)
		}
	}
	out := map[string]any{"ok": true, "call_id": id, "deal_id": dealID}
	if ev.Entity == "lead" && ev.EntityID != "" {
		out["lead_id"] = ev.EntityID
	} else if dealID != "" {
		out["deal_id"] = dealID
	}
	writeJSON(w, 200, out)
}

func buildSoftphoneEvent(uid, provider, direction, peer, phase, disp string, duration int, userID string) (callEvent, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return callEvent{}, fmt.Errorf("нет uid звонка")
	}
	peer = strings.TrimSpace(peer)
	if peer == "" {
		return callEvent{}, fmt.Errorf("нет номера")
	}
	prov := strings.ToLower(strings.TrimSpace(provider))
	switch prov {
	case "vox", "voximplant":
		prov = "voximplant"
	case "exolve", "mts":
		prov = "exolve"
	default:
		if prov == "" {
			prov = "voximplant"
		}
	}
	dir := strings.ToLower(strings.TrimSpace(direction))
	switch {
	case dir == "inbound" || dir == "incoming" || dir == "in":
		dir = "inbound"
	case dir == "outbound" || dir == "outgoing" || dir == "out":
		dir = "outbound"
	default:
		dir = "inbound"
	}
	status := strings.ToUpper(strings.TrimSpace(disp))
	if strings.EqualFold(strings.TrimSpace(phase), "start") && status == "" {
		status = "RINGING"
	}
	ev := callEvent{
		UID:       "softphone:" + prov + ":" + uid,
		Provider:  prov,
		Direction: dir,
		UserID:    strings.TrimSpace(userID),
		Duration:  duration,
		Disp:      status,
	}
	if dir == "inbound" {
		ev.From = peer
	} else {
		ev.To = peer
	}
	return ev, nil
}

func (a *App) demoInboundCall(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только админ"})
		return
	}
	ev := callEvent{
		Provider:  "voximplant",
		UID:       "voximplant:demo:" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Direction: "inbound",
		From:      "79001234567",
		To:        "78005553535",
		Disp:      "ANSWERED",
		Duration:  18,
	}
	id, dealID, err := a.recordCall(r.Context(), ev)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось создать тестовый звонок"})
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "call_id": id, "deal_id": dealID})
}

func (a *App) recordCall(ctx context.Context, ev callEvent) (callID, dealID string, err error) {
	callID, err = a.upsertCall(ctx, ev)
	if err != nil {
		return "", "", err
	}
	dealID = a.attachCallCard(ctx, callID, ev)
	a.hub.Broadcast("call.updated", map[string]any{"id": callID, "deal_id": dealID, "provider": ev.Provider})
	eventType := "call.started"
	if ev.Duration > 0 || strings.TrimSpace(ev.Disp) != "" {
		eventType = "call.completed"
	}
	a.enqueueCallAIEventFromDB(ctx, eventType, callID, ev.UserID, int(time.Now().Unix()))
	return callID, dealID, nil
}

func (a *App) checkCallHook(r *http.Request) bool {
	want := a.cfg.CallWebhookSecret
	if want == "" {
		return false
	}
	got := chi.URLParam(r, "secret")
	if got == "" {
		got = r.URL.Query().Get("k")
	}
	if got == "" {
		got = r.Header.Get("X-Webhook-Secret")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func flattenHook(raw map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range raw {
		out[k] = v
	}
	for _, nest := range []string{"call", "data", "payload", "result", "session"} {
		m, ok := raw[nest].(map[string]any)
		if !ok {
			continue
		}
		for k, v := range m {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	return out
}

func parseCallHook(provider string, raw map[string]any) callEvent {
	raw = flattenHook(raw)
	s := func(keys ...string) string {
		for _, k := range keys {
			if v := firstScalar(raw[k]); v != "" {
				return v
			}
			for rk, rv := range raw {
				if strings.EqualFold(rk, k) {
					if v := firstScalar(rv); v != "" {
						return v
					}
				}
			}
		}
		return ""
	}
	ev := callEvent{
		Provider: provider,
		UID: s("sessionId", "session_id", "CallID", "call_id", "callid",
			"call_session_id", "entry_id", "Uniqueid", "callSessionHistoryId"),
		From: s("callerid", "callerId", "CallerIDNum", "caller_id_num", "callerid_num",
			"from", "contact_phone_number", "src_num", "src"),
		To: s("destination", "destinationNumber", "CalledNumber", "called_number",
			"CalledDID", "to", "virtual_phone_number", "dst_num", "dst"),
		Disp:     s("Status", "status", "callState", "finish_reason", "disposition"),
		Duration: atoi(s("Duration", "duration", "durationSec", "talkDuration", "talk_time")),
	}
	if ev.UID != "" {
		ev.UID = provider + ":" + ev.UID
	}
	kind := strings.ToLower(s("EventType", "event_type", "event", "direction", "callType"))
	switch {
	case strings.Contains(kind, "out") || strings.Contains(kind, "dial-out"):
		ev.Direction = "outbound"
	case strings.Contains(kind, "in") || strings.Contains(kind, "incoming") || strings.Contains(kind, "dial-in"):
		ev.Direction = "inbound"
	default:
		if len(digitsOnly(ev.From)) <= 4 {
			ev.Direction = "outbound"
		} else {
			ev.Direction = "inbound"
		}
	}
	return ev
}

func firstInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		switch t := m[k].(type) {
		case float64:
			return int(t)
		case json.Number:
			n, _ := t.Int64()
			return int(n)
		case string:
			n, _ := strconv.Atoi(t)
			return n
		case int:
			return t
		}
	}
	return 0
}

func firstScalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case []any:
		if len(t) > 0 {
			return firstScalar(t[0])
		}
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func e164RU(s string) string {
	d := digitsOnly(s)
	if d == "" {
		return ""
	}
	if len(d) == 11 && (d[0] == '8' || d[0] == '7') {
		return "7" + d[1:]
	}
	if len(d) == 10 {
		return "7" + d
	}
	return d
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
