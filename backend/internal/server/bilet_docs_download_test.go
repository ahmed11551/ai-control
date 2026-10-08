package server

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Отдельная база, не forgecrm. Живые карточки не читаются и не меняются.
func TestBiletDocsLoadAndDownload(t *testing.T) {
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, "postgres://forge:forge@127.0.0.1:55432/postgres?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := admin.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	const dbname = "bilet_doc_dl_test"
	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1 AND pid <> pg_backend_pid()`, dbname)
	_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbname)
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbname); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		cleaner, err := pgxpool.New(cctx, "postgres://forge:forge@127.0.0.1:55432/postgres?sslmode=disable")
		if err != nil {
			return
		}
		defer cleaner.Close()
		_, _ = cleaner.Exec(cctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1 AND pid <> pg_backend_pid()`, dbname)
		_, _ = cleaner.Exec(cctx, `DROP DATABASE IF EXISTS `+dbname)
	})

	pool, err := pgxpool.New(ctx, "postgres://forge:forge@127.0.0.1:55432/"+dbname+"?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	for _, stmt := range []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE deals (
			id UUID PRIMARY KEY,
			contact_id UUID,
			owner_id UUID,
			pipeline_id UUID,
			stage_id UUID,
			logistics_needed BOOLEAN NOT NULL DEFAULT false
		)`,
		`CREATE TABLE contacts (
			id UUID PRIMARY KEY,
			phone TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE pipelines (
			id UUID PRIMARY KEY,
			bitrix_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE inbox_lines (
			id UUID PRIMARY KEY,
			external_ref TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE inbox_threads (
			id UUID PRIMARY KEY,
			deal_id UUID,
			contact_id UUID,
			phone TEXT NOT NULL DEFAULT '',
			line_id UUID
		)`,
		`CREATE TABLE inbox_messages (
			id UUID PRIMARY KEY,
			thread_id UUID NOT NULL,
			direction TEXT NOT NULL DEFAULT 'inbound',
			media_kind TEXT NOT NULL DEFAULT '',
			media_filename TEXT NOT NULL DEFAULT '',
			media_mime TEXT NOT NULL DEFAULT '',
			media_storage_key TEXT NOT NULL DEFAULT '',
			media_wappi_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	dealID := "11111111-1111-1111-1111-111111111111"
	ownerID := "22222222-2222-2222-2222-222222222222"
	contactID := "33333333-3333-3333-3333-333333333333"
	threadID := "44444444-4444-4444-4444-444444444444"
	photoID := "55555555-5555-5555-5555-555555555555"
	webpID := "55555555-5555-5555-5555-555555555556"
	pdfID := "55555555-5555-5555-5555-555555555557"
	voiceID := "55555555-5555-5555-5555-555555555558"
	stickerID := "55555555-5555-5555-5555-555555555559"
	adminID := "66666666-6666-6666-6666-666666666666"
	logistID := "66666666-6666-6666-6666-666666666667"

	photoBytes := []byte("jpeg-photo-bytes")
	webpBytes := []byte("webp-photo-bytes")
	pdfBytes := []byte("%PDF-1.4 passport")
	voiceBytes := []byte("voice-ogg")
	stickerBytes := []byte("sticker-webp")
	writeMedia := func(name string, body []byte) string {
		dir := filepath.Join(tmp, "data", "inbox-media", threadID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o640); err != nil {
			t.Fatal(err)
		}
		return threadID + "/" + name
	}
	photoKey := writeMedia("photo.jpg", photoBytes)
	webpKey := writeMedia("scan.webp", webpBytes)
	pdfKey := writeMedia("passport.pdf", pdfBytes)
	voiceKey := writeMedia("voice.ogg", voiceBytes)
	stickerKey := writeMedia("sticker.webp", stickerBytes)

	if _, err := pool.Exec(ctx, `INSERT INTO contacts (id, phone) VALUES ($1::uuid, '79990001122')`, contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO deals (id, contact_id, owner_id, logistics_needed)
		VALUES ($1::uuid, $2::uuid, $3::uuid, true)`, dealID, contactID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inbox_threads (id, phone) VALUES ($1::uuid, '+7 999 000-11-22')`, threadID); err != nil {
		t.Fatal(err)
	}
	insertMsg := func(id, kind, filename, mime, key string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO inbox_messages (id, thread_id, direction, media_kind, media_filename, media_mime, media_storage_key, created_at)
			VALUES ($1::uuid, $2::uuid, 'inbound', $3, $4, $5, $6, now())
		`, id, threadID, kind, filename, mime, key); err != nil {
			t.Fatal(err)
		}
	}
	insertMsg(photoID, "image", "photo.jpg", "image/jpeg", photoKey)
	insertMsg(webpID, "image", "scan.webp", "image/webp", webpKey)
	insertMsg(pdfID, "", "", "", "")
	insertMsg(voiceID, "voice", "voice.ogg", "audio/ogg", voiceKey)
	insertMsg(stickerID, "sticker", "sticker.webp", "image/webp", stickerKey)

	app := &App{db: pool}
	app.ensureLogisticsSchema(ctx)
	app.ensureInboxMediaFilesSchema(ctx)
	if _, err := pool.Exec(ctx, `
		INSERT INTO inbox_message_files (message_id, sort_order, kind, filename, mime, storage_key)
		VALUES ($1::uuid, 0, 'document', 'passport.pdf', 'application/pdf', $2)
	`, pdfID, pdfKey); err != nil {
		t.Fatal(err)
	}

	copied := app.pullDealChatDocs(ctx, dealID, adminID)
	if copied != 3 {
		t.Fatalf("first load copied=%d want 3 (jpg, webp, pdf)", copied)
	}
	again := app.pullDealChatDocs(ctx, dealID, adminID)
	if again != 0 {
		t.Fatalf("second load must not copy again, copied=%d", again)
	}

	var owner, stage string
	var needed bool
	if err := pool.QueryRow(ctx, `
		SELECT owner_id::text, coalesce(stage_id::text,''), logistics_needed
		FROM deals WHERE id=$1::uuid`, dealID).Scan(&owner, &stage, &needed); err != nil {
		t.Fatal(err)
	}
	if owner != ownerID || stage != "" || !needed {
		t.Fatalf("deal changed owner=%s stage=%s needed=%v", owner, stage, needed)
	}

	rows, err := pool.Query(ctx, `
		SELECT filename, mime, source, kind FROM deal_ticket_files WHERE deal_id=$1::uuid ORDER BY filename`, dealID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, mime, source, kind string
		if err := rows.Scan(&name, &mime, &source, &kind); err != nil {
			t.Fatal(err)
		}
		if source != "inbox" || kind != "doc" {
			t.Fatalf("stored file %s source=%s kind=%s", name, source, kind)
		}
		got[name] = mime
	}
	if got["photo.jpg"] != "image/jpeg" || got["scan.webp"] != "image/webp" || got["passport.pdf"] != "application/pdf" {
		t.Fatalf("loaded files = %#v", got)
	}
	if _, voice := got["voice.ogg"]; voice {
		t.Fatal("voice was loaded")
	}
	if _, sticker := got["sticker.webp"]; sticker {
		t.Fatal("sticker was loaded")
	}

	var noteID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO deal_ticket_files (deal_id, kind, filename, mime, size_bytes, storage_key, source)
		VALUES ($1::uuid, 'note', 'комментарий почему нет файла', 'text/plain', 10, '', 'bilet')
		RETURNING id::text`, dealID).Scan(&noteID); err != nil {
		t.Fatal(err)
	}

	files := app.listBiletDocFiles(ctx, dealID, true)
	var sawPhoto, sawPDF, sawNote bool
	for _, f := range files {
		name := fmt.Sprint(f["filename"])
		switch name {
		case "photo.jpg":
			sawPhoto = true
			if !docShowsAsPhoto(fmt.Sprint(f["kind"]), fmt.Sprint(f["mime"]), name) {
				t.Fatalf("photo must display as image: %#v", f)
			}
		case "passport.pdf":
			sawPDF = true
			if docShowsAsPhoto(fmt.Sprint(f["kind"]), fmt.Sprint(f["mime"]), name) {
				t.Fatalf("pdf must stay a file, not a photo: %#v", f)
			}
		case "комментарий почему нет файла":
			sawNote = true
		}
	}
	if !sawPhoto || !sawPDF || !sawNote {
		t.Fatalf("list missing items photo=%v pdf=%v note=%v files=%#v", sawPhoto, sawPDF, sawNote, files)
	}

	var photoFileID, pdfFileID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM deal_ticket_files WHERE deal_id=$1::uuid AND filename='photo.jpg'`, dealID).Scan(&photoFileID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM deal_ticket_files WHERE deal_id=$1::uuid AND filename='passport.pdf'`, dealID).Scan(&pdfFileID); err != nil {
		t.Fatal(err)
	}

	download := func(role, uid, fileID string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/deals/"+dealID+"/ticket/files/"+fileID, nil)
		req = withIDs(req, role, uid, map[string]string{"id": dealID, "fileId": fileID})
		rr := httptest.NewRecorder()
		app.getDealTicketFile(rr, req)
		return rr
	}

	photoRes := download("admin", adminID, photoFileID)
	if photoRes.Code != 200 || !bytes.Equal(photoRes.Body.Bytes(), photoBytes) {
		t.Fatalf("admin photo download code=%d body=%q", photoRes.Code, photoRes.Body.String())
	}
	if !strings.Contains(photoRes.Header().Get("Content-Type"), "image/jpeg") {
		t.Fatalf("photo content-type %s", photoRes.Header().Get("Content-Type"))
	}
	pdfRes := download("logist", logistID, pdfFileID)
	if pdfRes.Code != 200 || !bytes.Equal(pdfRes.Body.Bytes(), pdfBytes) {
		t.Fatalf("logist pdf download code=%d body=%q", pdfRes.Code, pdfRes.Body.String())
	}
	if !strings.Contains(pdfRes.Header().Get("Content-Type"), "application/pdf") {
		t.Fatalf("pdf content-type %s", pdfRes.Header().Get("Content-Type"))
	}
	for _, role := range []string{"manager", "sales", "rop"} {
		res := download(role, adminID, photoFileID)
		if res.Code != 404 || bytes.Contains(res.Body.Bytes(), photoBytes) {
			t.Fatalf("%s must not download code=%d body=%q", role, res.Code, res.Body.String())
		}
	}
	noteRes := download("admin", adminID, noteID)
	if noteRes.Code != 404 {
		t.Fatalf("note must not download code=%d", noteRes.Code)
	}

	archive := func(role, uid string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/deals/"+dealID+"/bilet-doc/archive", nil)
		req = withIDs(req, role, uid, map[string]string{"id": dealID})
		rr := httptest.NewRecorder()
		app.downloadBiletDocsArchive(rr, req)
		return rr
	}
	all := archive("admin", adminID)
	if all.Code != 200 {
		t.Fatalf("archive code=%d body=%s", all.Code, all.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(all.Body.Bytes()), int64(all.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	zipped := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
		zipped[f.Name] = buf.String()
	}
	if zipped["photo.jpg"] != string(photoBytes) || zipped["scan.webp"] != string(webpBytes) || zipped["passport.pdf"] != string(pdfBytes) {
		t.Fatalf("archive contents %#v", zipped)
	}
	if len(zipped) != 3 {
		t.Fatalf("archive must be photo+webp+pdf, got %#v", zipped)
	}
	mgrArchive := archive("manager", adminID)
	if mgrArchive.Code != 403 {
		t.Fatalf("manager archive code=%d", mgrArchive.Code)
	}

	pullReq := httptest.NewRequest(http.MethodPost, "/deals/"+dealID+"/bilet-doc/from-chat", strings.NewReader("{}"))
	pullReq = withIDs(pullReq, "admin", adminID, map[string]string{"id": dealID})
	pullRes := httptest.NewRecorder()
	app.pullBiletDocsFromChat(pullRes, pullReq)
	if pullRes.Code != 200 || !strings.Contains(pullRes.Body.String(), `"copied":0`) {
		t.Fatalf("admin pull code=%d body=%s", pullRes.Code, pullRes.Body.String())
	}
	deny := httptest.NewRequest(http.MethodPost, "/deals/"+dealID+"/bilet-doc/from-chat", strings.NewReader("{}"))
	deny = withIDs(deny, "manager", adminID, map[string]string{"id": dealID})
	denyRes := httptest.NewRecorder()
	app.pullBiletDocsFromChat(denyRes, deny)
	if denyRes.Code != 403 {
		t.Fatalf("manager pull code=%d body=%s", denyRes.Code, denyRes.Body.String())
	}
}

func withIDs(req *http.Request, role, uid string, params map[string]string) *http.Request {
	ctx := context.WithValue(req.Context(), ctxUserID, uid)
	ctx = context.WithValue(ctx, ctxRole, role)
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
}

// docShowsAsPhoto — то же правило, что в окне документов: фото в карусель, pdf и прочее списком.
func docShowsAsPhoto(kind, mime, filename string) bool {
	if kind == "note" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(mime), "image/") {
		return true
	}
	lower := strings.ToLower(filename)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".heic", ".heif"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
