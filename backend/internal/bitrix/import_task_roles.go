package bitrix

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (im *Importer) importTaskRoles(ctx context.Context) error {
	var afterID int64
	_ = im.db.QueryRow(ctx, `SELECT coalesce(last_id, 0) FROM bitrix_sync_state WHERE entity='task_roles'`).Scan(&afterID)
	var already int
	_ = im.db.QueryRow(ctx, `SELECT count(DISTINCT task_id) FROM task_members`).Scan(&already)
	var localTotal int
	_ = im.db.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE bitrix_id IS NOT NULL`).Scan(&localTotal)
	im.setCount("task_roles", already, localTotal, nil)
	im.emit("task_roles")

	params := map[string]any{
		"select": []string{
			"ID", "RESPONSIBLE_ID", "CREATED_BY", "ACCOMPLICES", "AUDITORS",
			"GROUP_ID", "TAGS", "UF_CRM_TASK",
		},
		"order": map[string]any{"ID": "asc"},
	}
	err := im.client.EachAfter(ctx, "tasks.task.list", params, afterID, func(page []map[string]any, total int) error {
		if err := im.upsertTaskRolePage(ctx, page); err != nil {
			return err
		}
		for _, row := range page {
			if id, err := strconv.ParseInt(rowString(row, "ID", "id"), 10, 64); err == nil && id > afterID {
				afterID = id
			}
		}
		already += len(page)
		if total > 0 && already < total {
			im.setCount("task_roles", already, total, nil)
		} else {
			im.setCount("task_roles", already, localTotal, nil)
		}
		im.emit("task_roles")
		_, _ = im.db.Exec(ctx, `
			INSERT INTO bitrix_sync_state (entity, last_id, last_sync_at)
			VALUES ('task_roles', $1, now())
			ON CONFLICT (entity) DO UPDATE SET last_id=EXCLUDED.last_id, last_sync_at=now()`, afterID)
		return nil
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "insufficient_scope") {
			return fmt.Errorf("в входящем вебхуке нет права «Задачи» — добавьте task и повторите")
		}
		return err
	}
	return nil
}

func (im *Importer) upsertTaskRolePage(ctx context.Context, page []map[string]any) error {
	ids := make([]string, 0, len(page))
	for _, row := range page {
		if id := rowString(row, "ID", "id"); id != "" {
			ids = append(ids, id)
		}
	}
	local := map[string]string{}
	if len(ids) > 0 {
		rows, err := im.db.Query(ctx, `SELECT bitrix_id, id::text FROM tasks WHERE bitrix_id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			var bid, id string
			if rows.Scan(&bid, &id) == nil {
				local[bid] = id
			}
		}
		rows.Close()
	}

	batch := &pgx.Batch{}
	queued := 0
	for _, row := range page {
		bid := rowString(row, "ID", "id")
		taskID, ok := local[bid]
		if !ok {
			continue
		}
		assignee := im.users[rowString(row, "RESPONSIBLE_ID", "responsibleId")]
		creator := im.users[rowString(row, "CREATED_BY", "createdBy")]
		groupID := rowString(row, "GROUP_ID", "groupId")
		projectID := ""
		if groupID != "" && groupID != "0" {
			var err error
			projectID, err = im.upsertProject(ctx, groupID)
			if err != nil {
				log.Println("import project", groupID, err)
			}
		}
		tags := taskTags(row["TAGS"])
		if len(tags) == 0 {
			tags = taskTags(row["tags"])
		}
		et, eid := crmBindings(row["UF_CRM_TASK"])
		if et == "" {
			et, eid = crmBindings(row["ufCrmTask"])
		}
		entityID := im.entityUUID(et, eid)

		batch.Queue(`
			UPDATE tasks SET
				assignee_id=nullif($2,'')::uuid,
				creator_id=nullif($3,'')::uuid,
				project_id=coalesce(nullif($4,'')::uuid, project_id),
				tags=CASE WHEN cardinality($5::text[])>0 THEN $5::text[] ELSE tags END,
				entity_type=coalesce(nullif($6,''), entity_type),
				entity_id=coalesce(nullif($7,'')::uuid, entity_id),
				updated_at=now()
			WHERE id=$1::uuid`,
			taskID, assignee, creator, projectID, tags, et, entityID,
		)
		queued++
		batch.Queue(`DELETE FROM task_members WHERE task_id=$1::uuid`, taskID)
		queued++
		addMember := func(uid, role string) {
			if uid == "" {
				return
			}
			batch.Queue(`
				INSERT INTO task_members (task_id, user_id, role)
				VALUES ($1::uuid,$2::uuid,$3) ON CONFLICT DO NOTHING`, taskID, uid, role)
			queued++
		}
		addMember(assignee, "assignee")
		addMember(creator, "creator")
		for _, id := range bitrixIDList(firstPresent(row, "ACCOMPLICES", "accomplices")) {
			addMember(im.users[id], "helper")
		}
		for _, id := range bitrixIDList(firstPresent(row, "AUDITORS", "auditors")) {
			addMember(im.users[id], "watcher")
		}
	}
	if queued == 0 {
		return nil
	}
	br := im.db.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < queued; i++ {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (im *Importer) upsertProject(ctx context.Context, bitrixID string) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM projects WHERE bitrix_id=$1`, bitrixID).Scan(&id)
	if err == nil {
		return id, nil
	}
	name := "Проект " + bitrixID
	err = im.db.QueryRow(ctx, `INSERT INTO projects (name, bitrix_id) VALUES ($1,$2) RETURNING id::text`, name, bitrixID).Scan(&id)
	return id, err
}

func firstPresent(row map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := row[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
