package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var talkMsRe = regexp.MustCompile(`talk_ms=(\d+)`)

const (
	nedozvon3BitrixID = hirePipelineKey + ":5"
	autodialDIDLease  = 3 * time.Minute
)

// Общий пул для менеджеров: Недозвон 1/2/3 (без полного номера в UI).
var nedozvonPoolBitrixIDs = []string{
	hirePipelineKey + ":3",
	hirePipelineKey + ":4",
	hirePipelineKey + ":5",
}

func nedozvonPoolSQLIn() string {
	parts := make([]string, len(nedozvonPoolBitrixIDs))
	for i, id := range nedozvonPoolBitrixIDs {
		parts[i] = "'" + id + "'"
	}
	return strings.Join(parts, ",")
}

func (a *App) maybeStartNedozvonAutodial(ctx context.Context, dealID, stageID string) {
	if !autodialFeatureEnabled() {
		return
	}
	var bid string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipeline_stages WHERE id=$1`, stageID).Scan(&bid)
	if bid != nedozvon3BitrixID {
		return
	}
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := a.startNedozvonAutodial(cctx, dealID); err != nil {
			autodialLogf("start FAIL deal=%s: %v", dealID, err)
		}
	}()
}

func (a *App) startNedozvonAutodial(ctx context.Context, dealID string) error {
	if !autodialFeatureEnabled() {
		return fmt.Errorf("%s", autodialPausedMsg)
	}
	if a.cfg.ExolveAPIKey == "" {
		return fmt.Errorf("нет EXOLVE_API_KEY")
	}
	retryMin := a.cfg.AutodialRetryMinutes
	if retryMin <= 0 {
		retryMin = 30
	}

	var recent string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM autodial_attempts
		WHERE deal_id=$1::uuid AND status IN ('started','ringing','connected')
		  AND started_at > now() - make_interval(mins => $2)
		LIMIT 1`, dealID, retryMin).Scan(&recent)
	if recent != "" {
		autodialLogf("skip active deal=%s attempt=%s", dealID, recent)
		return nil
	}
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM autodial_attempts
		WHERE deal_id=$1::uuid AND status='started'
		  AND started_at > now() - interval '2 minutes'
		LIMIT 1`, dealID).Scan(&recent)
	if recent != "" {
		return nil
	}

	var title, phone string
	err := a.db.QueryRow(ctx, `
		SELECT d.title, coalesce(c.phone,'')
		FROM deals d LEFT JOIN contacts c ON c.id=d.contact_id
		WHERE d.id=$1`, dealID).Scan(&title, &phone)
	if err != nil {
		return fmt.Errorf("сделка не найдена")
	}
	phone = e164RU(phone)
	if phone == "" {
		_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='no_phone' WHERE id=$1`, dealID)
		return fmt.Errorf("нет телефона кандидата")
	}

	sips := a.autodialManagerSIPs(ctx)
	if len(sips) == 0 {
		_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='no_operators' WHERE id=$1`, dealID)
		return fmt.Errorf("нет менеджеров с SIP Exolve")
	}
	if len(sips) > 10 {
		sips = sips[:10]
	}

	// Не стартовать второй MakeCallback, пока оператор уже в разговоре скорозвона
	// (иначе line_2 → USER_BUSY и FE/SIP рвут текущий звонок).
	var opBusy bool
	_ = a.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM autodial_attempts
			WHERE finished_at IS NULL
			  AND status = 'connected'
			  AND started_at > now() - interval '30 minutes'
		)`).Scan(&opBusy)
	if opBusy {
		autodialLogf("skip dial deal=%s: оператор уже в разговоре (connected)", dealID)
		return fmt.Errorf("оператор уже в разговоре скорозвона")
	}

	numberCode, err := a.pickAutodialNumberCode(ctx)
	if err != nil {
		_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='error' WHERE id=$1`, dealID)
		return err
	}
	a.ensureAutodialDIDAmd(ctx, numberCode)
	// Не звонить на свой же DID Exolve
	if normalizeExolveDID(phone) == normalizeExolveDID(numberCode) {
		a.releaseAutodialDID(ctx, numberCode)
		_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='error' WHERE id=$1`, dealID)
		return fmt.Errorf("телефон кандидата совпадает с DID Exolve (%s)", numberCode)
	}
	for _, sip := range sips {
		if normalizeExolveDID(phone) == normalizeExolveDID(sip) || strings.Contains(phone, strings.TrimPrefix(sip, "88314")) {
			a.releaseAutodialDID(ctx, numberCode)
			_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='error' WHERE id=$1`, dealID)
			return fmt.Errorf("телефон кандидата совпадает с SIP менеджера")
		}
	}
	cbID, err := a.ensureExolveCallbackResource(ctx)
	if err != nil {
		a.releaseAutodialDID(ctx, numberCode)
		_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='error' WHERE id=$1`, dealID)
		return err
	}

	var attemptID string
	var originalOwner string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM deals WHERE id=$1::uuid`, dealID).Scan(&originalOwner)
	if originalOwner != "" {
		err = a.db.QueryRow(ctx, `
			INSERT INTO autodial_attempts (deal_id, status, number_code, original_owner_id)
			VALUES ($1::uuid, 'started', $2, $3::uuid) RETURNING id::text`,
			dealID, numberCode, originalOwner).Scan(&attemptID)
	} else {
		err = a.db.QueryRow(ctx, `
			INSERT INTO autodial_attempts (deal_id, status, number_code)
			VALUES ($1::uuid, 'started', $2) RETURNING id::text`, dealID, numberCode).Scan(&attemptID)
	}
	if err != nil {
		a.releaseAutodialDID(ctx, numberCode)
		return err
	}
	_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='dialing' WHERE id=$1`, dealID)

	autodialLogf("dial start deal=%s phone=%s cli=%s sips=%v", dealID, phone, numberCode, sips)
	callID, err := a.exolveMakeCallback(ctx, numberCode, cbID, phone, sips, "deal:"+dealID)
	if err != nil {
		_, _ = a.db.Exec(ctx, `
			UPDATE autodial_attempts SET status='error', error=$2, finished_at=now() WHERE id=$1::uuid`,
			attemptID, err.Error())
		_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='error' WHERE id=$1`, dealID)
		a.releaseAutodialDID(ctx, numberCode)
		autodialLogf("dial FAIL deal=%s: %v", dealID, err)
		return err
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE autodial_attempts SET status='ringing', exolve_call_id=$2 WHERE id=$1::uuid`,
		attemptID, callID)
	_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status='ringing' WHERE id=$1`, dealID)
	autodialLogf("dial OK deal=%s call_id=%s → гудки", dealID, callID)

	a.hub.Broadcast("autodial.dialing", map[string]any{
		"deal_id": dealID,
		"title":   title,
		"call_id": callID,
		"cli":     numberCode, // From на softphone = наш DID, не кандидат
	})
	a.broadcastDealUpdated(ctx, dealID, map[string]any{"autodial_status": "ringing"})
	a.logDealLeadAct(ctx, dealID, "", "autodial_started", map[string]any{
		"call_id": callID, "number_code": numberCode, "operators": len(sips),
	})
	go a.watchAutodialCall(dealID, attemptID, callID)
	return nil
}

// watchAutodialCall поллит call/v1/GetInfo и шлёт WS:
// autodial.offer — кандидат ответил; autodial.ended — финал.
// После offer не рвём сессию по history/ложным terminal — только live GetInfo с ended
// или явный fail до ответа кандидата.
func (a *App) watchAutodialCall(dealID, attemptID, callID string) {
	idNum, err := strconv.ParseUint(strings.TrimSpace(callID), 10, 64)
	if err != nil || idNum == 0 {
		autodialLogf("watch bad call_id=%q", callID)
		return
	}
	deadline := time.Now().Add(3 * time.Minute)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var title string
	_ = a.db.QueryRow(context.Background(), `SELECT title FROM deals WHERE id=$1`, dealID).Scan(&title)
	offered := false
	prevKey := ""
	autodialLogf("гудки START deal=%s call=%s", dealID, callID)

	for {
		// Менеджер уже dismiss / webhook закрыл attempt — не шлём повторный offer.
		var done bool
		_ = a.db.QueryRow(context.Background(), `
			SELECT finished_at IS NOT NULL FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&done)
		if done {
			autodialLogf("watch stop already finished deal=%s call=%s", dealID, callID)
			return
		}

		st, detail, endedAt, liveOK := a.exolveLiveCallMeta(context.Background(), callID)
		src := "live"
		ok := liveOK
		if !ok && idNum > 0 {
			if !offered {
				var histDetail string
				st, histDetail, ok = a.exolveCallHistoryStatus(context.Background(), idNum)
				detail = histDetail
				endedAt = ""
				src = "history"
			}
		}
		if ok {
			low := strings.ToLower(strings.TrimSpace(st))
			key := src + "|" + low + "|" + endedAt
			if key != prevKey {
				prevKey = key
				autodialLogf("гудки %s deal=%s call=%s status=%s ended=%q %s",
					src, dealID, callID, low, endedAt, detail)
			}
			switch low {
			case "answered", "talk":
				// Offer только из webhook (type=s side=A). Watch — fallback finish.
				tag, _ := a.db.Exec(context.Background(), `
					UPDATE autodial_attempts SET status='connected' WHERE id=$1::uuid AND status IN ('started','ringing')`,
					attemptID)
				if tag.RowsAffected() > 0 {
					_, _ = a.db.Exec(context.Background(), `
						UPDATE deals SET autodial_status='connected' WHERE id=$1 AND autodial_status IN ('dialing','ringing')`,
						dealID)
					// Если webhook ещё не пришёл — один fallback offer.
					if !offered {
						offered = true
						var cli string
						_ = a.db.QueryRow(context.Background(), `
							SELECT coalesce(number_code,'') FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&cli)
						a.hub.Broadcast("autodial.offer", map[string]any{
							"deal_id": dealID,
							"title":   title,
							"call_id": callID,
							"status":  low,
							"cli":     cli,
						})
						a.broadcastDealUpdated(context.Background(), dealID, map[string]any{"autodial_status": "connected"})
						autodialLogf("watch fallback offer deal=%s call=%s status=%s", dealID, callID, low)
						a.logDealLeadAct(context.Background(), dealID, "", "autodial_offered", map[string]any{
							"call_id": callID,
							"status":  low,
							"cli":     cli,
							"via":     "watch",
						})
						a.scheduleAutodialPrompt(dealID, attemptID, callID, title, cli)
					}
				} else {
					offered = true
				}
				if liveOK && strings.TrimSpace(endedAt) != "" {
					a.finishAutodial(dealID, attemptID, callID, "connected", detail)
					return
				}
			case "play_audio_start", "play_audio_stop":
				if offered && liveOK && strings.TrimSpace(endedAt) != "" {
					a.finishAutodial(dealID, attemptID, callID, "connected", detail)
					return
				}
			case "ringing", "initiated", "queued":
				// гудки
			case "completed":
				if offered {
					if liveOK && strings.TrimSpace(endedAt) == "" {
						break
					}
					a.finishAutodial(dealID, attemptID, callID, "connected", detail)
				} else {
					a.finishAutodial(dealID, attemptID, callID, "no_answer", detail)
				}
				return
			case "rejected", "no_answer", "canceled", "busy", "error", "failed":
				// Offer часто приходит только из webhook — локальный offered=false.
				// Смотрим БД: после connected/claim GetInfo врёт (canceled/rejected на line_1).
				var acceptedBy, attStatus string
				_ = a.db.QueryRow(context.Background(), `
					SELECT coalesce(accepted_by::text,''), coalesce(status,'')
					FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&acceptedBy, &attStatus)
				attStatus = strings.ToLower(strings.TrimSpace(attStatus))
				if offered || acceptedBy != "" || attStatus == "connected" {
					autodialLogf("watch ignore terminal after offer status=%s accepted=%v att=%s ended=%q deal=%s",
						low, acceptedBy != "", attStatus, endedAt, dealID)
					if !offered {
						offered = true
					}
					break
				}
				if !liveOK {
					break
				}
				autodialLogf("сброс/отказ deal=%s call=%s status=%s", dealID, callID, low)
				a.finishAutodial(dealID, attemptID, callID, low, detail)
				return
			default:
				autodialLogf("гудки unknown status=%s deal=%s", low, dealID)
			}
		}
		if time.Now().After(deadline) {
			final := "timeout"
			if offered {
				final = "connected"
			}
			a.finishAutodial(dealID, attemptID, callID, final, "watch deadline 3m")
			return
		}
		<-ticker.C
	}
}

func (a *App) finishAutodial(dealID, attemptID, callID, status, detail string) {
	ctx := context.Background()
	autodialLogf("финиш deal=%s call=%s status=%s %s", dealID, callID, status, detail)
	var numberCode string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(number_code,'') FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&numberCode)
	tag, err := a.db.Exec(ctx, `
		UPDATE autodial_attempts SET status=$2, error=$3, finished_at=now()
		WHERE id=$1::uuid AND finished_at IS NULL`, attemptID, status, detail)
	if err != nil {
		return
	}
	if tag.RowsAffected() == 0 {
		return
	}
	a.releaseAutodialDID(ctx, numberCode)
	dealStatus := status
	switch status {
	case "connected":
		dealStatus = "connected"
	case "timeout":
		dealStatus = "no_answer"
	case "rejected", "no_answer", "canceled", "busy":
		dealStatus = status
		if status == "canceled" || status == "rejected" {
			dealStatus = "no_answer"
		}
	default:
		if status == "error" {
			dealStatus = "error"
		}
	}
	_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status=$2 WHERE id=$1`, dealID, dealStatus)
	a.hub.Broadcast("autodial.ended", map[string]any{
		"deal_id": dealID,
		"call_id": callID,
		"status":  status,
		"detail":  detail,
	})
	a.broadcastDealUpdated(ctx, dealID, map[string]any{"autodial_status": dealStatus})

	finishKind := "talk"
	switch dealStatus {
	case "no_answer", "busy", "canceled", "rejected":
		finishKind = "no_answer"
	case "error", "failed", "timeout":
		finishKind = "error"
	case "connected":
		finishKind = "talk"
	default:
		if dealStatus != "connected" {
			finishKind = "reject"
		}
	}
	if status == "rejected" || status == "canceled" {
		finishKind = "reject"
	}
	if status == "error" || status == "failed" || strings.Contains(strings.ToUpper(detail), "UNALLOCATED") {
		finishKind = "error"
	}
	a.logDealLeadAct(ctx, dealID, "", "autodial_finished", map[string]any{
		"call_id":     callID,
		"status":      status,
		"deal_status": dealStatus,
		"detail":      detail,
		"kind":        finishKind,
	})

	// Запись разговора появляется в Exolve с задержкой — poll если webhook crr не придёт.
	if finishKind == "talk" || status == "connected" {
		a.scheduleExolveRecordingPoll(callID)
	}

	// Недозвон / сброс кандидата / busy менеджера — повторный звонок со стадии недозвон.
	if dealStatus == "no_answer" || dealStatus == "busy" {
		a.scheduleNedozvonRetry(dealID)
	}
}

// scheduleNedozvonRetry — если сделка всё ещё на «Недозвон 3», перезвонить через retry.
func (a *App) scheduleNedozvonRetry(dealID string) {
	retryMin := a.cfg.AutodialRetryMinutes
	if retryMin <= 0 {
		retryMin = 30
	}
	// Короткий backoff между попытками одной кампании (не ждать полные 30м для busy softphone).
	wait := 45 * time.Second
	if retryMin > 0 && time.Duration(retryMin)*time.Minute < wait {
		wait = time.Duration(retryMin) * time.Minute
	}
	go func() {
		time.Sleep(wait)
		cctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		var stageID, bid string
		err := a.db.QueryRow(cctx, `
			SELECT d.stage_id::text, coalesce(ps.bitrix_id,'')
			FROM deals d
			JOIN pipeline_stages ps ON ps.id = d.stage_id
			WHERE d.id=$1::uuid`, dealID).Scan(&stageID, &bid)
		if err != nil || bid != nedozvon3BitrixID {
			return
		}
		if err := a.startNedozvonAutodial(cctx, dealID); err != nil {
			autodialLogf("nedozvon retry skip deal=%s: %v", dealID, err)
			// Оператор занят — ещё раз через тот же backoff.
			if strings.Contains(err.Error(), "оператор уже в разговоре") {
				a.scheduleNedozvonRetry(dealID)
			}
		} else {
			autodialLogf("nedozvon retry start deal=%s", dealID)
		}
	}()
}

func (a *App) releaseAutodialDID(ctx context.Context, numberCode string) {
	n := normalizeExolveDID(numberCode)
	if n == "" {
		return
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE exolve_dids SET cli_lease_until=NULL, updated_at=now()
		WHERE active AND regexp_replace(number,'[^0-9]','','g') IN ($1, '8'||substr($1,2))`, n)
}

// ensureAutodialDIDAmd — детект автоответчика на исходящем CLI.
// Без услуги в ЛК Exolve API вернёт 403 — звонки всё равно идут, просто AMD нет.
func (a *App) ensureAutodialDIDAmd(ctx context.Context, numberCode string) {
	n := normalizeExolveDID(numberCode)
	code, err := strconv.ParseUint(n, 10, 64)
	if err != nil || code == 0 {
		return
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/number/v1/SetAmdState", map[string]any{
		"number_code": code,
		"amd":         true,
	}, nil); err != nil {
		autodialLogf("AMD enable FAIL cli=%s: %v (включите услугу в ЛК Exolve)", n, err)
		return
	}
	autodialLogf("AMD enable OK cli=%s", n)
}

func (a *App) exolveCallHistoryStatus(ctx context.Context, callID uint64) (status, detail string, ok bool) {
	var out struct {
		Call []struct {
			Status  string `json:"status"`
			EndTime string `json:"end_time"`
			Legs    []struct {
				Status    string  `json:"status"`
				CauseCode any     `json:"cause_code"`
				TalkTime  float64 `json:"talk_time"`
				To        string  `json:"to"`
			} `json:"legs"`
		} `json:"call"`
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/statistics/call-history/v2/GetInfo", map[string]any{
		"call_id": []uint64{callID},
	}, &out); err != nil {
		return "", "", false
	}
	if len(out.Call) == 0 {
		return "", "", false
	}
	c := out.Call[0]
	st := strings.TrimSpace(c.Status)
	if st == "" {
		return "", "", false
	}
	detail = st
	hasTalk := false
	failSt := ""
	for _, leg := range c.Legs {
		ls := strings.ToLower(strings.TrimSpace(leg.Status))
		if leg.TalkTime > 0.5 || ls == "answered" {
			if leg.TalkTime > 0.5 {
				hasTalk = true
			}
		}
		switch ls {
		case "rejected", "no_answer", "canceled", "busy":
			if failSt == "" {
				failSt = ls
			}
			detail = fmt.Sprintf("%s leg=%s cause=%v to=%s", st, leg.Status, leg.CauseCode, leg.To)
		}
	}
	if failSt != "" && !hasTalk {
		return failSt, detail, true
	}
	low := strings.ToLower(st)
	terminal := low == "rejected" || low == "no_answer" || low == "canceled" || low == "busy" || low == "error"
	if terminal {
		return low, detail, true
	}
	if low == "answered" {
		if strings.TrimSpace(c.EndTime) != "" {
			return "answered", detail, true
		}
		return "answered", "", true
	}
	return low, detail, true
}

func (a *App) autodialCheck(w http.ResponseWriter, r *http.Request) {
	if !autodialFeatureEnabled() {
		writeAutodialPaused(w)
		return
	}
	callID := strings.TrimSpace(r.URL.Query().Get("call_id"))
	if callID == "" {
		writeJSON(w, 400, map[string]string{"error": "call_id required"})
		return
	}
	ctx := r.Context()

	// Уже offered в CRM: GetInfo снова даёт ringing на line_2 — это offer, не dialing.
	var attemptStatus string
	var finished bool
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(status,''), finished_at IS NOT NULL
		FROM autodial_attempts
		WHERE exolve_call_id=$1
		ORDER BY started_at DESC LIMIT 1`, callID).Scan(&attemptStatus, &finished)
	attemptStatus = strings.ToLower(strings.TrimSpace(attemptStatus))
	alreadyOffered := attemptStatus == "connected"

	st, detail, ended, ok := a.exolveLiveCallMeta(ctx, callID)
	if !ok {
		if n, err := strconv.ParseUint(callID, 10, 64); err == nil {
			var d2 string
			st, d2, ok = a.exolveCallHistoryStatus(ctx, n)
			detail = d2
			ended = ""
		}
	}
	if !ok {
		// Нет live — если attempt уже connected и не finished, держим offer
		if alreadyOffered && !finished {
			writeJSON(w, 200, map[string]any{
				"phase":  "offer",
				"allow":  true,
				"status": "connected",
				"detail": "attempt connected, GetInfo miss",
			})
			return
		}
		writeJSON(w, 200, map[string]any{
			"phase":  "unknown",
			"allow":  false,
			"status": "",
			"detail": "нет статуса в Exolve",
		})
		return
	}
	low := strings.ToLower(st)
	endedSet := strings.TrimSpace(ended) != ""
	var phase string
	var allow bool
	switch {
	case finished || low == "rejected" || low == "no_answer" || low == "canceled" ||
		low == "busy" || low == "error" || low == "failed" ||
		(low == "completed" && (endedSet || finished)):
		phase, allow = "ended", false
	case alreadyOffered && !endedSet &&
		(low == "ringing" || low == "answered" || low == "talk" ||
			low == "play_audio_start" || low == "play_audio_stop" ||
			low == "initiated" || low == "queued"):
		// line_2 ringing после уже подтверждённого ответа (answered/talk)
		phase, allow = "offer", true
	case low == "answered" || low == "talk":
		if endedSet {
			phase, allow = "ended", false
		} else {
			phase, allow = "offer", true
		}
	case low == "ringing" || low == "initiated" || low == "queued" ||
		low == "play_audio_start" || low == "play_audio_stop":
		// play_audio на гудках — ещё dialing, не Принять
		phase, allow = "dialing", false
	case low == "completed":
		phase, allow = "ended", false
	default:
		phase, allow = "unknown", false
	}
	writeJSON(w, 200, map[string]any{
		"phase":   phase,
		"allow":   allow,
		"status":  low,
		"detail":  detail,
		"ended":   ended,
		"attempt": attemptStatus,
	})
}

// autodialDismiss — менеджер нажал «Отклонить»: сразу закрываем attempt, не ждём USER_BUSY.
func (a *App) autodialDismiss(w http.ResponseWriter, r *http.Request) {
	if !autodialFeatureEnabled() {
		writeAutodialPaused(w)
		return
	}
	ctx := r.Context()
	callID := strings.TrimSpace(r.URL.Query().Get("call_id"))
	dealID := strings.TrimSpace(r.URL.Query().Get("deal_id"))
	var attemptID, foundDeal, foundCall string
	var err error
	if callID != "" {
		err = a.db.QueryRow(ctx, `
			SELECT id::text, deal_id::text, coalesce(exolve_call_id,'')
			FROM autodial_attempts
			WHERE exolve_call_id=$1 AND finished_at IS NULL
			ORDER BY started_at DESC LIMIT 1`, callID).Scan(&attemptID, &foundDeal, &foundCall)
	} else if dealID != "" {
		err = a.db.QueryRow(ctx, `
			SELECT id::text, deal_id::text, coalesce(exolve_call_id,'')
			FROM autodial_attempts
			WHERE deal_id=$1::uuid AND finished_at IS NULL
			ORDER BY started_at DESC LIMIT 1`, dealID).Scan(&attemptID, &foundDeal, &foundCall)
	} else {
		writeJSON(w, 400, map[string]string{"error": "call_id или deal_id"})
		return
	}
	if err != nil || attemptID == "" {
		writeJSON(w, 200, map[string]any{"ok": true, "already": true})
		return
	}
	a.finishAutodial(foundDeal, attemptID, foundCall, "busy", "manager dismiss")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// autodialClaim — первый менеджер «Ответить» атомарно занимает звонок; остальные → 409.
func (a *App) autodialClaim(w http.ResponseWriter, r *http.Request) {
	if !autodialFeatureEnabled() {
		writeAutodialPaused(w)
		return
	}
	if !isManager(r) {
		writeJSON(w, 403, map[string]string{"error": "скорозвон только для менеджеров"})
		return
	}
	uid := actorID(r)
	if uid == "" {
		writeJSON(w, 401, map[string]string{"error": "нет сессии"})
		return
	}
	var canCall bool
	_ = a.db.QueryRow(r.Context(), `SELECT coalesce(can_call,true) FROM users WHERE id=$1::uuid`, uid).Scan(&canCall)
	if !canCall {
		writeJSON(w, 403, map[string]string{"error": "нет права на звонки"})
		return
	}

	callID := strings.TrimSpace(r.URL.Query().Get("call_id"))
	dealID := strings.TrimSpace(r.URL.Query().Get("deal_id"))
	if callID == "" && dealID == "" {
		writeJSON(w, 400, map[string]string{"error": "call_id или deal_id"})
		return
	}

	ctx := r.Context()
	var attemptID, foundDeal, foundCall string
	var alreadyBy string
	var err error

	if callID != "" {
		err = a.db.QueryRow(ctx, `
			SELECT id::text, deal_id::text, coalesce(exolve_call_id,''), coalesce(accepted_by::text,'')
			FROM autodial_attempts
			WHERE exolve_call_id=$1 AND finished_at IS NULL
			ORDER BY started_at DESC LIMIT 1`, callID).Scan(&attemptID, &foundDeal, &foundCall, &alreadyBy)
	} else {
		err = a.db.QueryRow(ctx, `
			SELECT id::text, deal_id::text, coalesce(exolve_call_id,''), coalesce(accepted_by::text,'')
			FROM autodial_attempts
			WHERE deal_id=$1::uuid AND finished_at IS NULL
			ORDER BY started_at DESC LIMIT 1`, dealID).Scan(&attemptID, &foundDeal, &foundCall, &alreadyBy)
	}
	if err != nil || attemptID == "" {
		writeJSON(w, 404, map[string]string{"error": "нет активного скорозвона"})
		return
	}
	if alreadyBy != "" {
		if alreadyBy == uid {
			writeJSON(w, 200, map[string]any{"ok": true, "claimed": true, "mine": true, "deal_id": foundDeal, "call_id": foundCall})
			return
		}
		writeJSON(w, 409, map[string]any{"error": "already_taken", "accepted_by": alreadyBy, "deal_id": foundDeal, "call_id": foundCall})
		return
	}

	tag, err := a.db.Exec(ctx, `
		UPDATE autodial_attempts
		SET accepted_by=$2::uuid, accepted_at=now()
		WHERE id=$1::uuid AND finished_at IS NULL AND accepted_by IS NULL`, attemptID, uid)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "claim failed"})
		return
	}
	if tag.RowsAffected() == 0 {
		var other string
		_ = a.db.QueryRow(ctx, `SELECT coalesce(accepted_by::text,'') FROM autodial_attempts WHERE id=$1::uuid`, attemptID).Scan(&other)
		writeJSON(w, 409, map[string]any{"error": "already_taken", "accepted_by": other, "deal_id": foundDeal, "call_id": foundCall})
		return
	}

	// Владельца не меняем на claim — решение после разговора (§8.2).
	a.logDealLeadAct(ctx, foundDeal, uid, "autodial_claimed", map[string]any{
		"call_id":    foundCall,
		"attempt_id": attemptID,
		"manager":    a.userFullName(ctx, uid),
		"manager_id": uid,
	})
	a.hub.Broadcast("autodial.taken", map[string]any{
		"deal_id": foundDeal,
		"call_id": foundCall,
		"user_id": uid,
	})
	a.hub.Broadcast("autodial.accepted", map[string]any{
		"deal_id": foundDeal,
		"call_id": foundCall,
		"user_id": uid,
	})
	autodialLogf("claim OK user=%s deal=%s call=%s", uid, foundDeal, foundCall)
	writeJSON(w, 200, map[string]any{"ok": true, "claimed": true, "deal_id": foundDeal, "call_id": foundCall})
}

// autodialDisposition — после разговора: bad | return | take (ТЗ §8.2).
func (a *App) autodialDisposition(w http.ResponseWriter, r *http.Request) {
	if !autodialFeatureEnabled() {
		writeAutodialPaused(w)
		return
	}
	if !isManager(r) {
		writeJSON(w, 403, map[string]string{"error": "скорозвон только для менеджеров"})
		return
	}
	uid := actorID(r)
	if uid == "" {
		writeJSON(w, 401, map[string]string{"error": "нет сессии"})
		return
	}
	var in struct {
		Action string `json:"action"` // bad | return | take
		DealID string `json:"deal_id"`
		CallID string `json:"call_id"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action != "bad" && action != "return" && action != "take" {
		writeJSON(w, 400, map[string]string{"error": "action: bad | return | take"})
		return
	}
	dealID := strings.TrimSpace(in.DealID)
	callID := strings.TrimSpace(in.CallID)
	reason := strings.TrimSpace(in.Reason)
	if dealID == "" && callID == "" {
		writeJSON(w, 400, map[string]string{"error": "deal_id или call_id"})
		return
	}

	ctx := r.Context()
	var attemptID, foundDeal, foundCall, acceptedBy, originalOwner, existingDisp string
	var err error
	if callID != "" {
		err = a.db.QueryRow(ctx, `
			SELECT id::text, deal_id::text, coalesce(exolve_call_id,''),
			       coalesce(accepted_by::text,''), coalesce(original_owner_id::text,''),
			       coalesce(disposition,'')
			FROM autodial_attempts
			WHERE exolve_call_id=$1
			ORDER BY started_at DESC LIMIT 1`, callID).
			Scan(&attemptID, &foundDeal, &foundCall, &acceptedBy, &originalOwner, &existingDisp)
	} else {
		err = a.db.QueryRow(ctx, `
			SELECT id::text, deal_id::text, coalesce(exolve_call_id,''),
			       coalesce(accepted_by::text,''), coalesce(original_owner_id::text,''),
			       coalesce(disposition,'')
			FROM autodial_attempts
			WHERE deal_id=$1::uuid
			ORDER BY started_at DESC LIMIT 1`, dealID).
			Scan(&attemptID, &foundDeal, &foundCall, &acceptedBy, &originalOwner, &existingDisp)
	}
	if err != nil || attemptID == "" {
		writeJSON(w, 404, map[string]string{"error": "попытка скорозвона не найдена"})
		return
	}
	if existingDisp != "" {
		writeJSON(w, 200, map[string]any{"ok": true, "already": true, "disposition": existingDisp, "deal_id": foundDeal})
		return
	}
	if acceptedBy != "" && acceptedBy != uid && !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "решение только у принявшего менеджера"})
		return
	}
	if foundDeal == "" {
		foundDeal = dealID
	}

	prevOwner := originalOwner
	if prevOwner == "" {
		_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM deals WHERE id=$1::uuid`, foundDeal).Scan(&prevOwner)
	}

	newOwner := uid
	stageKey := ""
	switch action {
	case "bad":
		stageKey = "bad"
		if reason == "" {
			reason = "Некачественный лид после скорозвона"
		}
		// владелец — кто закрыл (принявший), либо остаётся как был
		if acceptedBy != "" {
			newOwner = acceptedBy
		}
	case "return":
		stageKey = "after_sk"
		if originalOwner != "" {
			newOwner = originalOwner
		} else if prevOwner != "" {
			newOwner = prevOwner
		}
		if reason == "" {
			reason = "Возврат первоначальному менеджеру"
		}
	case "take":
		// В канбан взявшего — «После скор.»; у прежнего владельца исчезает (смена owner).
		stageKey = "after_sk"
		newOwner = uid
		if reason == "" {
			reason = "Забрал лид себе после скорозвона"
		}
	}

	// Закреплённый лид нельзя «забрать себе» после скорозвона; при «bad» владельца тоже не трогаем.
	if newOwner != "" && newOwner != prevOwner {
		if cur, sticky := a.dealStickyInfo(ctx, foundDeal); leadStickyBlocksTransfer(cur, sticky, newOwner) != nil {
			if action == "take" {
				writeJSON(w, 409, map[string]any{
					"error":    "лид закреплён за другим менеджером — забрать нельзя, используйте «вернуть»",
					"owner_id": cur,
					"deal_id":  foundDeal,
				})
				return
			}
			newOwner = cur
		}
	}
	if newOwner != "" {
		if leadID := a.dealLinkedLeadID(ctx, foundDeal); leadID != "" {
			// Атомарно, с проверкой липкости в UPDATE: если владелец успел начать работу — не перетираем.
			if !a.setLeadOwnerGuarded(ctx, leadID, newOwner, "", false) {
				if action == "take" {
					cur, _ := a.leadStickyInfo(ctx, leadID)
					writeJSON(w, 409, map[string]any{
						"error":    "лид закреплён за другим менеджером — забрать нельзя, используйте «вернуть»",
						"owner_id": cur,
						"deal_id":  foundDeal,
					})
					return
				}
				newOwner = prevOwner
			}
		} else {
			_, _ = a.db.Exec(ctx, `UPDATE deals SET owner_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, foundDeal, newOwner)
		}
	}
	if stageKey != "" {
		a.moveDealToHireStage(ctx, foundDeal, stageKey, uid)
	}

	_, _ = a.db.Exec(ctx, `
		UPDATE autodial_attempts
		SET disposition=$2, disposition_reason=$3, disposition_at=now(),
		    finished_at=coalesce(finished_at, now())
		WHERE id=$1::uuid`, attemptID, action, reason)
	_, _ = a.db.Exec(ctx, `UPDATE deals SET autodial_status=$2 WHERE id=$1::uuid`, foundDeal, "done_"+action)

	stageName := ""
	if stageKey != "" {
		for _, s := range adaptationStages {
			if s.key == stageKey {
				stageName = s.name
				break
			}
		}
	}
	a.logDealLeadAct(ctx, foundDeal, uid, "autodial_disposition", map[string]any{
		"action":              action,
		"reason":              reason,
		"previous_owner":      prevOwner,
		"previous_owner_name": a.userFullName(ctx, prevOwner),
		"new_owner":           newOwner,
		"new_owner_name":      a.userFullName(ctx, newOwner),
		"stage_key":           stageKey,
		"stage_name":          stageName,
		"call_id":             foundCall,
		"attempt_id":          attemptID,
	})

	if action == "return" && newOwner != "" && newOwner != uid {
		title := ""
		_ = a.db.QueryRow(ctx, `SELECT coalesce(title,'') FROM deals WHERE id=$1::uuid`, foundDeal).Scan(&title)
		_, _ = a.db.Exec(ctx, `
			INSERT INTO notifications (user_id, channel, title, body, payload)
			VALUES ($1::uuid, 'in_app', 'Лид после скорозвона', $2, $3::jsonb)`,
			newOwner,
			fmt.Sprintf("Кандидат «%s» возвращён вам после скорозвона", title),
			mustJSON(map[string]any{"deal_id": foundDeal, "action": "return", "from_user": uid}),
		)
		a.hub.Broadcast("notification", map[string]any{"user_id": newOwner, "deal_id": foundDeal, "kind": "autodial_return"})
	}

	// Системная пометка в связанном чате при смене владельца
	if action == "take" || action == "return" {
		msg := "Диалог заблокирован — лид передан другому менеджеру"
		if action == "take" {
			msg = "Лид забран другим менеджером после скорозвона"
		}
		if action == "return" {
			msg = "Лид возвращён первоначальному менеджеру после скорозвона"
		}
		_, _ = a.db.Exec(ctx, `
			INSERT INTO inbox_messages (thread_id, direction, body, author_label)
			SELECT t.id, 'system', $2, 'Скорозвон'
			FROM inbox_threads t WHERE t.deal_id=$1::uuid`, foundDeal, msg)
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_threads SET last_body=$2, last_at=now() WHERE deal_id=$1::uuid`, foundDeal, msg)
	}

	a.hub.Broadcast("autodial.disposition", map[string]any{
		"deal_id": foundDeal,
		"call_id": foundCall,
		"action":  action,
		"user_id": uid,
	})
	a.broadcastDealUpdated(r.Context(), foundDeal, map[string]any{"owner_id": newOwner, "autodial_status": "done_" + action})
	autodialLogf("disposition %s user=%s deal=%s call=%s → owner=%s", action, uid, foundDeal, foundCall, newOwner)
	writeJSON(w, 200, map[string]any{
		"ok": true, "action": action, "deal_id": foundDeal, "owner_id": newOwner, "stage_key": stageKey,
	})
}

func (a *App) moveDealToHireStage(ctx context.Context, dealID, stageKey, actor string) {
	_ = actor
	bid := hirePipelineKey + ":" + stageKey
	var stageID, pipeID string
	err := a.db.QueryRow(ctx, `
		SELECT s.id::text, s.pipeline_id::text FROM pipeline_stages s WHERE s.bitrix_id=$1`, bid).
		Scan(&stageID, &pipeID)
	if err != nil || stageID == "" {
		a.ensureRecruitFunnels(ctx)
		_ = a.db.QueryRow(ctx, `
			SELECT s.id::text, s.pipeline_id::text FROM pipeline_stages s WHERE s.bitrix_id=$1`, bid).
			Scan(&stageID, &pipeID)
	}
	if stageID == "" {
		return
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE deals SET stage_id=$2::uuid, pipeline_id=coalesce(nullif($3,'')::uuid, pipeline_id), updated_at=now()
		WHERE id=$1::uuid`, dealID, stageID, pipeID)
	// stage_changed не пишем: disposition уже отражает исход (take/return/bad).
}

// autodialPending — активный скорозвон (если FE пропустил WS dialing/offer).
func (a *App) autodialPending(w http.ResponseWriter, r *http.Request) {
	if !autodialFeatureEnabled() {
		writeAutodialPaused(w)
		return
	}
	ctx := r.Context()
	var dealID, title, callID, cli, status string
	err := a.db.QueryRow(ctx, `
		SELECT a.deal_id::text, coalesce(d.title,''), coalesce(a.exolve_call_id,''),
		       coalesce(a.number_code,''), a.status
		FROM autodial_attempts a
		JOIN deals d ON d.id = a.deal_id
		WHERE a.finished_at IS NULL
		  AND a.status IN ('started','ringing','connected')
		  AND a.started_at > now() - interval '5 minutes'
		ORDER BY a.started_at DESC LIMIT 1`).Scan(&dealID, &title, &callID, &cli, &status)
	if err != nil {
		writeJSON(w, 200, map[string]any{"active": false})
		return
	}
	writeJSON(w, 200, map[string]any{
		"active":  true,
		"deal_id": dealID,
		"title":   title,
		"call_id": callID,
		"cli":     cli,
		"status":  status,
		"allow":   status == "connected",
	})
}

// exolveOurDIDs — номера Exolve (CLI), чтобы не показывать их как «Входящий звонок».
func (a *App) exolveOurDIDs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Только связующие скорозвона — личные CLI сюда не входят (иначе обычный исходящий
	// ошибочно выглядит как скорозвон).
	nums := make([]string, 0, 8)
	seen := map[string]bool{}
	for _, n := range a.autodialBridgeDIDs(ctx) {
		nn := normalizeExolveDID(n)
		if nn == "" || seen[nn] {
			continue
		}
		seen[nn] = true
		nums = append(nums, nn)
	}
	writeJSON(w, 200, map[string]any{"numbers": nums})
}

// exolveLiveCallMeta — call/v1/GetInfo.
// Возвращает status, detail, ended (непустой = звонок реально завершён на стороне Exolve).
func (a *App) exolveLiveCallMeta(ctx context.Context, callID string) (status, detail, ended string, ok bool) {
	var out struct {
		Status      string `json:"status"`
		Started     string `json:"started"`
		Ended       string `json:"ended"`
		Duration    int    `json:"duration"`
		Destination string `json:"destination"`
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/call/v1/GetInfo", map[string]any{
		"call_id": strings.TrimSpace(callID),
	}, &out); err != nil {
		return "", "", "", false
	}
	st := strings.TrimSpace(out.Status)
	if st == "" {
		return "", "", "", false
	}
	ended = strings.TrimSpace(out.Ended)
	detail = fmt.Sprintf("%s dest=%s dur=%d started=%q ended=%q",
		st, out.Destination, out.Duration, strings.TrimSpace(out.Started), ended)
	return strings.ToLower(st), detail, ended, true
}

func (a *App) autodialManagerSIPs(ctx context.Context) []string {
	// line_2 = softphone менеджеров (личные SIP). Связующий — это DID (number_code), не SIP.
	rows := a.queryMaps(ctx, `
		SELECT DISTINCT es.sip_login
		FROM exolve_sips es
		JOIN users u ON u.id=es.user_id AND u.is_active=true AND coalesce(u.can_call,true)=true
		JOIN roles r ON r.id=u.role_id
		WHERE es.active=true AND es.sip_login <> ''
		  AND lower(r.name) IN ('manager','sales')
		ORDER BY es.sip_login`)
	out := make([]string, 0, len(rows)+1)
	seen := map[string]bool{}
	for _, r := range rows {
		sip := strings.TrimSpace(fmt.Sprint(r["sip_login"]))
		if sip == "" || seen[sip] {
			continue
		}
		seen[sip] = true
		out = append(out, sip)
	}
	// Dev fallback: если в БД нет manager SIP — связующий SIP из env (локальная отладка).
	if len(out) == 0 {
		if sip := strings.TrimSpace(a.cfg.ExolveSIPLogin); sip != "" {
			autodialLogf("manager SIPs empty — fallback EXOLVE_SIP_LOGIN")
			out = append(out, sip)
		}
	}
	return out
}

// pickAutodialNumberCode — карусель DID: параллельные скорозвоны берут разные номера.
// Сидирует пул из env/API в exolve_dids и атомарно claim'ит (FOR UPDATE SKIP LOCKED).
func (a *App) pickAutodialNumberCode(ctx context.Context) (string, error) {
	a.ensureAutodialDIDPool(ctx)

	if n, err := a.claimAutodialDIDFromPool(ctx); err == nil && n != "" {
		return n, nil
	} else if err != nil && err != pgx.ErrNoRows {
		autodialLogf("CLI claim: %v", err)
	}

	// Крайний fallback без строк в exolve_dids (редко): advisory lock по номеру
	n, err := a.claimAutodialDIDFallback(ctx)
	if err != nil {
		return "", err
	}
	return n, nil
}

// ensureAutodialDIDPool кладёт известные DID в exolve_dids, чтобы карусель/lease работали как у softphone.
func (a *App) ensureAutodialDIDPool(ctx context.Context) {
	cands := a.autodialFallbackDIDs(ctx)
	if len(cands) == 0 {
		return
	}
	var owner string
	_ = a.db.QueryRow(ctx, `
		SELECT u.id::text FROM users u
		JOIN roles r ON r.id=u.role_id
		WHERE u.is_active AND lower(r.name)='admin'
		ORDER BY u.created_at LIMIT 1`).Scan(&owner)
	if owner == "" {
		_ = a.db.QueryRow(ctx, `SELECT id::text FROM users WHERE is_active ORDER BY created_at LIMIT 1`).Scan(&owner)
	}
	if owner == "" {
		return
	}
	for _, n := range cands {
		var existing string
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM exolve_dids
			WHERE regexp_replace(number,'[^0-9]','','g') IN ($1, '8'||substr($1,2))
			LIMIT 1`, n).Scan(&existing)
		if existing != "" {
			_, _ = a.db.Exec(ctx, `
				UPDATE exolve_dids SET active=true, updated_at=now()
				WHERE id=$1::uuid AND NOT active`, existing)
			continue
		}
		_, err := a.db.Exec(ctx, `
			INSERT INTO exolve_dids (number, user_id, active, updated_at)
			VALUES ($1,$2::uuid,true,now())`, n, owner)
		if err != nil {
			autodialLogf("seed DID %s: %v", n, err)
		}
	}
}

func (a *App) claimAutodialDIDFromPool(ctx context.Context) (string, error) {
	bridge := a.autodialBridgeDIDs(ctx)
	if len(bridge) == 0 {
		return "", pgx.ErrNoRows
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var id, number string
	err = tx.QueryRow(ctx, `
		SELECT id::text, number FROM exolve_dids
		WHERE active=true AND number <> '' AND number NOT LIKE '88314%'
		  AND regexp_replace(number,'[^0-9]','','g') = ANY($1::text[])
		  AND (cli_lease_until IS NULL OR cli_lease_until < now())
		  AND regexp_replace(number,'[^0-9]','','g') NOT IN (
		    SELECT regexp_replace(coalesce(number_code,''),'[^0-9]','','g')
		    FROM autodial_attempts
		    WHERE finished_at IS NULL
		      AND status IN ('started','ringing','connected')
		      AND coalesce(number_code,'') <> ''
		  )
		ORDER BY last_used_at NULLS FIRST, number
		FOR UPDATE SKIP LOCKED
		LIMIT 1`, bridge).Scan(&id, &number)
	if err != nil {
		return "", err
	}
	leaseUntil := time.Now().Add(autodialDIDLease)
	if _, err := tx.Exec(ctx, `
		UPDATE exolve_dids
		SET last_used_at=now(), cli_lease_until=$2, updated_at=now()
		WHERE id=$1::uuid`, id, leaseUntil); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return normalizeExolveDID(number), nil
}

func (a *App) claimAutodialDIDFallback(ctx context.Context) (string, error) {
	cands := a.autodialBridgeDIDs(ctx)
	if len(cands) == 0 {
		return "", fmt.Errorf("нет связующих DID (VITE_/EXOLVE_SIP_LOGIN / EXOLVE_INBOUND_NUMBERS)")
	}
	busy := a.activeAutodialDIDs(ctx)
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// Стабильный порядок + advisory lock, чтобы два параллельных старта не взяли один номер
	for _, n := range cands {
		if busy[n] {
			continue
		}
		var key int64
		for _, r := range n {
			key = key*10 + int64(r-'0')
		}
		if key == 0 {
			continue
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, key).Scan(&ok); err != nil || !ok {
			continue
		}
		// ещё раз busy внутри tx
		var hit int
		_ = tx.QueryRow(ctx, `
			SELECT 1 FROM autodial_attempts
			WHERE finished_at IS NULL AND status IN ('started','ringing','connected')
			  AND regexp_replace(coalesce(number_code,''),'[^0-9]','','g') IN ($1, '8'||substr($1,2))
			LIMIT 1`, n).Scan(&hit)
		if hit == 1 {
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return n, nil
	}
	return "", fmt.Errorf("все связующие DID заняты параллельным скорозвоном (в пуле %d)", len(cands))
}

func (a *App) activeAutodialDIDs(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	rows, err := a.db.Query(ctx, `
		SELECT coalesce(number_code,'') FROM autodial_attempts
		WHERE finished_at IS NULL AND status IN ('started','ringing','connected')`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		if n := normalizeExolveDID(raw); n != "" {
			out[n] = true
		}
	}
	return out
}

// autodialBridgeDIDs — только связующие DID скорозвона (не CLI обычных исходящих).
// Приоритет: EXOLVE_AUTODIAL_NUMBERS → EXOLVE_INBOUND_NUMBERS → CLI/forwarding SIP из .env.
func (a *App) autodialBridgeDIDs(ctx context.Context) []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		n := normalizeExolveDID(raw)
		if n == "" || strings.HasPrefix(n, "88314") || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}

	explicit := strings.TrimSpace(a.cfg.ExolveAutodialNums)
	if explicit == "" {
		explicit = strings.TrimSpace(a.cfg.ExolveInboundNums)
	}
	if explicit != "" {
		for _, raw := range strings.Split(explicit, ",") {
			add(raw)
		}
		return out
	}

	// Legacy: без явного списка — берём CLI/переадресации SIP из .env (часто это связующий).
	if sip := strings.TrimSpace(a.cfg.ExolveSIPLogin); sip != "" {
		if _, cli, forwarded, err := a.exolveSIPContext(ctx, sip); err == nil {
			add(cli)
			for _, n := range forwarded {
				add(n)
			}
		}
	}
	return out
}

func (a *App) autodialBridgeDIDSet(ctx context.Context) map[string]bool {
	set := map[string]bool{}
	for _, n := range a.autodialBridgeDIDs(ctx) {
		set[n] = true
		if len(n) == 11 && strings.HasPrefix(n, "7") {
			set["8"+n[1:]] = true
		}
	}
	return set
}

func (a *App) cliAllowlistSet() map[string]bool {
	raw := strings.TrimSpace(a.cfg.ExolveCLINums)
	if raw == "" {
		return nil
	}
	set := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		n := normalizeExolveDID(part)
		if n == "" || strings.HasPrefix(n, "88314") {
			continue
		}
		set[n] = true
		if len(n) == 11 && strings.HasPrefix(n, "7") {
			set["8"+n[1:]] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// isCLIEligibleDID — номер можно крутить как CLI обычного исходящего (не скорозвон).
func (a *App) isCLIEligibleDID(ctx context.Context, number string) bool {
	n := normalizeExolveDID(number)
	if n == "" || strings.HasPrefix(n, "88314") {
		return false
	}
	if a.autodialBridgeDIDSet(ctx)[n] {
		return false
	}
	if allow := a.cliAllowlistSet(); allow != nil && !allow[n] {
		return false
	}
	return true
}

// autodialFallbackDIDs — alias для сида пула / совместимости.
func (a *App) autodialFallbackDIDs(ctx context.Context) []string {
	return a.autodialBridgeDIDs(ctx)
}

func (a *App) ensureExolveCallbackResource(ctx context.Context) (uint64, error) {
	if id := strings.TrimSpace(a.cfg.ExolveCallbackResourceID); id != "" {
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("EXOLVE_CALLBACK_RESOURCE_ID: %w", err)
		}
		return n, nil
	}
	var list struct {
		Callbacks []struct {
			ID string `json:"callback_resource_id"`
		} `json:"callbacks"`
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/callback/v1/GetList", map[string]any{}, &list); err != nil {
		return 0, err
	}
	if len(list.Callbacks) > 0 {
		n, err := strconv.ParseUint(strings.TrimSpace(list.Callbacks[0].ID), 10, 64)
		if err == nil && n > 0 {
			return n, nil
		}
	}
	var created struct {
		ID string `json:"callback_resource_id"`
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/callback/v1/Create", map[string]any{
		"callback_name": "CRM autodial",
		"description":   "Недозвон 3 автодозвон",
	}, &created); err != nil {
		return 0, fmt.Errorf("создайте Callback в ЛК Exolve или задайте EXOLVE_CALLBACK_RESOURCE_ID: %w", err)
	}
	n, err := strconv.ParseUint(strings.TrimSpace(created.ID), 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("пустой callback_resource_id от Exolve")
	}
	return n, nil
}

func (a *App) exolveMakeCallback(ctx context.Context, numberCode string, callbackResourceID uint64, candidatePhone string, operatorSIPs []string, requestDesc string) (string, error) {
	code, err := strconv.ParseUint(normalizeExolveDID(numberCode), 10, 64)
	if err != nil {
		return "", fmt.Errorf("number_code: %w", err)
	}
	// Скрипт звонит кандидату (line_1); softphone менеджеров (line_2) — только после ответа кандидата.
	line1Dest := []map[string]any{{"number": candidatePhone, "timeout": uint32(35)}}
	line2Dest := make([]map[string]any, 0, len(operatorSIPs))
	for _, sip := range operatorSIPs {
		line2Dest = append(line2Dest, map[string]any{"number": sip, "timeout": uint32(45)})
	}
	body := map[string]any{
		"request_description":  requestDesc,
		"number_code":          code,
		"callback_resource_id": callbackResourceID,
		"duration":             uint32(1800),
		"line_1": map[string]any{
			"destinations": line1Dest,
			"ring_logic":   1,
			// Без TTS: иначе GetInfo даёт play_audio_* ещё на гудках / без answered,
			// а после TTS часто busy без line_2 — в админке нет «Принять».
		},
		"line_2": map[string]any{
			"destinations": line2Dest,
			"ring_logic":   2,
		},
	}
	var out struct {
		CallID string `json:"call_id"`
	}
	if err := a.exolvePOST(ctx, "https://api.exolve.ru/call/v1/MakeCallback", body, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.CallID), nil
}

// autodialHistory — админ: журнал всех скорозвонов + сводка.
func (a *App) autodialHistory(w http.ResponseWriter, r *http.Request) {
	if !autodialFeatureEnabled() {
		writeAutodialPaused(w)
		return
	}
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для админа"})
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tab := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tab")))
	if tab == "" {
		tab = "all"
	}
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	offset := 0
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n >= 0 {
		offset = n
	}

	where := []string{"true"}
	args := []any{}
	argN := 1

	days := 0
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && (n == 1 || n == 7 || n == 30) {
		days = n
		where = append(where, fmt.Sprintf("a.started_at >= now() - ($%d::text || ' days')::interval", argN))
		args = append(args, strconv.Itoa(days))
		argN++
	}

	switch tab {
	case "active":
		where = append(where, "a.finished_at IS NULL AND lower(a.status) IN ('started','dialing','ringing','connected')")
	case "connected":
		where = append(where, "lower(a.status)='connected'")
	case "accepted":
		where = append(where, "a.accepted_by IS NOT NULL")
	case "miss":
		where = append(where, "lower(a.status) IN ('no_answer','rejected','canceled','failed','error')")
	}

	if q != "" {
		like := "%" + q + "%"
		where = append(where, fmt.Sprintf(`(
			coalesce(c.phone,'') ILIKE $%d
			OR coalesce(c.full_name,'') ILIKE $%d
			OR coalesce(d.title,'') ILIKE $%d
			OR coalesce(a.exolve_call_id,'') ILIKE $%d
			OR coalesce(a.number_code,'') ILIKE $%d
			OR coalesce(op.full_name,'') ILIKE $%d
		)`, argN, argN, argN, argN, argN, argN))
		args = append(args, like)
		argN++
	}
	whereSQL := strings.Join(where, " AND ")

	fromSQL := `
		FROM autodial_attempts a
		JOIN deals d ON d.id=a.deal_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN users op ON op.id=a.accepted_by`

	var totalFiltered int
	countSQL := `SELECT count(*) ` + fromSQL + ` WHERE ` + whereSQL
	if err := a.db.QueryRow(ctx, countSQL, args...).Scan(&totalFiltered); err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось посчитать"})
		return
	}

	listArgs := append(append([]any{}, args...), limit, offset)
	listSQL := fmt.Sprintf(`
		SELECT a.id::text, a.deal_id::text, a.status, coalesce(a.exolve_call_id,''),
		       coalesce(a.number_code,''), coalesce(a.error,''),
		       a.started_at, a.finished_at,
		       coalesce(a.accepted_by::text,''), a.accepted_at,
		       coalesce(d.title,''), coalesce(c.full_name,''), coalesce(c.phone,''),
		       coalesce(op.full_name,''),
		       CASE WHEN a.finished_at IS NOT NULL AND a.started_at IS NOT NULL
		            THEN greatest(0, floor(extract(epoch from (a.finished_at - a.started_at))))::int
		            ELSE 0 END AS duration_sec
		%s
		WHERE %s
		ORDER BY a.started_at DESC
		LIMIT $%d OFFSET $%d`, fromSQL, whereSQL, argN, argN+1)

	rows, err := a.db.Query(ctx, listSQL, listArgs...)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось загрузить историю"})
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id, dealID, status, callID, cli, errText, title, contact, phone, operator string
			acceptedBy                                                                string
			started                                                                   time.Time
			finished, acceptedAt                                                      *time.Time
			durSec                                                                    int
		)
		if rows.Scan(
			&id, &dealID, &status, &callID, &cli, &errText,
			&started, &finished, &acceptedBy, &acceptedAt,
			&title, &contact, &phone, &operator, &durSec,
		) != nil {
			continue
		}
		item := map[string]any{
			"id":           id,
			"deal_id":      dealID,
			"status":       status,
			"call_id":      callID,
			"cli":          cli,
			"error":        errText,
			"started_at":   started.UTC().Format(time.RFC3339),
			"title":        title,
			"contact":      contact,
			"phone":        phone,
			"operator":     operator,
			"accepted_by":  acceptedBy,
			"duration_sec": durSec,
		}
		if finished != nil {
			item["finished_at"] = finished.UTC().Format(time.RFC3339)
		}
		if acceptedAt != nil {
			item["accepted_at"] = acceptedAt.UTC().Format(time.RFC3339)
		}
		if m := talkMsRe.FindStringSubmatch(errText); len(m) == 2 {
			if ms, err := strconv.Atoi(m[1]); err == nil && ms > 0 {
				item["talk_sec"] = (ms + 500) / 1000
			}
		}
		items = append(items, item)
	}

	type bucket struct {
		Total     int `json:"total"`
		Active    int `json:"active"`
		Connected int `json:"connected"`
		Accepted  int `json:"accepted"`
		Miss      int `json:"miss"`
		Dialed    int `json:"dialed"`
	}
	var stats bucket
	statsWhere := "true"
	statsArgs := []any{}
	if days > 0 {
		statsWhere = "started_at >= now() - ($1::text || ' days')::interval"
		statsArgs = append(statsArgs, strconv.Itoa(days))
	}
	_ = a.db.QueryRow(ctx, `
		SELECT
			count(*)::int,
			count(*) FILTER (WHERE finished_at IS NULL AND lower(status) IN ('started','dialing','ringing','connected'))::int,
			count(*) FILTER (WHERE lower(status)='connected')::int,
			count(*) FILTER (WHERE accepted_by IS NOT NULL)::int,
			count(*) FILTER (WHERE lower(status) IN ('no_answer','rejected','canceled','failed','error'))::int,
			count(*) FILTER (WHERE finished_at IS NOT NULL OR lower(status) NOT IN ('started','dialing'))::int
		FROM autodial_attempts WHERE `+statsWhere, statsArgs...).Scan(
		&stats.Total, &stats.Active, &stats.Connected, &stats.Accepted, &stats.Miss, &stats.Dialed,
	)

	writeJSON(w, 200, map[string]any{
		"items":  items,
		"total":  totalFiltered,
		"limit":  limit,
		"offset": offset,
		"tab":    tab,
		"days":   days,
		"stats":  stats,
	})
}
