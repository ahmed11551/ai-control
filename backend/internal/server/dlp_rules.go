package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type dlpRule struct {
	ID       string
	Code     string
	Kind     string
	Pattern  string
	Action   string
	Severity string
	Enabled  bool
}

func (a *App) auditDLPRule(ctx context.Context, ruleID, action, reason string, snapshot map[string]any, actor string) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		raw = []byte("{}")
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO dlp_rule_audit (rule_id, actor_id, action, reason, snapshot)
		VALUES (nullif($1,'')::uuid, nullif($2,'')::uuid, $3, $4, $5::jsonb)`,
		strings.TrimSpace(ruleID), strings.TrimSpace(actor), action, strings.TrimSpace(reason), string(raw))
}

func (a *App) listDLPRules(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "правила доступны только администратору"})
		return
	}
	a.ensureSecuritySchema(r.Context())
	rows := a.queryMaps(r.Context(), `
		SELECT id::text, code, kind, pattern, action, severity, enabled, created_at
		FROM dlp_rules ORDER BY enabled DESC, created_at DESC`)
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

func (a *App) listDLPRuleAudit(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "аудит правил доступен только администратору"})
		return
	}
	a.ensureSecuritySchema(r.Context())
	ruleID := strings.TrimSpace(r.URL.Query().Get("rule_id"))
	where := "1=1"
	args := []any{}
	if ruleID != "" {
		where = "a.rule_id=$1::uuid"
		args = append(args, ruleID)
	}
	rows := a.queryMaps(r.Context(), `
		SELECT a.id::text, a.rule_id::text, a.action, a.reason, a.snapshot, a.created_at,
		       coalesce(a.actor_id::text,'') AS actor_id, coalesce(u.full_name,'') AS actor_name,
		       coalesce(r.code,'') AS rule_code
		FROM dlp_rule_audit a
		LEFT JOIN users u ON u.id=a.actor_id
		LEFT JOIN dlp_rules r ON r.id=a.rule_id
		WHERE `+where+`
		ORDER BY a.created_at DESC LIMIT 200`, args...)
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

func (a *App) createDLPRule(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "правила доступны только администратору"})
		return
	}
	var in struct {
		Code     string `json:"code"`
		Kind     string `json:"kind"`
		Pattern  string `json:"pattern"`
		Action   string `json:"action"`
		Severity string `json:"severity"`
		Enabled  *bool  `json:"enabled"`
		Reason   string `json:"reason"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный JSON"})
		return
	}
	in.Code = strings.TrimSpace(in.Code)
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.Pattern = strings.TrimSpace(in.Pattern)
	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	in.Severity = strings.ToLower(strings.TrimSpace(in.Severity))
	if in.Code == "" || in.Pattern == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите код и шаблон правила"})
		return
	}
	if in.Kind != "phrase" && in.Kind != "domain" {
		writeJSON(w, 400, map[string]string{"error": "тип правила: phrase или domain"})
		return
	}
	if in.Action != "block" && in.Action != "log" {
		writeJSON(w, 400, map[string]string{"error": "действие: block или log"})
		return
	}
	if in.Severity != "low" && in.Severity != "medium" && in.Severity != "high" {
		in.Severity = "medium"
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO dlp_rules (code, kind, pattern, action, severity, enabled)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id::text`,
		in.Code, in.Kind, in.Pattern, in.Action, in.Severity, enabled).Scan(&id)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "не удалось создать правило"})
		return
	}
	a.auditDLPRule(r.Context(), id, "created", in.Reason, map[string]any{"code": in.Code, "kind": in.Kind, "pattern": in.Pattern, "action": in.Action, "severity": in.Severity, "enabled": enabled}, actorID(r))
	writeJSON(w, 201, map[string]any{"id": id, "ok": true})
}

func (a *App) patchDLPRule(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "правила доступны только администратору"})
		return
	}
	var in struct {
		Enabled  *bool  `json:"enabled"`
		Action   string `json:"action"`
		Severity string `json:"severity"`
		Reason   string `json:"reason"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "некорректный JSON"})
		return
	}
	id := chi.URLParam(r, "id")
	var before map[string]any
	if rows := a.queryMaps(r.Context(), `SELECT code, kind, pattern, action, severity, enabled FROM dlp_rules WHERE id=$1::uuid`, id); len(rows) > 0 {
		before = rows[0]
	}
	_, err := a.db.Exec(r.Context(), `
		UPDATE dlp_rules SET
			enabled=coalesce($2,enabled),
			action=CASE WHEN $3='' THEN action ELSE $3 END,
			severity=CASE WHEN $4='' THEN severity ELSE $4 END,
			updated_at=$5
		WHERE id=$1::uuid`, id, in.Enabled, strings.ToLower(strings.TrimSpace(in.Action)), strings.ToLower(strings.TrimSpace(in.Severity)), time.Now())
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "правило не найдено"})
		return
	}
	if before == nil {
		writeJSON(w, 404, map[string]string{"error": "правило не найдено"})
		return
	}
	a.auditDLPRule(r.Context(), id, "updated", in.Reason, before, actorID(r))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) deleteDLPRule(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "правила доступны только администратору"})
		return
	}
	id := chi.URLParam(r, "id")
	var snapshot map[string]any
	if rows := a.queryMaps(r.Context(), `SELECT code, kind, pattern, action, severity, enabled FROM dlp_rules WHERE id=$1::uuid`, id); len(rows) > 0 {
		snapshot = rows[0]
	}
	if snapshot == nil {
		writeJSON(w, 404, map[string]string{"error": "правило не найдено"})
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = readJSON(r, &in)
	a.auditDLPRule(r.Context(), id, "deleted", in.Reason, snapshot, actorID(r))
	_, err := a.db.Exec(r.Context(), `DELETE FROM dlp_rules WHERE id=$1::uuid`, id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "правило не найдено"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) matchCustomDLPRule(ctx context.Context, raw string) (dlpRule, bool) {
	a.ensureSecuritySchema(ctx)
	rows := a.queryMaps(ctx, `
		SELECT id::text, code, kind, pattern, action, severity, enabled
		FROM dlp_rules WHERE enabled ORDER BY created_at ASC`)
	norm := normalizePolicyText(raw)
	for _, row := range rows {
		kind := strings.TrimSpace(str(row, "kind"))
		pattern := strings.TrimSpace(str(row, "pattern"))
		matched := false
		if kind == "domain" {
			matched = strings.Contains(strings.ToLower(raw), strings.ToLower(pattern))
		} else {
			matched = strings.Contains(norm, normalizePolicyText(pattern))
		}
		if matched {
			return dlpRule{ID: str(row, "id"), Code: str(row, "code"), Kind: kind, Pattern: pattern, Action: str(row, "action"), Severity: str(row, "severity"), Enabled: true}, true
		}
	}
	return dlpRule{}, false
}
