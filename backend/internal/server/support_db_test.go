package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func resetSupportSchema() {
	supportSchemaMu.Lock()
	supportSchemaDone = false
	supportSchemaMu.Unlock()
}

// Отдельная база. Живую forgecrm этот тест не трогает.
func TestSupportChatFlow(t *testing.T) {
	resetSupportSchema()
	t.Cleanup(resetSupportSchema)

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, "postgres://forge:forge@127.0.0.1:55432/postgres?sslmode=disable")
	if err != nil {
		t.Skip(err)
	}
	defer adminPool.Close()
	if err := adminPool.Ping(ctx); err != nil {
		t.Skip(err)
	}
	const dbname = "support_chat_iso_test"
	_, _ = adminPool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1 AND pid <> pg_backend_pid()`, dbname)
	_, _ = adminPool.Exec(ctx, `DROP DATABASE IF EXISTS `+dbname)
	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+dbname); err != nil {
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
	if _, err := pool.Exec(ctx, `
		CREATE TABLE roles (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT NOT NULL);
		CREATE TABLE departments (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), name TEXT NOT NULL);
		CREATE TABLE users (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			full_name TEXT NOT NULL,
			role_id UUID REFERENCES roles(id),
			department_id UUID REFERENCES departments(id)
		)`); err != nil {
		t.Fatal(err)
	}
	var adminID, managerID, otherID string
	if err := pool.QueryRow(ctx, `
		WITH ra AS (INSERT INTO roles (name) VALUES ('admin') RETURNING id)
		INSERT INTO users (full_name, role_id) SELECT 'Админ', id FROM ra RETURNING id::text`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		WITH rm AS (INSERT INTO roles (name) VALUES ('manager') RETURNING id),
		     d AS (INSERT INTO departments (name) VALUES ('Отдел Георгия') RETURNING id)
		INSERT INTO users (full_name, role_id, department_id) SELECT 'Иван Петров', rm.id, d.id FROM rm, d RETURNING id::text`).Scan(&managerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (full_name) VALUES ('Другой') RETURNING id::text`).Scan(&otherID); err != nil {
		t.Fatal(err)
	}

	app := &App{db: pool, hub: NewHub()}
	call := func(h http.HandlerFunc, req *http.Request) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h(rec, req)
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	msgsOf := func(body map[string]any) []any {
		list, _ := body["messages"].([]any)
		return list
	}

	// Повтор с тем же client_id не создаёт второе сообщение.
	code, first := call(app.postSupportMessage, quietReq("POST", "/support/messages", "manager", managerID, `{"body":"не открывается карточка","client_id":"c_flow_000001"}`))
	if code != 201 {
		t.Fatalf("first send code=%d body=%v", code, first)
	}
	threadID, _ := first["thread_id"].(string)
	firstMsg, _ := first["message"].(map[string]any)
	code, again := call(app.postSupportMessage, quietReq("POST", "/support/messages", "manager", managerID, `{"body":"не открывается карточка","client_id":"c_flow_000001"}`))
	againMsg, _ := again["message"].(map[string]any)
	if code != 200 || again["duplicate"] != true || againMsg["id"] != firstMsg["id"] {
		t.Fatalf("duplicate send code=%d body=%v", code, again)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM support_messages WHERE thread_id=$1::uuid`, threadID).Scan(&n)
	if n != 1 {
		t.Fatalf("messages after duplicate = %d", n)
	}

	// Список у админа: отдел, роль, непрочитанное.
	code, list := call(app.getSupport, quietReq("GET", "/support", "admin", adminID, ""))
	threads, _ := list["threads"].([]any)
	if code != 200 || len(threads) != 1 {
		t.Fatalf("admin list code=%d body=%v", code, list)
	}
	th, _ := threads[0].(map[string]any)
	if th["department"] != "Отдел Георгия" || th["role"] != "manager" || th["unread"] != float64(1) {
		t.Fatalf("thread row = %v", th)
	}

	// Открытие диалога обнуляет непрочитанное.
	code, _ = call(app.getSupportThread, quietWithID(quietReq("GET", "/support/threads/"+threadID, "admin", adminID, ""), threadID))
	_ = pool.QueryRow(ctx, `SELECT admin_unread FROM support_threads WHERE id=$1::uuid`, threadID).Scan(&n)
	if code != 200 || n != 0 {
		t.Fatalf("open thread code=%d admin_unread=%d", code, n)
	}

	// Реакции: ставит автор диалога, чужой не может, на служебную нельзя.
	msgID, _ := firstMsg["id"].(string)
	code, re := call(app.postSupportReaction, quietWithID(quietReq("POST", "/x", "admin", adminID, `{"reaction":"👀"}`), msgID))
	if code != 200 {
		t.Fatalf("admin reaction code=%d body=%v", code, re)
	}
	code, re = call(app.postSupportReaction, quietWithID(quietReq("POST", "/x", "manager", managerID, `{"reaction":"👀"}`), msgID))
	reactions, _ := re["reactions"].([]any)
	if code != 200 || len(reactions) != 1 || reactions[0].(map[string]any)["count"] != float64(2) {
		t.Fatalf("manager reaction code=%d body=%v", code, re)
	}
	code, _ = call(app.postSupportReaction, quietWithID(quietReq("POST", "/x", "manager", otherID, `{"reaction":"👍"}`), msgID))
	if code != 403 {
		t.Fatalf("foreign reaction code=%d", code)
	}
	code, re = call(app.postSupportReaction, quietWithID(quietReq("POST", "/x", "manager", managerID, `{"reaction":""}`), msgID))
	reactions, _ = re["reactions"].([]any)
	if code != 200 || len(reactions) != 1 || reactions[0].(map[string]any)["mine"] != false {
		t.Fatalf("remove reaction code=%d body=%v", code, re)
	}

	// «Решено» пишет служебную строку, повтор ничего не добавляет, сотрудник может не видеть кнопку.
	code, _ = call(app.postSupportResolve, quietWithID(quietReq("POST", "/x", "manager", managerID, `{"resolved":true}`), threadID))
	if code != 403 {
		t.Fatalf("manager resolve code=%d", code)
	}
	code, res := call(app.postSupportResolve, quietWithID(quietReq("POST", "/x", "admin", adminID, `{"resolved":true}`), threadID))
	sys, _ := res["message"].(map[string]any)
	if code != 200 || sys["kind"] != "system" || sys["body"] != "Админ закрыл обращение" {
		t.Fatalf("resolve code=%d body=%v", code, res)
	}
	code, res = call(app.postSupportResolve, quietWithID(quietReq("POST", "/x", "admin", adminID, `{"resolved":true}`), threadID))
	if _, has := res["message"]; code != 200 || has {
		t.Fatalf("second resolve code=%d body=%v", code, res)
	}
	code, _ = call(app.postSupportReaction, quietWithID(quietReq("POST", "/x", "admin", adminID, `{"reaction":"👍"}`), sys["id"].(string)))
	if code != 400 {
		t.Fatalf("reaction on system message code=%d", code)
	}

	// Новое сообщение сотрудника возвращает диалог в работу.
	code, _ = call(app.postSupportMessage, quietReq("POST", "/support/messages", "manager", managerID, `{"body":"опять не работает","client_id":"c_flow_000002"}`))
	var resolved bool
	_ = pool.QueryRow(ctx, `SELECT resolved_at IS NOT NULL FROM support_threads WHERE id=$1::uuid`, threadID).Scan(&resolved)
	if code != 201 || resolved {
		t.Fatalf("reopen code=%d resolved=%v", code, resolved)
	}

	// Страницы: 40 последних, затем остальные, без пересечений.
	if _, err := pool.Exec(ctx, `
		INSERT INTO support_messages (thread_id, author_id, body, created_at)
		SELECT $1::uuid, $2::uuid, 'старое '||g, now() - interval '1 day' + g * interval '1 second'
		FROM generate_series(1,45) g`, threadID, managerID); err != nil {
		t.Fatal(err)
	}
	code, page1 := call(app.getSupport, quietReq("GET", "/support?limit=40", "manager", managerID, ""))
	p1 := msgsOf(page1)
	if code != 200 || len(p1) != 40 || page1["has_more"] != true {
		t.Fatalf("page1 code=%d len=%d more=%v", code, len(p1), page1["has_more"])
	}
	oldest := p1[0].(map[string]any)["id"].(string)
	code, page2 := call(app.getSupport, quietReq("GET", "/support?limit=40&before="+oldest, "manager", managerID, ""))
	p2 := msgsOf(page2)
	if code != 200 || len(p2) != 48-40 || page2["has_more"] != false {
		t.Fatalf("page2 code=%d len=%d more=%v", code, len(p2), page2["has_more"])
	}
	seen := map[string]bool{}
	for _, m := range append(p1, p2...) {
		id := m.(map[string]any)["id"].(string)
		if seen[id] {
			t.Fatalf("message %s on both pages", id)
		}
		seen[id] = true
	}
	newest := p1[len(p1)-1].(map[string]any)["id"].(string)
	code, after := call(app.getSupport, quietReq("GET", "/support?after="+newest, "manager", managerID, ""))
	if code != 200 || len(msgsOf(after)) != 0 {
		t.Fatalf("after newest code=%d len=%d", code, len(msgsOf(after)))
	}
}

