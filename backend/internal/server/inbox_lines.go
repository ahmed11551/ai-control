package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var inboxLineHistoryFromOnce sync.Once

func (a *App) ensureInboxLineHistoryFrom(ctx context.Context) {
	inboxLineHistoryFromOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `ALTER TABLE inbox_lines ADD COLUMN IF NOT EXISTS history_from TIMESTAMPTZ`)
	})
}

// cutInboxLineHistory — при (пере)привязке MAX начинаем историю «с сейчас».
func (a *App) cutInboxLineHistory(ctx context.Context, lineID string) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return
	}
	a.ensureInboxLineHistoryFrom(ctx)
	_, _ = a.db.Exec(ctx, `UPDATE inbox_lines SET history_from=now() WHERE id=$1::uuid`, lineID)
}

func (a *App) inboxLineHistoryFrom(ctx context.Context, lineID string) (time.Time, bool) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return time.Time{}, false
	}
	a.ensureInboxLineHistoryFrom(ctx)
	var t *time.Time
	_ = a.db.QueryRow(ctx, `SELECT history_from FROM inbox_lines WHERE id=$1::uuid`, lineID).Scan(&t)
	if t == nil || t.IsZero() {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func normProvider(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "max", "maxbot":
		return "max"
	case "wappi", "avito", "other":
		return s
	default:
		if s == "" {
			return "max"
		}
		return "other"
	}
}

func normKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "pool", "spam", "personal":
		return s
	default:
		return "personal"
	}
}

func (a *App) maskInboxLinePhones(row map[string]any) {
	// Номер нашего аккаунта MAX в мессенджере показываем целиком.
	// profile_id не отдаём менеджеру
	row["external_ref"] = ""
}

func (a *App) listInboxLines(w http.ResponseWriter, r *http.Request) {
	a.syncWappiLineLabels(r.Context())
	a.ensureMaxSendDown(r.Context())
	if !isAdmin(r) {
		a.ensureManagerMaxLine(r.Context(), actorID(r))
	}
	ch := r.URL.Query().Get("channel")
	sql := `
		SELECT l.id::text, l.provider, l.channel, l.name, l.kind, l.operator_label,
		       l.external_ref, l.is_active, l.owner_user_id::text AS owner_id,
		       coalesce(l.max_send_down,false) AS send_down,
		       coalesce(l.max_cold_limit,false) AS cold_limit,
		       coalesce(u.full_name,'') AS owner,
		       (SELECT count(*)::int FROM inbox_threads t
		         WHERE t.line_id=l.id
		           AND EXISTS (
		             SELECT 1 FROM inbox_messages m
		             WHERE m.thread_id=t.id
		               AND length(trim(coalesce(m.body,''))) > 0
		               AND m.body <> '__DEMO_PASSPORT__'
		           )
		       ) AS threads
		FROM inbox_lines l
		LEFT JOIN users u ON u.id=l.owner_user_id`
	args := []any{}
	where := []string{`l.is_active = true`}
	if ch != "" {
		where = append(where, fmt.Sprintf("l.channel=$%d", len(args)+1))
		args = append(args, normChannel(ch))
	}
	// В мессенджере чужие линии не показываем: свои — у менеджера и РОПа, все — только у админа.
	if !isAdmin(r) {
		where = append(where, fmt.Sprintf("l.owner_user_id=$%d::uuid", len(args)+1))
		args = append(args, actorID(r))
	}
	if len(where) > 0 {
		sql += ` WHERE ` + strings.Join(where, ` AND `)
	}
	sql += ` ORDER BY l.provider, l.channel, l.name`
	items := a.queryMaps(r.Context(), sql, args...)
	if items == nil {
		items = []map[string]any{}
	}
	if !isAdmin(r) {
		for _, row := range items {
			a.maskInboxLinePhones(row)
		}
	}
	var active, total int
	_ = a.db.QueryRow(r.Context(), `SELECT count(*), count(*) FILTER (WHERE is_active) FROM inbox_lines`).Scan(&total, &active)
	writeJSON(w, 200, map[string]any{
		"items": items, "total": total, "active": active,
		"can_manage": canManageStaff(r),
	})
}

func (a *App) createInboxLine(w http.ResponseWriter, r *http.Request) {
	if !canManageStaff(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор или РОП может подключать аккаунты"})
		return
	}
	var in struct {
		Provider      string `json:"provider"`
		Channel       string `json:"channel"`
		Name          string `json:"name"`
		Kind          string `json:"kind"`
		OperatorLabel string `json:"operator_label"`
		ExternalRef   string `json:"external_ref"`
		OwnerUserID   string `json:"owner_user_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите название линии"})
		return
	}
	provider := normProvider(in.Provider)
	channel := normChannel(in.Channel)
	if channel == "max" && !canBindMax(r) {
		writeJSON(w, 403, map[string]string{"error": "подключать и отключать MAX может только администратор"})
		return
	}
	extRef := strings.TrimSpace(in.ExternalRef)
	opLabel := strings.TrimSpace(in.OperatorLabel)
	ownerID := strings.TrimSpace(in.OwnerUserID)
	kind := normKind(in.Kind)

	// Wappi MAX: один profile_id = одна линия. Обновляем существующую, не плодим ярлыки.
	if provider == "wappi" && channel == "max" && extRef != "" {
		var existing string
		_ = a.db.QueryRow(r.Context(), `
			SELECT id::text FROM inbox_lines
			WHERE provider='wappi' AND channel='max' AND external_ref=$1
			ORDER BY is_active DESC, name LIMIT 1`, extRef).Scan(&existing)
		if existing != "" {
			_, err := a.db.Exec(r.Context(), `
				UPDATE inbox_lines SET
					is_active=true,
					kind=COALESCE(NULLIF($2,''), kind),
					operator_label=COALESCE(NULLIF($3,''), operator_label),
					name=COALESCE(NULLIF($4,''), name),
					owner_user_id=COALESCE(nullif($5,'')::uuid, owner_user_id)
				WHERE id=$1::uuid`, existing, kind, opLabel, name, ownerID)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": "не удалось сохранить линию"})
				return
			}
			if ownerID != "" {
				a.cutInboxLineHistory(r.Context(), existing)
			}
			writeJSON(w, 200, map[string]any{"id": existing})
			return
		}
	}

	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO inbox_lines (provider, channel, name, kind, operator_label, external_ref, owner_user_id)
		VALUES ($1,$2,$3,$4,$5,$6,nullif($7,'')::uuid)
		ON CONFLICT (provider, channel, name) DO UPDATE SET
			kind=EXCLUDED.kind,
			operator_label=COALESCE(NULLIF(EXCLUDED.operator_label,''), inbox_lines.operator_label),
			external_ref=COALESCE(NULLIF(EXCLUDED.external_ref,''), inbox_lines.external_ref),
			owner_user_id=COALESCE(EXCLUDED.owner_user_id, inbox_lines.owner_user_id),
			is_active=true
		RETURNING id::text`,
		provider, channel, name, kind, opLabel, extRef, ownerID).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось сохранить линию"})
		return
	}
	if ownerID != "" {
		a.cutInboxLineHistory(r.Context(), id)
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) patchInboxLine(w http.ResponseWriter, r *http.Request) {
	if !canManageStaff(r) {
		writeJSON(w, 403, map[string]string{"error": "только администратор или РОП может менять линии"})
		return
	}
	id := chi.URLParam(r, "id")
	if !isAdmin(r) {
		var channel string
		err := a.db.QueryRow(r.Context(), `SELECT channel FROM inbox_lines WHERE id=$1::uuid`, id).Scan(&channel)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "линия не найдена"})
			return
		}
		if normChannel(channel) == "max" {
			writeJSON(w, 403, map[string]string{"error": "подключать и отключать MAX может только администратор"})
			return
		}
	}
	var in struct {
		Name          *string `json:"name"`
		Kind          *string `json:"kind"`
		OperatorLabel *string `json:"operator_label"`
		ExternalRef   *string `json:"external_ref"`
		IsActive      *bool   `json:"is_active"`
		OwnerUserID   *string `json:"owner_user_id"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	var op any
	if in.OperatorLabel != nil {
		op = strings.TrimSpace(*in.OperatorLabel)
	}
	kind := ""
	if in.Kind != nil {
		kind = normKind(*in.Kind)
	}
	var ext any
	if in.ExternalRef != nil {
		ext = strings.TrimSpace(*in.ExternalRef)
	}
	_, err := a.db.Exec(r.Context(), `
		UPDATE inbox_lines SET
			name=COALESCE(nullif($2,''), name),
			kind=COALESCE(nullif($3,''), kind),
			operator_label=COALESCE($4, operator_label),
			external_ref=COALESCE($5, external_ref),
			is_active=COALESCE($6, is_active),
			owner_user_id=CASE WHEN $7 THEN nullif($8,'')::uuid ELSE owner_user_id END
		WHERE id=$1`,
		id, ptrStr(in.Name), kind, op, ext, in.IsActive, in.OwnerUserID != nil, ptrStr(in.OwnerUserID))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось обновить"})
		return
	}
	if in.OwnerUserID != nil {
		a.cutInboxLineHistory(r.Context(), id)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func ptrStr(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
