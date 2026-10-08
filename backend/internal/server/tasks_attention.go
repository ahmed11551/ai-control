package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const importantLeadWindow = 2 * time.Minute

func (a *App) ensureTaskAttentionSchema(ctx context.Context) {
	a.taskAttentionOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS is_important BOOLEAN NOT NULL DEFAULT false`)
		_, _ = a.db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'task'`)
		_, _ = a.db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS result TEXT`)
		_, _ = a.db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS result_note TEXT NOT NULL DEFAULT ''`)
		_, _ = a.db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ`)
		_, _ = a.db.Exec(ctx, `ALTER TABLE tasks ADD COLUMN IF NOT EXISTS block_work BOOLEAN NOT NULL DEFAULT false`)
	})
}

// GET /tasks/attention — обязательные события, блокирующие работу (ТЗ §17)
func (a *App) listTaskAttention(w http.ResponseWriter, r *http.Request) {
	a.ensureTaskAttentionSchema(r.Context())
	uid := actorID(r)
	if uid == "" {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	leadSec := int(importantLeadWindow.Seconds())
	items := a.queryMaps(r.Context(), `
		SELECT t.id::text AS id,
		       t.title,
		       coalesce(t.kind,'task') AS kind,
		       coalesce(t.is_important,false) AS "isImportant",
		       coalesce(t.block_work,false) AS "blockWork",
		       t.due_at AS deadline,
		       coalesce(t.entity_type,'') AS "entityType",
		       coalesce(t.entity_id::text,'') AS "entityId",
		       coalesce(t.result_note,'') AS "resultNote",
		       CASE
		         WHEN coalesce(t.kind,'')='missed_call' THEN 'missed_call'
		         ELSE 'important'
		       END AS "attentionType",
		       CASE
		         WHEN coalesce(t.entity_type,'')='lead' THEN (
		           SELECT coalesce(nullif(l.title,''), nullif(c.full_name,''), 'Лид')
		           FROM leads l
		           LEFT JOIN contacts c ON c.id=l.contact_id
		           WHERE l.id=t.entity_id
		         )
		         WHEN coalesce(t.entity_type,'')='deal' THEN (
		           SELECT coalesce(nullif(d.title,''), nullif(c.full_name,''), 'Сделка')
		           FROM deals d
		           LEFT JOIN contacts c ON c.id=d.contact_id
		           WHERE d.id=t.entity_id
		         )
		         ELSE ''
		       END AS "entityLabel"
		FROM tasks t
		WHERE t.resolved_at IS NULL
		  AND t.status NOT IN ('completed','done','closed')
		  AND coalesce(t.block_work,false)=true
		  AND (t.assignee_id=$1::uuid OR t.creator_id=$1::uuid)
		  AND (
		    coalesce(t.kind,'')='missed_call'
		    OR (
		      coalesce(t.is_important,false)=true
		      AND t.due_at IS NOT NULL
		      AND t.due_at <= now() + make_interval(secs => $2)
		    )
		  )
		ORDER BY
		  CASE WHEN coalesce(t.kind,'')='missed_call' THEN 0 ELSE 1 END,
		  t.due_at NULLS LAST,
		  t.created_at ASC
		LIMIT 20`, uid, leadSec)
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{
		"items":     items,
		"lead_secs": leadSec,
		"blocked":   len(items) > 0,
	})
}

// POST /tasks/{id}/resolve — фиксация результата важной задачи / пропущенного
func (a *App) resolveTask(w http.ResponseWriter, r *http.Request) {
	a.ensureTaskAttentionSchema(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var in struct {
		Result       string `json:"result"`
		Note         string `json:"note"`
		RescheduleAt string `json:"reschedule_at"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	result := strings.ToLower(strings.TrimSpace(in.Result))
	switch result {
	case "done", "completed", "выполнена":
		result = "done"
	case "not_done", "failed", "не_выполнена", "не выполнена":
		result = "not_done"
	case "no_answer", "не_ответил", "не ответил":
		result = "no_answer"
	case "reschedule", "перенести":
		result = "reschedule"
	case "other", "иное":
		result = "other"
	default:
		writeJSON(w, 400, map[string]string{"error": "некорректный результат"})
		return
	}
	note := strings.TrimSpace(in.Note)
	uid := actorID(r)

	var title, kind, entityType, entityID, assigneeID string
	var isImportant, blockWork bool
	err := a.db.QueryRow(r.Context(), `
		SELECT title, coalesce(kind,'task'), coalesce(is_important,false), coalesce(block_work,false),
		       coalesce(entity_type,''), coalesce(entity_id::text,''), coalesce(assignee_id::text,'')
		FROM tasks WHERE id=$1::uuid`, id).Scan(
		&title, &kind, &isImportant, &blockWork, &entityType, &entityID, &assigneeID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if (kind == "missed_call" || result == "other") && note == "" {
		writeJSON(w, 400, map[string]string{"error": "нужен комментарий"})
		return
	}

	_, err = a.db.Exec(r.Context(), `
		UPDATE tasks SET
			result=$2,
			result_note=$3,
			resolved_at=now(),
			status='completed',
			block_work=false,
			activity_at=now(),
			updated_at=now()
		WHERE id=$1::uuid`, id, result, note)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.logAct(r.Context(), "task", id, uid, "resolved", map[string]any{
		"result": result, "note": note, "kind": kind,
	})
	if entityType != "" && entityID != "" {
		a.logAct(r.Context(), entityType, entityID, uid, "task_resolved", map[string]any{
			"task_id": id, "title": title, "result": result, "note": note, "kind": kind,
		})
	}

	var followID string
	if result == "reschedule" {
		when := strings.TrimSpace(in.RescheduleAt)
		if when == "" {
			when = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
		}
		followID = a.createAttentionTask(r.Context(), attentionTaskInput{
			Title:       "Перенос: " + title,
			Kind:        "task",
			Important:   true,
			Block:       true,
			DueAt:       when,
			EntityType:  entityType,
			EntityID:    entityID,
			AssigneeID:  nz(assigneeID, uid),
			CreatorID:   uid,
			Description: "Перенесено из задачи " + id,
		})
	}

	out := map[string]any{"ok": true, "result": result}
	if followID != "" {
		out["follow_up_id"] = followID
	}
	writeJSON(w, 200, out)
}

type attentionTaskInput struct {
	Title, Kind, DueAt, EntityType, EntityID, AssigneeID, CreatorID, Description string
	Important, Block                                                             bool
}

func (a *App) createAttentionTask(ctx context.Context, in attentionTaskInput) string {
	a.ensureTaskAttentionSchema(ctx)
	if in.Kind == "" {
		in.Kind = "task"
	}
	if in.CreatorID == "" {
		in.CreatorID = in.AssigneeID
	}
	var id string
	err := a.db.QueryRow(ctx, `
		INSERT INTO tasks (
			title, description, entity_type, entity_id, assignee_id, creator_id,
			due_at, status, priority, is_important, kind, block_work, activity_at
		) VALUES (
			$1, nullif($2,''), nullif($3,''), nullif($4,'')::uuid, nullif($5,'')::uuid, nullif($6,'')::uuid,
			nullif($7,'')::timestamptz, 'in_progress',
			CASE WHEN $8 THEN 'high' ELSE 'normal' END,
			$8, $9, $10, now()
		) RETURNING id::text`,
		in.Title, in.Description, in.EntityType, in.EntityID, in.AssigneeID, in.CreatorID,
		in.DueAt, in.Important, in.Kind, in.Block,
	).Scan(&id)
	if err != nil {
		return ""
	}
	if in.CreatorID != "" {
		a.replaceTaskMember(ctx, id, in.CreatorID, "creator")
	}
	if in.AssigneeID != "" {
		a.replaceTaskMember(ctx, id, in.AssigneeID, "assignee")
	}
	return id
}

func (a *App) ensureMissedCallAttention(ctx context.Context, entityType, entityID, userID, peer string) {
	if entityID == "" || userID == "" {
		return
	}
	a.ensureTaskAttentionSchema(ctx)
	var exists string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM tasks
		WHERE kind='missed_call' AND entity_type=$1 AND entity_id=$2::uuid
		  AND assignee_id=$3::uuid AND resolved_at IS NULL
		  AND status NOT IN ('completed','done','closed')
		  AND created_at > now() - interval '6 hours'
		LIMIT 1`, entityType, entityID, userID).Scan(&exists)
	if exists != "" {
		return
	}
	title := "Пропущенный звонок"
	if peer != "" {
		title = "Пропущенный звонок · " + peer
	}
	a.createAttentionTask(ctx, attentionTaskInput{
		Title:       title,
		Kind:        "missed_call",
		Important:   true,
		Block:       true,
		DueAt:       time.Now().UTC().Format(time.RFC3339),
		EntityType:  entityType,
		EntityID:    entityID,
		AssigneeID:  userID,
		CreatorID:   userID,
		Description: "Нужно открыть карточку и зафиксировать результат (ТЗ §17).",
	})
}
