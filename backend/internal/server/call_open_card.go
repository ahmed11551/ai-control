package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var (
	errCallCardNoPhone = errors.New("нет номера клиента")
	errCallCardHidden  = errors.New("карточка с этим номером уже есть у другого менеджера")
	errCallCardFailed  = errors.New("не удалось создать карточку")
	errCallNotFound    = errors.New("звонок не найден")
)

var softphoneUIDRe = regexp.MustCompile(`^[A-Za-z0-9_]{4,64}$`)

// callClientPhone — номер клиента в звонке, не номер менеджера.
func callClientPhone(direction, from, to string) string {
	raw := from
	if callDirectionOutbound(direction) {
		raw = to
	}
	return e164RU(raw)
}

func validCallClientPhone(phone string) bool {
	p := e164RU(phone)
	return len(p) == 11 && strings.HasPrefix(p, "7")
}

// sqlCallLeadByPhone — живая карточка с этим номером.
// Спящие не считаются: звонок открывает обычный лид, даже если номер есть только там.
const sqlCallLeadByPhone = `
	SELECT l.id::text
	FROM leads l
	JOIN contacts c ON c.id = l.contact_id
	WHERE right(regexp_replace(coalesce(c.phone,''), '[^0-9]', '', 'g'), 10) = right($1, 10)
	  AND coalesce(l.status, '') <> 'rejected'
	  AND NOT EXISTS (
	    SELECT 1 FROM deals sd
	    LEFT JOIN pipelines sp ON sp.id = sd.pipeline_id
	    WHERE (sd.lead_id = l.id OR sd.id = l.converted_deal_id)
	      AND (
	        coalesce(sp.bitrix_id, '') = '` + oldLeadsPipelineKey + `'
	        OR coalesce(sd.sleeping, false)
	      )
	  )
	  AND NOT EXISTS (
	    SELECT 1 FROM deals ld
	    JOIN pipeline_stages ls ON ls.id = ld.stage_id
	    WHERE (ld.lead_id = l.id OR ld.id = l.converted_deal_id)
	      AND ls.is_lost
	  )
	ORDER BY (l.owner_id = $2::uuid) DESC, l.created_at DESC
	LIMIT 1`

// openCallCard создаёт лид только по нажатию «Открыть карточку».
// Сам звонок этот запрос не ждёт.
func (a *App) openCallCard(w http.ResponseWriter, r *http.Request) {
	actor, _ := r.Context().Value(ctxUserID).(string)
	actor = strings.TrimSpace(actor)
	if actor == "" {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	var in struct {
		CallID string `json:"call_id"`
		UID    string `json:"uid"`
		Peer   string `json:"peer"`
	}
	_ = readJSON(r, &in)
	phone, callID, attachUID, err := a.phoneForOpenCard(r.Context(), actor, in.CallID, in.UID, in.Peer)
	if err != nil {
		writeOpenCardErr(w, err)
		return
	}
	leadID, dealID, created, err := a.ensureCallLead(r.Context(), a.crmScope(r), phone)
	if err != nil {
		writeOpenCardErr(w, err)
		return
	}
	a.attachOpenedCall(r.Context(), actor, callID, attachUID, leadID, dealID)
	writeJSON(w, 200, map[string]any{
		"lead_id": leadID,
		"deal_id": dealID,
		"created": created,
	})
}

func writeOpenCardErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errCallCardNoPhone):
		writeJSON(w, 400, map[string]string{"error": err.Error()})
	case errors.Is(err, errCallNotFound):
		writeJSON(w, 404, map[string]string{"error": err.Error()})
	case errors.Is(err, errCallCardHidden):
		writeJSON(w, 409, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, 500, map[string]string{"error": errCallCardFailed.Error()})
	}
}

// phoneForOpenCard берёт номер из строки звонка. Номер с клиента — только если
// журнал ещё не успел записать звонок, а кнопка уже нажата.
func samePhoneTail(a, b string) bool {
	da, db := e164RU(a), e164RU(b)
	if len(da) < 10 || len(db) < 10 {
		return false
	}
	return da[len(da)-10:] == db[len(db)-10:]
}

func (a *App) phoneForOpenCard(ctx context.Context, actor, callID, softUID, peer string) (string, string, string, error) {
	callID = strings.TrimSpace(callID)
	softUID = strings.TrimSpace(softUID)
	if !softphoneUIDRe.MatchString(softUID) {
		softUID = ""
	}
	if a == nil || a.db == nil {
		if callID != "" || softUID != "" || validCallClientPhone(peer) {
			return "", "", "", errCallCardFailed
		}
		return "", "", "", errCallCardNoPhone
	}
	if callID != "" {
		var dir, from, to, owner string
		err := a.db.QueryRow(ctx, `
			SELECT coalesce(direction,''), coalesce(from_number,''), coalesce(to_number,''), coalesce(user_id::text,'')
			FROM calls WHERE id=$1::uuid`, callID).Scan(&dir, &from, &to, &owner)
		if err != nil || owner != actor {
			return "", "", "", errCallNotFound
		}
		phone := callClientPhone(dir, from, to)
		if !validCallClientPhone(phone) {
			return "", "", "", errCallCardNoPhone
		}
		return phone, callID, "", nil
	}
	peerPhone := e164RU(peer)
	peerOK := validCallClientPhone(peerPhone)
	if softUID != "" {
		var id, dir, from, to string
		err := a.db.QueryRow(ctx, `
			SELECT id::text, coalesce(direction,''), coalesce(from_number,''), coalesce(to_number,'')
			FROM calls
			WHERE user_id=$1::uuid
			  AND asterisk_uniqueid IN ($2, $3)
			ORDER BY started_at DESC
			LIMIT 1`,
			actor,
			"softphone:voximplant:"+softUID,
			"softphone:exolve:"+softUID,
		).Scan(&id, &dir, &from, &to)
		if err == nil {
			phone := callClientPhone(dir, from, to)
			if validCallClientPhone(phone) && (!peerOK || samePhoneTail(phone, peerPhone)) {
				return phone, id, softUID, nil
			}
			// uid от прошлого звонка, на экране уже другой номер — карточку делаем по нему.
			if peerOK {
				return peerPhone, "", "", nil
			}
			if validCallClientPhone(phone) {
				return phone, id, softUID, nil
			}
			return "", "", "", errCallCardNoPhone
		}
	}
	if !peerOK {
		return "", "", "", errCallCardNoPhone
	}
	return peerPhone, "", softUID, nil
}

// ensureCallLead открывает живой лид с этим номером или создаёт один.
// Повтор под тем же замком не создаёт вторую карточку.
func (a *App) ensureCallLead(ctx context.Context, scope crmScope, phone string) (leadID, dealID string, created bool, err error) {
	phone = e164RU(phone)
	if !validCallClientPhone(phone) {
		return "", "", false, errCallCardNoPhone
	}
	if a == nil || a.db == nil || strings.TrimSpace(scope.UserID) == "" {
		return "", "", false, errCallCardFailed
	}
	actor := strings.TrimSpace(scope.UserID)
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", "", false, errCallCardFailed
	}
	defer tx.Rollback(ctx)

	lockKey := "callcard:" + phone[len(phone)-10:]
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(abs(hashtext($1)::bigint))`, lockKey); err != nil {
		return "", "", false, errCallCardFailed
	}

	var existing string
	err = tx.QueryRow(ctx, sqlCallLeadByPhone, phone, actor).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, errCallCardFailed
	}
	if strings.TrimSpace(existing) != "" {
		if err = tx.Commit(ctx); err != nil {
			return "", "", false, errCallCardFailed
		}
		existing = strings.TrimSpace(existing)
		if !a.canSeeLead(ctx, scope, existing) {
			return "", "", false, errCallCardHidden
		}
		dealID = a.anyDealIDForLead(ctx, existing)
		if dealID == "" {
			dealID = a.ensureDealForLead(ctx, existing, "", nil)
		}
		return existing, dealID, false, nil
	}

	var contactID string
	_ = tx.QueryRow(ctx, `
		SELECT id::text FROM contacts
		WHERE right(regexp_replace(coalesce(phone,''), '[^0-9]', '', 'g'), 10) = right($1, 10)
		ORDER BY created_at DESC
		LIMIT 1`, phone).Scan(&contactID)
	if strings.TrimSpace(contactID) == "" {
		if err = tx.QueryRow(ctx, `
			INSERT INTO contacts (full_name, phone, source)
			VALUES ($1, $2, 'call')
			RETURNING id::text`, phone, phone).Scan(&contactID); err != nil || contactID == "" {
			return "", "", false, errCallCardFailed
		}
	}
	if err = tx.QueryRow(ctx, `
		INSERT INTO leads (title, source, status, owner_id, contact_id, city, card_note)
		VALUES ($1, 'call', 'new', $2::uuid, $3::uuid, '', '')
		RETURNING id::text`, phone, actor, contactID).Scan(&leadID); err != nil || leadID == "" {
		return "", "", false, errCallCardFailed
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", false, errCallCardFailed
	}
	a.logAct(ctx, "lead", leadID, actor, "created", map[string]any{
		"title": phone, "source": "call",
	})
	dealID = a.ensureDealForLead(ctx, leadID, actor, map[string]any{"title": phone})
	if dealID == "" {
		dealID = a.anyDealIDForLead(ctx, leadID)
	}
	return leadID, dealID, true, nil
}

func (a *App) attachOpenedCall(ctx context.Context, actor, callID, softUID, leadID, dealID string) {
	if a == nil || a.db == nil {
		return
	}
	leadID = strings.TrimSpace(leadID)
	if leadID == "" {
		return
	}
	ent, eid := "lead", leadID
	if strings.TrimSpace(dealID) != "" {
		ent, eid = "deal", strings.TrimSpace(dealID)
	}
	callID = strings.TrimSpace(callID)
	// Пустой звонок или временная привязка к контакту (журнал успел раньше сделки)
	// переводится на карточку. Чужую сделку не трогаем.
	const attachWhere = `
		  AND (
		    entity_id IS NULL
		    OR entity_id = $DEAL::uuid
		    OR entity_id = $LEAD::uuid
		    OR (
		      coalesce(entity_type,'') = 'contact'
		      AND entity_id = (SELECT contact_id FROM leads WHERE id = $LEAD::uuid)
		    )
		  )`
	if callID != "" {
		q := strings.ReplaceAll(strings.ReplaceAll(`
			UPDATE calls
			SET entity_type=$3, entity_id=$4::uuid
			WHERE id=$1::uuid AND user_id=$2::uuid
		`+attachWhere, "$DEAL", "$4"), "$LEAD", "$5")
		_, _ = a.db.Exec(ctx, q, callID, actor, ent, eid, leadID)
	}
	softUID = strings.TrimSpace(softUID)
	if softUID == "" || !softphoneUIDRe.MatchString(softUID) {
		return
	}
	q := strings.ReplaceAll(strings.ReplaceAll(`
		UPDATE calls
		SET entity_type=$2, entity_id=$3::uuid
		WHERE user_id=$1::uuid
		  AND asterisk_uniqueid IN ($4, $5)
	`+attachWhere, "$DEAL", "$3"), "$LEAD", "$6")
	_, _ = a.db.Exec(ctx, q,
		actor, ent, eid,
		"softphone:voximplant:"+softUID,
		"softphone:exolve:"+softUID,
		leadID)
}
