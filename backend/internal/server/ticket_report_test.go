package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTicketReportText(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	depart := time.Date(2026, time.January, 5, 21, 57, 0, 0, loc)
	arrive := time.Date(2026, time.January, 8, 18, 5, 0, 0, loc)
	got := ticketReportText("Грозный", "Москва", "поезд", "3", "5", "5", &depart, &arrive)
	want := strings.Join([]string{
		"Билет оформлен",
		"",
		"Грозный → Москва",
		"Поезд",
		"Рейс: 3",
		"Вагон: 5",
		"Место: 5",
		"Выезд: 5 января 2026, ночь",
		"Прибытие: 8 января 2026, вечер",
	}, "\n")
	if got != want {
		t.Fatalf("text:\n%s\nwant:\n%s", got, want)
	}

	short := ticketReportText("Грозный", "Москва", "самолет", "  ", "", "", &depart, nil)
	if strings.Contains(short, "Рейс:") || strings.Contains(short, "Вагон:") || strings.Contains(short, "Место:") || strings.Contains(short, "Прибытие:") {
		t.Fatalf("empty flight, wagon, seat and arrival must be omitted:\n%s", short)
	}
	if !strings.Contains(short, "Самолёт") || !strings.Contains(short, "Выезд: 5 января 2026, ночь") {
		t.Fatalf("short text:\n%s", short)
	}
	if strings.Contains(got, "паспорт") {
		t.Fatal("passport comment must not be in the report")
	}

	midnight := time.Date(2026, time.January, 5, 0, 0, 0, 0, loc)
	plain := ticketReportText("А", "Б", "поезд", "", "", "", &midnight, nil)
	if strings.Contains(plain, "ночь") || strings.Contains(plain, "00:00") || strings.Contains(plain, "января 2026,") {
		t.Fatalf("midnight must stay without a daypart or clock:\n%s", plain)
	}
	if !strings.Contains(plain, "Выезд: 5 января 2026") {
		t.Fatalf("midnight date:\n%s", plain)
	}
}

func TestTicketReportDeniedBeforeDB(t *testing.T) {
	a := &App{}
	for _, role := range []string{"manager", "sales", "rop"} {
		req := httptest.NewRequest(http.MethodPost, "/deals/x/ticket/report-to-manager", strings.NewReader(`{}`))
		req = req.WithContext(context.WithValue(req.Context(), ctxRole, role))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "00000000-0000-0000-0000-000000000099")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		a.reportTicketToManager(rr, req)
		if rr.Code != 403 {
			t.Fatalf("%s report must be 403, code=%d body=%s", role, rr.Code, rr.Body.String())
		}
	}
}

// Отдельная база, не forgecrm. Живые карточки не читаются и не меняются.
func TestTicketReportKeepsStageAndNotifiesOwner(t *testing.T) {
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, "postgres://forge:forge@127.0.0.1:55432/postgres?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := admin.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	const dbname = "ticket_report_iso_test"
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
		`CREATE TABLE roles (id UUID PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE users (
			id UUID PRIMARY KEY,
			full_name TEXT NOT NULL DEFAULT '',
			role_id UUID REFERENCES roles(id),
			is_active BOOLEAN NOT NULL DEFAULT true
		)`,
		`CREATE TABLE leads (id UUID PRIMARY KEY)`,
		`CREATE TABLE contacts (id UUID PRIMARY KEY, full_name TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE pipelines (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'deals',
			bitrix_id TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE pipeline_stages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			pipeline_id UUID REFERENCES pipelines(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			sort_order INT NOT NULL DEFAULT 0,
			is_won BOOLEAN NOT NULL DEFAULT false,
			is_lost BOOLEAN NOT NULL DEFAULT false,
			probability INT NOT NULL DEFAULT 0,
			bitrix_id TEXT
		)`,
		`CREATE TABLE deals (
			id UUID PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			contact_id UUID,
			owner_id UUID,
			lead_id UUID,
			pipeline_id UUID,
			stage_id UUID,
			logistics_needed BOOLEAN NOT NULL DEFAULT false
		)`,
		`CREATE TABLE notifications (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			channel TEXT NOT NULL DEFAULT 'in_app',
			title TEXT NOT NULL,
			body TEXT,
			is_read BOOLEAN NOT NULL DEFAULT false,
			payload JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE deal_tickets (
			deal_id UUID PRIMARY KEY REFERENCES deals(id) ON DELETE CASCADE,
			transport TEXT NOT NULL DEFAULT '',
			from_city TEXT NOT NULL DEFAULT '',
			to_city TEXT NOT NULL DEFAULT '',
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
		)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	const (
		dealID   = "11111111-1111-1111-1111-111111111111"
		ownerID  = "22222222-2222-2222-2222-222222222222"
		logistID = "33333333-3333-3333-3333-333333333333"
		adminID  = "44444444-4444-4444-4444-444444444444"
		otherID  = "55555555-5555-5555-5555-555555555555"
		stageID  = "66666666-6666-6666-6666-666666666666"
		pipeID   = "77777777-7777-7777-7777-777777777777"
		roleMgr  = "88888888-8888-8888-8888-888888888881"
		roleLog  = "88888888-8888-8888-8888-888888888882"
		roleAdm  = "88888888-8888-8888-8888-888888888883"
		contact  = "99999999-9999-9999-9999-999999999999"
		bareDeal = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	)
	for _, stmt := range []string{
		`INSERT INTO roles (id, name) VALUES ('` + roleMgr + `','manager'), ('` + roleLog + `','logist'), ('` + roleAdm + `','admin')`,
		`INSERT INTO users (id, full_name, role_id) VALUES
			('` + ownerID + `','Менеджер Тест','` + roleMgr + `'),
			('` + logistID + `','Логист Тест','` + roleLog + `'),
			('` + adminID + `','Админ Тест','` + roleAdm + `'),
			('` + otherID + `','Другой логист','` + roleLog + `')`,
		`INSERT INTO contacts (id, full_name) VALUES ('` + contact + `','Кандидат Тест')`,
		`INSERT INTO pipelines (id, name, bitrix_id) VALUES ('` + pipeID + `','Адаптация','local:hire')`,
		`INSERT INTO pipeline_stages (id, pipeline_id, name, sort_order, bitrix_id) VALUES ('` + stageID + `','` + pipeID + `','Билет',1,'local:hire:bilet')`,
		`INSERT INTO deals (id, title, contact_id, owner_id, pipeline_id, stage_id, logistics_needed)
			VALUES ('` + dealID + `','Кандидат Тест','` + contact + `','` + ownerID + `','` + pipeID + `','` + stageID + `', true)`,
		`INSERT INTO deals (id, title, pipeline_id, stage_id, logistics_needed)
			VALUES ('` + bareDeal + `','Без владельца','` + pipeID + `','` + stageID + `', true)`,
		`INSERT INTO deal_tickets (deal_id, notes) VALUES ('` + dealID + `','паспорт 4510 123456')`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	app := &App{db: pool, hub: NewHub()}
	post := func(role, userID, deal, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/deals/"+deal+"/ticket/report-to-manager", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := context.WithValue(req.Context(), ctxRole, role)
		ctx = context.WithValue(ctx, ctxUserID, userID)
		req = req.WithContext(ctx)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", deal)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		app.reportTicketToManager(rr, req)
		return rr
	}

	body := `{
		"from_city":"Грозный","to_city":"Москва","transport":"поезд",
		"flight_or_train":"3","wagon":"5","seat":"5",
		"depart_at":"2026-01-05T18:57:00Z","arrive_at":"2026-01-08T15:05:00Z"
	}`
	rr := post("logist", logistID, dealID, body)
	if rr.Code != 200 {
		t.Fatalf("logist report code=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	text, _ := resp["text"].(string)
	want := strings.Join([]string{
		"Билет оформлен",
		"",
		"Грозный → Москва",
		"Поезд",
		"Рейс: 3",
		"Вагон: 5",
		"Место: 5",
		"Выезд: 5 января 2026, ночь",
		"Прибытие: 8 января 2026, вечер",
	}, "\n")
	if text != want {
		t.Fatalf("message:\n%s", text)
	}
	if strings.Contains(text, "паспорт") || strings.Contains(text, "4510") {
		t.Fatalf("passport comment leaked:\n%s", text)
	}

	var owner, stage, notes, flight string
	if err := pool.QueryRow(ctx, `
		SELECT d.owner_id::text, d.stage_id::text, t.notes, t.flight_or_train
		FROM deals d JOIN deal_tickets t ON t.deal_id=d.id
		WHERE d.id=$1::uuid`, dealID).Scan(&owner, &stage, &notes, &flight); err != nil {
		t.Fatal(err)
	}
	if owner != ownerID || stage != stageID {
		t.Fatalf("owner/stage changed: owner=%s stage=%s", owner, stage)
	}
	if notes != "паспорт 4510 123456" || flight != "3" {
		t.Fatalf("notes=%q flight=%q", notes, flight)
	}
	var msg string
	if err := pool.QueryRow(ctx, `
		SELECT m.body FROM staff_dialog_messages m
		JOIN staff_dialogs sd ON sd.id=m.dialog_id
		WHERE sd.deal_id=$1::uuid AND sd.status='open'`, dealID).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	if msg != want {
		t.Fatalf("stored message:\n%s", msg)
	}
	var nTitle, nUser string
	if err := pool.QueryRow(ctx, `
		SELECT title, user_id::text FROM notifications WHERE channel='staff_dialog'`).Scan(&nTitle, &nUser); err != nil {
		t.Fatal(err)
	}
	if nUser != ownerID || !strings.Contains(nTitle, "Логист Тест") {
		t.Fatalf("notification title=%q user=%s", nTitle, nUser)
	}

	rr = post("admin", adminID, dealID, `{
		"from_city":"Грозный","to_city":"Москва","transport":"поезд",
		"flight_or_train":"3","wagon":"","seat":"5",
		"depart_at":"2026-01-05T18:57:00Z","arrive_at":"2026-01-08T15:05:00Z"
	}`)
	if rr.Code != 200 {
		t.Fatalf("admin report code=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	text, _ = resp["text"].(string)
	if strings.Contains(text, "Вагон:") || !strings.Contains(text, "Рейс: 3") || !strings.Contains(text, "Место: 5") {
		t.Fatalf("empty wagon must be omitted:\n%s", text)
	}
	if err := pool.QueryRow(ctx, `SELECT owner_id::text, stage_id::text FROM deals WHERE id=$1::uuid`, dealID).Scan(&owner, &stage); err != nil {
		t.Fatal(err)
	}
	if owner != ownerID || stage != stageID {
		t.Fatalf("admin report changed owner/stage: owner=%s stage=%s", owner, stage)
	}
	var adminNote int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM notifications
		WHERE user_id=$1::uuid AND title LIKE 'Билет оформлен%'`, ownerID).Scan(&adminNote); err != nil {
		t.Fatal(err)
	}
	if adminNote != 1 {
		t.Fatalf("owner ticket notification count=%d", adminNote)
	}
	var otherNotes int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id=$1::uuid`, otherID).Scan(&otherNotes); err != nil {
		t.Fatal(err)
	}
	if otherNotes != 0 {
		t.Fatalf("other logist must not be notified, count=%d", otherNotes)
	}

	rr = post("logist", logistID, bareDeal, body)
	if rr.Code != 400 || !strings.Contains(rr.Body.String(), "нет ответственного") {
		t.Fatalf("missing owner code=%d body=%s", rr.Code, rr.Body.String())
	}
	var dialogs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM staff_dialogs WHERE deal_id=$1::uuid`, bareDeal).Scan(&dialogs); err != nil {
		t.Fatal(err)
	}
	if dialogs != 0 {
		t.Fatalf("missing owner still opened a dialog: %d", dialogs)
	}
	if err := pool.QueryRow(ctx, `SELECT stage_id::text, coalesce(owner_id::text,'') FROM deals WHERE id=$1::uuid`, bareDeal).Scan(&stage, &owner); err != nil {
		t.Fatal(err)
	}
	if stage != stageID || owner != "" {
		t.Fatalf("bare deal changed stage=%s owner=%s", stage, owner)
	}
}
