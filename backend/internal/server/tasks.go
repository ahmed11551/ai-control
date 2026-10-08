package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const taskSelect = `
	SELECT t.id::text AS id,
	       t.title,
	       CASE WHEN t.status IN ('completed', 'done', 'closed') THEN 'completed' ELSE 'in_progress' END AS status,
	       t.due_at AS deadline,
	       t.planned_at AS "plannedDate",
	       coalesce(cr.full_name, '') AS creator,
	       coalesce(nullif(t.assignee_label, ''), asg.full_name, '') AS assignee,
	       nullif(coalesce(nullif(t.project_label, ''), p.name, ''), '') AS project,
	       coalesce(to_json(t.tags), '[]'::json) AS tags,
	       t.created_at AS "createdAt",
	       t.activity_at AS "activityAt",
	       (SELECT count(*) FROM task_comments c WHERE c.task_id=t.id) AS "commentCount",
	       coalesce(t.priority,'normal') AS priority,
	       coalesce(t.is_important,false) AS "isImportant",
	       coalesce(t.kind,'task') AS kind,
	       coalesce(t.block_work,false) AS "blockWork",
	       coalesce(t.entity_type,'') AS "entityType",
	       coalesce(t.entity_id::text,'') AS "entityId",
	       coalesce(t.result,'') AS result,
	       coalesce(t.result_note,'') AS "resultNote"
	FROM tasks t
	LEFT JOIN users cr ON cr.id = t.creator_id
	LEFT JOIN users asg ON asg.id = t.assignee_id
	LEFT JOIN projects p ON p.id = t.project_id
`

func (a *App) listTasks(w http.ResponseWriter, r *http.Request) {
	a.ensureTaskAttentionSchema(r.Context())
	a.closeOrphanLeadProcessTasks(r.Context())
	q, limit, offset := parseList(r, 50, 200)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	ctx := r.Context()
	where, args := []string{}, []any{}
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	switch status {
	case "completed":
		where = append(where, "t.status IN ('completed','done','closed')")
	case "in_progress":
		where = append(where, "t.status NOT IN ('completed','done','closed')")
	}
	if q != "" {
		p := next(likePattern(q))
		where = append(where, "(t.title ILIKE "+p+" OR coalesce(cr.full_name,'') ILIKE "+p+" OR coalesce(asg.full_name,'') ILIKE "+p+" OR coalesce(t.assignee_label,'') ILIKE "+p+")")
	}
	uid := actorID(r)
	roleFilter := strings.TrimSpace(r.URL.Query().Get("role"))
	if roleFilter != "" && uid != "" {
		p := next(uid)
		switch roleFilter {
		case "doing", "assignee":
			where = append(where, "(t.assignee_id="+p+"::uuid OR EXISTS (SELECT 1 FROM task_members m WHERE m.task_id=t.id AND m.user_id="+p+"::uuid AND m.role='assignee'))")
		case "helping", "helper":
			where = append(where, "EXISTS (SELECT 1 FROM task_members m WHERE m.task_id=t.id AND m.user_id="+p+"::uuid AND m.role='helper')")
		case "assigned", "creator":
			where = append(where, "(t.creator_id="+p+"::uuid OR EXISTS (SELECT 1 FROM task_members m WHERE m.task_id=t.id AND m.user_id="+p+"::uuid AND m.role='creator'))")
		case "watching", "watcher":
			where = append(where, "EXISTS (SELECT 1 FROM task_members m WHERE m.task_id=t.id AND m.user_id="+p+"::uuid AND m.role='watcher')")
		}
	}
	// Не админ — только свои задачи (в т.ч. РОП не видит чужие и сиротские автозадачи).
	if uid != "" && !isAdmin(r) {
		p := next(uid)
		where = append(where, `(
			t.assignee_id=`+p+`::uuid
			OR t.creator_id=`+p+`::uuid
			OR EXISTS (SELECT 1 FROM task_members m WHERE m.task_id=t.id AND m.user_id=`+p+`::uuid)
		)`)
	}
	// Сиротские «Обработать лид…» без исполнителя/постановщика не показываем никому.
	where = append(where, `(
		NOT (
			t.assignee_id IS NULL AND t.creator_id IS NULL
			AND (t.title ILIKE 'Обработать лид%' OR t.title = 'Автозадача')
		)
	)`)
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	countArgs := append([]any{}, args...)
	overdueWhere := append(append([]string{}, where...), "t.due_at IS NOT NULL AND t.due_at < now() AND t.status NOT IN ('completed','done','closed')")
	overdueClause := " WHERE " + strings.Join(overdueWhere, " AND ")
	args = append(args, limit, offset)
	lim := "$" + strconv.Itoa(len(args)-1)
	off := "$" + strconv.Itoa(len(args))
	items := a.queryMaps(ctx, taskSelect+clause+` ORDER BY t.created_at DESC LIMIT `+lim+` OFFSET `+off, args...)
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{
		"items":        items,
		"total":        a.queryInt(ctx, `SELECT count(*) FROM tasks t LEFT JOIN users cr ON cr.id=t.creator_id LEFT JOIN users asg ON asg.id=t.assignee_id LEFT JOIN projects p ON p.id=t.project_id`+clause, countArgs...),
		"limit":        limit,
		"offset":       offset,
		"overdue":      a.queryInt(ctx, `SELECT count(*) FROM tasks t LEFT JOIN users cr ON cr.id=t.creator_id LEFT JOIN users asg ON asg.id=t.assignee_id LEFT JOIN projects p ON p.id=t.project_id`+overdueClause, countArgs...),
		"commentTotal": a.queryInt(ctx, `SELECT count(*) FROM task_comments`),
	})
}

// closeOrphanLeadProcessTasks — закрыть массовые «Обработать лид…» без исполнителя (баг автозадания VK).
func (a *App) closeOrphanLeadProcessTasks(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `
		UPDATE tasks
		SET status='completed',
		    activity_at=now(),
		    result=COALESCE(NULLIF(result,''), 'auto_closed_orphan')
		WHERE assignee_id IS NULL
		  AND creator_id IS NULL
		  AND status NOT IN ('completed','done','closed')
		  AND (
		    title ILIKE 'Обработать лид%'
		    OR title = 'Автозадача'
		  )`)
}

func (a *App) getTask(w http.ResponseWriter, r *http.Request) {
	items := a.queryMaps(r.Context(), taskSelect+` WHERE t.id=$1`, chi.URLParam(r, "id"))
	if len(items) == 0 {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	a.attachTaskPeople(r.Context(), items[0])
	writeJSON(w, 200, items[0])
}

func (a *App) attachTaskPeople(ctx context.Context, item map[string]any) {
	id, _ := item["id"].(string)
	if id == "" {
		return
	}
	helpers, watchers := []string{}, []string{}
	rows, err := a.db.Query(ctx, `
		SELECT m.role, u.full_name
		FROM task_members m JOIN users u ON u.id=m.user_id
		WHERE m.task_id=$1::uuid AND m.role IN ('helper','watcher')
		ORDER BY u.full_name`, id)
	if err != nil {
		item["helpers"] = helpers
		item["watchers"] = watchers
		return
	}
	defer rows.Close()
	for rows.Next() {
		var role, name string
		if rows.Scan(&role, &name) != nil || name == "" {
			continue
		}
		if role == "helper" {
			helpers = append(helpers, name)
		} else {
			watchers = append(watchers, name)
		}
	}
	item["helpers"] = helpers
	item["watchers"] = watchers
}

func (a *App) replaceTaskMember(ctx context.Context, taskID, userID, role string) {
	_, _ = a.db.Exec(ctx, `DELETE FROM task_members WHERE task_id=$1::uuid AND role=$2`, taskID, role)
	if userID == "" {
		return
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO task_members (task_id, user_id, role)
		VALUES ($1::uuid,$2::uuid,$3) ON CONFLICT DO NOTHING`, taskID, userID, role)
}

func (a *App) createTask(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	title := strings.TrimSpace(str(in, "title"))
	if title == "" {
		writeJSON(w, 400, map[string]string{"error": "title required"})
		return
	}
	uid := actorID(r)
	assigneeLabel := strings.TrimSpace(nz(str(in, "assignee"), str(in, "assignee_label")))
	projectLabel := strings.TrimSpace(nz(str(in, "project"), str(in, "project_label")))
	assigneeID := a.resolveUserID(r.Context(), assigneeLabel, uid)
	projectID := a.resolveProjectID(r.Context(), projectLabel)
	a.ensureTaskAttentionSchema(r.Context())

	important := false
	switch v := in["is_important"].(type) {
	case bool:
		important = v
	case string:
		important = strings.EqualFold(v, "true") || v == "1"
	}
	if strings.EqualFold(str(in, "priority"), "important") || strings.EqualFold(str(in, "kind"), "important") {
		important = true
	}
	kind := strings.TrimSpace(str(in, "kind"))
	if kind == "" || kind == "important" {
		kind = "task"
	}
	block := important
	if v, ok := in["block_work"].(bool); ok {
		block = v
	}

	dueRaw := nz(str(in, "deadline"), str(in, "due_at"))
	// datetime-local → timestamptz-friendly
	dueRaw = strings.ReplaceAll(dueRaw, " ", "T")
	if len(dueRaw) == 16 {
		dueRaw += ":00"
	}

	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO tasks (
			title, description, entity_type, entity_id, project_id, assignee_id, creator_id,
			due_at, planned_at, status, priority, tags, assignee_label, project_label, activity_at,
			is_important, kind, block_work
		) VALUES (
			$1, nullif($2,''), nullif($3,''), nullif($4,'')::uuid, $5, $6::uuid, $7::uuid,
			nullif($8,'')::timestamptz, nullif($9,'')::date, 'in_progress',
			coalesce(nullif($10,''), CASE WHEN $14 THEN 'high' ELSE 'normal' END),
			$11, nullif($12,''), nullif($13,''), now(),
			$14, $15, $16
		) RETURNING id::text`,
		title, str(in, "description"), str(in, "entity_type"), str(in, "entity_id"),
		projectID, assigneeID, uid,
		dueRaw,
		nz(str(in, "plannedDate"), str(in, "planned_at")),
		str(in, "priority"), stringSlice(in["tags"]),
		assigneeLabel, projectLabel,
		important, kind, block,
	).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.replaceTaskMember(r.Context(), id, uid, "creator")
	a.replaceTaskMember(r.Context(), id, assigneeID, "assignee")
	a.logAct(r.Context(), "task", id, uid, "created", in)
	entityType := strings.TrimSpace(str(in, "entity_type"))
	entityID := strings.TrimSpace(str(in, "entity_id"))
	if entityType != "" && entityID != "" {
		a.logAct(r.Context(), entityType, entityID, uid, "task_planned", map[string]any{
			"task_id":      id,
			"title":        title,
			"due_at":       dueRaw,
			"description":  str(in, "description"),
			"is_important": important,
			"kind":         kind,
		})
	}
	items := a.queryMaps(r.Context(), taskSelect+` WHERE t.id=$1`, id)
	if len(items) == 0 {
		writeJSON(w, 201, map[string]any{"id": id})
		return
	}
	a.enqueueTaskAIEvent(r.Context(), "task.created", id, uid, 1, map[string]any{
		"title": title, "status": "in_progress", "due_at": dueRaw, "assignee": items[0]["assignee"], "entity_type": entityType, "entity_id": entityID,
	})
	writeJSON(w, 201, items[0])
}

func (a *App) patchTask(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	id := chi.URLParam(r, "id")
	sets := []string{"activity_at=now()", "updated_at=now()"}
	args := []any{}
	i := 1

	add := func(frag string, val any) {
		sets = append(sets, strings.ReplaceAll(frag, "?", "$"+strconv.Itoa(i)))
		args = append(args, val)
		i++
	}

	if v, ok := in["title"]; ok {
		add("title=?", strings.TrimSpace(fmt.Sprint(v)))
	}
	if v, ok := in["description"]; ok {
		add("description=nullif(?,'')", fmt.Sprint(v))
	}
	if _, ok := in["status"]; ok {
		add("status=?", normalizeTaskStatus(str(in, "status")))
	}
	if _, ok := in["deadline"]; ok || hasKey(in, "due_at") {
		due := nz(str(in, "deadline"), str(in, "due_at"))
		due = strings.ReplaceAll(due, " ", "T")
		if len(due) == 16 {
			due += ":00"
		}
		add("due_at=nullif(?,'')::timestamptz", due)
	}
	if _, ok := in["plannedDate"]; ok || hasKey(in, "planned_at") {
		add("planned_at=nullif(?,'')::date", nz(str(in, "plannedDate"), str(in, "planned_at")))
	}
	assigneeChanged := false
	assigneeID := ""
	if _, ok := in["assignee"]; ok || hasKey(in, "assignee_label") {
		label := strings.TrimSpace(nz(str(in, "assignee"), str(in, "assignee_label")))
		assigneeID = a.resolveUserID(r.Context(), label, actorID(r))
		add("assignee_label=nullif(?,'')", label)
		add("assignee_id=?::uuid", assigneeID)
		assigneeChanged = true
	}
	if _, ok := in["project"]; ok || hasKey(in, "project_label") {
		label := strings.TrimSpace(nz(str(in, "project"), str(in, "project_label")))
		add("project_label=nullif(?,'')", label)
		pid := a.resolveProjectID(r.Context(), label)
		if pid == nil {
			sets = append(sets, "project_id=NULL")
		} else {
			add("project_id=?", pid)
		}
	}
	if _, ok := in["tags"]; ok {
		add("tags=?", stringSlice(in["tags"]))
	}
	if _, ok := in["priority"]; ok {
		add("priority=?", nz(str(in, "priority"), "normal"))
	}
	if v, ok := in["is_important"].(bool); ok {
		add("is_important=?", v)
		if v {
			add("block_work=?", true)
		}
	}
	if _, ok := in["kind"]; ok {
		add("kind=?", nz(str(in, "kind"), "task"))
	}
	if v, ok := in["block_work"].(bool); ok {
		add("block_work=?", v)
	}

	args = append(args, id)
	q := "UPDATE tasks SET " + strings.Join(sets, ",") + " WHERE id=$" + strconv.Itoa(i)
	if _, err := a.db.Exec(r.Context(), q, args...); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if assigneeChanged {
		a.replaceTaskMember(r.Context(), id, assigneeID, "assignee")
	}
	items := a.queryMaps(r.Context(), taskSelect+` WHERE t.id=$1`, id)
	if len(items) == 0 {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	a.enqueueTaskAIEvent(r.Context(), "task.updated", id, actorID(r), int(time.Now().Unix()), map[string]any{
		"title": items[0]["title"], "status": items[0]["status"], "due_at": items[0]["deadline"], "assignee": items[0]["assignee"],
	})
	writeJSON(w, 200, items[0])
}

func (a *App) listTaskComments(w http.ResponseWriter, r *http.Request) {
	items := a.queryMaps(r.Context(), `
		SELECT c.id::text, c.body, coalesce(nullif(c.author_label,''), u.full_name, '') AS author,
		       c.created_at AS "createdAt"
		FROM task_comments c
		LEFT JOIN users u ON u.id=c.author_id
		WHERE c.task_id=$1::uuid
		ORDER BY c.created_at ASC`, chi.URLParam(r, "id"))
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, 200, items)
}

func (a *App) createTaskComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		writeJSON(w, 400, map[string]string{"error": "пустой комментарий"})
		return
	}
	uid := actorID(r)
	id := chi.URLParam(r, "id")
	var commentID string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO task_comments (task_id, author_id, author_label, body)
		SELECT $1::uuid, $2::uuid, coalesce(u.full_name, u.email, ''), $3
		FROM users u WHERE u.id=$2::uuid
		RETURNING id::text`, id, uid, body).Scan(&commentID)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	_, _ = a.db.Exec(r.Context(), `UPDATE tasks SET activity_at=now(), updated_at=now() WHERE id=$1::uuid`, id)
	items := a.queryMaps(r.Context(), `
		SELECT c.id::text, c.body, coalesce(nullif(c.author_label,''), u.full_name, '') AS author,
		       c.created_at AS "createdAt"
		FROM task_comments c
		LEFT JOIN users u ON u.id=c.author_id
		WHERE c.id=$1::uuid`, commentID)
	if len(items) == 0 {
		writeJSON(w, 201, map[string]any{"id": commentID})
		return
	}
	writeJSON(w, 201, items[0])
}

func (a *App) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tag, err := a.db.Exec(r.Context(), `DELETE FROM tasks WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) resolveUserID(ctx context.Context, name, fallback string) string {
	if name == "" {
		return fallback
	}
	var id string
	if err := a.db.QueryRow(ctx, `SELECT id::text FROM users WHERE lower(full_name)=lower($1) LIMIT 1`, name).Scan(&id); err != nil {
		return fallback
	}
	return id
}

func (a *App) resolveProjectID(ctx context.Context, name string) any {
	if name == "" {
		return nil
	}
	var id string
	if err := a.db.QueryRow(ctx, `SELECT id FROM projects WHERE lower(name)=lower($1) LIMIT 1`, name).Scan(&id); err != nil {
		return nil
	}
	return id
}

func normalizeTaskStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "completed", "done", "closed":
		return "completed"
	default:
		return "in_progress"
	}
}

func stringSlice(v any) []string {
	out := []string{}
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			s := strings.TrimSpace(fmt.Sprint(x))
			if s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.Split(t, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}
