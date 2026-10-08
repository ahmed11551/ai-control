package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// exolveCallbackEventsHook принимает POST с ЛК Exolve → «Уведомления о событиях» (Callback).
// URL: https://<host>/webhooks/exolve/callback/<CALL_WEBHOOK_SECRET>
//
// Реальный формат Exolve (не GetInfo-status):
//
//	{"type":"b"|"o"|"s"|"h"|"d"|"e", "call_id":"…", "event_side":"A"|"B",
//	 "to":"…", "hangup_cause":"USER_BUSY", "talk_time":31120, …}
//
// b=begin, o=originate/ring, s=start/answered, h=hangup, d=disconnect details, e=end.
func (a *App) exolveCallbackEventsHook(w http.ResponseWriter, r *http.Request) {
	if !a.checkCallHook(r) {
		writeJSON(w, 403, map[string]string{"error": "секрет вебхука не совпал"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "тело запроса"})
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO inbound_webhooks (source, payload) VALUES ('exolve_callback', $1::jsonb)`,
		string(body))

	events := parseExolveCallEvents(body)
	if len(events) == 0 {
		autodialLogf("webhook пустой/неизвестный payload bytes=%d", len(body))
		writeJSON(w, 200, map[string]any{"ok": true, "matched": 0})
		return
	}
	matched := 0
	for _, ev := range events {
		if ev.Type == "crr" {
			peers := exolveEventPeerPhones(ev)
			if a.attachExolveRecordingRef(r.Context(), ev.CallID, ev.Path, peers, ev.RecordDuration) {
				matched++
			}
			continue
		}
		a.stampExolveCallIDOnCalls(r.Context(), ev)
		if a.applyExolveCallbackEvent(r.Context(), ev) {
			matched++
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "matched": matched, "events": len(events)})
}

type exolveCallEvent struct {
	CallID         string
	Type           string // b/o/s/h/d/e/crr или GetInfo-status
	Status         string // нормализованный: ringing/answered/busy/…
	EventSide      string // A=кандидат, B=оператор
	EndedAt        string
	StartedAt      string
	TalkTimeMs     int
	HangupCause    string
	CauseCode      string
	Source         string
	Destination    string
	RequestDesc    string
	RawDetail      string
	Path           string // crr: URL скачивания записи
	ExpiresAt      string
	RecordDuration int
	NumberA        string
	CalledNumber   string
	CallingNumber  string
}

func parseExolveCallEvents(body []byte) []exolveCallEvent {
	trim := bytesTrimSpace(body)
	if len(trim) == 0 {
		return nil
	}
	var objs []map[string]any
	switch trim[0] {
	case '[':
		_ = json.Unmarshal(trim, &objs)
	case '{':
		var one map[string]any
		if err := json.Unmarshal(trim, &one); err == nil {
			for _, key := range []string{"events", "calls", "data", "call"} {
				if arr, ok := one[key].([]any); ok && len(arr) > 0 {
					for _, item := range arr {
						if m, ok := item.(map[string]any); ok {
							objs = append(objs, m)
						}
					}
					break
				}
			}
			if len(objs) == 0 {
				objs = append(objs, one)
			}
		}
	}
	out := make([]exolveCallEvent, 0, len(objs))
	for _, raw := range objs {
		flat := flattenHook(raw)
		ev := exolveCallEvent{
			CallID:         exolveEventString(flat, "call_id", "CallId", "uid"),
			Type:           strings.ToLower(exolveEventString(flat, "type")),
			Status:         strings.ToLower(exolveEventString(flat, "status", "Status", "call_status")),
			EventSide:      strings.ToUpper(exolveEventString(flat, "event_side", "side")),
			EndedAt:        exolveEventString(flat, "ended", "ended_at", "end_time", "EndTime"),
			StartedAt:      exolveEventString(flat, "started", "started_at", "start_time"),
			TalkTimeMs:     firstInt(flat, "talk_time"),
			HangupCause:    strings.ToUpper(exolveEventString(flat, "hangup_cause", "HangupCause")),
			CauseCode:      exolveEventString(flat, "cause_code", "CauseCode"),
			Source:         exolveEventString(flat, "from", "source", "cli"),
			Destination:    exolveEventString(flat, "to", "destination", "dst"),
			RequestDesc:    exolveEventString(flat, "request_description", "description"),
			Path:           exolveEventString(flat, "path"),
			ExpiresAt:      exolveEventString(flat, "expires_at"),
			RecordDuration: firstInt(flat, "record_duration", "duration"),
			NumberA:        exolveEventString(flat, "number_a", "numberA"),
			CalledNumber:   exolveEventString(flat, "called_number", "calledNumber"),
			CallingNumber:  exolveEventString(flat, "calling_number", "callingNumber"),
		}
		if ev.CallID == "" && ev.Type == "" && ev.Status == "" {
			continue
		}
		if ev.Type == "crr" {
			ev.Status = "call_record_ready"
			ev.RawDetail = fmt.Sprintf("webhook type=crr uid=%s path=%q dur=%d",
				ev.CallID, ev.Path, ev.RecordDuration)
			out = append(out, ev)
			continue
		}
		if ev.Status == "" {
			ev.Status = mapExolveCallbackType(ev)
		}
		ev.RawDetail = fmt.Sprintf("webhook type=%s side=%s status=%s cause=%s talk_ms=%d ended=%q to=%s",
			ev.Type, ev.EventSide, ev.Status, ev.HangupCause, ev.TalkTimeMs, ev.EndedAt, ev.Destination)
		out = append(out, ev)
	}
	return out
}

// mapExolveCallbackType переводит letter-type + hangup_cause в наши статусы.
func mapExolveCallbackType(ev exolveCallEvent) string {
	switch ev.Type {
	case "b":
		return "queued"
	case "o":
		return "ringing"
	case "s":
		return "answered" // старт разговора на ноге
	case "h":
		return "hangup" // промежуточный сигнал, ждём d/e
	case "e":
		return "completed"
	case "d":
		return mapExolveHangupCause(ev.HangupCause, ev.CauseCode, ev.TalkTimeMs)
	case "crr":
		return "call_record_ready"
	default:
		// уже status-стиль (answered/busy/…) или неизвестно
		return ev.Status
	}
}

func mapExolveHangupCause(cause, code string, talkMs int) string {
	cause = strings.ToUpper(strings.TrimSpace(cause))
	code = strings.TrimSpace(code)
	switch cause {
	case "AMD", "MACHINE", "ANSWERING_MACHINE", "VOICEMAIL":
		// Детектор автоответчика — не «дозвон», а недозвон.
		return "no_answer"
	case "USER_BUSY":
		return "busy"
	case "NO_ANSWER", "NO_USER_RESPONSE", "RECOVERY_ON_TIMER_EXPIRE":
		return "no_answer"
	case "CALL_REJECTED", "USER_REJECTED":
		return "rejected"
	case "ORIGINATOR_CANCEL", "NORMAL_UNSPECIFIED":
		if talkMs > 500 {
			return "connected"
		}
		return "canceled"
	case "NORMAL_CLEARING":
		if talkMs > 500 {
			return "connected"
		}
		// 16 без talk — часто сброс после гудков
		return "no_answer"
	}
	switch code {
	case "17":
		return "busy"
	case "19", "18", "480":
		return "no_answer"
	case "21":
		return "rejected"
	case "16":
		if talkMs > 500 {
			return "connected"
		}
		return "no_answer"
	}
	if talkMs > 500 {
		return "connected"
	}
	if cause != "" {
		return "failed"
	}
	return "completed"
}

func isExolveAmdCause(cause string) bool {
	c := strings.ToUpper(strings.TrimSpace(cause))
	return c == "AMD" || c == "MACHINE" || c == "ANSWERING_MACHINE" || c == "VOICEMAIL" ||
		strings.Contains(c, "AMD")
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func exolveEventString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				return s
			}
		case float64:
			return strconv.FormatInt(int64(t), 10)
		case json.Number:
			return strings.TrimSpace(t.String())
		default:
			s := strings.TrimSpace(fmt.Sprint(t))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func (a *App) applyExolveCallbackEvent(ctx context.Context, ev exolveCallEvent) bool {
	callID := strings.TrimSpace(ev.CallID)
	status := strings.ToLower(strings.TrimSpace(ev.Status))
	if callID == "" {
		return false
	}

	attemptID, dealID, ok := a.findAutodialAttempt(ctx, callID, ev.RequestDesc)
	if !ok {
		autodialLogf("webhook miss call=%s type=%s status=%s", callID, ev.Type, status)
		return false
	}
	autodialLogf("webhook гудки deal=%s call=%s type=%s side=%s → %s cause=%s",
		dealID, callID, ev.Type, ev.EventSide, status, ev.HangupCause)

	detail := ev.RawDetail
	side := strings.ToUpper(ev.EventSide)

	switch status {
	case "answered", "talk":
		if side == "B" {
			a.maybeClaimAutodialFromWebhook(ctx, attemptID, callID, dealID, ev.Destination)
			var acceptedBy string
			_ = a.db.QueryRow(ctx, `
				SELECT coalesce(accepted_by::text,'') FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&acceptedBy)
			payload := map[string]any{"deal_id": dealID, "call_id": callID}
			if acceptedBy != "" {
				payload["user_id"] = acceptedBy
			}
			a.hub.Broadcast("autodial.accepted", payload)
			a.hub.Broadcast("autodial.taken", payload)
			_, _ = a.db.Exec(ctx, `
				UPDATE autodial_attempts SET status='connected'
				WHERE id=$1::uuid AND finished_at IS NULL AND status IN ('started','ringing')`, attemptID)
			_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='connected' WHERE id=$1::uuid`, dealID)
			autodialLogf("webhook менеджер ответил deal=%s call=%s user=%s", dealID, callID, acceptedBy)
			return true
		}
		a.autodialOfferFromWebhook(ctx, dealID, attemptID, callID, status)
		return true

	case "ringing", "initiated", "queued", "modified":
		return true

	case "hangup":
		if side == "A" {
			var st string
			_ = a.db.QueryRow(ctx, `SELECT status FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&st)
			st = strings.ToLower(strings.TrimSpace(st))
			if st == "started" || st == "ringing" {
				autodialLogf("webhook кандидат hangup side=A pre-offer deal=%s call=%s", dealID, callID)
				a.finishAutodial(dealID, attemptID, callID, "canceled", "candidate hangup side=A pre-offer")
			}
		}
		return true

	case "busy", "rejected", "no_answer", "canceled", "error", "failed":
		cause := strings.ToUpper(strings.TrimSpace(ev.HangupCause))
		// Parallel: проигравший / битый SIP (LOSE_RACE, UNALLOCATED) — не рвём весь скорозвон.
		if side == "B" && (cause == "LOSE_RACE" || cause == "CALL_REJECTED" || cause == "UNALLOCATED_NUMBER") {
			autodialLogf("webhook side=B race/bad-sip ignore cause=%s deal=%s call=%s to=%s",
				cause, dealID, callID, ev.Destination)
			if cause == "UNALLOCATED_NUMBER" {
				dest := strings.TrimSpace(ev.Destination)
				if dest != "" {
					if tag, err := a.db.Exec(ctx, `
						UPDATE exolve_sips SET active=false, updated_at=now()
						WHERE sip_login=$1 AND active`, dest); err == nil && tag.RowsAffected() > 0 {
						autodialLogf("SIP %s отключён (UNALLOCATED_NUMBER) — больше не в line_2", dest)
					}
				}
			}
			return true
		}
		if side == "B" {
			var acceptedBy, st string
			_ = a.db.QueryRow(ctx, `
				SELECT coalesce(accepted_by::text,''), coalesce(status,'')
				FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&acceptedBy, &st)
			st = strings.ToLower(strings.TrimSpace(st))
			// После offer (connected) или claim — terminal на другом SIP игнорируем.
			if acceptedBy != "" || st == "connected" {
				autodialLogf("webhook side=B terminal after offer/claim ignore status=%s cause=%s deal=%s to=%s",
					status, cause, dealID, ev.Destination)
				return true
			}
		}
		fin := status
		if isExolveAmdCause(ev.HangupCause) {
			fin = "no_answer"
			autodialLogf("webhook AMD machine → no_answer deal=%s call=%s side=%s", dealID, callID, side)
		} else if side == "A" && (status == "busy" || status == "rejected") {
			fin = "no_answer"
		}
		if side == "B" && status == "busy" && !isExolveAmdCause(ev.HangupCause) {
			fin = "busy"
		}
		autodialLogf("webhook terminal side=%s → %s deal=%s call=%s", side, fin, dealID, callID)
		a.finishAutodial(dealID, attemptID, callID, fin, detail)
		return true

	case "connected", "completed":
		if isExolveAmdCause(ev.HangupCause) {
			autodialLogf("webhook AMD machine (connected/completed) → no_answer deal=%s call=%s", dealID, callID)
			a.finishAutodial(dealID, attemptID, callID, "no_answer", detail)
			return true
		}
		var cur, acceptedBy string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(status,''), coalesce(accepted_by::text,'')
			FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&cur, &acceptedBy)
		cur = strings.ToLower(strings.TrimSpace(cur))
		if acceptedBy != "" || ev.TalkTimeMs > 500 {
			a.finishAutodial(dealID, attemptID, callID, "connected", detail)
			return true
		}
		if status == "connected" {
			autodialLogf("webhook connected without claim — wait deal=%s call=%s", dealID, callID)
			return true
		}
		if ev.Type == "e" || strings.TrimSpace(ev.EndedAt) != "" {
			a.finishAutodial(dealID, attemptID, callID, "no_answer", detail)
		}
		return true

	default:
		autodialLogf("webhook unknown type=%s status=%s deal=%s", ev.Type, status, dealID)
		return true
	}
}

func (a *App) autodialOfferFromWebhook(ctx context.Context, dealID, attemptID, callID, status string) {
	if !autodialFeatureEnabled() {
		return
	}
	// Только первый переход started/ringing → connected — иначе бар снова после Отклонить.
	tag, err := a.db.Exec(ctx, `
		UPDATE autodial_attempts SET status='connected'
		WHERE id=$1::uuid AND finished_at IS NULL AND status IN ('started','ringing')`,
		attemptID)
	if err != nil || tag.RowsAffected() == 0 {
		autodialLogf("webhook offer skip (уже connected/finished) deal=%s call=%s", dealID, callID)
		return
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE deals SET autodial_status='connected' WHERE id=$1 AND autodial_status IN ('dialing','ringing')`,
		dealID)
	var title, cli string
	_ = a.db.QueryRow(ctx, `SELECT title FROM deals WHERE id=$1`, dealID).Scan(&title)
	_ = a.db.QueryRow(ctx, `SELECT coalesce(number_code,'') FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&cli)
	a.hub.Broadcast("autodial.offer", map[string]any{
		"deal_id": dealID,
		"title":   title,
		"call_id": callID,
		"status":  status,
		"cli":     cli,
	})
	a.broadcastDealUpdated(ctx, dealID, map[string]any{"autodial_status": "connected"})
	autodialLogf("webhook ответ A → offer (бар CRM ждёт AMD) deal=%s call=%s status=%s", dealID, callID, status)
	a.logDealLeadAct(ctx, dealID, "", "autodial_offered", map[string]any{
		"call_id": callID,
		"status":  status,
		"cli":     cli,
	})
	a.scheduleAutodialPrompt(dealID, attemptID, callID, title, cli)
}

// maybeClaimAutodialFromWebhook — если FE не успел claim, фиксируем accepted_by по SIP destination.
func (a *App) maybeClaimAutodialFromWebhook(ctx context.Context, attemptID, callID, dealID, destSIP string) {
	if !autodialFeatureEnabled() {
		return
	}
	var already string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(accepted_by::text,'') FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&already)
	if already != "" {
		return
	}
	sip := strings.TrimSpace(destSIP)
	if sip == "" {
		return
	}
	var uid string
	_ = a.db.QueryRow(ctx, `
		SELECT u.id::text
		FROM exolve_sips es
		JOIN users u ON u.id=es.user_id AND u.is_active=true
		JOIN roles r ON r.id=u.role_id
		WHERE es.active AND es.sip_login <> ''
		  AND regexp_replace(es.sip_login,'[^0-9]','','g') = regexp_replace($1,'[^0-9]','','g')
		  AND lower(r.name) IN ('manager','sales')
		LIMIT 1`, sip).Scan(&uid)
	if uid == "" {
		return
	}
	tag, err := a.db.Exec(ctx, `
		UPDATE autodial_attempts
		SET accepted_by=$2::uuid, accepted_at=now()
		WHERE id=$1::uuid AND finished_at IS NULL AND accepted_by IS NULL`, attemptID, uid)
	if err != nil || tag.RowsAffected() == 0 {
		return
	}
	autodialLogf("webhook claim by SIP user=%s deal=%s call=%s", uid, dealID, callID)
	a.logDealLeadAct(ctx, dealID, uid, "autodial_claimed", map[string]any{
		"call_id":    callID,
		"attempt_id": attemptID,
		"manager":    a.userFullName(ctx, uid),
		"manager_id": uid,
		"via":        "webhook_sip",
	})
	a.hub.Broadcast("autodial.taken", map[string]any{
		"deal_id": dealID,
		"call_id": callID,
		"user_id": uid,
	})
}

// scheduleAutodialPrompt — FE-бар «Ответить» только если attempt жив после окна AMD.
// Не таймер на клиенте: при cause=AMD finishAutodial уже закрыл attempt → prompt не шлём.
func (a *App) scheduleAutodialPrompt(dealID, attemptID, callID, title, cli string) {
	go func() {
		time.Sleep(5 * time.Second)
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var finished bool
		err := a.db.QueryRow(cctx, `
			SELECT finished_at IS NOT NULL FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&finished)
		if err != nil || finished {
			autodialLogf("prompt skip (AMD/finish) deal=%s call=%s", dealID, callID)
			return
		}
		a.hub.Broadcast("autodial.prompt", map[string]any{
			"deal_id": dealID,
			"title":   title,
			"call_id": callID,
			"cli":     cli,
		})
		autodialLogf("prompt OK deal=%s call=%s — показать Ответить", dealID, callID)
	}()
}

func (a *App) findAutodialAttempt(ctx context.Context, callID, requestDesc string) (attemptID, dealID string, ok bool) {
	callID = strings.TrimSpace(callID)
	if callID != "" {
		// Только живой attempt — late webhook после dismiss не поднимает бар.
		err := a.db.QueryRow(ctx, `
			SELECT id::text, deal_id::text FROM autodial_attempts
			WHERE exolve_call_id=$1 AND finished_at IS NULL
			ORDER BY started_at DESC LIMIT 1`, callID).Scan(&attemptID, &dealID)
		if err == nil && attemptID != "" {
			return attemptID, dealID, true
		}
	}
	desc := strings.TrimSpace(requestDesc)
	if strings.HasPrefix(desc, "deal:") {
		did := strings.TrimSpace(strings.TrimPrefix(desc, "deal:"))
		if did != "" {
			err := a.db.QueryRow(ctx, `
				SELECT id::text, deal_id::text FROM autodial_attempts
				WHERE deal_id=$1::uuid AND finished_at IS NULL
				ORDER BY started_at DESC LIMIT 1`, did).Scan(&attemptID, &dealID)
			if err == nil && attemptID != "" {
				return attemptID, dealID, true
			}
		}
	}
	return "", "", false
}
