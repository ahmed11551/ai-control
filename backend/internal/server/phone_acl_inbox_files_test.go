package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInboxFilesOnlyMishaDept(t *testing.T) {
	dsn := os.Getenv("PHONE_ACL_TEST_DSN")
	if dsn == "" {
		t.Skip("PHONE_ACL_TEST_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx, `
		CREATE EXTENSION IF NOT EXISTS pgcrypto;
		CREATE TABLE IF NOT EXISTS departments (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			department_id UUID REFERENCES departments(id)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	insertDept := func(name string) string {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ($1) RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	insertUser := func(dept string) string {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO users (department_id) VALUES ($1::uuid) RETURNING id::text`, dept).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	mishaID := insertDept("Отдел Миши")
	baurovID := insertDept("Отдел Баурова")
	pwwwwID := insertDept("Отдел Pwwww")
	fontankaID := insertDept("Отдел Фонтанка")
	mishaMgr := insertUser(mishaID)
	mishaROP := insertUser(mishaID)
	baurovMgr := insertUser(baurovID)
	baurovROP := insertUser(baurovID)
	fontankaMgr := insertUser(fontankaID)
	fontankaROP := insertUser(fontankaID)

	app := &App{db: pool}

	if !app.userCanViewInboxMedia(ctx, mishaMgr, "manager") {
		t.Fatal("Misha manager must download chat files")
	}
	if !app.userCanViewInboxMedia(ctx, mishaROP, "rop") {
		t.Fatal("Misha ROP must download chat files")
	}
	if app.userCanViewInboxMedia(ctx, baurovMgr, "manager") || app.userCanViewInboxMedia(ctx, baurovROP, "rop") {
		t.Fatal("Baurov must not download chat files")
	}
	if app.userCanViewInboxMedia(ctx, fontankaMgr, "manager") || app.userCanViewInboxMedia(ctx, fontankaROP, "rop") {
		t.Fatal("Fontanka must not download chat files")
	}
	if app.userCanViewInboxMedia(ctx, fontankaMgr, "support") {
		t.Fatal("other roles must not download chat files via dept flag")
	}

	var phones, files bool
	mustFlags := func(dept, label string, wantPhones, wantFiles bool) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT see_client_phones, see_inbox_files FROM departments WHERE id=$1::uuid`, dept).Scan(&phones, &files); err != nil {
			t.Fatal(err)
		}
		if phones != wantPhones || files != wantFiles {
			t.Fatalf("%s phones=%v files=%v, want phones=%v files=%v", label, phones, files, wantPhones, wantFiles)
		}
	}
	mustFlags(mishaID, "misha", true, true)
	if !app.userCanSeeClientPhones(ctx, mishaMgr, "manager") || !app.userCanSeeClientPhones(ctx, mishaROP, "rop") {
		t.Fatal("Misha must see client phones")
	}
	mustFlags(baurovID, "baurov", true, false)
	mustFlags(pwwwwID, "pwwww", true, false)
	pwwwwMgr := insertUser(pwwwwID)
	if !app.userCanSeeClientPhones(ctx, pwwwwMgr, "manager") {
		t.Fatal("Pwwww must see client phones")
	}
	if app.userCanViewInboxMedia(ctx, pwwwwMgr, "manager") {
		t.Fatal("Pwwww must not download chat files")
	}
	mustFlags(fontankaID, "fontanka", false, false)

	msg := []map[string]any{{
		"id": "m1", "direction": "inbound", "body": "[изображение]",
		"media_kind": "image", "media_storage_key": "k", "media_count": 1, "media_ready": 1,
		"media_filename": "a.jpg",
	}}
	checkDownload := func(userID, role string, want bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/inbox/x", nil)
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxUserID, userID), ctxRole, role))
		out := app.redactInboxMessagesForRole(req, msg)
		got, _ := out[0]["media_download"].(bool)
		if got != want {
			t.Fatalf("%s/%s media_download=%v want %v", role, userID, got, want)
		}
	}
	checkDownload(mishaMgr, "manager", true)
	checkDownload(baurovMgr, "manager", false)
	checkDownload(fontankaMgr, "manager", false)
	checkDownload(fontankaROP, "rop", false)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/inbox/messages/x/media", nil)
	req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxUserID, fontankaMgr), ctxRole, "manager"))
	app.getInboxMessageMedia(rec, req)
	if rec.Code != 403 {
		t.Fatalf("fontanka download status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/inbox/messages/x/media", nil)
	req2 = req2.WithContext(context.WithValue(context.WithValue(req2.Context(), ctxUserID, baurovMgr), ctxRole, "manager"))
	app.getInboxMessageMedia(rec2, req2)
	if rec2.Code != 403 {
		t.Fatalf("baurov download status=%d body=%s", rec2.Code, rec2.Body.String())
	}
}
