package server

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func ticketFilesRoot() string {
	root, _ := os.Getwd()
	return filepath.Join(root, "data", "ticket-files")
}

func (a *App) ensureLogisticsSchema(ctx context.Context) {
	a.logisticsSchemaOnce.Do(func() {
		a.ensureLogisticsSchemaRun(ctx)
	})
}

func (a *App) ensureLogisticsSchemaRun(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_needed BOOLEAN NOT NULL DEFAULT false`)
	_, _ = a.db.Exec(ctx, `ALTER TABLE deals ADD COLUMN IF NOT EXISTS visual_only BOOLEAN NOT NULL DEFAULT false`)
	_, _ = a.db.Exec(ctx, `ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_assigned_to UUID`)
	_, _ = a.db.Exec(ctx, `ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_needed_at TIMESTAMPTZ`)
	_, _ = a.db.Exec(ctx, `ALTER TABLE deals ADD COLUMN IF NOT EXISTS sleeping BOOLEAN NOT NULL DEFAULT false`)
	// Колонка менеджера после «Билет». Стадия логиста (stage_id) при этом не меняется.
	_, _ = a.db.Exec(ctx, `ALTER TABLE deals ADD COLUMN IF NOT EXISTS manager_stage_id UUID`)
	_, _ = a.db.Exec(ctx, `
		UPDATE deals d SET sleeping=true
		FROM pipelines p
		WHERE d.pipeline_id=p.id
		  AND coalesce(p.bitrix_id,'')='`+oldLeadsPipelineKey+`'
		  AND NOT d.sleeping`)
	_, _ = a.db.Exec(ctx, `ALTER TABLE inbox_messages ADD COLUMN IF NOT EXISTS media_sent_to_logist_at TIMESTAMPTZ`)
	_, _ = a.db.Exec(ctx, `ALTER TABLE deal_tickets ADD COLUMN IF NOT EXISTS from_city TEXT NOT NULL DEFAULT ''`)
	_, _ = a.db.Exec(ctx, `ALTER TABLE deal_tickets ADD COLUMN IF NOT EXISTS to_city TEXT NOT NULL DEFAULT ''`)
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_tickets (
			deal_id UUID PRIMARY KEY REFERENCES deals(id) ON DELETE CASCADE,
			transport TEXT NOT NULL DEFAULT '',
			flight_or_train TEXT NOT NULL DEFAULT '',
			depart_at TIMESTAMPTZ,
			arrive_at TIMESTAMPTZ,
			route TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			wagon TEXT NOT NULL DEFAULT '',
			seat TEXT NOT NULL DEFAULT '',
			amount NUMERIC(12,2),
			updated_by UUID,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_ticket_files (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
			kind TEXT NOT NULL DEFAULT 'ticket',
			filename TEXT NOT NULL DEFAULT '',
			mime TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			storage_key TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'upload',
			source_message_id UUID,
			uploaded_by UUID,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_night_notes (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
			author_id UUID,
			body TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	_, _ = a.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS deal_night_notes_deal_idx ON deal_night_notes (deal_id, created_at)`)
	_, _ = a.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS deal_ticket_files_deal_idx ON deal_ticket_files (deal_id)`)
	_, _ = a.db.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS deal_tickets_filled_idx ON deal_tickets (deal_id)
		WHERE coalesce(from_city,'') <> ''
		  AND coalesce(to_city,'') <> ''
		  AND coalesce(transport,'') <> ''
		  AND depart_at IS NOT NULL`)
	// Имена в окне ночного логиста. Пустая строка значит «брать с карточки».
	_, _ = a.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deal_night_briefs (
			deal_id UUID PRIMARY KEY REFERENCES deals(id) ON DELETE CASCADE,
			recruiter TEXT NOT NULL DEFAULT '',
			candidate TEXT NOT NULL DEFAULT '',
			rop TEXT NOT NULL DEFAULT '',
			updated_by UUID,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
}

func canSeeTicketSensitive(role string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	return r == "admin" || r == "rop" || isLogistRole(r)
}

// canDownloadProtectedDocs — паспорт/документы из чата: только admin и логист (не ROP, не менеджер).
func canDownloadProtectedDocs(role string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	return r == "admin" || isLogistRole(r)
}

// canSeeLogistTicketFiles — билет/фото от логиста: только admin и логист.
// Менеджер и РОП — никогда (не видят и не скачивают).
func canSeeLogistTicketFiles(role string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	return r == "admin" || isLogistRole(r)
}

// ticketFileKindSensitive — паспорт/документы из чата: только логист/admin.
func ticketFileKindSensitive(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "doc", "passport", "document", "id":
		return true
	default:
		return false
	}
}

func filterTicketFilesForRole(files []map[string]any, role string) []map[string]any {
	if files == nil {
		return []map[string]any{}
	}
	role = strings.ToLower(strings.TrimSpace(role))
	// Менеджер не видит ничего из файлов логиста (билет, фото, паспорт).
	if !canSeeLogistTicketFiles(role) {
		return []map[string]any{}
	}
	canDocs := canDownloadProtectedDocs(role)
	if canDocs {
		return files
	}
	out := make([]map[string]any, 0, len(files))
	for _, f := range files {
		kind, _ := f["kind"].(string)
		if ticketFileKindSensitive(kind) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func countSensitiveTicketFiles(files []map[string]any) int {
	n := 0
	for _, f := range files {
		kind, _ := f["kind"].(string)
		if ticketFileKindSensitive(kind) {
			n++
		}
	}
	return n
}

func (a *App) escortStageID(ctx context.Context, stageKey string) (pipeID, stageID string) {
	a.ensureRecruitFunnels(ctx)
	_ = a.db.QueryRow(ctx, `
		SELECT p.id::text, s.id::text
		FROM pipelines p
		JOIN pipeline_stages s ON s.pipeline_id=p.id
		WHERE p.bitrix_id=$1 AND s.bitrix_id=$2`,
		escortPipelineKey, escortPipelineKey+":"+stageKey).Scan(&pipeID, &stageID)
	return
}

func (a *App) hireStageID(ctx context.Context, stageKey string) (pipeID, stageID string) {
	a.ensureRecruitFunnels(ctx)
	_ = a.db.QueryRow(ctx, `
		SELECT p.id::text, s.id::text
		FROM pipelines p
		JOIN pipeline_stages s ON s.pipeline_id=p.id
		WHERE p.bitrix_id=$1 AND s.bitrix_id=$2`,
		hirePipelineKey, hirePipelineKey+":"+stageKey).Scan(&pipeID, &stageID)
	return
}

// markDealSentToLogist — менеджер остаётся на «Адаптация → Билет», логист видит как «Заявка».
func (a *App) markDealSentToLogist(ctx context.Context, dealID string) (pipeID, stageID string, err error) {
	a.ensureLogisticsSchema(ctx)
	if a.dealIsSleeping(ctx, dealID) || a.dealOnOldLeadsPipeline(ctx, dealID) {
		pipeID, stageID = a.oldLeadsStageID(ctx, "bilet")
		if pipeID == "" || stageID == "" {
			return "", "", fmt.Errorf("нет стадии «Билет»")
		}
		tag, execErr := a.db.Exec(ctx, `
			UPDATE deals
			SET logistics_needed=true,
			    logistics_needed_at=COALESCE(logistics_needed_at, now()),
			    sleeping=true,
			    pipeline_id=$2::uuid,
			    stage_id=$3::uuid
			WHERE id=$1::uuid`, dealID, pipeID, stageID)
		if execErr != nil {
			return "", "", execErr
		}
		if tag.RowsAffected() == 0 {
			return "", "", fmt.Errorf("сделка не найдена")
		}
		go a.notifyLogistsDealToBilet(context.Background(), dealID)
		return pipeID, stageID, nil
	}
	pipeID, stageID = a.hireStageID(ctx, "bilet")
	if pipeID == "" || stageID == "" {
		return "", "", fmt.Errorf("нет стадии «Билет»")
	}
	tag, execErr := a.db.Exec(ctx, `
		UPDATE deals
		SET logistics_needed=true,
		    logistics_needed_at=COALESCE(logistics_needed_at, now()),
		    pipeline_id=$2::uuid,
		    stage_id=$3::uuid
		WHERE id=$1::uuid`, dealID, pipeID, stageID)
	if execErr != nil {
		return "", "", execErr
	}
	if tag.RowsAffected() == 0 {
		return "", "", fmt.Errorf("сделка не найдена")
	}
	go a.notifyLogistsDealToBilet(context.Background(), dealID)
	return pipeID, stageID, nil
}

const errBiletFileRequired = "Вы не можете перейти в стадию «Билет». Сначала отправьте файл или напишите комментарий, почему файла нет."
const errBiletCitiesRequired = "Сначала заполните билет."

// dealHasBiletMaterial — файл уже отправлен логисту в переписке или прикреплён к карточке.
func (a *App) dealHasBiletMaterial(ctx context.Context, dealID string) bool {
	dealID = strings.TrimSpace(dealID)
	if dealID == "" {
		return false
	}
	a.ensureLogisticsSchema(ctx)
	var ok bool
	err := a.db.QueryRow(ctx, `
		SELECT
			EXISTS (
				SELECT 1 FROM deal_ticket_files f WHERE f.deal_id=$1::uuid
			)
			OR EXISTS (
				SELECT 1
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
				JOIN inbox_messages m ON m.thread_id=t.id
				WHERE d.id=$1::uuid
				  AND m.media_sent_to_logist_at IS NOT NULL
			)`, dealID).Scan(&ok)
	return err == nil && ok
}

func (a *App) biletFileRequired(ctx context.Context, dealID, targetStageID string) bool {
	if !a.isHireBiletStage(ctx, targetStageID) && !a.isOldLeadsBiletStage(ctx, targetStageID) {
		return false
	}
	var cur string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(stage_id::text,'') FROM deals WHERE id=$1::uuid`, dealID).Scan(&cur)
	if cur == targetStageID {
		return false
	}
	return !a.dealHasBiletMaterial(ctx, dealID)
}

func (a *App) dealHasBiletCities(ctx context.Context, dealID string) bool {
	return a.dealHasFilledBilet(ctx, dealID)
}

func (a *App) dealHasFilledBilet(ctx context.Context, dealID string) bool {
	a.ensureLogisticsSchema(ctx)
	var ok bool
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(from_city,'') <> ''
		   AND coalesce(to_city,'') <> ''
		   AND coalesce(transport,'') <> ''
		   AND depart_at IS NOT NULL
		FROM deal_tickets WHERE deal_id=$1::uuid`, dealID).Scan(&ok)
	return err == nil && ok
}

func (a *App) stageNeedsFilledBilet(ctx context.Context, stageID string) bool {
	stageID = strings.TrimSpace(stageID)
	if stageID == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipeline_stages WHERE id=$1::uuid`, stageID).Scan(&key)
	return stageNeedsFilledBiletKey(key)
}

func (a *App) biletCitiesRequired(ctx context.Context, dealID, targetStageID string) bool {
	if !a.stageNeedsFilledBilet(ctx, targetStageID) {
		return false
	}
	var cur string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(stage_id::text,'') FROM deals WHERE id=$1::uuid`, dealID).Scan(&cur)
	if cur == targetStageID {
		return false
	}
	return !a.dealHasBiletCities(ctx, dealID)
}

func (a *App) biletGate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	stageID := strings.TrimSpace(r.URL.Query().Get("stage_id"))
	if stageID == "" {
		_, stageID = a.hireStageID(r.Context(), "bilet")
	}
	bilet := a.stageNeedsFilledBilet(r.Context(), stageID)
	var cur string
	_ = a.db.QueryRow(r.Context(), `SELECT coalesce(stage_id::text,'') FROM deals WHERE id=$1::uuid`, id).Scan(&cur)
	if cur == stageID {
		bilet = false
	}
	needTicket := bilet && !a.dealHasFilledBilet(r.Context(), id)
	writeJSON(w, 200, map[string]any{
		"required":    false,
		"need_ticket": needTicket,
		"need_cities": needTicket,
		"bilet":       bilet,
	})
}

func biletDocRoleOK(role string) bool {
	switch role {
	case "admin", "rop", "logist", "night_logist", "manager", "sales":
		return true
	default:
		return false
	}
}

func (a *App) getBiletDocs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if !biletDocRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нельзя смотреть документы"})
		return
	}
	a.ensureLogisticsSchema(r.Context())
	writeJSON(w, 200, map[string]any{"files": a.listBiletDocFiles(r.Context(), id, canBrowseDealDocs(roleName(r)))})
}

func canBrowseDealDocs(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "logist", "night_logist", "admin":
		return true
	default:
		return false
	}
}

func (a *App) listBiletDocFiles(ctx context.Context, dealID string, includeChat bool) []map[string]any {
	files := a.queryMaps(ctx, `
		SELECT id::text, kind, filename, coalesce(mime,'') AS mime, size_bytes, created_at
		FROM deal_ticket_files
		WHERE deal_id=$1::uuid AND (
			source='bilet'
			OR ($2::bool AND source='inbox' AND coalesce(kind,'')='doc')
		)
		ORDER BY created_at`, dealID, includeChat)
	if files == nil {
		return []map[string]any{}
	}
	return files
}

// pullBiletDocsFromChat — логист открыл «Документы»: забрать фото и файлы из переписки на карточку.
func (a *App) pullBiletDocsFromChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !canBrowseDealDocs(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "документы из переписки смотрит логист"})
		return
	}
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	a.ensureLogisticsSchema(r.Context())
	a.ensureInboxMediaFilesSchema(r.Context())
	copied := a.pullDealChatDocs(r.Context(), id, actorID(r))
	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"copied": copied,
		"files":  a.listBiletDocFiles(r.Context(), id, true),
	})
}

func (a *App) downloadBiletDocsArchive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !canBrowseDealDocs(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "скачивает логист"})
		return
	}
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	a.ensureLogisticsSchema(r.Context())
	rows, err := a.db.Query(r.Context(), `
		SELECT filename, coalesce(mime,''), storage_key
		FROM deal_ticket_files
		WHERE deal_id=$1::uuid
		  AND coalesce(kind,'') <> 'note'
		  AND storage_key <> ''
		  AND (
			source='bilet'
			OR (source='inbox' AND coalesce(kind,'')='doc')
		  )
		ORDER BY created_at`, id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось собрать файлы"})
		return
	}
	defer rows.Close()
	root, rerr := filepath.Abs(ticketFilesRoot())
	if rerr != nil {
		writeJSON(w, 500, map[string]string{"error": "хранилище"})
		return
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	used := map[string]int{}
	n := 0
	for rows.Next() {
		var filename, mime, key string
		if rows.Scan(&filename, &mime, &key) != nil {
			continue
		}
		abs, aerr := filepath.Abs(filepath.Join(ticketFilesRoot(), filepath.FromSlash(key)))
		if aerr != nil || !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			continue
		}
		data, rerr := os.ReadFile(abs)
		if rerr != nil || len(data) == 0 {
			continue
		}
		name := sanitizeMediaFilename(filename)
		if name == "" || name == "." || name == "file.bin" {
			name = fmt.Sprintf("file-%d", n+1)
		}
		if c := used[name]; c > 0 {
			ext := filepath.Ext(name)
			base := strings.TrimSuffix(name, ext)
			name = fmt.Sprintf("%s-%d%s", base, c+1, ext)
		}
		used[sanitizeMediaFilename(filename)]++
		entry, err := zw.Create(name)
		if err != nil {
			continue
		}
		if _, err := entry.Write(data); err != nil {
			continue
		}
		n++
		_ = mime
	}
	if err := zw.Close(); err != nil || n == 0 {
		writeJSON(w, 404, map[string]string{"error": "файлов нет"})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="dokumenty.zip"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(buf.Bytes())
}

func (a *App) postBiletDoc(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if isLogist(r) {
		writeJSON(w, 403, map[string]string{"error": "логист не добавляет документы"})
		return
	}
	if !biletDocRoleOK(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "нельзя отправить файл"})
		return
	}
	a.ensureLogisticsSchema(r.Context())
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "файл"})
		return
	}
	headers := []*multipart.FileHeader{}
	if r.MultipartForm != nil {
		headers = r.MultipartForm.File["file"]
	}
	note := strings.TrimSpace(r.FormValue("note"))
	if len([]rune(note)) > 2000 {
		note = string([]rune(note)[:2000])
	}
	if len(headers) == 0 {
		if note == "" {
			writeJSON(w, 400, map[string]string{"error": "выберите файлы или напишите комментарий, почему файла нет"})
			return
		}
		var noteID string
		err := a.db.QueryRow(r.Context(), `
			INSERT INTO deal_ticket_files (deal_id, kind, filename, mime, size_bytes, storage_key, source, uploaded_by)
			VALUES ($1::uuid,'note',$2,'text/plain',$3,'','bilet',nullif($4,'')::uuid)
			RETURNING id::text`,
			id, note, len(note), actorID(r)).Scan(&noteID)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить комментарий"})
			return
		}
		a.logDealLeadAct(r.Context(), id, actorID(r), "bilet_doc_note", map[string]any{
			"note_id": noteID,
			"note":    note,
		})
		writeJSON(w, 201, map[string]any{"ok": true, "files": []map[string]any{}, "notes": []map[string]any{{"id": noteID, "filename": note, "kind": "note"}}})
		return
	}
	if len(headers) > 10 {
		writeJSON(w, 400, map[string]string{"error": "за раз можно добавить не больше 10 файлов"})
		return
	}
	_ = os.MkdirAll(filepath.Join(ticketFilesRoot(), id), 0o750)
	saved := make([]map[string]any, 0, len(headers))
	var failed []string
	for _, hdr := range headers {
		file, err := hdr.Open()
		if err != nil {
			failed = append(failed, hdr.Filename)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(file, 32<<20))
		file.Close()
		if err != nil || len(data) == 0 {
			failed = append(failed, hdr.Filename)
			continue
		}
		name := filepath.Base(hdr.Filename)
		if name == "" || name == "." || name == "/" {
			name = "document.bin"
		}
		key := id + "/" + fmt.Sprintf("%d_%s", time.Now().UnixNano(), name)
		path := filepath.Join(ticketFilesRoot(), key)
		if err := os.WriteFile(path, data, 0o640); err != nil {
			failed = append(failed, name)
			continue
		}
		mime := hdr.Header.Get("Content-Type")
		var fileID string
		err = a.db.QueryRow(r.Context(), `
			INSERT INTO deal_ticket_files (deal_id, kind, filename, mime, size_bytes, storage_key, source, uploaded_by)
			VALUES ($1::uuid,'doc',$2,$3,$4,$5,'bilet',nullif($6,'')::uuid)
			RETURNING id::text`,
			id, name, mime, len(data), key, actorID(r)).Scan(&fileID)
		if err != nil {
			_ = os.Remove(path)
			failed = append(failed, name)
			continue
		}
		saved = append(saved, map[string]any{"id": fileID, "filename": name})
	}
	if len(saved) == 0 {
		writeJSON(w, 400, map[string]string{"error": "не удалось сохранить файлы"})
		return
	}
	names := make([]string, 0, len(saved))
	ids := make([]string, 0, len(saved))
	for _, f := range saved {
		names = append(names, fmt.Sprint(f["filename"]))
		ids = append(ids, fmt.Sprint(f["id"]))
	}
	a.logDealLeadAct(r.Context(), id, actorID(r), "bilet_doc_sent", map[string]any{
		"file_ids":  ids,
		"filenames": names,
		"note":      note,
	})
	writeJSON(w, 201, map[string]any{"ok": true, "files": saved, "failed": failed})
}

func (a *App) transferDealToLogist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	_, biletStage := a.hireStageID(r.Context(), "bilet")
	if a.biletCitiesRequired(r.Context(), id, biletStage) {
		writeJSON(w, 400, map[string]string{"error": errBiletCitiesRequired})
		return
	}
	pipeID, stageID, err := a.markDealSentToLogist(r.Context(), id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.logDealLeadAct(r.Context(), id, actorID(r), "logistics_transfer", map[string]any{
		"stage":        "bilet",
		"logist_stage": "ticket_request",
	})
	a.broadcastDealUpdated(r.Context(), id, map[string]any{"logistics_needed": true, "stage": "bilet"})
	writeJSON(w, 200, map[string]any{"ok": true, "pipeline_id": pipeID, "stage_id": stageID})
}

func (a *App) getDealTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	a.ensureLogisticsSchema(r.Context())
	role := roleName(r)
	sensitive := canSeeTicketSensitive(role)
	seeFiles, canDocs, tower := a.viewerSeesTicketFiles(r)
	ticket := a.loadDealTicketMap(r.Context(), id, sensitive)
	allFiles := a.queryMaps(r.Context(), `
		SELECT id::text, kind, filename, mime, size_bytes, source, created_at
		FROM deal_ticket_files WHERE deal_id=$1::uuid AND coalesce(kind,'') <> 'note' ORDER BY created_at DESC`, id)
	files := filterTicketFilesForViewer(allFiles, role, tower)
	protected := countSensitiveTicketFiles(allFiles)
	if !seeFiles {
		protected = 0
	}
	writeJSON(w, 200, map[string]any{
		"ticket":            ticket,
		"files":             files,
		"sensitive":         sensitive,
		"protected_docs":    protected,
		"can_see_files":     seeFiles,
		"can_download_docs": canDocs,
	})
}

func (a *App) loadDealTicketMap(ctx context.Context, dealID string, sensitive bool) map[string]any {
	var transport, fromCity, toCity, flight, route, notes, wagon, seat string
	var depart, arrive *time.Time
	var amount *float64
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(transport,''), coalesce(from_city,''), coalesce(to_city,''), coalesce(flight_or_train,''), depart_at, arrive_at,
		       coalesce(route,''), coalesce(notes,''), coalesce(wagon,''), coalesce(seat,''), amount
		FROM deal_tickets WHERE deal_id=$1::uuid`, dealID).Scan(
		&transport, &fromCity, &toCity, &flight, &depart, &arrive, &route, &notes, &wagon, &seat, &amount)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{
		"transport":       transport,
		"from_city":       fromCity,
		"to_city":         toCity,
		"flight_or_train": flight,
		"route":           route,
		"notes":           notes,
	}
	if depart != nil {
		out["depart_at"] = depart.UTC().Format(time.RFC3339)
	}
	if arrive != nil {
		out["arrive_at"] = arrive.UTC().Format(time.RFC3339)
	}
	if amount != nil {
		out["amount"] = *amount
	}
	if sensitive {
		out["wagon"] = wagon
		out["seat"] = seat
	}
	return out
}

func filterTicketFilesForViewer(files []map[string]any, role string, towerManager bool) []map[string]any {
	if towerManager {
		if files == nil {
			return []map[string]any{}
		}
		return files
	}
	return filterTicketFilesForRole(files, role)
}

func (a *App) viewerSeesTicketFiles(r *http.Request) (see bool, docs bool, tower bool) {
	role := roleName(r)
	tower = a.mskTowerManagerSeesFiles(r.Context(), actorID(r), role)
	return canSeeLogistTicketFiles(role) || tower, canDownloadProtectedDocs(role) || tower, tower
}

func (a *App) putDealTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if !canSeeTicketSensitive(roleName(r)) && !isManager(r) {
		writeJSON(w, 403, map[string]string{"error": "редактирует логист"})
		return
	}
	a.ensureLogisticsSchema(r.Context())
	var in struct {
		Transport     string   `json:"transport"`
		FromCity      string   `json:"from_city"`
		ToCity        string   `json:"to_city"`
		FlightOrTrain string   `json:"flight_or_train"`
		DepartAt      string   `json:"depart_at"`
		ArriveAt      string   `json:"arrive_at"`
		Route         string   `json:"route"`
		Notes         string   `json:"notes"`
		Wagon         string   `json:"wagon"`
		Seat          string   `json:"seat"`
		Amount        *float64 `json:"amount"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	var depart, arrive any
	if t := strings.TrimSpace(in.DepartAt); t != "" {
		if tm, err := time.Parse(time.RFC3339, t); err == nil {
			depart = tm
		}
	}
	if t := strings.TrimSpace(in.ArriveAt); t != "" {
		if tm, err := time.Parse(time.RFC3339, t); err == nil {
			arrive = tm
		}
	}
	_, err := a.db.Exec(r.Context(), `
		INSERT INTO deal_tickets (deal_id, transport, from_city, to_city, flight_or_train, depart_at, arrive_at, route, notes, wagon, seat, amount, updated_by, updated_at)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,nullif($13,'')::uuid, now())
		ON CONFLICT (deal_id) DO UPDATE SET
			transport=EXCLUDED.transport,
			from_city=CASE WHEN EXCLUDED.from_city <> '' THEN EXCLUDED.from_city ELSE deal_tickets.from_city END,
			to_city=CASE WHEN EXCLUDED.to_city <> '' THEN EXCLUDED.to_city ELSE deal_tickets.to_city END,
			flight_or_train=CASE WHEN $14::bool THEN deal_tickets.flight_or_train ELSE EXCLUDED.flight_or_train END,
			depart_at=EXCLUDED.depart_at,
			arrive_at=EXCLUDED.arrive_at,
			route=EXCLUDED.route,
			notes=EXCLUDED.notes,
			wagon=CASE WHEN $14::bool THEN deal_tickets.wagon ELSE EXCLUDED.wagon END,
			seat=CASE WHEN $14::bool THEN deal_tickets.seat ELSE EXCLUDED.seat END,
			amount=CASE WHEN $14::bool THEN deal_tickets.amount ELSE EXCLUDED.amount END,
			updated_by=EXCLUDED.updated_by,
			updated_at=now()`,
		id, strings.TrimSpace(in.Transport), strings.TrimSpace(in.FromCity), strings.TrimSpace(in.ToCity), strings.TrimSpace(in.FlightOrTrain),
		depart, arrive, strings.TrimSpace(in.Route), strings.TrimSpace(in.Notes),
		strings.TrimSpace(in.Wagon), strings.TrimSpace(in.Seat), in.Amount, actorID(r), isManager(r))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if !isManager(r) && (strings.TrimSpace(in.Transport) != "" || strings.TrimSpace(in.FlightOrTrain) != "") {
		pipeID, stageID := a.escortStageID(r.Context(), "ticket")
		if pipeID != "" && stageID != "" {
			_, _ = a.db.Exec(r.Context(), `
				UPDATE deals SET pipeline_id=$2::uuid, stage_id=$3::uuid
				WHERE id=$1::uuid AND logistics_needed=true`, id, pipeID, stageID)
		}
	}
	a.logDealLeadAct(r.Context(), id, actorID(r), "ticket_updated", map[string]any{
		"transport": in.Transport,
	})
	writeJSON(w, 200, map[string]any{"ok": true, "ticket": a.loadDealTicketMap(r.Context(), id, true)})
}

func (a *App) uploadDealTicketFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if !canSeeLogistTicketFiles(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "загружает логист"})
		return
	}
	a.ensureLogisticsSchema(r.Context())
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "файл"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "нет file"})
		return
	}
	defer file.Close()
	kind := strings.TrimSpace(r.FormValue("kind"))
	if kind == "" {
		kind = "ticket"
	}
	data, err := io.ReadAll(io.LimitReader(file, 32<<20))
	if err != nil || len(data) == 0 {
		writeJSON(w, 400, map[string]string{"error": "пустое тело"})
		return
	}
	_ = os.MkdirAll(filepath.Join(ticketFilesRoot(), id), 0o750)
	name := filepath.Base(hdr.Filename)
	if name == "" || name == "." {
		name = "ticket.bin"
	}
	key := id + "/" + fmt.Sprintf("%d_%s", time.Now().UnixNano(), name)
	path := filepath.Join(ticketFilesRoot(), key)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		writeJSON(w, 500, map[string]string{"error": "не удалось сохранить"})
		return
	}
	mime := hdr.Header.Get("Content-Type")
	var fileID string
	err = a.db.QueryRow(r.Context(), `
		INSERT INTO deal_ticket_files (deal_id, kind, filename, mime, size_bytes, storage_key, source, uploaded_by)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,'upload',nullif($7,'')::uuid)
		RETURNING id::text`,
		id, kind, name, mime, len(data), key, actorID(r)).Scan(&fileID)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.logDealLeadAct(r.Context(), id, actorID(r), "ticket_file_uploaded", map[string]any{
		"file_id":  fileID,
		"filename": name,
		"kind":     kind,
	})
	if strings.EqualFold(kind, "ticket") {
		a.notifyTicketUploaded(r.Context(), id, name, actorID(r))
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": fileID, "filename": name})
}

func (a *App) getDealTicketFile(w http.ResponseWriter, r *http.Request) {
	dealID := chi.URLParam(r, "id")
	fileID := chi.URLParam(r, "fileId")
	// Чужой отдел не скачивает файлы логиста. Менеджер «МСК Башня» — только просмотр.
	towerFiles := a.mskTowerManagerSeesFiles(r.Context(), actorID(r), roleName(r))
	if !canSeeLogistTicketFiles(roleName(r)) && !towerFiles {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), dealID)) {
		return
	}
	var key, filename, mime, kind string
	err := a.db.QueryRow(r.Context(), `
		SELECT storage_key, filename, coalesce(mime,''), coalesce(kind,'')
		FROM deal_ticket_files WHERE id=$1::uuid AND deal_id=$2::uuid`, fileID, dealID).Scan(&key, &filename, &mime, &kind)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	// Паспорт/документы — admin, логист и менеджер «МСК Башня».
	if ticketFileKindSensitive(kind) && !canDownloadProtectedDocs(roleName(r)) && !towerFiles {
		a.noteSensitiveFile(r, dealID, fileID, kind, false)
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	root, rerr := filepath.Abs(ticketFilesRoot())
	if rerr != nil {
		writeJSON(w, 500, map[string]string{"error": "хранилище"})
		return
	}
	abs, aerr := filepath.Abs(filepath.Join(ticketFilesRoot(), filepath.FromSlash(key)))
	if aerr != nil || !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		writeJSON(w, 404, map[string]string{"error": "файл не найден"})
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "файл на диске"})
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	if ticketFileKindSensitive(kind) {
		a.noteSensitiveFile(r, dealID, fileID, kind, true)
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func (a *App) sendDealTicketToCandidate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Менеджер не работает с файлом билета — отправляет логист/РОП/admin.
	if !canSeeLogistTicketFiles(roleName(r)) {
		writeJSON(w, 403, map[string]string{"error": "билет отправляет логист"})
		return
	}
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	a.ensureLogisticsSchema(r.Context())
	var fileID, filename, storageKey, mime string
	err := a.db.QueryRow(r.Context(), `
		SELECT id::text, filename, storage_key, coalesce(mime,'')
		FROM deal_ticket_files
		WHERE deal_id=$1::uuid AND kind='ticket'
		ORDER BY created_at DESC LIMIT 1`, id).Scan(&fileID, &filename, &storageKey, &mime)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "нет файла билета — сначала загрузите у логиста"})
		return
	}
	var phone, contactID string
	_ = a.db.QueryRow(r.Context(), `
		SELECT coalesce(c.phone,''), coalesce(d.contact_id::text,'')
		FROM deals d
		LEFT JOIN contacts c ON c.id=d.contact_id
		WHERE d.id=$1::uuid`, id).Scan(&phone, &contactID)

	ownerID, ownerName, leadID := a.dealOwnerForTicketSend(r.Context(), id)
	authorLabel := ownerName
	if authorLabel == "" {
		authorLabel = "Менеджер"
	}

	ticket := a.loadDealTicketMap(r.Context(), id, false)
	body := "Вам отправлен билет"
	if t, _ := ticket["transport"].(string); t != "" {
		body += " (" + transportLabel(t) + ")"
	}
	if f, _ := ticket["flight_or_train"].(string); f != "" {
		body += ": " + f
	}
	if d, _ := ticket["depart_at"].(string); d != "" {
		body += "\nОтправление: " + d
	}

	threadID, err := a.ensureInboxThreadForDeal(r.Context(), id, contactID, phone)
	if err != nil || threadID == "" {
		writeJSON(w, 400, map[string]string{"error": "нет диалога с кандидатом (привяжите MAX/телефон)"})
		return
	}

	// Линия менеджера (владельца сделки) — отправка «от его имени».
	lineID := a.wappiLineForLead(r.Context(), leadID, ownerID, "")
	if lineID == "" {
		lineID = a.wappiLineForLead(r.Context(), leadID, actorID(r), "")
	}
	lineProfile := a.wappiLineProfile(r.Context(), lineID)
	var channel, chatID, threadProfile string
	_ = a.db.QueryRow(r.Context(), `
		SELECT t.channel, t.external_id, coalesce(l.external_ref,'')
		FROM inbox_threads t
		LEFT JOIN inbox_lines l ON l.id=t.line_id
		WHERE t.id=$1::uuid`, threadID).Scan(&channel, &chatID, &threadProfile)
	if lineProfile == "" {
		lineProfile = threadProfile
	}
	if lineID != "" {
		_, _ = a.db.Exec(r.Context(), `UPDATE inbox_threads SET line_id=$2::uuid WHERE id=$1::uuid`, threadID, lineID)
	}

	msgID := ""
	if raw, rerr := os.ReadFile(filepath.Join(ticketFilesRoot(), storageKey)); rerr == nil {
		_ = os.MkdirAll(a.inboxMediaRoot(), 0o750)
		mediaKey := "ticket-" + fileID + filepath.Ext(filename)
		_ = os.WriteFile(filepath.Join(a.inboxMediaRoot(), mediaKey), raw, 0o640)
		_ = a.db.QueryRow(r.Context(), `
			INSERT INTO inbox_messages (thread_id, direction, body, author_label, author_id, media_kind, media_storage_key, media_filename, media_mime)
			VALUES ($1::uuid,'outbound',$2,$3,nullif($4,'')::uuid,'file',$5,$6,$7)
			RETURNING id::text`,
			threadID, body, authorLabel, ownerID, mediaKey, filename, mime).Scan(&msgID)
		_, _ = a.db.Exec(r.Context(), `
			UPDATE inbox_threads SET last_body=$2, last_at=now() WHERE id=$1::uuid`, threadID, body)
	}

	sentText, sentFile := false, false
	note := ""
	if channel != "" && (chatID != "" || phone != "") && lineProfile != "" {
		publicURL := a.ticketFilePublicURL(id, fileID)
		if publicURL == "" {
			if _, terr := a.wappiSend(r.Context(), channel, lineProfile, chatID, phone, body+"\n(файл билета сохранён в чате CRM — задайте PUBLIC_BASE_URL для отправки файла)"); terr == nil {
				sentText = true
			}
			note = "PUBLIC_BASE_URL не задан — файл в мессенджер не ушёл, только в CRM"
		} else if !a.publicURLReadyForWappi(publicURL) {
			note = "В CRM сохранено. В MAX файл не ушёл — PUBLIC_BASE_URL не отдаёт файл (локальный API?)"
			if _, terr := a.wappiSend(r.Context(), channel, lineProfile, chatID, phone, body+"\n(файл: "+filename+")"); terr == nil {
				sentText = true
				note = "В CRM сохранено. Текст в MAX ушёл, файл нет (PUBLIC_BASE_URL)"
			}
		} else if _, ferr := a.wappiSendFileURL(r.Context(), channel, lineProfile, chatID, phone, publicURL, filename, body); ferr != nil {
			note = "В CRM сохранено. В MAX не ушло: " + humanizeMessengerErr(ferr)
			if _, terr := a.wappiSend(r.Context(), channel, lineProfile, chatID, phone, body+"\n(файл: "+filename+")"); terr == nil {
				sentText = true
				note = "В CRM сохранено. Файл в MAX не ушёл (" + humanizeMessengerErr(ferr) + "), отправлен текст"
			}
		} else {
			sentFile = true
			sentText = true
		}
	} else if lineProfile == "" {
		note = "нет MAX-линии менеджера — билет только в CRM"
	}

	a.logDealLeadAct(r.Context(), id, actorID(r), "ticket_sent_candidate", map[string]any{
		"file_id":     fileID,
		"thread_id":   threadID,
		"message_id":  msgID,
		"as_owner_id": ownerID,
		"as_owner":    authorLabel,
		"sent_file":   sentFile,
		"sent_text":   sentText,
		"note":        note,
	})
	writeJSON(w, 200, map[string]any{
		"ok": true, "thread_id": threadID, "message_id": msgID,
		"sent_file": sentFile, "sent_text": sentText, "as_owner": authorLabel, "note": note,
		"crm_ok": true,
	})
}

func transportLabel(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "air", "avia", "авиа":
		return "авиа"
	case "rail", "train", "жд":
		return "жд"
	case "bus", "автобус":
		return "автобус"
	default:
		if t == "" {
			return "билет"
		}
		return t
	}
}

func (a *App) ensureInboxThreadForDeal(ctx context.Context, dealID, contactID, phone string) (string, error) {
	var tid string
	if contactID != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_threads
			WHERE contact_id=$1::uuid ORDER BY last_at DESC NULLS LAST LIMIT 1`, contactID).Scan(&tid)
		if tid != "" {
			return tid, nil
		}
	}
	ph := maxNormPhone(phone)
	if ph != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM inbox_threads
			WHERE regexp_replace(coalesce(phone,''),'[^0-9]','','g') LIKE '%'||$1
			ORDER BY last_at DESC NULLS LAST LIMIT 1`, peerTail(ph)).Scan(&tid)
		if tid != "" {
			return tid, nil
		}
	}
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM inbox_threads WHERE deal_id=$1::uuid
		ORDER BY last_at DESC NULLS LAST LIMIT 1`, dealID).Scan(&tid)
	if tid != "" {
		return tid, nil
	}
	return "", fmt.Errorf("нет треда")
}

func (a *App) listLogisticsTable(w http.ResponseWriter, r *http.Request) {
	s := a.crmScope(r)
	role := roleName(r)
	if !s.IsLogist && !s.All && role != "rop" && role != "admin" {
		writeJSON(w, 403, map[string]string{"error": "только логистика"})
		return
	}
	a.ensureLogisticsSchema(r.Context())
	// Не падать, если миграция ещё не накатилась — отдаём escort-сделки без ticket join.
	where := `coalesce(p.bitrix_id,'')=$1
		   OR coalesce(d.logistics_needed,false)=true
		   OR ` + sqlDealOnManagerHandoffStage("d")
	args := []any{escortPipelineKey}
	if s.IsLogist {
		where = sqlLogistBoardVisible("d")
		args = nil
	}
	items := a.queryMaps(r.Context(), `
		SELECT d.id::text, d.title,
		       coalesce(c.full_name,'') AS candidate,
		       coalesce(u.full_name,'') AS manager,
		       coalesce(s.name,'') AS stage,
		       coalesce(s.short_name,'') AS stage_short,
		       coalesce(d.logistics_needed,false) AS logistics_needed
		FROM deals d
		JOIN pipeline_stages s ON s.id=d.stage_id
		JOIN pipelines p ON p.id=d.pipeline_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN users u ON u.id=d.owner_id
		WHERE `+where+`
		ORDER BY d.created_at DESC
		LIMIT 200`, args...)
	// Обогатить transport/depart_at если таблица есть
	for _, row := range items {
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		var transport string
		var depart *string
		err := a.db.QueryRow(r.Context(), `
			SELECT coalesce(transport,''), CASE WHEN depart_at IS NULL THEN NULL ELSE depart_at::text END
			FROM deal_tickets WHERE deal_id=$1::uuid`, id).Scan(&transport, &depart)
		if err == nil {
			row["transport"] = transport
			if depart != nil {
				row["depart_at"] = *depart
			}
		}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (a *App) attachTicketToDealResponse(ctx context.Context, r *http.Request, deal map[string]any) {
	id, _ := deal["id"].(string)
	if id == "" {
		return
	}
	a.ensureLogisticsSchema(ctx)
	role := roleName(r)
	sensitive := canSeeTicketSensitive(role)
	seeFiles, canDocs, tower := a.viewerSeesTicketFiles(r)
	deal["ticket"] = a.loadDealTicketMap(ctx, id, sensitive)
	allFiles := a.queryMaps(ctx, `
		SELECT id::text, kind, filename, mime, size_bytes, source, created_at
		FROM deal_ticket_files WHERE deal_id=$1::uuid AND coalesce(kind,'') <> 'note' ORDER BY created_at DESC`, id)
	deal["ticket_files"] = filterTicketFilesForViewer(allFiles, role, tower)
	deal["ticket_sensitive"] = sensitive
	protected := countSensitiveTicketFiles(allFiles)
	if !seeFiles {
		protected = 0
	}
	deal["protected_docs"] = protected
	deal["can_see_files"] = seeFiles
	deal["can_download_docs"] = canDocs
	var needed bool
	_ = a.db.QueryRow(ctx, `SELECT coalesce(logistics_needed,false) FROM deals WHERE id=$1::uuid`, id).Scan(&needed)
	deal["logistics_needed"] = needed
}
