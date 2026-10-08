package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var mediaSafeNameRe = regexp.MustCompile(`[^a-zA-Z0-9._\-а-яА-ЯёЁ]+`)

func inboxMediaKinds() map[string]bool {
	return map[string]bool{
		"image": true, "video": true, "audio": true,
		"document": true, "file": true, "sticker": true, "ptt": true,
	}
}

func normalizeMediaKind(raw string) string {
	k := strings.ToLower(strings.TrimSpace(raw))
	switch k {
	case "img", "photo", "picture", "webp":
		return "image"
	case "doc", "docs", "application":
		return "document"
	case "voice", "ptt":
		return "audio"
	default:
		if inboxMediaKinds()[k] {
			return k
		}
		return ""
	}
}

func isInboxAudioKind(raw string) bool {
	return normalizeMediaKind(raw) == "audio"
}

// inboxFileIsAudio — одно голосовое. Картинка, документ и видео сюда не проходят,
// даже если у сообщения по ошибке стоит kind=audio.
func inboxFileIsAudio(file map[string]any, messageKind string) bool {
	if !isInboxAudioKind(messageKind) {
		return false
	}
	mime := strings.TrimSpace(fmt.Sprint(file["mime"]))
	if mime == "<nil>" {
		mime = ""
	}
	name := strings.TrimSpace(fmt.Sprint(file["filename"]))
	if name == "<nil>" {
		name = ""
	}
	detected := mediaKindFromContentType(mime, name)
	switch detected {
	case "image", "document", "video":
		return false
	}
	fk := strings.TrimSpace(fmt.Sprint(file["kind"]))
	if fk == "<nil>" {
		fk = ""
	}
	if fk != "" && !isInboxAudioKind(fk) && detected != "audio" {
		return false
	}
	return true
}

func audioMIME(mime, filename string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(m, "audio/") {
		return m
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".webm":
		return "audio/webm"
	default:
		return "audio/ogg"
	}
}

func mediaPlaceholder(kind string) string {
	switch kind {
	case "image":
		return "Изображение"
	case "video":
		return "Видео"
	case "audio":
		return "Аудио"
	case "passport_text":
		return "Паспортные данные"
	default:
		return "Файл"
	}
}

func (a *App) inboxMediaRoot() string {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	return filepath.Join(root, "data", "inbox-media")
}

var inboxMediaFilesOnce sync.Once

func (a *App) ensureInboxMediaFilesSchema(ctx context.Context) {
	inboxMediaFilesOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS inbox_message_files (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				message_id UUID NOT NULL REFERENCES inbox_messages(id) ON DELETE CASCADE,
				sort_order INT NOT NULL DEFAULT 0,
				kind TEXT NOT NULL DEFAULT 'file',
				filename TEXT NOT NULL DEFAULT '',
				mime TEXT NOT NULL DEFAULT '',
				size_bytes BIGINT NOT NULL DEFAULT 0,
				storage_key TEXT NOT NULL DEFAULT '',
				source_url TEXT NOT NULL DEFAULT '',
				external_ref TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`)
		_, _ = a.db.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS inbox_message_files_msg
			  ON inbox_message_files (message_id, sort_order)`)
	})
}

func (a *App) mediaAbsPath(storageKey string) (string, error) {
	root, err := filepath.Abs(a.inboxMediaRoot())
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Join(a.inboxMediaRoot(), filepath.FromSlash(storageKey)))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("bad path")
	}
	return abs, nil
}

func (a *App) mediaFileExists(storageKey string) bool {
	if strings.TrimSpace(storageKey) == "" {
		return false
	}
	abs, err := a.mediaAbsPath(storageKey)
	if err != nil {
		return false
	}
	st, err := os.Stat(abs)
	return err == nil && !st.IsDir()
}

func sanitizeMediaFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "file.bin"
	}
	base := filepath.Base(name)
	base = mediaSafeNameRe.ReplaceAllString(base, "_")
	base = strings.Trim(base, "._")
	if base == "" || base == "." || base == ".." {
		return "file.bin"
	}
	if len(base) > 120 {
		base = base[:120]
	}
	return base
}

func mediaKindFromContentType(mime, filename string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	switch {
	case strings.HasPrefix(m, "image/"):
		return "image"
	case strings.HasPrefix(m, "video/"):
		return "video"
	case strings.HasPrefix(m, "audio/"):
		return "audio"
	case strings.HasPrefix(m, "application/pdf"),
		strings.Contains(m, "document"),
		strings.Contains(m, "msword"),
		strings.Contains(m, "officedocument"):
		return "document"
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".bmp":
		return "image"
	case ".mp4", ".mov", ".avi", ".mkv", ".webm":
		return "video"
	case ".mp3", ".ogg", ".wav", ".m4a", ".aac":
		return "audio"
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".rtf":
		return "document"
	}
	return "file"
}

func (a *App) inboxMediaSign(messageID, fileID string, exp int64) string {
	secret := strings.TrimSpace(a.cfg.JWTSecret)
	if secret == "" {
		secret = "forge"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "inbox-media:%s:%s:%d", messageID, strings.TrimSpace(fileID), exp)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) inboxMediaPublicURL(messageID string) string {
	return a.inboxMediaPublicURLFile(messageID, "")
}

func (a *App) inboxMediaPublicURLFile(messageID, fileID string) string {
	base := strings.TrimRight(strings.TrimSpace(a.cfg.PublicBaseURL), "/")
	if base == "" || strings.TrimSpace(messageID) == "" {
		return ""
	}
	exp := time.Now().Add(ticketPublicTTL).Unix()
	sig := a.inboxMediaSign(messageID, fileID, exp)
	q := url.Values{}
	q.Set("m", messageID)
	q.Set("e", strconv.FormatInt(exp, 10))
	q.Set("s", sig)
	if strings.TrimSpace(fileID) != "" {
		q.Set("f", fileID)
	}
	return base + "/api/public/inbox-media?" + q.Encode()
}

// publicURLReadyForWappi — Wappi качает файл по PUBLIC_BASE_URL. Если ссылка
// не открывается (локальный API + прод URL) — не дергаем Wappi (иначе таймаут 20с).
func (a *App) publicURLReadyForWappi(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	cli := &http.Client{Timeout: 4 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	res, err := cli.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64))
	return res.StatusCode >= 200 && res.StatusCode < 300
}

// publicInboxMedia — короткоживущая ссылка для Wappi (скачать исходящий файл чата без JWT).
func (a *App) publicInboxMedia(w http.ResponseWriter, r *http.Request) {
	msgID := strings.TrimSpace(r.URL.Query().Get("m"))
	fileID := strings.TrimSpace(r.URL.Query().Get("f"))
	sig := strings.TrimSpace(r.URL.Query().Get("s"))
	exp, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("e")), 10, 64)
	if msgID == "" || sig == "" || exp == 0 {
		writeJSON(w, 400, map[string]string{"error": "bad token"})
		return
	}
	if time.Now().Unix() > exp {
		writeJSON(w, 410, map[string]string{"error": "ссылка истекла"})
		return
	}
	want := a.inboxMediaSign(msgID, fileID, exp)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		writeJSON(w, 403, map[string]string{"error": "неверная подпись"})
		return
	}
	var key, filename, mime string
	if fileID != "" {
		err := a.db.QueryRow(r.Context(), `
			SELECT coalesce(storage_key,''), coalesce(filename,''), coalesce(mime,'')
			FROM inbox_message_files
			WHERE id=$1::uuid AND message_id=$2::uuid`, fileID, msgID).
			Scan(&key, &filename, &mime)
		if err != nil || strings.TrimSpace(key) == "" {
			writeJSON(w, 404, map[string]string{"error": "файл не найден"})
			return
		}
	} else {
		err := a.db.QueryRow(r.Context(), `
			SELECT coalesce(media_storage_key,''), coalesce(media_filename,''), coalesce(media_mime,'')
			FROM inbox_messages WHERE id=$1::uuid AND direction='outbound'`, msgID).
			Scan(&key, &filename, &mime)
		if err != nil || strings.TrimSpace(key) == "" {
			writeJSON(w, 404, map[string]string{"error": "файл не найден"})
			return
		}
	}
	abs, err := a.mediaAbsPath(key)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл отсутствует"})
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл отсутствует"})
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	if filename == "" {
		filename = filepath.Base(key)
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

// mediaFetchCooldown — пауза после неудачной докачки, пока карточка лида
// каждые 5с перечитывает тред и иначе снова бьёт в Wappi media/download.
const mediaFetchCooldown = 2 * time.Minute

type mediaFetchGate struct {
	mu       sync.Mutex
	inflight map[string]struct{}
	nextTry  map[string]time.Time
}

func (g *mediaFetchGate) begin(msgID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight == nil {
		g.inflight = map[string]struct{}{}
		g.nextTry = map[string]time.Time{}
	}
	if _, ok := g.inflight[msgID]; ok {
		return false
	}
	g.inflight[msgID] = struct{}{}
	delete(g.nextTry, msgID)
	return true
}

func (g *mediaFetchGate) end(msgID string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inflight == nil {
		return
	}
	delete(g.inflight, msgID)
	if ok {
		delete(g.nextTry, msgID)
		return
	}
	g.nextTry[msgID] = time.Now().Add(mediaFetchCooldown)
	if len(g.nextTry) > 400 {
		now := time.Now()
		for id, until := range g.nextTry {
			if now.After(until) {
				delete(g.nextTry, id)
			}
		}
	}
}

func (g *mediaFetchGate) cooling(msgID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	until, ok := g.nextTry[msgID]
	return ok && time.Now().Before(until)
}

// inboxMessageHasMedia — скачивать только вложение. Id текстового сообщения в media_wappi_id не считается файлом.
func inboxMessageHasMedia(kind, fileLink string) bool {
	return strings.TrimSpace(kind) != "" || strings.TrimSpace(fileLink) != ""
}

// mediaWappiID — id для media/download только у вложения, не у текста.
func mediaWappiID(ev inboxEv) string {
	if !inboxMessageHasMedia(ev.MediaKind, ev.FileLink) && len(ev.Attaches) == 0 {
		return ""
	}
	return ev.MediaMsgID
}

func (a *App) scheduleInboxMediaFetch(msgID, threadID string, ev inboxEv) {
	if msgID == "" || !inboxMessageHasMedia(ev.MediaKind, ev.FileLink) {
		return
	}
	if !a.mediaFetch.begin(msgID) {
		return
	}
	go func() {
		ok := false
		defer func() { a.mediaFetch.end(msgID, ok) }()
		ok = a.fetchInboxMediaWithRetries(msgID, threadID, ev)
	}()
}

func (a *App) fetchInboxMediaWithRetries(msgID, threadID string, ev inboxEv) bool {
	// Wappi часто отдаёт file_link не сразу — пробуем несколько раз.
	delays := []time.Duration{0, 3 * time.Second, 8 * time.Second, 15 * time.Second, 30 * time.Second, 45 * time.Second}
	var lastErr error
	for i, wait := range delays {
		if wait > 0 {
			time.Sleep(wait)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := a.fetchAndStoreInboxMedia(ctx, msgID, threadID, ev)
		cancel()
		if err == nil {
			if a.hub != nil {
				a.hub.Broadcast("inbox.updated", map[string]any{"message_id": msgID, "thread_id": threadID, "media": true})
			}
			return true
		}
		lastErr = err
		log.Printf("inbox media fetch msg=%s attempt=%d: %v", msgID, i+1, err)
	}
	if lastErr != nil {
		log.Printf("inbox media fetch msg=%s gave up: %v", msgID, lastErr)
	}
	return false
}

func (a *App) fetchAndStoreInboxMedia(ctx context.Context, msgID, threadID string, ev inboxEv) error {
	var already, dbWappi, dbKind, dbName, dbMime, lineID string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(m.media_storage_key,''), coalesce(m.media_wappi_id,''), coalesce(m.media_kind,''),
		       coalesce(m.media_filename,''), coalesce(m.media_mime,''), coalesce(t.line_id::text,'')
		FROM inbox_messages m
		JOIN inbox_threads t ON t.id=m.thread_id
		WHERE m.id=$1::uuid`, msgID).Scan(&already, &dbWappi, &dbKind, &dbName, &dbMime, &lineID)
	if already != "" {
		if a.mediaFileExists(already) {
			return nil
		}
		_, _ = a.db.Exec(ctx, `UPDATE inbox_messages SET media_storage_key='' WHERE id=$1::uuid`, msgID)
		already = ""
	}
	if ev.MediaMsgID == "" {
		ev.MediaMsgID = dbWappi
	}
	if ev.MediaKind == "" {
		ev.MediaKind = dbKind
	}
	hasMedia := inboxMessageHasMedia(ev.MediaKind, ev.FileLink)
	if ev.MediaKind == "" && hasMedia {
		ev.MediaKind = "document"
	}
	if ev.FileName == "" {
		ev.FileName = dbName
	}
	if ev.Mime == "" {
		ev.Mime = dbMime
	}
	if threadID == "" {
		_ = a.db.QueryRow(ctx, `SELECT thread_id::text FROM inbox_messages WHERE id=$1::uuid`, msgID).Scan(&threadID)
	}

	link := strings.TrimSpace(ev.FileLink)
	filename := sanitizeMediaFilename(ev.FileName)
	mime := strings.TrimSpace(ev.Mime)

	if link == "" && hasMedia && ev.MediaMsgID != "" && a.cfg.WappiReady() {
		profile := strings.TrimSpace(ev.Profile)
		if profile == "" && lineID != "" {
			profile = a.wappiLineProfile(ctx, lineID)
		}
		if profile == "" {
			profile, _ = a.wappiProfileID(ctx)
		}
		if profile != "" {
			gotLink, gotName, err := a.wappiMediaDownloadLink(ctx, profile, ev.MediaMsgID)
			if err != nil {
				return err
			}
			link = gotLink
			if filename == "file.bin" && gotName != "" {
				filename = sanitizeMediaFilename(gotName)
			}
		}
	}
	if link == "" {
		return fmt.Errorf("нет ссылки на файл")
	}

	data, ctype, err := a.downloadURLBytes(ctx, link, 32<<20)
	if err != nil {
		return err
	}
	if mime == "" {
		mime = ctype
	}
	if mime == "" {
		mime = "application/octet-stream"
	}

	dir := filepath.Join(a.inboxMediaRoot(), threadID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	storageName := msgID + "_" + filename
	abs := filepath.Join(dir, storageName)
	if err := os.WriteFile(abs, data, 0o640); err != nil {
		return err
	}
	relKey := filepath.ToSlash(filepath.Join(threadID, storageName))

	_, err = a.db.Exec(ctx, `
		UPDATE inbox_messages SET
			media_kind=$2,
			media_filename=$3,
			media_mime=$4,
			media_size=$5,
			media_storage_key=$6,
			media_wappi_id=COALESCE(NULLIF(media_wappi_id,''), $7)
		WHERE id=$1::uuid`,
		msgID, ev.MediaKind, filename, mime, int64(len(data)), relKey, ev.MediaMsgID)
	return err
}

// syncInboxMessageFiles — все attaches сообщения в inbox_message_files + докачка.
// Не плодим дубли: исходящий файл из CRM уже в таблице с external_ref ”/'0',
// webhook Wappi часто приходит с другим id — обновляем ту же строку.
func (a *App) syncInboxMessageFiles(ctx context.Context, msgID, threadID string, ev inboxEv) {
	a.ensureInboxMediaFilesSchema(ctx)
	atts := ev.Attaches
	if len(atts) == 0 && inboxMessageHasMedia(ev.MediaKind, ev.FileLink) {
		atts = []inboxAttach{{
			Kind: ev.MediaKind, FileLink: ev.FileLink, FileName: ev.FileName, Mime: ev.Mime, ExtID: "0",
		}}
	}
	if len(atts) == 0 {
		return
	}
	for i, att := range atts {
		kind := att.Kind
		if kind == "" {
			kind = "file"
		}
		ref := strings.TrimSpace(att.ExtID)
		if ref == "" {
			ref = strconv.Itoa(i)
		}
		fileID := a.findInboxMessageFileID(ctx, msgID, ref, i, len(atts))
		if fileID == "" {
			_ = a.db.QueryRow(ctx, `
				INSERT INTO inbox_message_files (message_id, sort_order, kind, filename, mime, source_url, external_ref)
				VALUES ($1::uuid,$2,$3,$4,$5,$6,$7)
				RETURNING id::text`,
				msgID, i, kind, sanitizeMediaFilename(att.FileName), att.Mime, att.FileLink, ref).Scan(&fileID)
		} else {
			_, _ = a.db.Exec(ctx, `
				UPDATE inbox_message_files SET
					kind=COALESCE(NULLIF(kind,''), $2),
					filename=CASE WHEN coalesce(filename,'') IN ('','file.bin') AND $3<>'' THEN $3 ELSE filename END,
					mime=COALESCE(NULLIF(mime,''), $4),
					source_url=COALESCE(NULLIF(source_url,''), $5),
					external_ref=CASE WHEN coalesce(external_ref,'') IN ('','0') THEN $7 ELSE external_ref END,
					sort_order=$6
				WHERE id=$1::uuid`, fileID, kind, sanitizeMediaFilename(att.FileName), att.Mime, att.FileLink, i, ref)
		}
		if fileID == "" {
			continue
		}
		go func(fid string) {
			cctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if err := a.fetchAndStoreInboxMediaFile(cctx, fid, threadID, ev); err != nil {
				log.Printf("inbox media file fetch id=%s: %v", fid, err)
			} else if a.hub != nil {
				a.hub.Broadcast("inbox.updated", map[string]any{"message_id": msgID, "thread_id": threadID, "media": true})
			}
		}(fileID)
	}
	a.dedupeInboxMessageFiles(ctx, msgID)
}

// findInboxMessageFileID — найти строку вложения без создания дубля от webhook.
func (a *App) findInboxMessageFileID(ctx context.Context, msgID, ref string, sortOrder, attCount int) string {
	var fileID string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM inbox_message_files
		WHERE message_id=$1::uuid AND external_ref=$2
		LIMIT 1`, msgID, ref).Scan(&fileID)
	if fileID != "" {
		return fileID
	}
	// CRM кладёт '' или '0'; webhook — другой id / '0'.
	if ref == "0" || ref == "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_message_files
			WHERE message_id=$1::uuid AND coalesce(external_ref,'') IN ('','0') AND sort_order=$2
			LIMIT 1`, msgID, sortOrder).Scan(&fileID)
		if fileID != "" {
			return fileID
		}
	}
	// Одно вложение в событии и одна строка в CRM — это тот же файл.
	if attCount == 1 {
		var n int
		_ = a.db.QueryRow(ctx, `
			SELECT count(*)::int FROM inbox_message_files WHERE message_id=$1::uuid`, msgID).Scan(&n)
		if n == 1 {
			_ = a.db.QueryRow(ctx, `
				SELECT id::text FROM inbox_message_files
				WHERE message_id=$1::uuid ORDER BY sort_order, created_at LIMIT 1`, msgID).Scan(&fileID)
		}
	}
	return fileID
}

// dedupeInboxMessageFiles — убрать двойные строки после webhook (один файл → два «вложения»).
func (a *App) dedupeInboxMessageFiles(ctx context.Context, msgID string) {
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return
	}
	_, _ = a.db.Exec(ctx, `
		DELETE FROM inbox_message_files a
		USING inbox_message_files b
		WHERE a.message_id=$1::uuid AND b.message_id=$1::uuid AND a.id > b.id
		  AND (
		    (coalesce(a.storage_key,'')<>'' AND a.storage_key=b.storage_key)
		    OR (
		      coalesce(a.external_ref,'') IN ('','0')
		      AND coalesce(b.external_ref,'') IN ('','0')
		      AND a.sort_order=b.sort_order
		    )
		    OR (
		      coalesce(a.filename,'')<>'' AND a.filename NOT IN ('','file.bin')
		      AND a.filename=b.filename AND a.sort_order=b.sort_order
		      AND coalesce(a.size_bytes,0)=coalesce(b.size_bytes,0)
		    )
		  )`, msgID)
}

func (a *App) fetchAndStoreInboxMediaFile(ctx context.Context, fileID, threadID string, ev inboxEv) error {
	a.ensureInboxMediaFilesSchema(ctx)
	var msgID, kind, filename, mime, storage, sourceURL, extRef string
	var sortOrder int
	err := a.db.QueryRow(ctx, `
		SELECT message_id::text, kind, filename, mime, storage_key, source_url, external_ref, sort_order
		FROM inbox_message_files WHERE id=$1::uuid`, fileID).Scan(
		&msgID, &kind, &filename, &mime, &storage, &sourceURL, &extRef, &sortOrder)
	if err != nil {
		return err
	}
	if a.mediaFileExists(storage) {
		return nil
	}
	if storage != "" {
		_, _ = a.db.Exec(ctx, `UPDATE inbox_message_files SET storage_key='' WHERE id=$1::uuid`, fileID)
		storage = ""
	}
	if threadID == "" {
		_ = a.db.QueryRow(ctx, `SELECT thread_id::text FROM inbox_messages WHERE id=$1::uuid`, msgID).Scan(&threadID)
	}
	link := strings.TrimSpace(sourceURL)
	if link == "" && sortOrder == 0 && ev.MediaMsgID == "" {
		_ = a.db.QueryRow(ctx, `SELECT coalesce(media_wappi_id,'') FROM inbox_messages WHERE id=$1::uuid`, msgID).Scan(&ev.MediaMsgID)
	}
	if link == "" && sortOrder == 0 && (ev.MediaMsgID != "" || strings.TrimSpace(ev.FileLink) != "") {
		// Первый файл можно добрать через Wappi message media/download.
		sub := ev
		sub.FileLink = ev.FileLink
		sub.MediaKind = kind
		sub.FileName = filename
		sub.Mime = mime
		if err := a.fetchAndStoreInboxMedia(ctx, msgID, threadID, sub); err != nil {
			return err
		}
		var key, fn, mm string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(media_storage_key,''), coalesce(media_filename,''), coalesce(media_mime,'')
			FROM inbox_messages WHERE id=$1::uuid`, msgID).Scan(&key, &fn, &mm)
		if !a.mediaFileExists(key) {
			return fmt.Errorf("файл не сохранился")
		}
		_, err = a.db.Exec(ctx, `
			UPDATE inbox_message_files SET storage_key=$2, filename=$3, mime=$4, size_bytes=$5
			WHERE id=$1::uuid`, fileID, key, fn, mm, 0)
		return err
	}
	if link == "" {
		return fmt.Errorf("нет ссылки на файл")
	}
	data, ctype, err := a.downloadURLBytes(ctx, link, 32<<20)
	if err != nil {
		return err
	}
	if mime == "" {
		mime = ctype
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	filename = sanitizeMediaFilename(filename)
	if filename == "file.bin" {
		filename = sanitizeMediaFilename(filepath.Base(strings.Split(link, "?")[0]))
	}
	dir := filepath.Join(a.inboxMediaRoot(), threadID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	storageName := fileID + "_" + filename
	abs := filepath.Join(dir, storageName)
	if err := os.WriteFile(abs, data, 0o640); err != nil {
		return err
	}
	relKey := filepath.ToSlash(filepath.Join(threadID, storageName))
	_, err = a.db.Exec(ctx, `
		UPDATE inbox_message_files SET
			kind=$2, filename=$3, mime=$4, size_bytes=$5, storage_key=$6,
			source_url=COALESCE(NULLIF(source_url,''), $7)
		WHERE id=$1::uuid`,
		fileID, kind, filename, mime, int64(len(data)), relKey, link)
	if err != nil {
		return err
	}
	// Первый файл дублируем в колонки сообщения — совместимость со старым UI/логистом.
	if sortOrder == 0 {
		_, _ = a.db.Exec(ctx, `
			UPDATE inbox_messages SET
				media_kind=COALESCE(NULLIF(media_kind,''), $2),
				media_filename=$3, media_mime=$4, media_size=$5, media_storage_key=$6
			WHERE id=$1::uuid`, msgID, kind, filename, mime, int64(len(data)), relKey)
	}
	return nil
}

func (a *App) listInboxMessageFiles(ctx context.Context, msgID string) []map[string]any {
	a.ensureInboxMediaFilesSchema(ctx)
	a.dedupeInboxMessageFiles(ctx, msgID)
	rows := a.queryMaps(ctx, `
		SELECT id::text, sort_order, kind, filename, mime, size_bytes, storage_key, source_url, external_ref
		FROM inbox_message_files WHERE message_id=$1::uuid ORDER BY sort_order, created_at`, msgID)
	if len(rows) > 0 {
		return rows
	}
	// Legacy: одна колонка media_* на сообщении.
	var kind, filename, mime, key string
	var size int64
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(media_kind,''), coalesce(media_filename,''), coalesce(media_mime,''),
		       coalesce(media_size,0), coalesce(media_storage_key,'')
		FROM inbox_messages WHERE id=$1::uuid`, msgID).Scan(&kind, &filename, &mime, &size, &key)
	if kind == "" && key == "" {
		return nil
	}
	return []map[string]any{{
		"id": "", "sort_order": 0, "kind": kind, "filename": filename, "mime": mime,
		"size_bytes": size, "storage_key": key, "source_url": "", "external_ref": "0",
	}}
}

// kickPendingInboxMedia — при открытии чата докачиваем вложения без storage_key.
// Повторный опрос того же треда (карточка лида, каждые 5с) не ставит новую
// закачку, пока предыдущая идёт или не вышла пауза после неудачи.
func (a *App) kickPendingInboxMedia(ctx context.Context, threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || !a.cfg.WappiReady() {
		return
	}
	rows, err := a.db.Query(ctx, `
		SELECT m.id::text, coalesce(m.media_kind,''), coalesce(m.media_wappi_id,''),
		       coalesce(m.media_filename,''), coalesce(m.media_mime,''),
		       coalesce(l.external_ref,'')
		FROM inbox_messages m
		JOIN inbox_threads t ON t.id=m.thread_id
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE m.thread_id=$1::uuid
		  AND coalesce(m.media_storage_key,'')=''
		  AND coalesce(m.media_kind,'')<>''
		ORDER BY m.created_at DESC
		LIMIT 20`, threadID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind, wappiID, filename, mime, profile string
		if rows.Scan(&id, &kind, &wappiID, &filename, &mime, &profile) != nil || id == "" {
			continue
		}
		if a.mediaFetch.cooling(id) {
			continue
		}
		a.scheduleInboxMediaFetch(id, threadID, inboxEv{
			MediaKind:  kind,
			MediaMsgID: wappiID,
			FileName:   filename,
			Mime:       mime,
			Profile:    profile,
		})
	}
}

func (a *App) wappiMediaDownloadLink(ctx context.Context, profileID, messageID string) (link, fileName string, err error) {
	u := "https://wappi.pro/maxapi/sync/message/media/download?profile_id=" + url.QueryEscape(profileID) +
		"&message_id=" + url.QueryEscape(messageID)
	body, err := a.wappiJSON(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	var out struct {
		FileLink string `json:"file_link"`
		Detail   string `json:"detail"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", "", fmt.Errorf("media/download: bad json")
	}
	link = strings.TrimSpace(out.FileLink)
	if link == "" {
		return "", "", fmt.Errorf("media/download: пустой file_link")
	}
	if d := strings.TrimSpace(out.Detail); d != "" {
		if i := strings.LastIndex(d, ":"); i >= 0 {
			fileName = strings.TrimSpace(d[i+1:])
		}
	}
	return link, fileName, nil
}

func (a *App) downloadURLBytes(ctx context.Context, rawURL string, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	cli := &http.Client{Timeout: 60 * time.Second}
	res, err := cli.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("скачивание файла: сеть")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("скачивание файла: %s", res.Status)
	}
	ctype := res.Header.Get("Content-Type")
	if i := strings.Index(ctype, ";"); i >= 0 {
		ctype = strings.TrimSpace(ctype[:i])
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("файл слишком большой")
	}
	return data, ctype, nil
}

func (a *App) redactInboxMessagesForRole(r *http.Request, msgs []map[string]any) []map[string]any {
	if msgs == nil {
		return []map[string]any{}
	}
	canView := a.userCanViewInboxMedia(r.Context(), actorID(r), roleName(r))
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		row := map[string]any{}
		for k, v := range m {
			row[k] = v
		}
		kind := strings.TrimSpace(str(row, "media_kind"))
		storage := strings.TrimSpace(str(row, "media_storage_key"))
		mediaCount := asInt(row["media_count"])
		if mediaCount <= 0 && (kind != "" || storage != "") {
			mediaCount = 1
		}
		hasMedia := mediaCount > 0 || kind != "" || storage != ""
		dir := strings.ToLower(strings.TrimSpace(str(row, "direction")))
		body := strings.TrimSpace(str(row, "body"))
		// Текст с паспортными данными — как вложение: менеджеру скрыто + «Отправить логисту».
		passportText := !hasMedia && dir == "inbound" && looksLikePassportText(body)
		if passportText {
			row["sensitive_doc"] = true
			if !canView {
				hasMedia = true
				kind = "passport_text"
				row["media_kind"] = kind
				mediaCount = 1
			}
		}
		row["has_media"] = hasMedia
		row["media_count"] = mediaCount
		sent := false
		switch v := row["media_sent_to_logist"].(type) {
		case bool:
			sent = v
		case string:
			sent = v == "t" || v == "true" || v == "1"
		}
		if !sent {
			if _, ok := row["media_sent_to_logist_at"]; ok && row["media_sent_to_logist_at"] != nil {
				if s := strings.TrimSpace(fmt.Sprint(row["media_sent_to_logist_at"])); s != "" && s != "<nil>" {
					sent = true
				}
			}
		}
		row["sent_to_logist"] = sent
		delete(row, "media_sent_to_logist")
		delete(row, "media_sent_to_logist_at")
		if hasMedia {
			if kind == "" {
				kind = "file"
				row["media_kind"] = kind
			}
			if kind == "passport_text" && !canView {
				row["body"] = mediaPlaceholder(kind)
			} else if kind != "passport_text" {
				row["body"] = mediaPlaceholder(kind)
			}
		}
		delete(row, "media_storage_key")
		delete(row, "media_wappi_id")
		if !canView {
			delete(row, "media_filename")
			delete(row, "media_mime")
			delete(row, "media_size")
		}
		ready := asInt(row["media_ready"])
		row["files_open"] = canView
		row["media_download"] = canView && hasMedia && kind != "passport_text" && (storage != "" || ready > 0)
		delete(row, "media_ready")
		out = append(out, row)
	}
	return out
}

// userMayStreamInboxAudio — менеджер без права на файлы может только прослушать
// голосовое из диалога, который ему и так виден.
func (a *App) userMayStreamInboxAudio(r *http.Request, threadID, messageID string) bool {
	if !a.canAccessInboxThread(r, threadID) {
		return false
	}
	args := []any{messageID, threadID}
	vis, args := inboxMessageVisibleSQL(r, "m", args)
	var n int
	err := a.db.QueryRow(r.Context(), `
		SELECT 1 FROM inbox_messages m
		JOIN inbox_threads t ON t.id=m.thread_id
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE m.id=$1::uuid AND m.thread_id=$2::uuid
		  AND (l.history_from IS NULL OR m.created_at >= l.history_from)
		  AND (`+vis+`)`, args...).Scan(&n)
	return err == nil && n == 1
}

func (a *App) getInboxMessageMedia(w http.ResponseWriter, r *http.Request) {
	canView := a.userCanViewInboxMedia(r.Context(), actorID(r), roleName(r))
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	a.ensureInboxMediaFilesSchema(ctx)

	var threadID, wappiID, kind, profile, legacyKey, legacyName, legacyMime string
	err := a.db.QueryRow(ctx, `
		SELECT m.thread_id::text, coalesce(m.media_wappi_id,''), coalesce(m.media_kind,''),
		       coalesce(l.external_ref,''),
		       coalesce(m.media_storage_key,''), coalesce(m.media_filename,''), coalesce(m.media_mime,'')
		FROM inbox_messages m
		JOIN inbox_threads t ON t.id=m.thread_id
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE m.id=$1::uuid`, id).Scan(&threadID, &wappiID, &kind, &profile, &legacyKey, &legacyName, &legacyMime)
	if err != nil {
		if !canView {
			writeJSON(w, 403, map[string]string{"error": "нет доступа к файлу"})
			return
		}
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	listenOnly := false
	if !canView {
		if !isInboxAudioKind(kind) || !a.userMayStreamInboxAudio(r, threadID, id) {
			writeJSON(w, 403, map[string]string{"error": "нет доступа к файлу"})
			return
		}
		listenOnly = true
	}

	// Битый ключ после рестарта контейнера без volume — сбрасываем и качаем снова.
	if legacyKey != "" && !a.mediaFileExists(legacyKey) {
		_, _ = a.db.Exec(ctx, `UPDATE inbox_messages SET media_storage_key='' WHERE id=$1::uuid`, id)
		_, _ = a.db.Exec(ctx, `UPDATE inbox_message_files SET storage_key='' WHERE message_id=$1::uuid AND storage_key=$2`, id, legacyKey)
		legacyKey = ""
	}

	files := a.listInboxMessageFiles(ctx, id)
	if len(files) == 0 && kind != "" {
		a.syncInboxMessageFiles(ctx, id, threadID, inboxEv{
			MediaKind: kind, MediaMsgID: wappiID, FileName: legacyName, Mime: legacyMime, Profile: profile,
		})
		files = a.listInboxMessageFiles(ctx, id)
	}

	ev := inboxEv{MediaKind: kind, MediaMsgID: wappiID, FileName: legacyName, Mime: legacyMime, Profile: profile}
	var ready []map[string]any
	var lastErr error
	for _, f := range files {
		fid := strings.TrimSpace(fmt.Sprint(f["id"]))
		key := strings.TrimSpace(fmt.Sprint(f["storage_key"]))
		if key != "" && !a.mediaFileExists(key) {
			if fid != "" {
				_, _ = a.db.Exec(ctx, `UPDATE inbox_message_files SET storage_key='' WHERE id=$1::uuid`, fid)
			}
			key = ""
		}
		if key == "" {
			if fid != "" {
				if err := a.fetchAndStoreInboxMediaFile(ctx, fid, threadID, ev); err != nil {
					lastErr = err
				}
			} else {
				if err := a.fetchAndStoreInboxMedia(ctx, id, threadID, ev); err != nil {
					lastErr = err
				}
			}
			// refresh row
			files = a.listInboxMessageFiles(ctx, id)
			continue
		}
		ready = append(ready, f)
	}
	// refresh after fetches
	files = a.listInboxMessageFiles(ctx, id)
	ready = ready[:0]
	for _, f := range files {
		key := strings.TrimSpace(fmt.Sprint(f["storage_key"]))
		if a.mediaFileExists(key) {
			ready = append(ready, f)
		}
	}
	if len(ready) == 0 && legacyKey == "" {
		// last chance: classic single-file fetch
		ferr := a.fetchAndStoreInboxMedia(ctx, id, threadID, ev)
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(media_storage_key,''), coalesce(media_filename,''), coalesce(media_mime,'')
			FROM inbox_messages WHERE id=$1::uuid`, id).Scan(&legacyKey, &legacyName, &legacyMime)
		if a.mediaFileExists(legacyKey) {
			ready = []map[string]any{{
				"storage_key": legacyKey, "filename": legacyName, "mime": legacyMime,
			}}
		} else {
			msg := "файл ещё не готов у MAX — нажмите ещё раз через несколько секунд"
			if ferr != nil {
				msg = "не удалось сохранить файл: " + ferr.Error()
			} else if lastErr != nil {
				msg = "не удалось сохранить файл: " + lastErr.Error()
			}
			writeJSON(w, 409, map[string]string{"error": msg})
			return
		}
	}
	if len(ready) == 0 {
		msg := "файл не найден на диске"
		if lastErr != nil {
			msg = "не удалось сохранить файл: " + lastErr.Error()
		}
		writeJSON(w, 404, map[string]string{"error": msg})
		return
	}
	if listenOnly && (len(ready) != 1 || !inboxFileIsAudio(ready[0], kind)) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа к файлу"})
		return
	}

	if len(ready) == 1 {
		key := strings.TrimSpace(fmt.Sprint(ready[0]["storage_key"]))
		filename := sanitizeMediaFilename(fmt.Sprint(ready[0]["filename"]))
		mime := strings.TrimSpace(fmt.Sprint(ready[0]["mime"]))
		playAudio := inboxFileIsAudio(ready[0], kind)
		abs, err := a.mediaAbsPath(key)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "файл не найден"})
			return
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "файл не найден"})
			return
		}
		if playAudio {
			mime = audioMIME(mime, filename)
			if filename == "" || filename == "file.bin" {
				filename = "voice.ogg"
			}
		} else if mime == "" {
			mime = "application/octet-stream"
		}
		if filename == "" || filename == "file.bin" {
			filename = "file.bin"
		}
		w.Header().Set("Content-Type", mime)
		disp := "attachment"
		if playAudio {
			disp = "inline"
		}
		w.Header().Set("Content-Disposition", disp+`; filename="`+filename+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(200)
		_, _ = w.Write(data)
		return
	}

	// Несколько файлов — zip.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	used := map[string]int{}
	for _, f := range ready {
		key := strings.TrimSpace(fmt.Sprint(f["storage_key"]))
		name := sanitizeMediaFilename(fmt.Sprint(f["filename"]))
		if name == "" || name == "file.bin" {
			name = "file.bin"
		}
		if n := used[name]; n > 0 {
			ext := filepath.Ext(name)
			base := strings.TrimSuffix(name, ext)
			name = fmt.Sprintf("%s_%d%s", base, n+1, ext)
		}
		used[fmt.Sprint(f["filename"])]++
		abs, err := a.mediaAbsPath(key)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		fw, err := zw.Create(name)
		if err != nil {
			continue
		}
		_, _ = fw.Write(data)
	}
	if err := zw.Close(); err != nil {
		writeJSON(w, 500, map[string]string{"error": "zip"})
		return
	}
	if buf.Len() == 0 {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="attachments.zip"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(buf.Bytes())
}

func (a *App) sendInboxMediaToLogist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, _ = a.db.Exec(r.Context(), `
		ALTER TABLE inbox_messages ADD COLUMN IF NOT EXISTS media_sent_to_logist_at TIMESTAMPTZ`)

	var kind, storage, filename, mime, wappiID, body string
	var threadID, contactID, dealID, lineProfile string
	var alreadySent bool
	err := a.db.QueryRow(r.Context(), `
		SELECT coalesce(m.media_kind,''), coalesce(m.media_storage_key,''),
		       coalesce(m.media_filename,''), coalesce(m.media_mime,''),
		       coalesce(m.media_wappi_id,''), coalesce(m.body,''),
		       m.thread_id::text,
		       coalesce(t.contact_id::text,''), coalesce(t.deal_id::text,''),
		       coalesce(l.external_ref,''),
		       (m.media_sent_to_logist_at IS NOT NULL)
		FROM inbox_messages m
		JOIN inbox_threads t ON t.id=m.thread_id
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE m.id=$1::uuid`, id).Scan(
		&kind, &storage, &filename, &mime, &wappiID, &body, &threadID, &contactID, &dealID, &lineProfile, &alreadySent)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "сообщение не найдено"})
		return
	}
	passportText := looksLikePassportText(body)
	if kind == "" && storage == "" && wappiID == "" && !passportText {
		writeJSON(w, 400, map[string]string{"error": "в сообщении нет файла или паспортных данных"})
		return
	}

	a.ensureLogisticsSchema(r.Context())
	if dealID == "" && contactID != "" {
		_ = a.db.QueryRow(r.Context(), `
			SELECT id::text FROM deals WHERE contact_id=$1::uuid
			ORDER BY created_at DESC LIMIT 1`, contactID).Scan(&dealID)
	}
	if dealID == "" {
		writeJSON(w, 400, map[string]string{"error": "не найдена сделка для передачи логисту"})
		return
	}
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), dealID)) {
		return
	}

	actor := actorID(r)
	// Файл только отмечается отправленным. Стадия «Билет» сдвигается отдельно,
	// когда файл уже есть и билет заполнен.
	_, _ = a.db.Exec(r.Context(), `
		UPDATE inbox_messages SET media_sent_to_logist_at=COALESCE(media_sent_to_logist_at, now())
		WHERE id=$1::uuid`, id)

	var fileID string
	if alreadySent {
		_ = a.db.QueryRow(r.Context(), `
			SELECT id::text FROM deal_ticket_files
			WHERE source_message_id=$1::uuid ORDER BY created_at DESC LIMIT 1`, id).Scan(&fileID)
	}

	queued := fileID == "" && storage == "" && !passportText
	if fileID == "" && passportText && storage == "" && wappiID == "" {
		var copyErr error
		fileID, copyErr = a.copyPassportTextToTicketDoc(r.Context(), dealID, id, body, actor)
		if copyErr != nil {
			log.Printf("send-to-logist passport text msg=%s: %v", id, copyErr)
		}
		queued = false
	}
	if fileID == "" && storage != "" {
		var copyErr error
		fileID, copyErr = a.copyInboxMediaToTicketDoc(r.Context(), dealID, id, storage, filename, mime, actor)
		if copyErr != nil {
			log.Printf("send-to-logist sync copy msg=%s: %v", id, copyErr)
			queued = true
		}
	}
	if fileID == "" && (storage != "" || wappiID != "" || kind != "") {
		// Фон: докачать из Wappi при необходимости и положить в «Заявку».
		go a.finishSendInboxMediaToLogist(id, dealID, threadID, kind, filename, mime, wappiID, lineProfile, actor)
		queued = true
	}

	a.logDealLeadAct(r.Context(), dealID, actor, "doc_sent_to_logist", map[string]any{
		"message_id":    id,
		"file_id":       fileID,
		"filename":      filename,
		"queued":        queued || fileID == "",
		"passport_text": passportText && storage == "",
	})
	writeJSON(w, 200, map[string]any{
		"ok":             true,
		"deal_id":        dealID,
		"file_id":        fileID,
		"sent_to_logist": true,
		"queued":         queued || fileID == "",
	})
}

func (a *App) finishSendInboxMediaToLogist(msgID, dealID, threadID, kind, filename, mime, wappiID, profile, actor string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	storage := ""
	for attempt := 0; attempt < 8; attempt++ {
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(media_storage_key,''), coalesce(media_filename,''), coalesce(media_mime,''), coalesce(media_kind,'')
			FROM inbox_messages WHERE id=$1::uuid`, msgID).Scan(&storage, &filename, &mime, &kind)
		if storage != "" {
			break
		}
		if wappiID == "" || !a.cfg.WappiReady() {
			break
		}
		ev := inboxEv{
			MediaKind:  kind,
			MediaMsgID: wappiID,
			Profile:    profile,
			FileName:   filename,
			Mime:       mime,
		}
		if ev.MediaKind == "" {
			ev.MediaKind = "document"
		}
		if err := a.fetchAndStoreInboxMedia(ctx, msgID, threadID, ev); err != nil {
			log.Printf("send-to-logist fetch msg=%s attempt=%d: %v", msgID, attempt+1, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(3+attempt*2) * time.Second):
			}
			continue
		}
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(media_storage_key,''), coalesce(media_filename,''), coalesce(media_mime,'')
			FROM inbox_messages WHERE id=$1::uuid`, msgID).Scan(&storage, &filename, &mime)
		if storage != "" {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(3+attempt*2) * time.Second):
		}
	}
	if storage == "" {
		log.Printf("send-to-logist: файл так и не догрузился msg=%s deal=%s", msgID, dealID)
		return
	}
	fileID, err := a.copyInboxMediaToTicketDoc(ctx, dealID, msgID, storage, filename, mime, actor)
	if err != nil {
		log.Printf("send-to-logist copy msg=%s: %v", msgID, err)
		return
	}
	a.logDealLeadAct(ctx, dealID, actor, "doc_ready_for_logist", map[string]any{
		"message_id": msgID, "file_id": fileID, "filename": filename,
	})
	a.broadcastDealUpdated(ctx, dealID, map[string]any{"ticket_file": fileID})
}

// logistChatDocWanted — в карусель логиста: входящие фото и документы, без голоса, стикеров и видео.
func logistChatDocWanted(kind, mime, filename string) bool {
	switch normalizeMediaKind(kind) {
	case "audio", "sticker", "video":
		return false
	case "image", "document":
		return true
	}
	switch mediaKindFromContentType(mime, filename) {
	case "image", "document":
		return true
	default:
		return false
	}
}

func (a *App) copyInboxMediaToTicketDoc(ctx context.Context, dealID, msgID, storage, filename, mime, actor string) (string, error) {
	var existing string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM deal_ticket_files
		WHERE source_message_id=$1::uuid ORDER BY created_at DESC LIMIT 1`, msgID).Scan(&existing)
	if existing != "" {
		return existing, nil
	}
	root, err := filepath.Abs(a.inboxMediaRoot())
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Join(a.inboxMediaRoot(), filepath.FromSlash(storage)))
	if err != nil || !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("файл вложения не найден")
	}
	raw, err := os.ReadFile(abs)
	if err != nil || len(raw) == 0 {
		return "", fmt.Errorf("файл вложения не найден на диске")
	}
	_ = os.MkdirAll(filepath.Join(ticketFilesRoot(), dealID), 0o750)
	safeName := sanitizeMediaFilename(filename)
	if safeName == "" || safeName == "." {
		safeName = filepath.Base(storage)
	}
	if safeName == "" || safeName == "." {
		safeName = "document.bin"
	}
	key := dealID + "/" + fmt.Sprintf("%d_%s", time.Now().UnixNano(), safeName)
	dest := filepath.Join(ticketFilesRoot(), key)
	if err := os.WriteFile(dest, raw, 0o640); err != nil {
		return "", err
	}
	var fileID string
	err = a.db.QueryRow(ctx, `
		INSERT INTO deal_ticket_files (deal_id, kind, filename, mime, size_bytes, storage_key, source, source_message_id, uploaded_by)
		VALUES ($1::uuid,'doc',$2,$3,$4,$5,'inbox',$6::uuid,nullif($7,'')::uuid)
		RETURNING id::text`,
		dealID, safeName, mime, len(raw), key, msgID, actor).Scan(&fileID)
	if err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return fileID, nil
}

// copyChatFileToTicketDoc копирует уже скачанный файл переписки на карточку.
// Повтор того же сообщения и имени не создаёт вторую копию.
func (a *App) copyChatFileToTicketDoc(ctx context.Context, dealID, msgID, storage, filename, mime, actor string) (string, bool, error) {
	storage = strings.TrimSpace(storage)
	if storage == "" || !a.mediaFileExists(storage) {
		return "", false, nil
	}
	safeName := sanitizeMediaFilename(filename)
	if safeName == "" || safeName == "." || safeName == "file.bin" {
		base := sanitizeMediaFilename(filepath.Base(storage))
		if base != "" && base != "." && base != "file.bin" {
			safeName = base
		}
	}
	var existing string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM deal_ticket_files
		WHERE deal_id=$1::uuid AND source_message_id=$2::uuid AND filename=$3
		LIMIT 1`, dealID, msgID, safeName).Scan(&existing)
	if existing != "" {
		return existing, false, nil
	}
	abs, err := a.mediaAbsPath(storage)
	if err != nil {
		return "", false, err
	}
	raw, err := os.ReadFile(abs)
	if err != nil || len(raw) == 0 {
		return "", false, fmt.Errorf("файл вложения не найден на диске")
	}
	if err := os.MkdirAll(filepath.Join(ticketFilesRoot(), dealID), 0o750); err != nil {
		return "", false, err
	}
	key := dealID + "/" + fmt.Sprintf("%d_%s", time.Now().UnixNano(), safeName)
	dest := filepath.Join(ticketFilesRoot(), key)
	if err := os.WriteFile(dest, raw, 0o640); err != nil {
		return "", false, err
	}
	var fileID string
	err = a.db.QueryRow(ctx, `
		INSERT INTO deal_ticket_files (deal_id, kind, filename, mime, size_bytes, storage_key, source, source_message_id, uploaded_by)
		VALUES ($1::uuid,'doc',$2,$3,$4,$5,'inbox',$6::uuid,nullif($7,'')::uuid)
		RETURNING id::text`,
		dealID, safeName, mime, len(raw), key, msgID, actor).Scan(&fileID)
	if err != nil {
		_ = os.Remove(dest)
		return "", false, err
	}
	return fileID, true, nil
}

func (a *App) copyPassportTextToTicketDoc(ctx context.Context, dealID, msgID, body, actor string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("пустой текст")
	}
	var existing string
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM deal_ticket_files
		WHERE source_message_id=$1::uuid ORDER BY created_at DESC LIMIT 1`, msgID).Scan(&existing)
	if existing != "" {
		return existing, nil
	}
	_ = os.MkdirAll(filepath.Join(ticketFilesRoot(), dealID), 0o750)
	safeName := "passport_data.txt"
	key := dealID + "/" + fmt.Sprintf("%d_%s", time.Now().UnixNano(), safeName)
	dest := filepath.Join(ticketFilesRoot(), key)
	raw := []byte(body)
	if err := os.WriteFile(dest, raw, 0o640); err != nil {
		return "", err
	}
	var fileID string
	err := a.db.QueryRow(ctx, `
		INSERT INTO deal_ticket_files (deal_id, kind, filename, mime, size_bytes, storage_key, source, source_message_id, uploaded_by)
		VALUES ($1::uuid,'passport',$2,'text/plain',$3,$4,'inbox',$5::uuid,nullif($6,'')::uuid)
		RETURNING id::text`,
		dealID, safeName, len(raw), key, msgID, actor).Scan(&fileID)
	if err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return fileID, nil
}

type dealChatDoc struct {
	msgID, threadID, kind, filename, mime, storage, wappi, profile string
}

// pullDealChatDocs копирует входящие фото и документы переписки на карточку.
// Уже лежащие файлы копируются сразу. Если файл ещё не скачан, одна попытка в общем лимите времени.
// Стадию и ответственного не меняет.
func (a *App) pullDealChatDocs(ctx context.Context, dealID, actor string) int {
	dealID = strings.TrimSpace(dealID)
	if dealID == "" {
		return 0
	}
	rows, err := a.db.Query(ctx, `
		SELECT m.id::text, m.thread_id::text,
		       coalesce(m.media_kind,''), coalesce(m.media_filename,''), coalesce(m.media_mime,''),
		       coalesce(m.media_storage_key,''), coalesce(m.media_wappi_id,''),
		       coalesce(l.external_ref,'')
		FROM deals d
		LEFT JOIN contacts c ON c.id=d.contact_id
		JOIN inbox_threads t ON (
			t.deal_id=d.id
			OR (d.contact_id IS NOT NULL AND t.contact_id=d.contact_id)
			OR (
				length(right(regexp_replace(coalesce(c.phone,''), '[^0-9]', '', 'g'), 10)) >= 10
				AND right(regexp_replace(coalesce(t.phone,''), '[^0-9]', '', 'g'), 10)
					= right(regexp_replace(coalesce(c.phone,''), '[^0-9]', '', 'g'), 10)
			)
		)
		JOIN inbox_messages m ON m.thread_id=t.id AND m.direction='inbound'
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE d.id=$1::uuid
		ORDER BY m.created_at
		LIMIT 80`, dealID)
	if err != nil {
		log.Printf("pull chat docs deal=%s: %v", dealID, err)
		return 0
	}
	defer rows.Close()
	var msgs []dealChatDoc
	for rows.Next() {
		var item dealChatDoc
		if rows.Scan(&item.msgID, &item.threadID, &item.kind, &item.filename, &item.mime, &item.storage, &item.wappi, &item.profile) != nil {
			continue
		}
		msgs = append(msgs, item)
	}
	if err := rows.Err(); err != nil {
		return 0
	}

	started := time.Now()
	copied := 0
	const maxCopies = 40
	fetched := map[string]string{}
	for _, msg := range msgs {
		if copied >= maxCopies || ctx.Err() != nil {
			break
		}
		for _, part := range a.chatDocParts(ctx, msg) {
			if copied >= maxCopies || ctx.Err() != nil {
				break
			}
			if !logistChatDocWanted(part.kind, part.mime, part.filename) {
				continue
			}
			storage := strings.TrimSpace(part.storage)
			if storage == "" || !a.mediaFileExists(storage) {
				storage = a.fetchChatDocOnce(ctx, msg, started, fetched)
			}
			if storage == "" {
				continue
			}
			name := part.filename
			mime := part.mime
			if name == "" {
				name = msg.filename
			}
			if mime == "" {
				mime = msg.mime
			}
			_, fresh, err := a.copyChatFileToTicketDoc(ctx, dealID, msg.msgID, storage, name, mime, actor)
			if err != nil {
				log.Printf("pull chat docs copy deal=%s msg=%s: %v", dealID, msg.msgID, err)
				continue
			}
			if fresh {
				copied++
			}
		}
	}
	return copied
}

func (a *App) chatDocParts(ctx context.Context, msg dealChatDoc) []dealChatDoc {
	rows, err := a.db.Query(ctx, `
		SELECT coalesce(kind,''), coalesce(filename,''), coalesce(mime,''), coalesce(storage_key,'')
		FROM inbox_message_files
		WHERE message_id=$1::uuid
		ORDER BY sort_order, created_at`, msg.msgID)
	if err != nil {
		return []dealChatDoc{msg}
	}
	defer rows.Close()
	var parts []dealChatDoc
	for rows.Next() {
		part := msg
		var storage string
		if rows.Scan(&part.kind, &part.filename, &part.mime, &storage) != nil {
			continue
		}
		if strings.TrimSpace(storage) == "" {
			continue
		}
		part.storage = storage
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return []dealChatDoc{msg}
	}
	return parts
}

func (a *App) fetchChatDocOnce(ctx context.Context, msg dealChatDoc, started time.Time, fetched map[string]string) string {
	if storage, ok := fetched[msg.msgID]; ok {
		return storage
	}
	if a.mediaFileExists(msg.storage) {
		fetched[msg.msgID] = msg.storage
		return msg.storage
	}
	if msg.wappi == "" || !a.cfg.WappiReady() || time.Since(started) > 18*time.Second {
		fetched[msg.msgID] = ""
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	_ = a.fetchAndStoreInboxMedia(cctx, msg.msgID, msg.threadID, inboxEv{
		MediaKind:  msg.kind,
		MediaMsgID: msg.wappi,
		FileName:   msg.filename,
		Mime:       msg.mime,
		Profile:    msg.profile,
	})
	var storage string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(media_storage_key,'') FROM inbox_messages WHERE id=$1::uuid`, msg.msgID).Scan(&storage)
	if !a.mediaFileExists(storage) {
		storage = ""
	}
	fetched[msg.msgID] = storage
	return storage
}
