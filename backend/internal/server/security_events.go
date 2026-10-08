package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

const securitySchemaSQL = `
CREATE TABLE IF NOT EXISTS security_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    rule_code TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'medium',
    status TEXT NOT NULL DEFAULT 'open',
    object_type TEXT NOT NULL DEFAULT '',
    object_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    comment TEXT NOT NULL DEFAULT '',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS security_events_created_idx ON security_events (created_at DESC);
CREATE INDEX IF NOT EXISTS security_events_actor_idx ON security_events (actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS security_events_status_idx ON security_events (status, created_at DESC);
CREATE TABLE IF NOT EXISTS dlp_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'phrase',
    pattern TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT 'block',
    severity TEXT NOT NULL DEFAULT 'medium',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS dlp_rules_enabled_idx ON dlp_rules (enabled, created_at);
CREATE TABLE IF NOT EXISTS dlp_rule_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id UUID REFERENCES dlp_rules(id) ON DELETE SET NULL,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS dlp_rule_audit_created_idx ON dlp_rule_audit (created_at DESC);
CREATE INDEX IF NOT EXISTS dlp_rule_audit_rule_idx ON dlp_rule_audit (rule_id, created_at DESC);
CREATE TABLE IF NOT EXISTS passport_text_approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    manager_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    approved_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS passport_text_approvals_active_idx
    ON passport_text_approvals (manager_id, expires_at)
    WHERE revoked_at IS NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted_at TIMESTAMPTZ;
CREATE TABLE IF NOT EXISTS call_reviews (
    call_id UUID PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    transcript TEXT NOT NULL DEFAULT '',
    matched_rule TEXT NOT NULL DEFAULT '',
    incident_id UUID REFERENCES security_events(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS call_reviews_status_idx ON call_reviews (status, created_at DESC);
ALTER TABLE calls ADD COLUMN IF NOT EXISTS transcript TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS card_views (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS card_views_actor_time ON card_views (actor_id, viewed_at DESC);
`

func (a *App) ensureSecuritySchema(ctx context.Context) {
	a.securitySchemaOnce.Do(func() {
		if _, err := a.db.Exec(ctx, securitySchemaSQL); err != nil {
			log.Printf("security schema: %v", err)
		}
	})
}

type securityEvent struct {
	ActorID    string
	EventType  string
	Rule       string
	Severity   string
	Status     string
	ObjectType string
	ObjectID   string
	Summary    string
	Detail     map[string]any
}

func policyExcerpt(raw string) string {
	s := strings.TrimSpace(maskForModel(raw))
	if utf8.RuneCountInString(s) <= 180 {
		return s
	}
	return string([]rune(s)[:180])
}

func (a *App) recordSecurityEvent(ctx context.Context, ev securityEvent) string {
	a.ensureSecuritySchema(ctx)
	if ev.Status == "" {
		ev.Status = "open"
	}
	if ev.Severity == "" {
		ev.Severity = "medium"
	}
	detail := ev.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil || len(raw) == 0 {
		raw = []byte("{}")
	}
	var id string
	err = a.db.QueryRow(ctx, `
		INSERT INTO security_events (
			actor_id, event_type, rule_code, severity, status,
			object_type, object_id, summary, detail
		) VALUES (
			nullif($1,'')::uuid, $2, $3, $4, $5, $6, $7, $8, $9::jsonb
		) RETURNING id::text`,
		strings.TrimSpace(ev.ActorID), ev.EventType, ev.Rule, ev.Severity, ev.Status,
		ev.ObjectType, ev.ObjectID, ev.Summary, string(raw),
	).Scan(&id)
	if err != nil {
		log.Printf("security event: %v", err)
		return ""
	}
	if strings.TrimSpace(ev.ActorID) != "" {
		a.enqueueEmployeeAIEvent(ctx, ev.ActorID, ev.ActorID, int(time.Now().Unix()), map[string]any{
			"event_type": ev.EventType, "rule_code": ev.Rule, "severity": ev.Severity, "status": ev.Status,
			"object_type": ev.ObjectType, "object_id": ev.ObjectID, "summary": ev.Summary, "detail": ev.Detail,
		})
	}
	if ev.ActorID != "" && (ev.EventType == "message_blocked" || ev.EventType == "call_phrase") && (ev.Status == "open" || ev.Status == "confirmed") {
		a.refreshSessionRestriction(ctx, ev.ActorID, true)
	}
	return id
}

func (a *App) countRecentViolations(ctx context.Context, userID string) int {
	return a.queryInt(ctx, `
		SELECT count(*) FROM security_events
		WHERE actor_id=$1::uuid
		  AND event_type IN ('message_blocked','call_phrase')
		  AND status IN ('open','confirmed')
		  AND created_at > now() - interval '7 days'`, userID)
}

// allowLock — только новое нарушение может включить ограничение.
// Разбор инцидента снимает его, если открытых нарушений стало меньше порога, и не включает обратно.
func (a *App) refreshSessionRestriction(ctx context.Context, userID string, allowLock bool) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	a.ensureSecuritySchema(ctx)
	n := a.countRecentViolations(ctx, userID)
	if shouldRestrictSession(n) && allowLock {
		_, _ = a.db.Exec(ctx, `
			UPDATE users SET
				session_restricted=true,
				session_restricted_at=coalesce(session_restricted_at, now()),
				session_restricted_reason='повторные нарушения правил переписки и звонков'
			WHERE id=$1::uuid AND session_restricted=false`, userID)
		return
	}
	if !shouldRestrictSession(n) {
		_, _ = a.db.Exec(ctx, `
			UPDATE users SET session_restricted=false, session_restricted_reason='', session_restricted_at=NULL
			WHERE id=$1::uuid AND session_restricted=true`, userID)
	}
}

func (a *App) userSessionRestricted(ctx context.Context, userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	a.ensureSecuritySchema(ctx)
	var on bool
	err := a.db.QueryRow(ctx, `SELECT coalesce(session_restricted,false) FROM users WHERE id=$1::uuid`, userID).Scan(&on)
	return err == nil && on
}

// gateManagerOutbound пишет ответ и возвращает true, если сообщение менеджера отправлять нельзя.
// allowRestricted оставляет канал РОПу/админу (поддержка и внутренние диалоги).
func (a *App) gateManagerOutbound(w http.ResponseWriter, r *http.Request, objectType, objectID, body string, allowRestricted bool) bool {
	if !isManager(r) {
		return false
	}
	uid := actorID(r)
	if !allowRestricted && a.userSessionRestricted(r.Context(), uid) {
		writeJSON(w, 403, map[string]any{"error": msgSessionRestricted, "restricted": true})
		return true
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	custom, customOK := a.matchCustomDLPRule(r.Context(), body)
	if customOK && custom.Action == "log" {
		a.recordSecurityEvent(r.Context(), securityEvent{
			ActorID: uid, EventType: "message_observed", Rule: custom.Code, Severity: custom.Severity, Status: "noted",
			ObjectType: objectType, ObjectID: objectID, Summary: "Сработало пользовательское правило DLP",
			Detail: map[string]any{"excerpt": policyExcerpt(body), "pattern": custom.Pattern, "action": custom.Action},
		})
		return false
	}
	hit, ok := classifyOutbound(r.Context(), body)
	if ok && hit.Rule == rulePassportRequest && a.passportTextRequestAllowed(r.Context(), uid) {
		ok = false
	}
	if customOK && custom.Action == "block" {
		hit = PolicyHit{Rule: custom.Code, Severity: custom.Severity, Message: "Сообщение заблокировано пользовательским правилом DLP"}
		ok = true
	}
	if !ok {
		return false
	}
	id := a.recordSecurityEvent(r.Context(), securityEvent{
		ActorID:    uid,
		EventType:  "message_blocked",
		Rule:       hit.Rule,
		Severity:   hit.Severity,
		Status:     "open",
		ObjectType: objectType,
		ObjectID:   objectID,
		Summary:    hit.Message,
		Detail:     map[string]any{"excerpt": policyExcerpt(body), "channel": objectType},
	})
	a.logAct(r.Context(), "user", uid, uid, "message_blocked", map[string]any{
		"rule": hit.Rule, "incident_id": id, "object_type": objectType, "object_id": objectID,
	})
	writeJSON(w, 403, map[string]any{"error": hit.Message, "blocked": true, "incident_id": id})
	return true
}

func (a *App) noteCardView(r *http.Request, entityType, entityID string) {
	if !isManager(r) {
		return
	}
	uid := strings.TrimSpace(actorID(r))
	entityID = strings.TrimSpace(entityID)
	if uid == "" || entityID == "" {
		return
	}
	ctx := r.Context()
	a.ensureSecuritySchema(ctx)
	if _, err := a.db.Exec(ctx, `
		INSERT INTO card_views (actor_id, entity_type, entity_id)
		VALUES ($1::uuid, $2, $3::uuid)`, uid, entityType, entityID); err != nil {
		return
	}
	n := a.queryInt(ctx, `
		SELECT count(DISTINCT entity_id) FROM card_views
		WHERE actor_id=$1::uuid AND viewed_at > now() - interval '10 minutes'`, uid)
	if n < massViewThreshold {
		return
	}
	if a.queryInt(ctx, `
		SELECT count(*) FROM security_events
		WHERE actor_id=$1::uuid AND event_type='mass_view' AND status='open'
		  AND created_at > now() - interval '1 hour'`, uid) > 0 {
		return
	}
	a.recordSecurityEvent(ctx, securityEvent{
		ActorID:    uid,
		EventType:  "mass_view",
		Rule:       "mass_card_view",
		Severity:   "medium",
		Status:     "open",
		ObjectType: entityType,
		ObjectID:   entityID,
		Summary:    "Массовый просмотр карточек: 30 или больше за 10 минут",
		Detail:     map[string]any{"distinct_cards": n, "window_min": 10},
	})
}

func (a *App) noteSensitiveFile(r *http.Request, dealID, fileID, kind string, allowed bool) {
	if !ticketFileKindSensitive(kind) {
		return
	}
	a.ensureSecuritySchema(r.Context())
	uid := actorID(r)
	eventType := "sensitive_view"
	status := "noted"
	severity := "low"
	summary := "Просмотр чувствительного документа"
	if !allowed {
		eventType = "sensitive_denied"
		status = "open"
		severity = "high"
		summary = "Попытка открыть паспорт или документ без права"
	}
	ctx := r.Context()
	if a.queryInt(ctx, `
		SELECT count(*) FROM security_events
		WHERE actor_id=$1::uuid AND event_type=$2 AND object_id=$3
		  AND created_at > now() - interval '10 minutes'`, uid, eventType, fileID) > 0 {
		return
	}
	a.recordSecurityEvent(ctx, securityEvent{
		ActorID: uid, EventType: eventType, Rule: "sensitive_document", Severity: severity, Status: status,
		ObjectType: "deal_ticket_file", ObjectID: fileID, Summary: summary,
		Detail: map[string]any{"deal_id": dealID, "kind": kind},
	})
	a.logAct(ctx, "deal", dealID, uid, eventType, map[string]any{"file_id": fileID, "kind": kind})
}

func (a *App) canReviewSecurityOf(r *http.Request, targetUserID string) bool {
	if isAdmin(r) {
		return true
	}
	targetUserID = strings.TrimSpace(targetUserID)
	if targetUserID == "" {
		return false
	}
	if targetUserID == actorID(r) && (isManager(r) || isROP(r)) {
		return true
	}
	if !isROP(r) {
		return false
	}
	dept := a.actorDepartmentID(r.Context(), actorID(r))
	if dept == "" {
		return false
	}
	var targetDept string
	_ = a.db.QueryRow(r.Context(), `SELECT coalesce(department_id::text,'') FROM users WHERE id=$1::uuid`, targetUserID).Scan(&targetDept)
	return targetDept != "" && targetDept == dept
}

func (a *App) listSecurityIncidents(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "инциденты видит только администратор"})
		return
	}
	a.ensureSecuritySchema(r.Context())
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "open"
	}
	eventType := strings.TrimSpace(r.URL.Query().Get("type"))
	managerID := strings.TrimSpace(r.URL.Query().Get("manager_id"))
	where := []string{"1=1"}
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if isROP(r) && !isAdmin(r) {
		dept := a.actorDepartmentID(r.Context(), actorID(r))
		if dept == "" {
			writeJSON(w, 200, map[string]any{"items": []any{}})
			return
		}
		d := next(dept)
		self := next(actorID(r))
		where = append(where, `(e.actor_id IN (SELECT id FROM users WHERE department_id=`+d+`::uuid) OR e.actor_id=`+self+`::uuid)`)
	}
	if status != "all" {
		where = append(where, "e.status="+next(status))
	}
	if eventType != "" && eventType != "all" {
		where = append(where, "e.event_type="+next(eventType))
	}
	if managerID != "" {
		if !a.canReviewSecurityOf(r, managerID) {
			writeJSON(w, 403, map[string]string{"error": "нет доступа к этому сотруднику"})
			return
		}
		where = append(where, "e.actor_id="+next(managerID)+"::uuid")
	}
	if from := strings.TrimSpace(r.URL.Query().Get("from")); from != "" {
		where = append(where, "e.created_at>="+next(from)+"::timestamptz")
	}
	if to := strings.TrimSpace(r.URL.Query().Get("to")); to != "" {
		where = append(where, "e.created_at<"+next(to)+"::timestamptz")
	}
	items := a.queryMaps(r.Context(), `
		SELECT e.id::text, e.created_at, e.event_type, e.rule_code, e.severity, e.status,
		       e.object_type, e.object_id, e.summary, coalesce(e.comment,'') AS comment,
		       coalesce(e.actor_id::text,'') AS actor_id, coalesce(u.full_name,'') AS actor_name,
		       coalesce(u.session_restricted,false) AS restricted,
		       coalesce(e.detail->>'excerpt','') AS excerpt,
		       coalesce(e.detail->>'channel','') AS channel,
		       coalesce(e.detail->>'kind','') AS detail_kind,
		       coalesce(e.reviewed_by::text,'') AS reviewed_by,
		       coalesce(reviewer.full_name,'') AS reviewer_name,
		       e.reviewed_at
		FROM security_events e
		LEFT JOIN users u ON u.id=e.actor_id
		LEFT JOIN users reviewer ON reviewer.id=e.reviewed_by
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY e.created_at DESC
		LIMIT 200`, args...)
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (a *App) reviewSecurityIncident(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "разбирает только администратор"})
		return
	}
	a.ensureSecuritySchema(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var actorIDOnEvent string
	err := a.db.QueryRow(r.Context(), `SELECT coalesce(actor_id::text,'') FROM security_events WHERE id=$1::uuid`, id).Scan(&actorIDOnEvent)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "инцидент не найден"})
		return
	}
	if actorIDOnEvent != "" && !a.canReviewSecurityOf(r, actorIDOnEvent) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа к этому инциденту"})
		return
	}
	var in struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный JSON"})
		return
	}
	switch in.Status {
	case "open", "confirmed", "dismissed":
	default:
		writeJSON(w, 400, map[string]string{"error": "статус: open, confirmed или dismissed"})
		return
	}
	comment := strings.TrimSpace(in.Comment)
	if utf8.RuneCountInString(comment) > 2000 {
		comment = string([]rune(comment)[:2000])
	}
	_, err = a.db.Exec(r.Context(), `
		UPDATE security_events SET
			status=$2, comment=$3, reviewed_by=$4::uuid, reviewed_at=now()
		WHERE id=$1::uuid`, id, in.Status, comment, actorID(r))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось сохранить"})
		return
	}
	if actorIDOnEvent != "" {
		a.refreshSessionRestriction(r.Context(), actorIDOnEvent, false)
	}
	a.logAct(r.Context(), "user", nz(actorIDOnEvent, actorID(r)), actorID(r), "incident_reviewed", map[string]any{
		"incident_id": id, "status": in.Status,
	})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) releaseSessionRestriction(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "снимает ограничение только администратор"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !a.canReviewSecurityOf(r, id) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа к этому сотруднику"})
		return
	}
	a.ensureSecuritySchema(r.Context())
	tag, err := a.db.Exec(r.Context(), `
		UPDATE users SET session_restricted=false, session_restricted_reason='', session_restricted_at=NULL
		WHERE id=$1::uuid`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}
	a.recordSecurityEvent(r.Context(), securityEvent{
		ActorID: id, EventType: "restriction_lifted", Rule: "manual_release", Severity: "low", Status: "noted",
		ObjectType: "user", ObjectID: id, Summary: "Ограничение сессии снято",
		Detail: map[string]any{"by": actorID(r)},
	})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) securitySummary(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "сводка доступна только администратору"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if id == "" {
		id = actorID(r)
	}
	if !a.canReviewSecurityOf(r, id) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	a.ensureSecuritySchema(r.Context())
	var restricted bool
	var reason string
	if err := a.db.QueryRow(r.Context(), `
		SELECT coalesce(session_restricted,false), coalesce(session_restricted_reason,'')
		FROM users WHERE id=$1::uuid`, id).Scan(&restricted, &reason); err != nil {
		writeJSON(w, 404, map[string]string{"error": "сотрудник не найден"})
		return
	}
	recent := a.queryMaps(r.Context(), `
		SELECT id::text, created_at, event_type, summary, status, severity, rule_code
		FROM security_events
		WHERE actor_id=$1::uuid AND status <> 'noted'
		ORDER BY created_at DESC LIMIT 5`, id)
	if recent == nil {
		recent = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{
		"user_id":    id,
		"restricted": restricted,
		"reason":     reason,
		"open":       a.queryInt(r.Context(), `SELECT count(*) FROM security_events WHERE actor_id=$1::uuid AND status='open'`, id),
		"confirmed":  a.queryInt(r.Context(), `SELECT count(*) FROM security_events WHERE actor_id=$1::uuid AND status='confirmed'`, id),
		"recent":     recent,
	})
}

// securityFeedback возвращает только агрегаты по решениям администратора.
// Содержимое сообщений и персональные данные намеренно не выдаются.
func (a *App) securityFeedback(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "обратная связь доступна только администратору"})
		return
	}
	a.ensureSecuritySchema(r.Context())
	byRule := a.queryMaps(r.Context(), `
		SELECT rule_code, event_type,
		       count(*) FILTER (WHERE status='confirmed') AS confirmed,
		       count(*) FILTER (WHERE status='dismissed') AS dismissed,
		       count(*) AS total
		FROM security_events
		WHERE status IN ('confirmed','dismissed')
		GROUP BY rule_code, event_type
		ORDER BY total DESC, rule_code`)
	if byRule == nil {
		byRule = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{
		"confirmed": a.queryInt(r.Context(), `SELECT count(*) FROM security_events WHERE status='confirmed'`),
		"dismissed": a.queryInt(r.Context(), `SELECT count(*) FROM security_events WHERE status='dismissed'`),
		"by_rule":   byRule,
	})
}
