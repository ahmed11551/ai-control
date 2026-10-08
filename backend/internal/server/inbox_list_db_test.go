package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Отдельный Postgres, не локальная и не боевая база.
// Проверяет, что менеджер видит только свои диалоги, старый номер всё ещё открывает свою карточку,
// а чужой и короткий номер карточку не подменяют.
func TestInboxListMatchesOldDialogsWithoutPhoneScan(t *testing.T) {
	ctx := context.Background()
	pool := startInboxListDB(t)
	app := &App{db: pool}
	createInboxListFixture(t, ctx, pool)

	inboxPhoneTailMu.Lock()
	inboxPhoneTailReady = false
	inboxPhoneTailMu.Unlock()
	t.Cleanup(func() {
		inboxPhoneTailMu.Lock()
		inboxPhoneTailReady = false
		inboxPhoneTailMu.Unlock()
	})

	app.ensureInboxPhoneTail(ctx)
	if !inboxPhoneTailReady {
		t.Fatal("колонка последних 10 цифр не создалась")
	}

	assertPhoneTails(t, ctx, pool)
	assertLeadLookupUsesIndex(t, ctx, pool)

	mgr := inboxReq(managerA, "manager", "/inbox?limit=100")
	sqlText, args, limit := inboxListQuery(mgr)
	if limit != 100 {
		t.Fatalf("limit %d", limit)
	}
	pageAt := strings.Index(sqlText, "FROM page")
	if pageAt < 0 {
		t.Fatal("список должен сначала отобрать страницу")
	}
	if strings.Contains(sqlText[:pageAt], "phone_tail") || strings.Contains(sqlText[:pageAt], "regexp_replace") {
		t.Fatal("отбор страницы снова ищет лид по всем номерам")
	}
	if !strings.Contains(sqlText[pageAt:], "phone_tail") {
		t.Fatal("у видимой страницы карточка больше не ищется")
	}
	countSQL, countArgs := inboxCountQuery(mgr)
	if strings.Contains(countSQL, "phone_tail") || strings.Contains(countSQL, "regexp_replace") {
		t.Fatal("счётчики снова сканируют номера")
	}

	started := time.Now()
	rows := queryInboxMaps(t, ctx, pool, sqlText, args)
	if time.Since(started) > 2*time.Second {
		t.Fatalf("список занял %s при тысячах чужих диалогов", time.Since(started).Round(time.Millisecond))
	}
	explainInboxPlan(t, ctx, pool, sqlText, args)
	_ = countArgs
	queryInboxMaps(t, ctx, pool, countSQL, countArgs)

	byID := indexInboxRows(rows)
	for _, id := range managerVisible {
		if _, ok := byID[id]; !ok {
			t.Fatalf("менеджер не видит свой диалог %s", id)
		}
	}
	for _, id := range managerHidden {
		if _, ok := byID[id]; ok {
			t.Fatalf("менеджер видит чужой или пустой диалог %s", id)
		}
	}
	if len(byID) != len(managerVisible) {
		t.Fatalf("в списке %d диалогов, нужно %d", len(byID), len(managerVisible))
	}

	own := byID[threadOwn]
	if own["lead_id"] != leadFresh || own["lead_title"] != "Свежая карточка" {
		t.Fatalf("старый номер открыл не свою карточку: %+v", own)
	}
	if own["phone"] != "+7 (916) 123-45-67" {
		t.Fatalf("номер диалога подменился: %v", own["phone"])
	}
	if own["last_body"] != "свежее сообщение" {
		t.Fatalf("последнее сообщение не то: %v", own["last_body"])
	}
	if byID[threadByContact]["lead_id"] != leadFresh {
		t.Fatalf("диалог того же контакта открыл не свежую карточку: %v", byID[threadByContact]["lead_id"])
	}
	if byID[threadShort]["lead_id"] != "" || byID[threadNine]["lead_id"] != "" {
		t.Fatalf("короткий номер прилип к чужой карточке: short=%v nine=%v", byID[threadShort]["lead_id"], byID[threadNine]["lead_id"])
	}
	if byID[threadDeal]["lead_id"] != "" || byID[threadDeal]["deal_id"] != dealA {
		t.Fatalf("диалог сделки: %+v", byID[threadDeal])
	}
	if byID[threadFile]["last_body"] != "https://cdn.example/a.jpg" {
		t.Fatalf("файл в sql: %v", byID[threadFile]["last_body"])
	}

	body := callInboxList(t, app, managerA, "manager", "/inbox?limit=100")
	if body.HasMore {
		t.Fatal("у менеджера есть ещё страница, хотя видны только его диалоги")
	}
	httpRows := indexInboxRows(body.Items)
	if httpRows[threadOwn]["lead_title"] != "Свежая карточка" {
		t.Fatalf("ответ списка: %+v", httpRows[threadOwn])
	}
	if httpRows[threadFile]["last_body"] != "Файл" {
		t.Fatalf("ссылка на файл должна показываться как файл: %v", httpRows[threadFile]["last_body"])
	}
	if _, ok := httpRows[threadForeign]; ok {
		t.Fatal("чужая линия с тем же номером попала менеджеру в список")
	}
	counts := map[string]int{}
	for _, c := range body.Counts {
		counts[c.Channel] = c.N
	}
	if counts["max"] != 8 || counts["whatsapp"] != 1 || len(counts) != 2 {
		t.Fatalf("счётчики %#v", counts)
	}

	page := callInboxList(t, app, managerA, "manager", "/inbox?limit=2")
	if !page.HasMore || inboxIDs(page.Items) != threadFile+","+threadWA {
		t.Fatalf("первая страница: more=%v ids=%s", page.HasMore, inboxIDs(page.Items))
	}
	nextQ := "/inbox?limit=2&before_last_at=" + url.QueryEscape(str(page.Items[1], "last_at")) +
		"&before_id=" + url.QueryEscape(str(page.Items[1], "id"))
	next := callInboxList(t, app, managerA, "manager", nextQ)
	if inboxIDs(next.Items) != threadSearch+","+threadNine {
		t.Fatalf("вторая страница: %s", inboxIDs(next.Items))
	}

	onlyWA := callInboxList(t, app, managerA, "manager", "/inbox?channel=whatsapp&limit=100")
	if inboxIDs(onlyWA.Items) != threadWA || onlyWA.HasMore {
		t.Fatalf("фильтр whatsapp: %s more=%v", inboxIDs(onlyWA.Items), onlyWA.HasMore)
	}
	found := callInboxList(t, app, managerA, "manager", "/inbox?q="+url.QueryEscape("ИщуВасю"))
	if inboxIDs(found.Items) != threadSearch {
		t.Fatalf("поиск своего диалога: %s", inboxIDs(found.Items))
	}
	hiddenSearch := callInboxList(t, app, managerA, "manager", "/inbox?q="+url.QueryEscape("секретчужого"))
	if len(hiddenSearch.Items) != 0 {
		t.Fatalf("поиск нашёл чужой диалог: %s", inboxIDs(hiddenSearch.Items))
	}

	other := callInboxList(t, app, managerB, "manager", "/inbox?limit=100")
	otherIDs := indexInboxRows(other.Items)
	if _, ok := otherIDs[threadOwn]; ok {
		t.Fatal("второй менеджер видит чужую линию")
	}
	if otherIDs[threadForeign]["lead_title"] != "Свежая карточка" {
		t.Fatalf("на своей линии старый номер не открыл карточку: %+v", otherIDs[threadForeign])
	}

	rop := callInboxList(t, app, ropUser, "rop", "/inbox?limit=100")
	if len(rop.Items) != 0 {
		t.Fatalf("роп без своих линий видит чужие диалоги: %s", inboxIDs(rop.Items))
	}
	admin := callInboxList(t, app, adminUser, "admin", "/inbox?q="+url.QueryEscape("секретчужого"))
	if inboxIDs(admin.Items) != threadForeign {
		t.Fatalf("админ не нашёл диалог: %s", inboxIDs(admin.Items))
	}
	logist := callInboxList(t, app, logistUser, "logist", "/inbox?q="+url.QueryEscape("секретчужого"))
	if inboxIDs(logist.Items) != threadForeign {
		t.Fatalf("логист не нашёл диалог: %s", inboxIDs(logist.Items))
	}

	if !app.canAccessInboxThread(inboxReq(managerA, "manager", "/inbox"), threadOwn) {
		t.Fatal("менеджер не может открыть свой диалог")
	}
	if app.canAccessInboxThread(inboxReq(managerA, "manager", "/inbox"), threadNoise) {
		t.Fatal("менеджер открывает чужой шумовой диалог")
	}
	opened := callInboxThread(t, app, managerA, "manager", threadOwn)
	if opened["lead_id"] != leadFresh || opened["lead_title"] != "Свежая карточка" {
		t.Fatalf("открытый диалог: lead=%v title=%v", opened["lead_id"], opened["lead_title"])
	}
	msgs, _ := opened["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatalf("в диалоге нет переписки: %#v", opened["messages"])
	}
	denied := callInboxThreadStatus(t, app, managerA, "manager", threadNoise)
	if denied != 404 {
		t.Fatalf("чужой диалог открылся, код %d", denied)
	}
}

const (
	managerA   = "a0000000-0000-4000-8000-000000000001"
	managerB   = "a0000000-0000-4000-8000-000000000002"
	ropUser    = "a0000000-0000-4000-8000-000000000003"
	logistUser = "a0000000-0000-4000-8000-000000000004"
	adminUser  = "a0000000-0000-4000-8000-000000000005"

	lineA    = "b0000000-0000-4000-8000-000000000001"
	lineB    = "b0000000-0000-4000-8000-000000000002"
	lineHist = "b0000000-0000-4000-8000-000000000003"

	contactOld   = "c0000000-0000-4000-8000-000000000001"
	contactDecoy = "c0000000-0000-4000-8000-000000000002"
	contactNine  = "c0000000-0000-4000-8000-000000000003"

	leadFresh = "d0000000-0000-4000-8000-000000000001"
	leadOld   = "d0000000-0000-4000-8000-000000000002"
	leadDecoy = "d0000000-0000-4000-8000-000000000003"
	leadNine  = "d0000000-0000-4000-8000-000000000004"
	dealA     = "e0000000-0000-4000-8000-000000000001"

	threadOwn       = "f0000000-0000-4000-8000-000000000001"
	threadForeign   = "f0000000-0000-4000-8000-000000000002"
	threadAuthored  = "f0000000-0000-4000-8000-000000000003"
	threadByContact = "f0000000-0000-4000-8000-000000000004"
	threadDeal      = "f0000000-0000-4000-8000-000000000005"
	threadShort     = "f0000000-0000-4000-8000-000000000006"
	threadNine      = "f0000000-0000-4000-8000-000000000007"
	threadSearch    = "f0000000-0000-4000-8000-000000000008"
	threadFile      = "f0000000-0000-4000-8000-000000000009"
	threadWA        = "f0000000-0000-4000-8000-00000000000a"
	threadEmpty     = "f0000000-0000-4000-8000-00000000000b"
	threadDemo      = "f0000000-0000-4000-8000-00000000000c"
	threadHistory   = "f0000000-0000-4000-8000-00000000000d"
	threadOutB      = "f0000000-0000-4000-8000-00000000000e"
	threadNoise     = "f0000000-0000-4000-8000-00000000000f"
)

var managerVisible = []string{
	threadOwn, threadAuthored, threadByContact, threadDeal,
	threadShort, threadNine, threadSearch, threadFile, threadWA,
}

var managerHidden = []string{
	threadForeign, threadEmpty, threadDemo, threadHistory, threadOutB, threadNoise,
}

func startInboxListDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dir := t.TempDir()
	binaries := filepath.Join("..", "..", ".pg-embed")
	if abs, err := filepath.Abs(binaries); err == nil {
		binaries = abs
	}
	const port = 55441
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Username("forge").
		Password("forge").
		Database("inboxlist").
		Port(port).
		BinariesPath(binaries).
		RuntimePath(filepath.Join(dir, "rt")).
		DataPath(filepath.Join(dir, "data")).
		StartTimeout(2 * time.Minute).
		Logger(io.Discard))
	if err := pg.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Stop() })
	pool, err := pgxpool.New(context.Background(), "postgres://forge:forge@127.0.0.1:"+strconv.Itoa(port)+"/inboxlist?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createInboxListFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE departments (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL,
			see_client_phones BOOLEAN NOT NULL DEFAULT false,
			see_inbox_files BOOLEAN NOT NULL DEFAULT false
		)`,
		`CREATE TABLE roles (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT UNIQUE,
			permissions JSONB
		)`,
		`CREATE TABLE users (
			id UUID PRIMARY KEY,
			full_name TEXT NOT NULL DEFAULT '',
			login TEXT NOT NULL DEFAULT '',
			department_id UUID,
			see_client_phones BOOLEAN NOT NULL DEFAULT false
		)`,
		`CREATE TABLE contacts (
			id UUID PRIMARY KEY,
			full_name TEXT NOT NULL DEFAULT '',
			phone TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE leads (
			id UUID PRIMARY KEY,
			title TEXT NOT NULL,
			contact_id UUID,
			owner_id UUID,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE deals (
			id UUID PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			contact_id UUID,
			owner_id UUID
		)`,
		`CREATE TABLE inbox_lines (
			id UUID PRIMARY KEY,
			provider TEXT NOT NULL,
			channel TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'personal',
			operator_label TEXT NOT NULL DEFAULT '',
			owner_user_id UUID,
			is_active BOOLEAN NOT NULL DEFAULT true,
			history_from TIMESTAMPTZ,
			UNIQUE (provider, channel, name)
		)`,
		`CREATE TABLE inbox_threads (
			id UUID PRIMARY KEY,
			channel TEXT NOT NULL,
			external_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			phone TEXT,
			contact_id UUID,
			deal_id UUID,
			line_id UUID,
			last_body TEXT NOT NULL DEFAULT '',
			last_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			unread INT NOT NULL DEFAULT 0,
			UNIQUE (channel, external_id)
		)`,
		`CREATE TABLE inbox_messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			thread_id UUID NOT NULL,
			direction TEXT NOT NULL,
			body TEXT NOT NULL,
			author_label TEXT NOT NULL DEFAULT '',
			author_id UUID,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			media_kind TEXT NOT NULL DEFAULT '',
			media_filename TEXT NOT NULL DEFAULT '',
			media_mime TEXT NOT NULL DEFAULT '',
			media_size BIGINT NOT NULL DEFAULT 0,
			media_storage_key TEXT NOT NULL DEFAULT '',
			media_wappi_id TEXT NOT NULL DEFAULT '',
			delivery_status TEXT NOT NULL DEFAULT '',
			media_sent_to_logist_at TIMESTAMPTZ
		)`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users (id, full_name, see_client_phones) VALUES
		($1::uuid, 'Менеджер А', true),
		($2::uuid, 'Менеджер Б', true),
		($3::uuid, 'РОП', true),
		($4::uuid, 'Логист', true),
		($5::uuid, 'Админ', true)`,
		managerA, managerB, ropUser, logistUser, adminUser)
	exec(`INSERT INTO inbox_lines (id, provider, channel, name, owner_user_id) VALUES
		($1::uuid, 'wappi', 'max', 'линия А', $2::uuid),
		($3::uuid, 'wappi', 'max', 'линия Б', $4::uuid)`,
		lineA, managerA, lineB, managerB)
	exec(`INSERT INTO inbox_lines (id, provider, channel, name, owner_user_id, history_from)
		VALUES ($1::uuid, 'wappi', 'max', 'линия история', $2::uuid, '2026-09-10T00:00:00Z')`,
		lineHist, managerA)
	exec(`INSERT INTO contacts (id, full_name, phone) VALUES
		($1::uuid, 'Старый контакт', '8 (916) 123-45-67'),
		($2::uuid, 'Похожий хвост', '7 000 012-34-567'),
		($3::uuid, 'Девять цифр', '7 912 345-67-89')`,
		contactOld, contactDecoy, contactNine)
	exec(`INSERT INTO leads (id, title, contact_id, owner_id, created_at) VALUES
		($1::uuid, 'Старая заявка', $2::uuid, $3::uuid, '2020-01-01T00:00:00Z'),
		($4::uuid, 'Свежая карточка', $2::uuid, $3::uuid, '2024-06-01T00:00:00Z'),
		($5::uuid, 'Ложное совпадение', $6::uuid, $7::uuid, '2025-01-01T00:00:00Z'),
		($8::uuid, 'Карточка девяти цифр', $9::uuid, $3::uuid, '2024-01-01T00:00:00Z')`,
		leadOld, contactOld, managerA,
		leadFresh,
		leadDecoy, contactDecoy, managerB,
		leadNine, contactNine)
	exec(`INSERT INTO deals (id, title, owner_id) VALUES ($1::uuid, 'Сделка менеджера', $2::uuid)`, dealA, managerA)

	type thread struct {
		id, channel, ext, title, phone, contact, deal, line, last string
	}
	threads := []thread{
		{threadOwn, "max", "own", "Свой", "+7 (916) 123-45-67", "", "", lineA, "2026-09-01T12:00:00Z"},
		{threadForeign, "max", "foreign", "Чужая линия", "+7 (916) 123-45-67", "", "", lineB, "2026-08-15T12:00:00Z"},
		{threadAuthored, "max", "authored", "Где я писал", "79030000001", "", "", lineB, "2026-09-02T12:00:00Z"},
		{threadByContact, "max", "contact", "Тот же контакт", "", contactOld, "", lineB, "2026-09-03T12:00:00Z"},
		{threadDeal, "max", "deal", "Сделка", "", "", dealA, lineB, "2026-09-04T12:00:00Z"},
		{threadShort, "max", "short", "Короткий", "1234567", "", "", lineA, "2026-09-05T12:00:00Z"},
		{threadNine, "max", "nine", "Девять", "123456789", "", "", lineA, "2026-09-06T12:00:00Z"},
		{threadSearch, "max", "search", "ИщуВасю", "79030000008", "", "", lineA, "2026-09-07T12:00:00Z"},
		{threadFile, "max", "file", "Файл", "79030000009", "", "", lineA, "2026-09-09T12:00:00Z"},
		{threadWA, "whatsapp", "wa", "Вотсап", "79030000010", "", "", lineA, "2026-09-08T12:00:00Z"},
		{threadEmpty, "max", "empty", "Пустой", "79030000011", "", "", lineA, "2026-09-09T13:00:00Z"},
		{threadDemo, "max", "demo", "Демо", "79030000012", "", "", lineA, "2026-09-09T14:00:00Z"},
		{threadHistory, "max", "hist", "Старая история", "79030000013", "", "", lineHist, "2026-09-01T08:00:00Z"},
		{threadOutB, "max", "outb", "Чужой исходящий", "79030000014", "", "", lineA, "2026-09-09T15:00:00Z"},
		{threadNoise, "max", "noise-one", "шум один", "78000000999", "", "", lineB, "2019-01-01T00:00:00Z"},
	}
	for _, th := range threads {
		exec(`INSERT INTO inbox_threads (id, channel, external_id, title, phone, contact_id, deal_id, line_id, last_at)
			VALUES ($1::uuid, $2, $3, $4, NULLIF($5,''), NULLIF($6,'')::uuid, NULLIF($7,'')::uuid, $8::uuid, $9::timestamptz)`,
			th.id, th.channel, th.ext, th.title, th.phone, th.contact, th.deal, th.line, th.last)
	}
	exec(`INSERT INTO inbox_messages (thread_id, direction, body, created_at) VALUES
		($1::uuid, 'inbound', 'первое', '2026-09-01T10:00:00Z'),
		($1::uuid, 'inbound', 'свежее сообщение', '2026-09-01T11:00:00Z')`, threadOwn)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body, created_at) VALUES ($1::uuid, 'inbound', 'секретчужого', '2026-08-15T12:00:00Z')`, threadForeign)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body, author_id, created_at) VALUES ($1::uuid, 'outbound', 'я писал', $2::uuid, '2026-09-02T12:00:00Z')`, threadAuthored, managerA)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', 'контакт')`, threadByContact)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', 'сделка')`, threadDeal)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', 'короткий')`, threadShort)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', 'девять')`, threadNine)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', 'обычный текст')`, threadSearch)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', 'https://cdn.example/a.jpg')`, threadFile)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', 'вотсап')`, threadWA)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', '__DEMO_PASSPORT__')`, threadDemo)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body, created_at) VALUES ($1::uuid, 'inbound', 'до обрезки', '2026-09-01T08:00:00Z')`, threadHistory)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body, author_id) VALUES ($1::uuid, 'outbound', 'исходящее', $2::uuid)`, threadOutB, managerB)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body) VALUES ($1::uuid, 'inbound', 'шум')`, threadNoise)

	exec(`INSERT INTO contacts (id, full_name, phone)
		SELECT gen_random_uuid(), 'шум', '7800' || lpad(i::text, 7, '0')
		FROM generate_series(1, 4000) i`)
	exec(`INSERT INTO inbox_threads (id, channel, external_id, title, phone, line_id, last_at)
		SELECT gen_random_uuid(), 'max', 'noise-' || i, 'шум', '7810' || lpad(i::text, 7, '0'), $1::uuid, '2019-01-01T00:00:00Z'
		FROM generate_series(1, 800) i`, lineB)
	exec(`INSERT INTO inbox_messages (thread_id, direction, body)
		SELECT id, 'inbound', 'шумное сообщение'
		FROM inbox_threads WHERE title = 'шум'`)
}

func assertPhoneTails(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var tail, formatted, short, nine, blank string
	err := pool.QueryRow(ctx, `
		SELECT phone_tail_10('+7 (916) 123-45-67'),
		       phone_tail_10('8 916 123 45 67'),
		       coalesce(phone_tail_10('1234567'), ''),
		       coalesce(phone_tail_10('123456789'), ''),
		       coalesce(phone_tail_10(NULL), '')`).Scan(&tail, &formatted, &short, &nine, &blank)
	if err != nil {
		t.Fatal(err)
	}
	if tail != "9161234567" || formatted != tail || short != "" || nine != "" || blank != "" {
		t.Fatalf("хвосты: same=%s/%s short=%q nine=%q blank=%q", tail, formatted, short, nine, blank)
	}
	var missing int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM contacts
		WHERE length(regexp_replace(coalesce(phone,''), '[^0-9]', '', 'g')) >= 10
		  AND phone_tail IS NULL`).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("у %d старых контактов хвост не заполнился", missing)
	}
	var threadTail string
	if err := pool.QueryRow(ctx, `SELECT coalesce(phone_tail,'') FROM inbox_threads WHERE id=$1::uuid`, threadOwn).Scan(&threadTail); err != nil {
		t.Fatal(err)
	}
	if threadTail != "9161234567" {
		t.Fatalf("хвост старого диалога %q", threadTail)
	}
	var shortTail string
	if err := pool.QueryRow(ctx, `SELECT coalesce(phone_tail,'') FROM inbox_threads WHERE id=$1::uuid`, threadShort).Scan(&shortTail); err != nil {
		t.Fatal(err)
	}
	if shortTail != "" {
		t.Fatalf("короткий номер получил хвост %q", shortTail)
	}
}

func assertLeadLookupUsesIndex(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var indexes int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE indexname IN ('idx_contacts_phone_tail', 'idx_leads_contact_id')`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 2 {
		t.Fatalf("индексов %d", indexes)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN SELECT id FROM contacts WHERE phone_tail = $1`, "9161234567")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if !strings.Contains(plan.String(), "idx_contacts_phone_tail") {
		t.Fatalf("поиск по хвосту не идёт в индекс:\n%s", plan.String())
	}
}

func explainInboxPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sqlText string, args []any) {
	t.Helper()
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, FORMAT TEXT) "+sqlText, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	text := plan.String()
	if strings.Contains(text, "regexp_replace") {
		t.Fatalf("план снова сканирует номера:\n%s", text)
	}
	if !strings.Contains(text, "Execution Time:") {
		t.Fatalf("нет времени плана:\n%s", text)
	}
}

func queryInboxMaps(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sqlText string, args []any) []map[string]any {
	t.Helper()
	rows, err := pool.Query(ctx, sqlText, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	fds := rows.FieldDescriptions()
	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		for i, f := range fds {
			m[string(f.Name)] = stringify(vals[i])
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func indexInboxRows(rows []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, row := range rows {
		out[str(row, "id")] = row
	}
	return out
}

func inboxIDs(rows []map[string]any) string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, str(row, "id"))
	}
	return strings.Join(ids, ",")
}

type inboxListBody struct {
	Items   []map[string]any `json:"items"`
	HasMore bool             `json:"has_more"`
	Counts  []struct {
		Channel string `json:"channel"`
		N       int    `json:"n"`
	} `json:"counts"`
}

func inboxReq(user, role, rawURL string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	ctx := context.WithValue(req.Context(), ctxUserID, user)
	ctx = context.WithValue(ctx, ctxRole, role)
	return req.WithContext(ctx)
}

func callInboxList(t *testing.T, app *App, user, role, rawURL string) inboxListBody {
	t.Helper()
	req := inboxReq(user, role, rawURL)
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	app.listInbox(rr, req)
	if rr.Code != 200 {
		t.Fatalf("список %s: %d %s", rawURL, rr.Code, rr.Body.String())
	}
	var body inboxListBody
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func callInboxThread(t *testing.T, app *App, user, role, id string) map[string]any {
	t.Helper()
	rr := recordInboxThread(t, app, user, role, id)
	if rr.Code != 200 {
		t.Fatalf("диалог %s: %d %s", id, rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func callInboxThreadStatus(t *testing.T, app *App, user, role, id string) int {
	t.Helper()
	return recordInboxThread(t, app, user, role, id).Code
}

func recordInboxThread(t *testing.T, app *App, user, role, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := inboxReq(user, role, "/inbox/"+id)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	app.getInboxThread(rr, req)
	return rr
}
