package server

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

func (a *App) callReviewLoop() {
	time.Sleep(8 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		a.reviewPendingCalls(ctx)
		cancel()
		time.Sleep(45 * time.Second)
	}
}

// reviewPendingCalls ставит звонки с записью в очередь и прогоняет уже сохранённый транскрипт.
// Транскрибация внешним сервисом не вызывается: без текста статус остаётся pending.
func (a *App) reviewPendingCalls(ctx context.Context) {
	if a.db == nil {
		return
	}
	a.ensureSecuritySchema(ctx)
	rows, err := a.db.Query(ctx, `
		SELECT c.id::text, coalesce(c.user_id::text,''), coalesce(c.recording_key,''), coalesce(c.transcript,'')
		FROM calls c
		LEFT JOIN call_reviews cr ON cr.call_id=c.id
		WHERE c.started_at > now() - interval '14 days'
		  AND cr.call_id IS NULL
		ORDER BY c.started_at DESC
		LIMIT 40`)
	if err != nil {
		log.Printf("call review scan: %v", err)
		return
	}
	defer rows.Close()
	type pending struct{ id, user, recording, transcript string }
	var batch []pending
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.user, &item.recording, &item.transcript); err != nil {
			continue
		}
		batch = append(batch, item)
	}
	for _, item := range batch {
		if strings.TrimSpace(item.transcript) != "" {
			a.reviewCallTranscript(ctx, item.id, item.user, item.transcript)
			continue
		}
		status := "skipped"
		if strings.TrimSpace(item.recording) != "" {
			status = "pending"
		}
		_, _ = a.db.Exec(ctx, `
			INSERT INTO call_reviews (call_id, user_id, status)
			VALUES ($1::uuid, nullif($2,'')::uuid, $3)
			ON CONFLICT (call_id) DO NOTHING`, item.id, item.user, status)
	}

	ready, err := a.db.Query(ctx, `
		SELECT c.id::text, coalesce(c.user_id::text,''), c.transcript
		FROM calls c
		JOIN call_reviews cr ON cr.call_id=c.id
		WHERE cr.status='pending' AND length(btrim(c.transcript)) > 0
		LIMIT 20`)
	if err != nil {
		return
	}
	defer ready.Close()
	for ready.Next() {
		var id, user, text string
		if err := ready.Scan(&id, &user, &text); err != nil {
			continue
		}
		a.reviewCallTranscript(ctx, id, user, text)
	}
}

func (a *App) reviewCallTranscript(ctx context.Context, callID, userID, original string) (status, rule, incidentID string) {
	a.ensureSecuritySchema(ctx)
	original = strings.TrimSpace(original)
	masked := maskForModel(original)
	status = "clean"
	var prev string
	_ = a.db.QueryRow(ctx, `SELECT status FROM call_reviews WHERE call_id=$1::uuid`, callID).Scan(&prev)
	hit, ok := checkOutboundPolicy(original)
	if ok {
		status = "flagged"
		rule = hit.Rule
		if prev == "flagged" {
			_ = a.db.QueryRow(ctx, `SELECT coalesce(incident_id::text,'') FROM call_reviews WHERE call_id=$1::uuid`, callID).Scan(&incidentID)
		} else {
			incidentID = a.recordSecurityEvent(ctx, securityEvent{
				ActorID: userID, EventType: "call_phrase", Rule: hit.Rule, Severity: hit.Severity, Status: "open",
				ObjectType: "call", ObjectID: callID, Summary: hit.Message,
				Detail: map[string]any{"excerpt": policyExcerpt(original)},
			})
			if userID != "" {
				a.logAct(ctx, "user", userID, userID, "call_phrase", map[string]any{
					"call_id": callID, "rule": hit.Rule, "incident_id": incidentID,
				})
			}
		}
	}
	_, err := a.db.Exec(ctx, `
		INSERT INTO call_reviews (call_id, user_id, status, transcript, matched_rule, incident_id, reviewed_at)
		VALUES ($1::uuid, nullif($2,'')::uuid, $3, $4, $5, nullif($6,'')::uuid, now())
		ON CONFLICT (call_id) DO UPDATE SET
			status=EXCLUDED.status,
			transcript=EXCLUDED.transcript,
			matched_rule=EXCLUDED.matched_rule,
			incident_id=COALESCE(call_reviews.incident_id, EXCLUDED.incident_id),
			reviewed_at=now()`,
		callID, userID, status, masked, rule, incidentID)
	if err != nil {
		log.Printf("call review save: %v", err)
	}
	_, _ = a.db.Exec(ctx, `UPDATE calls SET transcript=$2 WHERE id=$1::uuid`, callID, masked)
	return status, rule, incidentID
}

func (a *App) postCallTranscript(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) && !isROP(r) {
		writeJSON(w, 403, map[string]string{"error": "транскрипт проверяет РОП или администратор"})
		return
	}
	callID := strings.TrimSpace(chi.URLParam(r, "id"))
	var in struct {
		Transcript string `json:"transcript"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный JSON"})
		return
	}
	text := strings.TrimSpace(in.Transcript)
	if text == "" {
		writeJSON(w, 400, map[string]string{"error": "пустой транскрипт"})
		return
	}
	if utf8.RuneCountInString(text) > 20000 {
		text = string([]rune(text)[:20000])
	}
	var userID, dept string
	err := a.db.QueryRow(r.Context(), `
		SELECT coalesce(c.user_id::text,''), coalesce(u.department_id::text,'')
		FROM calls c
		LEFT JOIN users u ON u.id=c.user_id
		WHERE c.id=$1::uuid
		ORDER BY c.started_at DESC
		LIMIT 1`, callID).Scan(&userID, &dept)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "звонок не найден"})
		return
	}
	if isROP(r) && !isAdmin(r) {
		mine := a.actorDepartmentID(r.Context(), actorID(r))
		if mine == "" || dept != mine {
			writeJSON(w, 403, map[string]string{"error": "звонок не вашего отдела"})
			return
		}
	}
	status, rule, incidentID := a.reviewCallTranscript(r.Context(), callID, userID, text)
	writeJSON(w, 200, map[string]any{
		"ok": true, "status": status, "rule": rule, "incident_id": incidentID,
	})
}
