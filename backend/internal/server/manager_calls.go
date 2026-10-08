package server

import (
	"fmt"
	"net/http"
	"strings"
)

// managerCallKind — фильтр вкладки менеджера.
// Исходящий важнее пропущенного: недозвон наружу остаётся исходящим.
func managerCallKind(direction, disp string) string {
	if callDirectionOutbound(direction) {
		return "outgoing"
	}
	if callDirectionInbound(direction) && callDispositionMissed(disp) {
		return "missed"
	}
	if callDirectionInbound(direction) {
		return "incoming"
	}
	return "other"
}

func callDirectionOutbound(direction string) bool {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "outbound", "outgoing", "out":
		return true
	default:
		return false
	}
}

func callDirectionInbound(direction string) bool {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "inbound", "incoming", "in":
		return true
	default:
		return false
	}
}

func callDispositionMissed(disp string) bool {
	d := strings.ToLower(strings.TrimSpace(disp))
	d = strings.ReplaceAll(d, " ", "_")
	d = strings.ReplaceAll(d, "-", "_")
	switch d {
	case "missed", "no_answer", "noanswer", "busy", "canceled", "cancelled", "rejected", "failed", "no_reach":
		return true
	default:
		return false
	}
}

// callPeerTail — последние 4 цифры номера клиента. Короткий внутренний номер не показываем.
func callPeerTail(direction, from, to string) string {
	raw := from
	if callDirectionOutbound(direction) {
		raw = to
	}
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) < 4 {
		return ""
	}
	return d[len(d)-4:]
}

func cleanID(v any) string {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}

// listManagerCalls — свои звонки менеджера: без полного номера, с лидом только если он всё ещё его.
func (a *App) listManagerCalls(w http.ResponseWriter, r *http.Request) {
	if !isManager(r) {
		writeJSON(w, 403, map[string]string{"error": "только для менеджера"})
		return
	}
	uid := actorID(r)
	if uid == "" {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	rows := a.queryMaps(r.Context(), `
		SELECT c.id::text, c.direction, c.from_number, c.to_number,
		       c.duration_sec, c.disposition, c.started_at,
		       coalesce(lead.id::text, '') AS lead_id,
		       coalesce(cr.status, '') AS review_status
		FROM calls c
		LEFT JOIN call_reviews cr ON cr.call_id = c.id
		LEFT JOIN LATERAL (
			SELECT l.id
			FROM leads l
			WHERE l.owner_id = $1::uuid
			  AND coalesce(l.status, '') <> 'rejected'
			  AND (
			    (c.entity_type = 'lead' AND l.id = c.entity_id)
			    OR (
			      c.entity_type = 'deal'
			      AND (
			        l.id = (SELECT d.lead_id FROM deals d WHERE d.id = c.entity_id)
			        OR l.converted_deal_id = c.entity_id
			      )
			    )
			  )
			ORDER BY l.created_at DESC
			LIMIT 1
		) lead ON true
		WHERE c.user_id = $1::uuid
		ORDER BY c.started_at DESC NULLS LAST
		LIMIT 400`, uid)
	if rows == nil {
		rows = []map[string]any{}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		dir := fmt.Sprint(row["direction"])
		from := fmt.Sprint(row["from_number"])
		to := fmt.Sprint(row["to_number"])
		out = append(out, map[string]any{
			"id":            cleanID(row["id"]),
			"kind":          managerCallKind(dir, fmt.Sprint(row["disposition"])),
			"duration_sec":  row["duration_sec"],
			"started_at":    row["started_at"],
			"tail":          callPeerTail(dir, from, to),
			"lead_id":       cleanID(row["lead_id"]),
			"review_status": cleanID(row["review_status"]),
		})
	}
	writeJSON(w, 200, map[string]any{"calls": out})
}
