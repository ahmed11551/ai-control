package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Нерабочий MAX остаётся у менеджера. Новый номер выдаётся только если
// телефон целиком никому не привязан: ни один профиль этого номера.
var (
	maxSendDownMu    sync.Mutex
	maxSendDownReady bool

	maxAssignMissMu sync.Mutex
	maxAssignMiss   = map[string]time.Time{}
)

func (a *App) ensureMaxSendDown(ctx context.Context) {
	maxSendDownMu.Lock()
	defer maxSendDownMu.Unlock()
	if maxSendDownReady {
		return
	}
	_, err := a.db.Exec(ctx, `
		ALTER TABLE inbox_lines
		ADD COLUMN IF NOT EXISTS max_send_down BOOLEAN NOT NULL DEFAULT false`)
	if err != nil {
		log.Printf("max_send_down: %v", err)
		return
	}
	_, err = a.db.Exec(ctx, `
		ALTER TABLE inbox_lines
		ADD COLUMN IF NOT EXISTS max_cold_limit BOOLEAN NOT NULL DEFAULT false`)
	if err != nil {
		log.Printf("max_send_down: %v", err)
		return
	}
	maxSendDownReady = true
}

func maxClientSendMiss(msg string) bool {
	d := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case strings.Contains(d, "клиент не найден"),
		strings.Contains(d, "не зарегистрирован"),
		strings.Contains(d, "failed to find user"),
		strings.Contains(d, "user not found"),
		strings.Contains(d, "chat not found"),
		strings.Contains(d, "слишком много"),
		strings.Contains(d, "rate limit"),
		strings.Contains(d, "нет телефона"),
		strings.Contains(d, "нет получателя"),
		strings.Contains(d, "public_base_url"):
		return true
	default:
		return false
	}
}

func maxAccountSendDead(msg string) bool {
	d := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case strings.Contains(d, "нет доступа к профилю"),
		strings.Contains(d, "профиль мессенджера не найден"),
		strings.Contains(d, "not authorized"),
		strings.Contains(d, "unauthorized"),
		strings.Contains(d, "forbidden"),
		strings.Contains(d, "spam"),
		strings.Contains(d, "offline"),
		strings.Contains(d, "сессия"):
		return true
	default:
		return false
	}
}

func phoneKey10(s string) string {
	d := digitsOnly(s)
	if len(d) == 11 && (d[0] == '7' || d[0] == '8') {
		d = d[1:]
	}
	if len(d) > 10 {
		d = d[len(d)-10:]
	}
	if len(d) < 10 {
		return ""
	}
	return d
}

func profileRefMatch(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	const n = 13
	return len(a) >= n && len(b) >= n && a[:n] == b[:n]
}

func phoneTail4(s string) string {
	d := digitsOnly(s)
	if len(d) < 4 {
		return ""
	}
	return d[len(d)-4:]
}

func tailLabel(s string) string {
	t := phoneTail4(s)
	if t == "" {
		return "этот номер"
	}
	return "····" + t
}

// pickFreeWorkingMax — рабочий профиль, чей телефон не занят ни одним профилем.
func pickFreeWorkingMax(profiles []wappiProfile, boundRefs, boundPhones []string) (wappiProfile, bool) {
	takenPhone := map[string]bool{}
	for _, raw := range boundPhones {
		if k := phoneKey10(raw); k != "" {
			takenPhone[k] = true
		}
	}
	for _, p := range profiles {
		if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
			continue
		}
		matched := false
		for _, ref := range boundRefs {
			if profileRefMatch(p.ProfileID, ref) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, raw := range []string{p.Phone, p.Name} {
			if k := phoneKey10(raw); k != "" {
				takenPhone[k] = true
			}
		}
	}

	var best wappiProfile
	found := false
	for _, p := range profiles {
		if !wappiProfileWorking(p) {
			continue
		}
		if p.Platform != "" && !strings.EqualFold(p.Platform, "max") {
			continue
		}
		ph := maxNormPhone(p.Phone)
		if ph == "" {
			ph = maxNormPhone(p.Name)
		}
		if ph == "" {
			continue
		}
		if takenPhone[phoneKey10(ph)] {
			continue
		}
		refTaken := false
		for _, ref := range boundRefs {
			if profileRefMatch(p.ProfileID, ref) {
				refTaken = true
				break
			}
		}
		if refTaken {
			continue
		}
		if !found || ph < maxNormPhone(best.Phone) {
			best = p
			found = true
		}
	}
	return best, found
}

func maxDownNotice(downPhone, alternatePhone, assignedPhone string, assigned bool) string {
	down := tailLabel(downPhone)
	if assigned && strings.TrimSpace(assignedPhone) != "" {
		return fmt.Sprintf("Не отправилось. Номер %s не работает. Вам добавлен рабочий номер %s. Новым клиентам пишите с него. Этот чат он не продолжает.", down, tailLabel(assignedPhone))
	}
	if strings.TrimSpace(alternatePhone) != "" {
		return fmt.Sprintf("Не отправилось. Номер %s не работает. Новым клиентам пишите с %s. Этот чат с другого номера не продолжится.", down, tailLabel(alternatePhone))
	}
	return fmt.Sprintf("Не отправилось. Номер %s не работает. Свободных рабочих номеров нет. Этот чат клиенту не дойдёт, старая переписка на месте.", down)
}

func maxColdNotice(downPhone string) string {
	return fmt.Sprintf("С номера %s нельзя написать новому человеку: лимит на новые контакты. Старые чаты с этого номера остаются.", tailLabel(downPhone))
}

func (a *App) maxSendFailureIsLine(ctx context.Context, lineID string, err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if maxClientSendMiss(msg) {
		return false
	}
	if maxAccountSendDead(msg) {
		return true
	}
	if !a.cfg.WappiReady() {
		return false
	}
	var ref string
	if a.db.QueryRow(ctx, `SELECT coalesce(external_ref,'') FROM inbox_lines WHERE id=$1::uuid`, lineID).Scan(&ref) != nil {
		return false
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	raw, err2 := a.wappiGetStatus(ctx, ref)
	if err2 != nil || raw == nil {
		return false
	}
	switch v := raw["authorized"].(type) {
	case bool:
		if !v {
			return true
		}
	case string:
		if strings.EqualFold(v, "false") || v == "0" {
			return true
		}
	}
	st := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw["app_status"])))
	return st == "close" || st == "offline"
}

func (a *App) otherWorkingMax(ctx context.Context, userID, exceptLineID string) (id, phone string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", ""
	}
	a.ensureMaxSendDown(ctx)
	_ = a.db.QueryRow(ctx, `
		SELECT l.id::text, coalesce(nullif(l.operator_label,''), l.name, '')
		FROM inbox_lines l
		WHERE l.owner_user_id=$1::uuid AND l.provider='wappi' AND l.channel='max' AND l.is_active
		  AND coalesce(l.max_send_down,false)=false
		  AND coalesce(l.max_cold_limit,false)=false
		  AND ($2='' OR l.id::text<>$2)
		ORDER BY l.name
		LIMIT 1`, userID, strings.TrimSpace(exceptLineID)).Scan(&id, &phone)
	return id, phone
}

func (a *App) countOwnedMax(ctx context.Context, userID string) int {
	var n int
	_ = a.db.QueryRow(ctx, `
		SELECT count(*) FROM inbox_lines
		WHERE owner_user_id=$1::uuid AND provider='wappi' AND channel='max'`, userID).Scan(&n)
	return n
}

func (a *App) ensureManagerMaxLine(ctx context.Context, userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" || a.countOwnedMax(ctx, userID) == 0 {
		return
	}
	if a.hasNonDownMax(ctx, userID) {
		return
	}
	_, _, _ = a.assignFreeWorkingMax(ctx, userID)
}

func (a *App) hasNonDownMax(ctx context.Context, userID string) bool {
	var id string
	_ = a.db.QueryRow(ctx, `
		SELECT l.id::text
		FROM inbox_lines l
		WHERE l.owner_user_id=$1::uuid AND l.provider='wappi' AND l.channel='max' AND l.is_active
		  AND coalesce(l.max_send_down,false)=false
		LIMIT 1`, userID).Scan(&id)
	return id != ""
}

func (a *App) loadBoundMax(ctx context.Context) (refs, phones []string) {
	rows, err := a.db.Query(ctx, `
		SELECT coalesce(external_ref,''), coalesce(operator_label,''), coalesce(name,'')
		FROM inbox_lines
		WHERE provider='wappi' AND channel='max' AND owner_user_id IS NOT NULL`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	for rows.Next() {
		var ref, label, name string
		if rows.Scan(&ref, &label, &name) != nil {
			continue
		}
		if strings.TrimSpace(ref) != "" {
			refs = append(refs, ref)
		}
		phones = append(phones, label, name)
	}
	return refs, phones
}

func (a *App) assignFreeWorkingMax(ctx context.Context, userID string) (lineID, phone string, ok bool) {
	return a.grantFreeWorkingMax(ctx, userID, false)
}

func ownedSessionWorking(lines []maxBoardLine, profiles []wappiProfile) bool {
	for _, ln := range lines {
		for _, p := range profiles {
			if !wappiProfileWorking(p) {
				continue
			}
			ph := maxNormPhone(p.Phone)
			if ph == "" {
				ph = maxNormPhone(p.Name)
			}
			if profileRefMatch(p.ProfileID, ln.Ref) || (phoneKey10(ph) != "" && phoneKey10(ph) == phoneKey10(ln.Phone)) {
				return true
			}
		}
	}
	return false
}

func (a *App) grantFreeWorkingMax(ctx context.Context, userID string, manual bool) (lineID, phone string, ok bool) {
	userID = strings.TrimSpace(userID)
	if userID == "" || !a.cfg.WappiReady() {
		return "", "", false
	}
	if !manual {
		maxAssignMissMu.Lock()
		if until, hit := maxAssignMiss[userID]; hit && time.Now().Before(until) {
			maxAssignMissMu.Unlock()
			return "", "", false
		}
		maxAssignMissMu.Unlock()
	}

	a.ensureMaxSendDown(ctx)
	a.ensureMaxProfileChecks(ctx)
	a.ensureInboxLineHistoryFrom(ctx)
	profiles, err := a.wappiListProfiles(ctx)
	if err != nil {
		return "", "", false
	}
	refs, phones := a.loadBoundMax(ctx)
	checks := a.loadMaxProfileChecks(ctx)

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", "", false
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(81493001)`); err != nil {
		return "", "", false
	}
	if manual {
		orows, qerr := tx.Query(ctx, `
			SELECT coalesce(external_ref,''), coalesce(operator_label,''), coalesce(name,'')
			FROM inbox_lines
			WHERE owner_user_id=$1::uuid AND provider='wappi' AND channel='max' AND is_active
			  AND coalesce(max_send_down,false)=false
			  AND coalesce(max_cold_limit,false)=false`, userID)
		if qerr != nil {
			return "", "", false
		}
		var owned []maxBoardLine
		for orows.Next() {
			var ref, label, name string
			if orows.Scan(&ref, &label, &name) != nil {
				continue
			}
			ph := maxNormPhone(label)
			if ph == "" {
				ph = maxNormPhone(name)
			}
			owned = append(owned, maxBoardLine{Ref: ref, Phone: ph})
		}
		orows.Close()
		if ownedSessionWorking(owned, profiles) {
			_ = tx.Commit(ctx)
			return "", "", false
		}
	} else {
		var alive string
		_ = tx.QueryRow(ctx, `
			SELECT id::text FROM inbox_lines
			WHERE owner_user_id=$1::uuid AND provider='wappi' AND channel='max' AND is_active
			  AND coalesce(max_send_down,false)=false
			  AND coalesce(max_cold_limit,false)=false
			LIMIT 1`, userID).Scan(&alive)
		if alive != "" {
			_ = tx.Commit(ctx)
			return "", "", false
		}
	}

	for range len(profiles) + 1 {
		picked, found := pickFreeWorkingMax(profiles, refs, phones)
		if !found {
			maxAssignMissMu.Lock()
			maxAssignMiss[userID] = time.Now().Add(2 * time.Minute)
			maxAssignMissMu.Unlock()
			_ = tx.Commit(ctx)
			return "", "", false
		}
		ph := maxNormPhone(picked.Phone)
		if ph == "" {
			ph = maxNormPhone(picked.Name)
		}
		tail := phoneKey10(ph)
		ref := strings.TrimSpace(picked.ProfileID)
		var taken string
		_ = tx.QueryRow(ctx, `
			SELECT id::text FROM inbox_lines
			WHERE provider='wappi' AND channel='max' AND owner_user_id IS NOT NULL
			  AND (
			    left(coalesce(external_ref,''), 13) = left($1, 13)
			    OR right(regexp_replace(coalesce(operator_label,''), '\D', '', 'g'), 10) = $2
			    OR right(regexp_replace(coalesce(name,''), '\D', '', 'g'), 10) = $2
			  )
			LIMIT 1`, ref, tail).Scan(&taken)
		mine := lookupMaxCheck(checks, ref)
		hasOK, _, hasAny := phoneCheckState(profiles, checks, ph)
		switch {
		case mine == "ok":
		case mine == "no" || mine == "unauthorized":
			refs = append(refs, ref)
			if !hasOK {
				phones = append(phones, ph)
			}
			continue
		case hasOK || hasAny:
			refs = append(refs, ref)
			if !hasOK {
				phones = append(phones, ph)
			}
			continue
		}
		if taken != "" {
			refs = append(refs, ref)
			phones = append(phones, ph)
			continue
		}
		name := "MAX · " + ph
		var existing, existingOwner string
		_ = tx.QueryRow(ctx, `
			SELECT id::text, coalesce(owner_user_id::text,'')
			FROM inbox_lines
			WHERE provider='wappi' AND channel='max'
			  AND coalesce(external_ref,'') <> ''
			  AND left(external_ref, 13) = left($1, 13)
			LIMIT 1`, ref).Scan(&existing, &existingOwner)
		if existing != "" && existingOwner != "" {
			refs = append(refs, ref)
			phones = append(phones, ph)
			continue
		}
		if existing != "" {
			_, err = tx.Exec(ctx, `
				UPDATE inbox_lines
				SET owner_user_id=$2::uuid, is_active=true, max_send_down=false, max_cold_limit=false,
				    operator_label=CASE WHEN coalesce(operator_label,'')='' THEN $3 ELSE operator_label END,
				    name=CASE WHEN coalesce(name,'') IN ('','MAX') THEN $4 ELSE name END,
				    external_ref=CASE WHEN coalesce(external_ref,'')='' THEN $5 ELSE external_ref END,
				    history_from=now()
				WHERE id=$1::uuid`, existing, userID, ph, name, ref)
			if err != nil {
				return "", "", false
			}
			if err := tx.Commit(ctx); err != nil {
				return "", "", false
			}
			return existing, ph, true
		}
		var newID string
		err = tx.QueryRow(ctx, `
			INSERT INTO inbox_lines (provider, channel, name, kind, operator_label, external_ref, owner_user_id, is_active, history_from, max_send_down)
			VALUES ('wappi','max',$1,'personal',$2,$3,$4::uuid,true,now(),false)
			RETURNING id::text`, name, ph, ref, userID).Scan(&newID)
		if err != nil {
			return "", "", false
		}
		if err := tx.Commit(ctx); err != nil {
			return "", "", false
		}
		return newID, ph, true
	}
	_ = tx.Commit(ctx)
	return "", "", false
}

func (a *App) recoverFailedMaxLine(ctx context.Context, lineID string, sendErr error) map[string]any {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" || sendErr == nil {
		return nil
	}
	if !a.maxSendFailureIsLine(ctx, lineID, sendErr) {
		return nil
	}
	a.ensureMaxSendDown(ctx)
	_, _ = a.db.Exec(ctx, `
		UPDATE inbox_lines SET max_send_down=true
		WHERE id=$1::uuid AND channel='max'`, lineID)
	return a.maxLineReplacement(ctx, lineID, false)
}

func (a *App) maxLineReplacement(ctx context.Context, lineID string, cold bool) map[string]any {
	var owner, phone string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(owner_user_id::text,''), coalesce(nullif(operator_label,''), name, '')
		FROM inbox_lines WHERE id=$1::uuid`, lineID).Scan(&owner, &phone)
	if cold {
		return map[string]any{
			"max_line_down":         false,
			"max_cold_limit":        true,
			"max_stuck_chat":        true,
			"max_notice":            maxColdNotice(phone),
			"max_alternate_line_id": "",
			"max_assigned":          false,
		}
	}
	altID, altPhone := a.otherWorkingMax(ctx, owner, lineID)
	assignedPhone := ""
	assigned := false
	if altID == "" && owner != "" {
		if _, ph, ok := a.assignFreeWorkingMax(ctx, owner); ok {
			assigned = true
			assignedPhone = ph
		}
	}
	notice := maxDownNotice(phone, altPhone, assignedPhone, assigned)
	return map[string]any{
		"max_line_down":         !cold,
		"max_cold_limit":        cold,
		"max_stuck_chat":        true,
		"max_notice":            notice,
		"max_alternate_line_id": altID,
		"max_assigned":          assigned,
	}
}

func (a *App) inboxReplyResult(ctx context.Context, sent bool, note, msgID, lineID string, sendErr error) map[string]any {
	out := map[string]any{"ok": true, "sent": sent, "note": note, "message_id": msgID}
	if sent {
		return out
	}
	extra := a.recoverFailedMaxLine(ctx, lineID, sendErr)
	for k, v := range extra {
		out[k] = v
	}
	return out
}
