package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

func normChannel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(s, "whats") || s == "wa":
		return "whatsapp"
	case strings.Contains(s, "max"):
		return "max"
	case strings.Contains(s, "tg") || strings.Contains(s, "telegram"):
		return "telegram"
	case strings.Contains(s, "avito"):
		return "avito"
	case strings.Contains(s, "insta"):
		return "instagram"
	case strings.Contains(s, "viber"):
		return "viber"
	default:
		if s == "" {
			return "other"
		}
		return s
	}
}

// inboxLeadJoin — лид диалога: тот же контакт или последние 10 цифр номера.
// Колонки phone_tail заполняются для уже существующих строк, не только для новых.
const inboxLeadJoin = `
		LEFT JOIN LATERAL (
			SELECT le.id::text AS id, le.title
			FROM leads le
			LEFT JOIN contacts lc ON lc.id=le.contact_id
			WHERE (t.contact_id IS NOT NULL AND le.contact_id=t.contact_id)
			   OR (t.phone_tail IS NOT NULL AND lc.phone_tail IS NOT NULL AND lc.phone_tail=t.phone_tail)
			ORDER BY le.created_at DESC
			LIMIT 1
		) ld ON true`

var (
	inboxPhoneTailMu    sync.Mutex
	inboxPhoneTailReady bool
)

// ensureInboxPhoneTail — последние 10 цифр номера отдельно, чтобы не сканировать все контакты.
// Повтор не запускается, пока все шаги не прошли: иначе список снова упрётся в старый поиск.
func (a *App) ensureInboxPhoneTail(ctx context.Context) {
	inboxPhoneTailMu.Lock()
	defer inboxPhoneTailMu.Unlock()
	if inboxPhoneTailReady {
		return
	}
	stmts := []string{
		`CREATE OR REPLACE FUNCTION phone_tail_10(raw text) RETURNS text
		 LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
		   SELECT CASE WHEN length(d) >= 10 THEN right(d, 10) ELSE NULL END
		   FROM (SELECT regexp_replace(coalesce(raw, ''), '[^0-9]', '', 'g') AS d) s
		 $$`,
		`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS phone_tail text
		   GENERATED ALWAYS AS (phone_tail_10(phone)) STORED`,
		`ALTER TABLE inbox_threads ADD COLUMN IF NOT EXISTS phone_tail text
		   GENERATED ALWAYS AS (phone_tail_10(phone)) STORED`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_phone_tail ON contacts (phone_tail) WHERE phone_tail IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_leads_contact_id ON leads (contact_id) WHERE contact_id IS NOT NULL`,
	}
	for _, stmt := range stmts {
		if _, err := a.db.Exec(ctx, stmt); err != nil {
			log.Printf("inbox phone tail: %v", err)
			return
		}
	}
	inboxPhoneTailReady = true
}

func (a *App) listInbox(w http.ResponseWriter, r *http.Request) {
	a.ensureInboxLineHistoryFrom(r.Context())
	a.ensureInboxPhoneTail(r.Context())
	sql, args, limit := inboxListQuery(r)
	items := a.queryMaps(r.Context(), sql, args...)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	for _, it := range items {
		if lb := strings.TrimSpace(str(it, "last_body")); looksLikeHTTPURL(lb) {
			it["last_body"] = "Файл"
		}
	}
	if items == nil {
		items = []map[string]any{}
	}
	countSQL, countArgs := inboxCountQuery(r)
	counts := a.queryMaps(r.Context(), countSQL, countArgs...)
	a.writePhones(w, r, 200, map[string]any{
		"items":      items,
		"has_more":   hasMore,
		"limit":      limit,
		"counts":     counts,
		"hook":       a.cfg.InboxWebhookSecret != "",
		"connectors": a.messengerStatus(),
	})
}

func inboxListQuery(r *http.Request) (string, []any, int) {
	ch := normChannel(r.URL.Query().Get("channel"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	args := []any{}
	visSQL, args := inboxMessageVisibleSQL(r, "m", args)
	accessSQL, args := inboxThreadListAccessSQL(r, "t", "l", args)

	lastBodyExpr := `(
			SELECT m.body FROM inbox_messages m
			WHERE m.thread_id=t.id
			  AND length(trim(coalesce(m.body,''))) > 0
			  AND m.body <> '__DEMO_PASSPORT__'
			  AND (` + visSQL + `)
			  AND (l.history_from IS NULL OR m.created_at >= l.history_from)
			ORDER BY m.created_at DESC LIMIT 1
		)`

	// Только диалоги с реальной перепиской (не пустые «открыл MAX» / демо).
	where := []string{`EXISTS (
		SELECT 1 FROM inbox_messages m
		WHERE m.thread_id=t.id
		  AND length(trim(coalesce(m.body,''))) > 0
		  AND m.body <> '__DEMO_PASSPORT__'
		  AND (` + visSQL + `)
		  AND (l.history_from IS NULL OR m.created_at >= l.history_from)
	)`, `(` + accessSQL + `)`}
	if r.URL.Query().Get("channel") != "" && ch != "other" {
		where = append(where, "t.channel=$"+strconv.Itoa(len(args)+1))
		args = append(args, ch)
	}
	if lid := strings.TrimSpace(r.URL.Query().Get("line_id")); lid != "" {
		where = append(where, "t.line_id=$"+strconv.Itoa(len(args)+1)+"::uuid")
		args = append(args, lid)
	}
	if q != "" {
		where = append(where, "(t.title ILIKE $"+strconv.Itoa(len(args)+1)+" OR coalesce(t.phone,'') ILIKE $"+strconv.Itoa(len(args)+1)+" OR coalesce("+lastBodyExpr+",'') ILIKE $"+strconv.Itoa(len(args)+1)+")")
		args = append(args, "%"+q+"%")
	}

	_, limit, _ := parseList(r, 40, 100)
	beforeLastAt := strings.TrimSpace(r.URL.Query().Get("before_last_at"))
	beforeID := strings.TrimSpace(r.URL.Query().Get("before_id"))
	if beforeLastAt != "" && beforeID != "" {
		where = append(where,
			"(t.last_at, t.id) < ($"+strconv.Itoa(len(args)+1)+"::timestamptz, $"+strconv.Itoa(len(args)+2)+"::uuid)")
		args = append(args, beforeLastAt, beforeID)
	}

	args = append(args, limit+1)
	// Страница отбирается без поиска лида по номеру. Карточка ищется уже у этих строк.
	sql := `
		WITH page AS (
			SELECT t.id
			FROM inbox_threads t
			LEFT JOIN inbox_lines l ON l.id=t.line_id
			WHERE ` + strings.Join(where, " AND ") + `
			ORDER BY t.last_at DESC, t.id DESC
			LIMIT $` + strconv.Itoa(len(args)) + `
		)
		SELECT t.id::text, t.channel, t.external_id, t.title, coalesce(t.phone,'') AS phone,
		       t.contact_id::text, t.deal_id::text, t.line_id::text,
		       coalesce(` + lastBodyExpr + `, t.last_body) AS last_body,
		       t.last_at, t.unread,
		       coalesce(c.full_name,'') AS contact, coalesce(d.title,'') AS deal,
		       coalesce(l.name,'') AS line_name,
		       coalesce(ld.id,'') AS lead_id, coalesce(ld.title,'') AS lead_title
		FROM page
		JOIN inbox_threads t ON t.id=page.id
		LEFT JOIN contacts c ON c.id=t.contact_id
		LEFT JOIN deals d ON d.id=t.deal_id
		LEFT JOIN inbox_lines l ON l.id=t.line_id` + inboxLeadJoin + `
		ORDER BY t.last_at DESC, t.id DESC`
	return sql, args, limit
}

func inboxCountQuery(r *http.Request) (string, []any) {
	countArgs := []any{}
	countVis, countArgs := inboxMessageVisibleSQL(r, "m", countArgs)
	countAccess, countArgs := inboxThreadListAccessSQL(r, "t", "l", countArgs)
	sql := `
		SELECT t.channel, count(*)::int AS n
		FROM inbox_threads t
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE EXISTS (
			SELECT 1 FROM inbox_messages m
			WHERE m.thread_id=t.id
			  AND length(trim(coalesce(m.body,''))) > 0
			  AND m.body <> '__DEMO_PASSPORT__'
			  AND (` + countVis + `)
			  AND (l.history_from IS NULL OR m.created_at >= l.history_from)
		)
		AND (` + countAccess + `)
		GROUP BY t.channel`
	return sql, countArgs
}

func inboxSeesAllMessages(r *http.Request) bool {
	// Только админ и логист. РОП — как менеджер: только свои диалоги.
	return isAdmin(r) || isLogist(r)
}

func (a *App) canAccessInboxThread(r *http.Request, threadID string) bool {
	a.ensureInboxPhoneTail(r.Context())
	args := []any{threadID}
	accessSQL, args := inboxThreadAccessSQL(r, "t", "l", args)
	var n int
	err := a.db.QueryRow(r.Context(), `
		SELECT 1 FROM inbox_threads t
		LEFT JOIN inbox_lines l ON l.id=t.line_id`+inboxLeadJoin+`
		WHERE t.id=$1::uuid AND (`+accessSQL+`)`, args...).Scan(&n)
	return err == nil && n == 1
}

// Менеджер/РОП: входящие + system + только свои исходящие. Админ/логист — всё.
func inboxMessageVisibleSQL(r *http.Request, alias string, args []any) (string, []any) {
	if inboxSeesAllMessages(r) {
		return "TRUE", args
	}
	args = append(args, actorID(r))
	p := strconv.Itoa(len(args))
	return alias + `.direction IN ('inbound','system') OR ` + alias + `.author_id=$` + p + `::uuid`, args
}

// Список и счётчики: без подбора лида по номеру на каждую строку.
// Карточка для видимых диалогов ищется отдельно, уже после отбора страницы.
func inboxThreadListAccessSQL(r *http.Request, threadAlias, lineAlias string, args []any) (string, []any) {
	if inboxSeesAllMessages(r) {
		return "TRUE", args
	}
	args = append(args, actorID(r))
	p := strconv.Itoa(len(args))
	t, l := threadAlias, lineAlias
	return `(` +
		l + `.owner_user_id=$` + p + `::uuid` +
		` OR EXISTS (SELECT 1 FROM inbox_messages mx WHERE mx.thread_id=` + t + `.id AND mx.author_id=$` + p + `::uuid)` +
		` OR EXISTS (SELECT 1 FROM leads le WHERE ` + t + `.contact_id IS NOT NULL AND le.contact_id=` + t + `.contact_id AND le.owner_id=$` + p + `::uuid)` +
		` OR EXISTS (SELECT 1 FROM deals d2 WHERE d2.id=` + t + `.deal_id AND d2.owner_id=$` + p + `::uuid)` +
		`)`, args
}

// Менеджер/РОП видит диалог только со своей линии / своего лида-сделки / где сам писал.
func inboxThreadAccessSQL(r *http.Request, threadAlias, lineAlias string, args []any) (string, []any) {
	if inboxSeesAllMessages(r) {
		return "TRUE", args
	}
	args = append(args, actorID(r))
	p := strconv.Itoa(len(args))
	t, l := threadAlias, lineAlias
	return `(` +
		l + `.owner_user_id=$` + p + `::uuid` +
		` OR EXISTS (SELECT 1 FROM inbox_messages mx WHERE mx.thread_id=` + t + `.id AND mx.author_id=$` + p + `::uuid)` +
		` OR (ld.id IS NOT NULL AND EXISTS (` +
		`SELECT 1 FROM leads le WHERE le.id=ld.id::uuid AND le.owner_id=$` + p + `::uuid))` +
		` OR EXISTS (SELECT 1 FROM deals d2 WHERE d2.id=` + t + `.deal_id AND d2.owner_id=$` + p + `::uuid)` +
		`)`, args
}

func (a *App) getInboxThread(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.ensureLogisticsSchema(r.Context())
	a.ensureInboxPhoneTail(r.Context())
	args := []any{id}
	accessSQL, args := inboxThreadAccessSQL(r, "t", "l", args)
	a.ensureMaxSendDown(r.Context())
	items := a.queryMaps(r.Context(), `
		SELECT t.id::text, t.channel, t.external_id, t.title, coalesce(t.phone,'') AS phone,
		       t.contact_id::text, t.deal_id::text, t.line_id::text, t.last_body, t.last_at, t.unread,
		       coalesce(c.full_name,'') AS contact, coalesce(d.title,'') AS deal,
		       coalesce(l.name,'') AS line_name,
		       coalesce(l.operator_label,'') AS from_phone,
		       coalesce(l.max_send_down,false) AS send_down,
		       coalesce(l.max_cold_limit,false) AS cold_limit,
		       coalesce(ld.id,'') AS lead_id, coalesce(ld.title,'') AS lead_title
		FROM inbox_threads t
		LEFT JOIN contacts c ON c.id=t.contact_id
		LEFT JOIN deals d ON d.id=t.deal_id
		LEFT JOIN inbox_lines l ON l.id=t.line_id`+inboxLeadJoin+`
		WHERE t.id=$1::uuid AND (`+accessSQL+`)`, args...)
	if len(items) == 0 {
		writeJSON(w, 404, map[string]string{"error": "диалог не найден"})
		return
	}
	a.kickPendingInboxMedia(r.Context(), id)
	a.ensureInboxMediaFilesSchema(r.Context())
	_, _ = a.db.Exec(r.Context(), `UPDATE inbox_threads SET unread=0 WHERE id=$1`, id)
	items[0]["unread"] = 0
	_, limit, _ := parseList(r, 60, 200)
	before := strings.TrimSpace(r.URL.Query().Get("before"))
	msgs, hasMore := a.loadInboxMessages(r, id, before, limit)
	items[0]["messages"] = msgs
	items[0]["has_more_messages"] = hasMore
	a.writePhones(w, r, 200, items[0])
}

func (a *App) listInboxMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !a.canAccessInboxThread(r, id) {
		writeJSON(w, 404, map[string]string{"error": "диалог не найден"})
		return
	}
	a.ensureInboxMediaFilesSchema(r.Context())
	_, limit, _ := parseList(r, 60, 200)
	before := strings.TrimSpace(r.URL.Query().Get("before"))
	msgs, hasMore := a.loadInboxMessages(r, id, before, limit)
	a.writePhones(w, r, 200, map[string]any{
		"items":    msgs,
		"has_more": hasMore,
		"limit":    limit,
	})
}

// loadInboxMessages — страница истории: последние limit, или старше before (id сообщения).
// Возвращает в порядке ASC (сверху старые).
func (a *App) loadInboxMessages(r *http.Request, threadID, beforeID string, limit int) ([]map[string]any, bool) {
	if limit < 1 {
		limit = 60
	}
	args := []any{threadID}
	visSQL, args := inboxMessageVisibleSQL(r, "m", args)
	where := []string{
		"m.thread_id=$1::uuid",
		"(" + visSQL + ")",
		"(l.history_from IS NULL OR m.created_at >= l.history_from)",
	}
	if beforeID != "" {
		args = append(args, beforeID)
		p := strconv.Itoa(len(args))
		where = append(where, `(m.created_at, m.id) < (
			SELECT b.created_at, b.id FROM inbox_messages b WHERE b.id=$`+p+`::uuid AND b.thread_id=$1::uuid
		)`)
	}
	args = append(args, limit+1)
	limP := strconv.Itoa(len(args))
	rows := a.queryMaps(r.Context(), `
		SELECT m.id::text, m.direction, m.body, m.author_label, m.created_at,
		       coalesce(m.author_id::text,'') AS author_id,
		       coalesce(m.media_kind,'') AS media_kind,
		       coalesce(m.media_filename,'') AS media_filename,
		       coalesce(m.media_mime,'') AS media_mime,
		       coalesce(m.media_size,0) AS media_size,
		       coalesce(m.media_storage_key,'') AS media_storage_key,
		       coalesce(m.media_wappi_id,'') AS media_wappi_id,
		       coalesce(m.delivery_status,'') AS delivery_status,
		       (m.media_sent_to_logist_at IS NOT NULL) AS media_sent_to_logist,
		       (SELECT count(*)::int FROM inbox_message_files f WHERE f.message_id=m.id) AS media_count,
		       (SELECT count(*)::int FROM inbox_message_files f
		         WHERE f.message_id=m.id AND coalesce(f.storage_key,'')<>'') AS media_ready
		FROM inbox_messages m
		LEFT JOIN inbox_threads t ON t.id=m.thread_id
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $`+limP, args...)
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	// В чат — от старых к новым.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	// Схлопнуть дубли вложений (CRM + webhook Wappi) до отдачи UI.
	for _, row := range rows {
		if asInt(row["media_count"]) <= 1 {
			continue
		}
		mid := strings.TrimSpace(fmt.Sprint(row["id"]))
		if mid == "" {
			continue
		}
		a.dedupeInboxMessageFiles(r.Context(), mid)
		var n, ready int
		_ = a.db.QueryRow(r.Context(), `
			SELECT count(*)::int,
			       count(*) FILTER (WHERE coalesce(storage_key,'')<>'')::int
			FROM inbox_message_files WHERE message_id=$1::uuid`, mid).Scan(&n, &ready)
		row["media_count"] = n
		row["media_ready"] = ready
	}
	return a.redactInboxMessagesForRole(r, rows), hasMore
}

func (a *App) replyInbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !a.canAccessInboxThread(r, id) {
		writeJSON(w, 404, map[string]string{"error": "диалог не найден"})
		return
	}

	var bodyText, preferLine string
	type outFile struct {
		bytes  []byte
		name   string
		mime   string
		kind   string
		key    string
		fileID string
	}
	var outFiles []outFile

	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось прочитать файл"})
			return
		}
		bodyText = strings.TrimSpace(r.FormValue("body"))
		preferLine = strings.TrimSpace(r.FormValue("line_id"))
		var hdrs []*multipart.FileHeader
		if r.MultipartForm != nil {
			hdrs = append(hdrs, r.MultipartForm.File["file"]...)
			hdrs = append(hdrs, r.MultipartForm.File["files"]...)
			hdrs = append(hdrs, r.MultipartForm.File["file[]"]...)
		}
		seen := map[string]bool{}
		for _, hdr := range hdrs {
			if hdr == nil {
				continue
			}
			dupKey := hdr.Filename + ":" + strconv.FormatInt(hdr.Size, 10)
			if seen[dupKey] {
				continue
			}
			seen[dupKey] = true
			f, err := hdr.Open()
			if err != nil {
				continue
			}
			raw, rerr := io.ReadAll(io.LimitReader(f, 32<<20))
			_ = f.Close()
			if rerr != nil || len(raw) == 0 {
				writeJSON(w, 400, map[string]string{"error": "пустой файл"})
				return
			}
			name := sanitizeMediaFilename(hdr.Filename)
			mime := strings.TrimSpace(hdr.Header.Get("Content-Type"))
			if mime == "" {
				mime = "application/octet-stream"
			}
			kind := mediaKindFromContentType(mime, name)
			if kind == "" {
				kind = "file"
			}
			outFiles = append(outFiles, outFile{bytes: raw, name: name, mime: mime, kind: kind})
		}
	} else {
		var in struct {
			Body   string `json:"body"`
			LineID string `json:"line_id"`
		}
		if err := readJSON(r, &in); err != nil {
			writeJSON(w, 400, map[string]string{"error": "некорректный JSON"})
			return
		}
		bodyText = strings.TrimSpace(in.Body)
		preferLine = strings.TrimSpace(in.LineID)
	}
	if bodyText == "" && len(outFiles) == 0 {
		writeJSON(w, 400, map[string]string{"error": "пустой текст"})
		return
	}
	if a.gateManagerOutbound(w, r, "inbox_thread", id, bodyText, false) {
		return
	}

	var channel, chatID, phone, provider, extRef, lineID, contactID string
	if err := a.db.QueryRow(r.Context(), `
		SELECT t.channel, t.external_id, coalesce(t.phone,''),
		       coalesce(l.provider,''), coalesce(l.external_ref,''), coalesce(t.line_id::text,''),
		       coalesce(t.contact_id::text,'')
		FROM inbox_threads t
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE t.id=$1`, id).Scan(&channel, &chatID, &phone, &provider, &extRef, &lineID, &contactID); err != nil {
		writeJSON(w, 404, map[string]string{"error": "диалог не найден"})
		return
	}
	if preferLine == "" {
		preferLine = lineID
	}
	// Всегда шлём с указанной / своей линии — без «случайного» профиля Wappi.
	if normChannel(channel) == "max" && (messengerVia(provider) == "wappi" || provider == "" || a.cfg.WappiReady()) {
		leadID := ""
		if contactID != "" {
			_ = a.db.QueryRow(r.Context(), `
				SELECT id::text FROM leads WHERE contact_id=$1::uuid ORDER BY created_at DESC LIMIT 1`, contactID).Scan(&leadID)
		}
		if leadID == "" {
			_ = a.db.QueryRow(r.Context(), `
				SELECT le.id::text FROM leads le
				LEFT JOIN contacts c ON c.id=le.contact_id
				WHERE regexp_replace(coalesce(c.phone,''),'[^0-9]','','g') LIKE '%' || right(regexp_replace($1,'[^0-9]','','g'), 10)
				ORDER BY le.created_at DESC LIMIT 1`, phone).Scan(&leadID)
		}
		// Маска из UI могла попасть в thread.phone (5 цифр) — чиним из CRM, номер наружу не отдаём.
		if maxNormPhone(phone) == "" {
			var crmPhone string
			if contactID != "" {
				_ = a.db.QueryRow(r.Context(), `SELECT coalesce(phone,'') FROM contacts WHERE id=$1::uuid`, contactID).Scan(&crmPhone)
			}
			if maxNormPhone(crmPhone) == "" && leadID != "" {
				_ = a.db.QueryRow(r.Context(), `
					SELECT coalesce(c.phone,'') FROM leads l
					LEFT JOIN contacts c ON c.id=l.contact_id
					WHERE l.id=$1::uuid`, leadID).Scan(&crmPhone)
			}
			if fixed := maxNormPhone(crmPhone); fixed != "" {
				phone = fixed
				if maxNormPhone(chatID) == "" {
					chatID = fixed
				}
				_, _ = a.db.Exec(r.Context(), `
					UPDATE inbox_threads SET phone=$2 WHERE id=$1::uuid`, id, fixed)
			}
		} else {
			phone = maxNormPhone(phone)
		}
		resolved := a.wappiLineForLead(r.Context(), leadID, actorID(r), preferLine)
		if resolved != "" && !keepMaxThreadLine(lineID, resolved) {
			lineID = resolved
			if pid := a.wappiLineProfile(r.Context(), lineID); pid != "" {
				extRef = pid
				provider = "wappi"
			}
			_, _ = a.db.Exec(r.Context(), `UPDATE inbox_threads SET line_id=$2::uuid WHERE id=$1::uuid`, id, lineID)
		}
		if strings.TrimSpace(extRef) == "" || looksLikeLegacyWazzupRef(extRef) {
			writeJSON(w, 400, map[string]string{
				"error": "нет привязанного аккаунта MAX — выберите свой номер",
			})
			return
		}
		if maxNormPhone(phone) == "" && chatPhone(chatID) == "" {
			writeJSON(w, 400, map[string]string{
				"error": "нет телефона контакта — укажите номер у лида",
			})
			return
		}
	}

	uid := actorID(r)
	author := a.userFullName(r.Context(), uid)
	if author == "" {
		_ = a.db.QueryRow(r.Context(), `SELECT coalesce(email,'') FROM users WHERE id=$1::uuid`, uid).Scan(&author)
		author = strings.TrimSpace(author)
	}
	if author == "" {
		author = "оператор"
	}

	var msgID string
	sent := false
	note := "сообщение записано у нас"

	if len(outFiles) > 0 {
		a.ensureInboxMediaFilesSchema(r.Context())
		caption := bodyText
		first := outFiles[0]
		displayBody := caption
		if displayBody == "" {
			if len(outFiles) == 1 {
				displayBody = mediaPlaceholder(first.kind)
				if first.name != "" && first.name != "file.bin" {
					displayBody = displayBody + ": " + first.name
				}
			} else {
				displayBody = fmt.Sprintf("%d вложения", len(outFiles))
			}
		}
		dir := filepath.Join(a.inboxMediaRoot(), id)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			writeJSON(w, 500, map[string]string{"error": "не удалось сохранить файл"})
			return
		}
		for i := range outFiles {
			mediaKey := fmt.Sprintf("%s/out-%d-%d-%s", id, time.Now().UnixNano(), i, outFiles[i].name)
			abs, err := a.mediaAbsPath(mediaKey)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": "некорректный путь файла"})
				return
			}
			if err := os.WriteFile(abs, outFiles[i].bytes, 0o640); err != nil {
				writeJSON(w, 500, map[string]string{"error": "не удалось сохранить файл"})
				return
			}
			outFiles[i].key = mediaKey
		}
		err := a.db.QueryRow(r.Context(), `
			INSERT INTO inbox_messages (
				thread_id, direction, body, author_label, author_id,
				media_kind, media_filename, media_mime, media_size, media_storage_key
			) VALUES (
				$1::uuid, 'outbound', $2, $3, $4::uuid,
				$5, $6, $7, $8, $9
			) RETURNING id::text`,
			id, displayBody, author, uid,
			first.kind, first.name, first.mime, len(first.bytes), first.key,
		).Scan(&msgID)
		if err != nil {
			for _, f := range outFiles {
				if abs, aerr := a.mediaAbsPath(f.key); aerr == nil {
					_ = os.Remove(abs)
				}
			}
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить"})
			return
		}
		for i, f := range outFiles {
			var fid string
			_ = a.db.QueryRow(r.Context(), `
				INSERT INTO inbox_message_files (message_id, sort_order, kind, filename, mime, size_bytes, storage_key, external_ref)
				VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
				RETURNING id::text`,
				msgID, i, f.kind, f.name, f.mime, len(f.bytes), f.key, strconv.Itoa(i)).Scan(&fid)
			outFiles[i].fileID = fid
		}
		_, _ = a.db.Exec(r.Context(), `UPDATE inbox_threads SET last_body=$2, last_at=now() WHERE id=$1`, id, displayBody)

		a.setInboxMessageDelivery(r.Context(), msgID, "pending", "", channel)
		var lineSendErr error
		if messengerVia(provider) == "wappi" || (provider == "" && a.cfg.WappiReady()) {
			sentN := 0
			var lastFileErr error
			publicOK := true
			for i, f := range outFiles {
				publicURL := a.inboxMediaPublicURLFile(msgID, f.fileID)
				if publicURL == "" && i == 0 {
					publicURL = a.inboxMediaPublicURL(msgID)
				}
				cap := ""
				if i == 0 {
					cap = caption
				}
				switch {
				case publicURL == "":
					publicOK = false
					lastFileErr = fmt.Errorf("PUBLIC_BASE_URL не задан")
				case !a.publicURLReadyForWappi(publicURL):
					publicOK = false
					lastFileErr = fmt.Errorf("PUBLIC_BASE_URL недоступен снаружи")
				default:
					if wid, ferr := a.wappiSendFileURL(r.Context(), channel, extRef, chatID, phone, publicURL, f.name, cap); ferr != nil {
						lastFileErr = ferr
					} else {
						sentN++
						if i == 0 {
							a.setInboxMessageDelivery(r.Context(), msgID, "sent", wid, channel)
						}
					}
				}
			}
			if sentN == len(outFiles) && sentN > 0 {
				sent = true
				note = "отправлено"
			} else if !publicOK {
				note = "файл сохранён в CRM; в MAX не ушёл — ссылка PUBLIC_BASE_URL недоступна снаружи (локальная разработка или другой сервер)"
				a.setInboxMessageDelivery(r.Context(), msgID, "failed", "", channel)
				if caption != "" {
					names := make([]string, 0, len(outFiles))
					for _, f := range outFiles {
						names = append(names, f.name)
					}
					if wid, terr := a.sendInboxOut(r.Context(), provider, channel, extRef, chatID, phone, caption+"\n("+strings.Join(names, ", ")+")"); terr == nil {
						sent = true
						note = "текст в MAX отправлен, файл только в CRM (PUBLIC_BASE_URL не отдаёт файл)"
						a.setInboxMessageDelivery(r.Context(), msgID, "sent", wid, channel)
					} else if terr != nil {
						note = note + "; текст: " + humanizeMessengerErr(terr)
					}
				}
			} else if sentN > 0 {
				sent = true
				note = fmt.Sprintf("отправлено %d из %d; часть: %s", sentN, len(outFiles), humanizeMessengerErr(lastFileErr))
				a.setInboxMessageDelivery(r.Context(), msgID, "sent", "", channel)
			} else {
				note = "файл в CRM; в MAX: " + humanizeMessengerErr(lastFileErr)
				a.setInboxMessageDelivery(r.Context(), msgID, "failed", "", channel)
				if caption != "" {
					names := make([]string, 0, len(outFiles))
					for _, f := range outFiles {
						names = append(names, f.name)
					}
					if twid, terr := a.sendInboxOut(r.Context(), provider, channel, extRef, chatID, phone, caption+"\n(файлы: "+strings.Join(names, ", ")+")"); terr == nil {
						sent = true
						note = "текст отправлен, файл: " + humanizeMessengerErr(lastFileErr)
						a.setInboxMessageDelivery(r.Context(), msgID, "sent", twid, channel)
					} else {
						lineSendErr = terr
					}
				}
				if !sent && lineSendErr == nil {
					lineSendErr = lastFileErr
				}
			}
		} else if wid, err := a.sendInboxOut(r.Context(), provider, channel, extRef, chatID, phone, displayBody); err != nil {
			lineSendErr = err
			note = humanizeMessengerErr(err)
			a.setInboxMessageDelivery(r.Context(), msgID, "failed", "", channel)
		} else {
			sent = true
			note = "отправлено"
			a.setInboxMessageDelivery(r.Context(), msgID, "sent", wid, channel)
		}
		a.writePhones(w, r, 201, a.inboxReplyResult(r.Context(), sent, note, msgID, lineID, lineSendErr))
		return
	}

	text := bodyText
	// Сначала пишем у себя — иначе webhook outgoing_* успевает раньше и получается два «нормально».
	err := a.db.QueryRow(r.Context(), `
		SELECT id::text FROM inbox_messages
		WHERE thread_id=$1::uuid AND direction='outbound' AND body=$2
		  AND created_at > now() - interval '90 seconds'
		  AND (author_id IS NULL OR author_id=$3::uuid)
		  AND coalesce(media_kind,'') = ''
		ORDER BY created_at DESC LIMIT 1`, id, text, uid).Scan(&msgID)
	if err != nil || msgID == "" {
		err = a.db.QueryRow(r.Context(), `
			INSERT INTO inbox_messages (thread_id, direction, body, author_label, author_id)
			VALUES ($1::uuid, 'outbound', $2, $3, $4::uuid)
			RETURNING id::text`, id, text, author, uid).Scan(&msgID)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить"})
			return
		}
	} else {
		_, _ = a.db.Exec(r.Context(), `
			UPDATE inbox_messages
			SET author_label=$2, author_id=COALESCE(author_id, $3::uuid)
			WHERE id=$1::uuid`, msgID, author, uid)
	}
	_, _ = a.db.Exec(r.Context(), `UPDATE inbox_threads SET last_body=$2, last_at=now() WHERE id=$1`, id, text)

	a.setInboxMessageDelivery(r.Context(), msgID, "pending", "", channel)
	var textSendErr error
	if wid, err := a.sendInboxOut(r.Context(), provider, channel, extRef, chatID, phone, text); err != nil {
		textSendErr = err
		note = err.Error()
		a.setInboxMessageDelivery(r.Context(), msgID, "failed", "", channel)
	} else {
		sent = true
		note = "отправлено"
		a.setInboxMessageDelivery(r.Context(), msgID, "sent", wid, channel)
	}
	a.writePhones(w, r, 201, a.inboxReplyResult(r.Context(), sent, note, msgID, lineID, textSendErr))
}

func (a *App) inboxHook(w http.ResponseWriter, r *http.Request) {
	if !a.checkInboxHook(r) {
		writeJSON(w, 403, map[string]string{"error": "секрет вебхука не совпал"})
		return
	}
	provider := strings.ToLower(chi.URLParam(r, "provider"))
	body, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if provider == "wappi" {
		_, _ = a.db.Exec(r.Context(),
			`INSERT INTO inbound_webhooks (source, payload) VALUES ('wappi', $1::jsonb)`,
			string(body))
	}
	raw := map[string]any{}
	_ = json.Unmarshal(body, &raw)
	deliveryN := 0
	if provider == "wappi" {
		deliveryN = a.applyWappiDeliveryStatuses(r.Context(), raw)
	}
	events := inboxEvents(provider, raw)
	n := 0
	for _, ev := range events {
		if ev.Body == "" && ev.ChatID == "" {
			continue
		}
		if err := a.upsertInbox(r.Context(), ev); err == nil {
			n++
			if a.hub != nil {
				a.hub.Broadcast("inbox.updated", map[string]any{
					"channel": ev.Channel,
					"chat_id": ev.ChatID,
				})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "accepted": n, "delivery": deliveryN})
}

func (a *App) checkInboxHook(r *http.Request) bool {
	want := a.cfg.InboxWebhookSecret
	if want == "" {
		return false
	}
	got := chi.URLParam(r, "secret")
	if got == "" {
		got = r.URL.Query().Get("k")
	}
	if got == "" {
		got = r.Header.Get("X-Webhook-Secret")
	}
	if got == "" {
		got = r.Header.Get("X-Max-Bot-Api-Secret")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

type inboxEv struct {
	Channel, ChatID, Title, Phone, Body, ExtID, Direction, Profile string
	LeadID, ContactID, DealID                                      string
	MediaKind, MediaMsgID, FileLink, FileName, Mime                string
	Attaches                                                       []inboxAttach
	At                                                             time.Time
}

type inboxAttach struct {
	Kind, FileLink, FileName, Mime, ExtID string
}

func inboxEvents(provider string, raw map[string]any) []inboxEv {
	if isMaxBotPayload(provider, raw) {
		return maxBotEvents(raw)
	}
	chDefault := hookChannel(provider)
	var out []inboxEv
	if list, ok := raw["messages"].([]any); ok {
		for _, item := range list {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			if ev, ok := inboxEvFrom(provider, chDefault, flattenInbox(m)); ok {
				out = append(out, ev)
			}
		}
		return out
	}
	// Wappi иногда шлёт messages одним объектом (например delivery_status).
	if m, ok := raw["messages"].(map[string]any); ok {
		if ev, ok := inboxEvFrom(provider, chDefault, flattenInbox(m)); ok {
			out = append(out, ev)
		}
		return out
	}
	if ev, ok := inboxEvFrom(provider, chDefault, flattenInbox(raw)); ok {
		out = append(out, ev)
	}
	return out
}

func isMaxBotPayload(provider string, raw map[string]any) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "max" || p == "maxbot" {
		return true
	}
	if firstScalar(raw["update_type"]) != "" {
		return true
	}
	_, hasUpdates := raw["updates"]
	return hasUpdates
}

func flattenInbox(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
		nested, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for nk, nv := range nested {
			out[k+"."+nk] = nv
			if _, exists := out[nk]; !exists {
				out[nk] = nv
			}
		}
	}
	return out
}

func inboxEvFrom(provider, ch string, m map[string]any) (inboxEv, bool) {
	s := func(keys ...string) string {
		for _, k := range keys {
			if v := firstScalar(m[k]); v != "" {
				return v
			}
			for rk, rv := range m {
				if strings.EqualFold(rk, k) {
					if v := firstScalar(rv); v != "" {
						return v
					}
				}
			}
		}
		return ""
	}
	wh := strings.ToLower(s("wh_type", "type"))
	switch {
	case strings.Contains(wh, "delivery"), strings.Contains(wh, "authorization"),
		strings.Contains(wh, "application"), strings.Contains(wh, "incoming_call"),
		wh == "status":
		return inboxEv{}, false
	}
	got := normChannel(s("channel", "chatType", "chat_type", "messenger"))
	if got == "other" || got == "chat" || got == "dialog" || got == "wappi" {
		got = ch
	}
	if got == "other" || got == "wappi" {
		got = inferChannel(provider, chatIDHint(s))
	}
	dir := "inbound"
	st := strings.ToLower(s("status", "direction", "wh_type"))
	echo := strings.EqualFold(firstScalar(m["isEcho"]), "true") ||
		strings.EqualFold(firstScalar(m["is_me"]), "true") ||
		strings.EqualFold(firstScalar(m["fromMe"]), "true") ||
		strings.EqualFold(firstScalar(m["from_me"]), "true")
	if echo || strings.Contains(st, "out") {
		dir = "outbound"
	}
	chatID := s("chatId", "chat_id", "chat")
	if chatID == "" {
		chatID = s("from")
	}
	phone := s("contact_phone", "phone", "contact.phone")
	if phone == "" {
		if cand := s("from"); cand != "" {
			dig := maxNormPhone(cand)
			if dig == "" {
				dig = maxNormPhone(chatPhone(cand))
			}
			if len(dig) >= 11 {
				phone = dig
			}
		}
	}
	if phone == "" {
		phone = chatPhone(s("from", "chatId"))
	} else if dig := maxNormPhone(phone); dig != "" {
		phone = dig
	} else if dig := chatPhone(phone); dig != "" {
		phone = dig
	}
	body := inboxHookBody(s)
	if body == "" && chatID == "" {
		return inboxEv{}, false
	}
	if isMaxSystemNotice(body) {
		return inboxEv{}, false
	}
	mediaKind := normalizeMediaKind(s("type"))
	fileLink := strings.TrimSpace(s("file_link", "fileLink"))
	fileName := strings.TrimSpace(s("file_name", "fileName", "filename"))
	mime := strings.TrimSpace(s("mimetype", "mime_type", "mime"))
	mediaMsgID := strings.TrimSpace(s("messageId", "message_id", "id"))
	var attaches []inboxAttach
	// attaches[] — все вложения сообщения (паспорт + фото и т.д.)
	if atts, ok := m["attaches"].([]any); ok {
		for i, rawAtt := range atts {
			am, _ := rawAtt.(map[string]any)
			if am == nil {
				continue
			}
			aLink := strings.TrimSpace(firstScalar(am["url"]))
			if aLink == "" {
				aLink = strings.TrimSpace(firstScalar(am["file_link"]))
			}
			aName := strings.TrimSpace(firstScalar(am["file_name"]))
			if aName == "" {
				aName = strings.TrimSpace(firstScalar(am["name"]))
			}
			if aName == "" {
				aName = strings.TrimSpace(firstScalar(am["fileName"]))
			}
			aKind := normalizeMediaKind(firstScalar(am["type"]))
			if aKind == "" && aLink != "" {
				aKind = "file"
			}
			aMime := strings.TrimSpace(firstScalar(am["mimetype"]))
			if aMime == "" {
				aMime = strings.TrimSpace(firstScalar(am["mime"]))
			}
			aExt := strings.TrimSpace(firstScalar(am["id"]))
			if aExt == "" {
				aExt = fmt.Sprintf("%d", i)
			}
			if aLink == "" && aKind == "" {
				continue
			}
			attaches = append(attaches, inboxAttach{
				Kind: aKind, FileLink: aLink, FileName: aName, Mime: aMime, ExtID: aExt,
			})
		}
	}
	if len(attaches) > 0 {
		if fileLink == "" {
			fileLink = attaches[0].FileLink
		}
		if fileName == "" {
			fileName = attaches[0].FileName
		}
		if mediaKind == "" {
			mediaKind = attaches[0].Kind
		}
		if mime == "" {
			mime = attaches[0].Mime
		}
	}
	if mediaKind == "" && fileLink != "" {
		mediaKind = "file"
	}
	// body у image/document — временный URL; в БД не кладём.
	if mediaKind != "" && looksLikeHTTPURL(body) {
		body = mediaPlaceholder(mediaKind)
		if cap := strings.TrimSpace(s("caption")); cap != "" && !looksLikeHTTPURL(cap) {
			body = cap
		}
	}
	if mediaKind != "" && fileLink == "" && looksLikeHTTPURL(s("body", "text", "message")) {
		fileLink = strings.TrimSpace(s("body", "text", "message"))
	}
	if len(attaches) == 0 && (mediaKind != "" || fileLink != "") {
		attaches = []inboxAttach{{
			Kind: mediaKind, FileLink: fileLink, FileName: fileName, Mime: mime, ExtID: "0",
		}}
	}
	return inboxEv{
		Channel:    got,
		ChatID:     chatID,
		Title:      s("contact_name", "senderName", "authorName", "author_name", "contact.name", "chatName", "username"),
		Phone:      phone,
		Body:       body,
		ExtID:      mediaMsgID,
		Direction:  dir,
		Profile:    s("profile_id", "profileId", "channelId", "channel_id", "profile", "line"),
		MediaKind:  mediaKind,
		MediaMsgID: mediaMsgID,
		FileLink:   fileLink,
		FileName:   fileName,
		Mime:       mime,
		Attaches:   attaches,
		At:         parseInboxEventTime(m),
	}, true
}

func parseInboxEventTime(m map[string]any) time.Time {
	keys := []string{
		"timestamp", "time", "date", "created_at", "createdAt",
		"messageTimestamp", "message_timestamp", "msg_time", "ts",
	}
	for _, k := range keys {
		raw := firstScalar(m[k])
		if raw == "" {
			for rk, rv := range m {
				if strings.EqualFold(rk, k) {
					raw = firstScalar(rv)
					break
				}
			}
		}
		if raw == "" {
			continue
		}
		if unix, err := strconv.ParseInt(raw, 10, 64); err == nil && unix > 0 {
			if unix > 1_000_000_000_000 {
				unix /= 1000
			}
			return time.Unix(unix, 0).UTC()
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, raw); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}

func looksLikeHTTPURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// isMaxSystemNotice — сервисные алерты MAX (вход в аккаунт и т.п.), не сообщения клиента.
func isMaxSystemNotice(body string) bool {
	b := strings.ToLower(body)
	// HTML от MAX: <b>Новый вход в MAX</b> …
	markers := []string{
		"новый вход в max",
		"обнаружили вход в профиль",
		"завершить все сессии",
		"настройки → устройства",
		"настройки -> устройства",
	}
	for _, m := range markers {
		if strings.Contains(b, m) {
			return true
		}
	}
	return false
}

// maxInboundMayBindContact — inbound MAX цепляет контакт только при совпадении телефонов.
func maxInboundMayBindContact(senderPhone, contactPhone string) bool {
	s := maxNormPhone(senderPhone)
	c := maxNormPhone(contactPhone)
	return s != "" && c != "" && s == c
}

func chatIDHint(s func(...string) string) string {
	if v := s("chatId", "chat_id", "from"); v != "" {
		return v
	}
	return s("chat")
}

func hookChannel(provider string) string {
	got := normChannel(provider)
	if got == "wappi" || got == "other" {
		return "other"
	}
	return got
}

func inferChannel(provider, chatID string) string {
	id := strings.ToLower(chatID)
	switch {
	case strings.Contains(id, "@c.us"), strings.Contains(id, "@g.us"), strings.Contains(id, "@lid"):
		return "whatsapp"
	}
	if strings.EqualFold(provider, "wappi") {
		// MAX chat_id обычно числовой (часто отрицательный), без @c.us.
		return "max"
	}
	return hookChannel(provider)
}

func inboxHookBody(s func(...string) string) string {
	typ := strings.ToLower(s("type"))
	cap := s("caption")
	text := s("text", "body", "message")
	switch typ {
	case "image", "video", "audio", "document", "file":
		if cap != "" {
			return cap
		}
		if typ == "image" {
			return "[изображение]"
		}
		return "[файл]"
	}
	if len(text) > 800 && cap != "" {
		return cap
	}
	return text
}

func (a *App) matchDefaultInboxLine(ctx context.Context, channel, provider string) string {
	var id string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM inbox_lines
		WHERE is_active AND channel=$1 AND provider=$2
		ORDER BY name LIMIT 1`, channel, provider).Scan(&id)
	return id
}

func (a *App) matchInboxLine(ctx context.Context, channel, hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return ""
	}
	var id string
	if channel != "" && channel != "other" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_lines
			WHERE is_active AND channel=$1 AND (
				external_ref ILIKE $2 OR name ILIKE $2 OR operator_label ILIKE $2
			)
			ORDER BY CASE WHEN external_ref ILIKE $2 THEN 0 ELSE 1 END, name LIMIT 1`, channel, hint).Scan(&id)
		if id != "" {
			return id
		}
	}
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM inbox_lines
		WHERE is_active AND (external_ref ILIKE $1 OR name ILIKE $1)
		ORDER BY CASE WHEN external_ref ILIKE $1 THEN 0 ELSE 1 END, name LIMIT 1`, hint).Scan(&id)
	return id
}

// mergeMaxThreadByPhone — только legacy-треды без префикса line:.
// Новые треды ключуются как lineId:phone и сюда не попадают.
func (a *App) mergeMaxThreadByPhone(ctx context.Context, chatID, phone, lineID string) {
	chatID = strings.TrimSpace(chatID)
	lineID = strings.TrimSpace(lineID)
	if chatID == "" || strings.Contains(chatID, ":") {
		return
	}
	phone = maxNormPhone(phone)
	if phone == "" || phone == chatID {
		return
	}
	if lineID != "" {
		var modern string
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_threads
			WHERE channel='max' AND external_id=$1`, lineID+":"+phone).Scan(&modern)
		if modern != "" {
			return
		}
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_threads
			SET external_id=$1, phone=COALESCE(NULLIF(phone,''), $2), line_id=COALESCE(line_id, $3::uuid)
			WHERE channel='max' AND external_id=$2 AND (line_id IS NULL OR line_id=$3::uuid)`,
			lineID+":"+phone, phone, lineID)
		return
	}
	var exists string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM inbox_threads WHERE channel='max' AND external_id=$1`, chatID).Scan(&exists)
	if exists != "" {
		return
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE inbox_threads
		SET external_id=$1, phone=COALESCE(NULLIF(phone,''), $2)
		WHERE channel='max' AND external_id=$2 AND position(':' in external_id)=0`, chatID, phone)
}

func (a *App) upsertInbox(ctx context.Context, ev inboxEv) error {
	if ev.ChatID == "" {
		ev.ChatID = ev.Phone
	}
	if ev.ChatID == "" {
		ev.ChatID = ev.ExtID
	}
	if ev.ChatID == "" {
		return nil
	}
	if ev.Title == "" {
		ev.Title = ev.Phone
	}
	if ev.Title == "" {
		ev.Title = ev.ChatID
	}
	ent, eid := "", ""
	var contact, deal string
	phone := maxNormPhone(ev.Phone)
	strictMaxIn := ev.Channel == "max" && ev.Direction == "inbound"

	if ev.LeadID != "" {
		var cID, cPhone, cName, title string
		if a.db.QueryRow(ctx, `
			SELECT coalesce(l.contact_id::text,''), coalesce(c.phone,''), coalesce(c.full_name,''), l.title
			FROM leads l LEFT JOIN contacts c ON c.id=l.contact_id
			WHERE l.id=$1`, ev.LeadID).Scan(&cID, &cPhone, &cName, &title) == nil {
			if cID != "" {
				contact = cID
			}
			// Inbound MAX: не подставляем телефон лида в sender — иначе чужой event «станет» клиентом.
			if !strictMaxIn && phone == "" {
				phone = maxNormPhone(cPhone)
				ev.Phone = phone
			}
			if ev.Title == "" || ev.Title == ev.ChatID {
				if cName != "" {
					ev.Title = cName
				} else if title != "" {
					ev.Title = title
				}
			}
			// Inbound MAX: lead_id только если sender phone = phone контакта лида.
			if strictMaxIn && !maxInboundMayBindContact(phone, cPhone) {
				contact = ""
			}
		}
	}
	if ev.ContactID != "" && contact == "" {
		contact = ev.ContactID
	}
	if ev.DealID != "" {
		deal = ev.DealID
		if contact == "" {
			_ = a.db.QueryRow(ctx, `SELECT contact_id::text FROM deals WHERE id=$1`, ev.DealID).Scan(&contact)
		}
	}
	// К лиду/контакту цепляем только по реальному телефону отправителя.
	if contact == "" && deal == "" && phone != "" {
		ent, eid = a.matchPhone(ctx, phone, "")
	}
	if ent == "contact" {
		contact = eid
	}
	if ent == "deal" {
		deal = eid
		_ = a.db.QueryRow(ctx, `SELECT contact_id::text FROM deals WHERE id=$1`, eid).Scan(&contact)
	}
	if ent == "lead" && contact == "" {
		_ = a.db.QueryRow(ctx, `SELECT contact_id::text FROM leads WHERE id=$1`, eid).Scan(&contact)
	}
	if strictMaxIn {
		if phone == "" {
			contact, deal = "", ""
		} else if contact != "" {
			var cPhone string
			_ = a.db.QueryRow(ctx, `SELECT coalesce(phone,'') FROM contacts WHERE id=$1::uuid`, contact).Scan(&cPhone)
			if !maxInboundMayBindContact(phone, cPhone) {
				contact, deal = "", ""
			}
		}
	}
	lineID := a.matchInboxLine(ctx, ev.Channel, ev.Profile)
	if lineID == "" {
		lineID = a.matchInboxLine(ctx, ev.Channel, ev.Title)
	}
	if lineID == "" && ev.Channel == "max" {
		lineID = a.matchDefaultInboxLine(ctx, "max", "wappi")
		if lineID == "" {
			lineID = a.matchDefaultInboxLine(ctx, "max", "max")
		}
	}
	// После перепривязки MAX не принимаем старую историю (webhook/poll dump).
	if lineID != "" {
		if since, ok := a.inboxLineHistoryFrom(ctx, lineID); ok {
			at := ev.At
			if at.IsZero() {
				at = time.Now().UTC()
			}
			if at.Before(since) {
				return nil
			}
		}
	}
	if lineID != "" && (ev.Channel == "" || ev.Channel == "other") {
		var lineCh string
		if a.db.QueryRow(ctx, `SELECT channel FROM inbox_lines WHERE id=$1`, lineID).Scan(&lineCh) == nil && lineCh != "" {
			ev.Channel = lineCh
		}
	}

	extID := ev.ChatID
	if ev.Channel == "max" && lineID != "" && phone != "" {
		extID = lineID + ":" + phone
	} else if ev.Channel == "max" && lineID != "" && phone == "" && strings.TrimSpace(ev.ChatID) != "" {
		// Без client phone — не мержить в line:phone тред лида.
		extID = lineID + ":chat:" + strings.TrimSpace(ev.ChatID)
	} else if ev.Channel == "max" && !strictMaxIn {
		a.mergeMaxThreadByPhone(ctx, ev.ChatID, ev.Phone, lineID)
	} else if ev.Channel == "max" && phone != "" {
		a.mergeMaxThreadByPhone(ctx, ev.ChatID, ev.Phone, lineID)
	}

	// Не писать inbound в чужой contact-тред, если телефон отправителя не совпал.
	if strictMaxIn && phone != "" {
		var existContact, existPhone string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(t.contact_id::text,''), coalesce(c.phone,'')
			FROM inbox_threads t
			LEFT JOIN contacts c ON c.id=t.contact_id
			WHERE t.channel=$1 AND t.external_id=$2`, ev.Channel, extID).Scan(&existContact, &existPhone)
		if existContact != "" && maxNormPhone(existPhone) != "" && maxNormPhone(existPhone) != phone {
			extID = lineID + ":chat:" + strings.TrimSpace(ev.ChatID)
			if extID == lineID+":chat:" {
				extID = lineID + ":chat:" + phone + ":mismatch"
			}
			contact, deal = "", ""
		}
	}

	var id string
	err := a.db.QueryRow(ctx, `
		INSERT INTO inbox_threads (channel, external_id, title, phone, contact_id, deal_id, line_id, last_body, last_at, unread)
		VALUES ($1,$2,$3,nullif($4,''),nullif($5,'')::uuid,nullif($6,'')::uuid,nullif($7,'')::uuid,$8,now(), CASE WHEN $9='inbound' THEN 1 ELSE 0 END)
		ON CONFLICT (channel, external_id) DO UPDATE SET
			title=COALESCE(NULLIF(EXCLUDED.title,''), inbox_threads.title),
			phone=COALESCE(NULLIF(EXCLUDED.phone,''), inbox_threads.phone),
			contact_id=COALESCE(EXCLUDED.contact_id, inbox_threads.contact_id),
			deal_id=COALESCE(EXCLUDED.deal_id, inbox_threads.deal_id),
			line_id=COALESCE(EXCLUDED.line_id, inbox_threads.line_id),
			last_body=EXCLUDED.last_body,
			last_at=now(),
			unread=inbox_threads.unread + CASE WHEN $9='inbound' THEN 1 ELSE 0 END
		RETURNING id::text`,
		ev.Channel, extID, ev.Title, phone, contact, deal, lineID, ev.Body, ev.Direction).Scan(&id)
	if err != nil {
		return err
	}
	if ev.Body == "" && ev.MediaKind == "" {
		return nil
	}
	if ev.Body == "" && ev.MediaKind != "" {
		ev.Body = mediaPlaceholder(ev.MediaKind)
	}
	ext := ""
	if ev.ExtID != "" {
		ext = ev.Channel + ":" + ev.ExtID
		var existingID, existingKey string
		_ = a.db.QueryRow(ctx, `
			SELECT id::text, coalesce(media_storage_key,'') FROM inbox_messages WHERE external_id=$1`, ext).Scan(&existingID, &existingKey)
		if existingID != "" {
			if ev.MediaKind != "" && existingKey == "" {
				_, _ = a.db.Exec(ctx, `
					UPDATE inbox_messages SET media_kind=COALESCE(NULLIF(media_kind,''), $2),
						media_wappi_id=COALESCE(NULLIF(media_wappi_id,''), $3)
					WHERE id=$1::uuid`, existingID, ev.MediaKind, ev.MediaMsgID)
				a.scheduleInboxMediaFetch(existingID, id, ev)
			}
			if len(ev.Attaches) > 0 || ev.MediaKind != "" {
				a.syncInboxMessageFiles(ctx, existingID, id, ev)
			}
			return nil
		}
	}
	// API уже записал outbound — вебхук outgoing_message_* не должен плодить второе «нормально».
	if ev.Direction == "outbound" {
		var mid string
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_messages
			WHERE thread_id=$1::uuid AND direction='outbound' AND body=$2
			  AND coalesce(external_id,'')=''
			  AND created_at > now() - interval '3 minutes'
			ORDER BY created_at DESC LIMIT 1`, id, ev.Body).Scan(&mid)
		// Файл из CRM: body у webhook часто другой («Изображение» vs «Изображение: name.jpg»).
		if mid == "" && (ev.MediaKind != "" || len(ev.Attaches) > 0) {
			_ = a.db.QueryRow(ctx, `
				SELECT id::text FROM inbox_messages
				WHERE thread_id=$1::uuid AND direction='outbound'
				  AND coalesce(external_id,'')=''
				  AND coalesce(media_kind,'')<>''
				  AND coalesce(media_storage_key,'')<>''
				  AND created_at > now() - interval '3 minutes'
				ORDER BY created_at DESC LIMIT 1`, id).Scan(&mid)
		}
		if mid != "" {
			if ext != "" {
				_, _ = a.db.Exec(ctx, `
					UPDATE inbox_messages SET
						external_id=$2,
						delivery_status = CASE
							WHEN coalesce(delivery_status,'') IN ('delivered','read') THEN delivery_status
							WHEN coalesce(delivery_status,'') = '' THEN 'sent'
							ELSE delivery_status
						END
					WHERE id=$1::uuid AND coalesce(external_id,'')=''`, mid, ext)
			} else {
				a.setInboxMessageDelivery(ctx, mid, "sent", "", ev.Channel)
			}
			if len(ev.Attaches) > 0 || ev.MediaKind != "" {
				a.syncInboxMessageFiles(ctx, mid, id, ev)
			}
			return nil
		}
	}
	var msgID string
	err = a.db.QueryRow(ctx, `
		INSERT INTO inbox_messages (thread_id, direction, body, author_label, external_id, media_kind, media_wappi_id)
		VALUES ($1::uuid,$2,$3,$4,nullif($5,''),$6,$7)
		RETURNING id::text`,
		id, ev.Direction, ev.Body, ev.Title, ext, ev.MediaKind, mediaWappiID(ev)).Scan(&msgID)
	if err != nil {
		return err
	}
	if ev.Direction == "inbound" && normChannel(ev.Channel) == "max" {
		a.notifyInboxMaxInbound(ctx, id, ev.Channel, ev.Body, ev.Title)
	}
	if ev.MediaKind != "" && ev.Direction == "inbound" {
		a.scheduleInboxMediaFetch(msgID, id, ev)
		a.syncInboxMessageFiles(ctx, msgID, id, ev)
	}
	return nil
}
