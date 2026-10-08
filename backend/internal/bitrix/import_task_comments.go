package bitrix

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var htmlTag = regexp.MustCompile(`(?s)<[^>]+>`)

type taskRef struct {
	bid string
	id  string
}

func (im *Importer) importTaskComments(ctx context.Context) error {
	var afterID int64
	_ = im.db.QueryRow(ctx, `SELECT coalesce(last_id, 0) FROM bitrix_sync_state WHERE entity='task_comments'`).Scan(&afterID)
	var already, localTasks int
	_ = im.db.QueryRow(ctx, `SELECT count(*) FROM task_comments`).Scan(&already)
	_ = im.db.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE bitrix_id IS NOT NULL`).Scan(&localTasks)
	im.setCount("task_comments", already, localTasks, nil)
	im.emit("task_comments")

	prefix := "task.commentitem.getlist?TASKID="
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var chunk []taskRef
		qrows, err := im.db.Query(ctx, `
			SELECT bitrix_id, id::text FROM tasks
			WHERE bitrix_id IS NOT NULL AND nullif(bitrix_id,'')::bigint > $1
			ORDER BY nullif(bitrix_id,'')::bigint ASC
			LIMIT 40`, afterID)
		if err != nil {
			return err
		}
		for qrows.Next() {
			var r taskRef
			if qrows.Scan(&r.bid, &r.id) == nil {
				chunk = append(chunk, r)
			}
		}
		qrows.Close()
		if len(chunk) == 0 {
			break
		}

		cmds := make(map[string]string, len(chunk))
		index := make(map[string]taskRef, len(chunk))
		for i, t := range chunk {
			key := fmt.Sprintf("c%d", i)
			cmds[key] = prefix + t.bid
			index[key] = t
		}
		out, err := im.client.Batch(ctx, cmds)
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "insufficient_scope") {
				return fmt.Errorf("в входящем вебхуке нет права «Задачи» — добавьте task и повторите")
			}
			if strings.Contains(low, "method_not_found") && strings.Contains(prefix, "getlist") {
				prefix = "task.commentitem.gettaskcomments?TASKID="
				continue
			}
			return err
		}
		if err := im.upsertCommentBatch(ctx, out, index); err != nil {
			return err
		}
		for _, t := range chunk {
			if id, err := strconv.ParseInt(t.bid, 10, 64); err == nil && id > afterID {
				afterID = id
			}
		}
		_ = im.db.QueryRow(ctx, `SELECT count(*) FROM task_comments`).Scan(&already)
		im.setCount("task_comments", already, localTasks, nil)
		im.emit("task_comments")
		_, _ = im.db.Exec(ctx, `
			INSERT INTO bitrix_sync_state (entity, last_id, last_sync_at)
			VALUES ('task_comments', $1, now())
			ON CONFLICT (entity) DO UPDATE SET last_id=EXCLUDED.last_id, last_sync_at=now()`, afterID)
	}
	return nil
}

func (im *Importer) upsertCommentBatch(ctx context.Context, out map[string]json.RawMessage, index map[string]taskRef) error {
	batch := &pgx.Batch{}
	queued := 0
	for key, raw := range out {
		task, ok := index[key]
		if !ok {
			continue
		}
		for _, row := range unwrapRows("task.commentitem.getlist", raw) {
			cid := rowString(row, "ID", "id")
			if cid == "" {
				continue
			}
			body := commentBody(row)
			if body == "" {
				continue
			}
			authorID := im.users[rowString(row, "AUTHOR_ID", "authorId")]
			author := nz(rowString(row, "AUTHOR_NAME", "authorName"), "")
			if author == "" && authorID != "" {
				author = authorID
			}
			when := parseBitrixTime(rowString(row, "POST_DATE", "postDate", "CREATED_DATE", "createdDate"))
			bitrixKey := task.bid + ":" + cid
			batch.Queue(`
				INSERT INTO task_comments (task_id, author_id, author_label, body, bitrix_id, created_at)
				VALUES ($1::uuid, nullif($2,'')::uuid, $3, $4, $5, $6)
				ON CONFLICT (bitrix_id) DO UPDATE SET
					body=EXCLUDED.body, author_label=EXCLUDED.author_label, author_id=EXCLUDED.author_id`,
				task.id, authorID, author, body, bitrixKey, when,
			)
			queued++
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

func commentBody(row map[string]any) string {
	raw := rowString(row, "POST_MESSAGE", "postMessage", "COMMENT_TEXT", "commentText", "TEXT", "text")
	if raw == "" {
		raw = rowString(row, "POST_MESSAGE_HTML", "postMessageHtml")
	}
	raw = html.UnescapeString(htmlTag.ReplaceAllString(raw, ""))
	return strings.TrimSpace(raw)
}

func parseBitrixTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now()
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"02.01.2006 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now()
}
