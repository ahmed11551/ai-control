package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDeptPhoneACLOtherManagersMasked(t *testing.T) {
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
	// name is not unique — insert explicitly
	var baurovID, otherID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM departments WHERE name='Отдел Баурова' LIMIT 1`).Scan(&baurovID); err != nil {
		if err := pool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ('Отдел Баурова') RETURNING id::text`).Scan(&baurovID); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO departments (name) VALUES ('Отдел Тёма') RETURNING id::text`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}

	insertUser := func(dept string) string {
		var id string
		if err := pool.QueryRow(ctx, `INSERT INTO users (department_id) VALUES ($1::uuid) RETURNING id::text`, dept).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	baurovMgr := insertUser(baurovID)
	otherMgr := insertUser(otherID)
	otherROP := insertUser(otherID)
	baurovLogist := insertUser(baurovID)

	app := &App{db: pool}

	if !app.userCanSeeClientPhones(ctx, baurovMgr, "manager") {
		t.Fatal("Baurov manager must see phones")
	}
	if !app.userCanSeeClientPhones(ctx, baurovMgr, "rop") {
		t.Fatal("Baurov rop must see phones")
	}
	if app.userCanSeeClientPhones(ctx, otherMgr, "manager") {
		t.Fatal("other department manager must not see phones")
	}
	if app.userCanSeeClientPhones(ctx, otherROP, "rop") {
		t.Fatal("other department rop must not see phones")
	}
	if app.userCanSeeClientPhones(ctx, baurovLogist, "logist") {
		t.Fatal("logist in Baurov must not see phones")
	}
	if !app.userCanSeeClientPhones(ctx, otherMgr, "admin") {
		t.Fatal("admin must see phones")
	}

	var flag bool
	if err := pool.QueryRow(ctx, `SELECT see_client_phones FROM departments WHERE id=$1::uuid`, baurovID).Scan(&flag); err != nil || !flag {
		t.Fatalf("Baurov flag=%v err=%v", flag, err)
	}
	if err := pool.QueryRow(ctx, `SELECT see_client_phones FROM departments WHERE id=$1::uuid`, otherID).Scan(&flag); err != nil || flag {
		t.Fatalf("other dept flag=%v err=%v", flag, err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/contacts", nil)
	req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxUserID, otherMgr), ctxRole, "manager"))
	app.writePhones(rec, req, 200, map[string]any{"phone": "79089215621", "title": "Реджеп"})
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["phone"] == "79089215621" {
		t.Fatalf("other manager got full phone: %#v", body)
	}
	if body["phone"] == "" || body["phone"] == "79089215621" {
		t.Fatalf("expected mask, got %#v", body["phone"])
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/contacts", nil)
	req2 = req2.WithContext(context.WithValue(context.WithValue(req2.Context(), ctxUserID, baurovMgr), ctxRole, "manager"))
	app.writePhones(rec2, req2, 200, map[string]any{"phone": "79089215621"})
	var body2 map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatal(err)
	}
	if body2["phone"] != "79089215621" {
		t.Fatalf("Baurov manager must get full phone, got %#v", body2["phone"])
	}
}
