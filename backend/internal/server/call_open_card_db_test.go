package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/forgecrm/server/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	callCardMgrA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1"
	callCardMgrB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbb2"
)

func TestOpenCallCardHard(t *testing.T) {
	ctx := context.Background()
	app, pool := startCallCardDB(t)
	app.ensureRecruitFunnels(ctx)

	t.Run("ringing does not create a lead", func(t *testing.T) {
		const phone = "79001000001"
		uid := "ring_nocard01"
		before := countLeads(t, ctx, pool)
		rec := postSoftphone(t, app, callCardMgrA, uid, "incoming", phone, "start", 0, "")
		if rec.Code != 200 {
			t.Fatalf("softphone start %d %s", rec.Code, rec.Body.String())
		}
		if got := countLeads(t, ctx, pool); got != before {
			t.Fatalf("ring created a lead: before=%d after=%d", before, got)
		}
		if got := countContacts(t, ctx, pool); got != 0 {
			t.Fatalf("ring created a contact: %d", got)
		}
		ent, eid := callEntity(t, ctx, pool, "softphone:voximplant:"+uid)
		if ent != "" || eid != "" {
			t.Fatalf("unmatched ring attached to %s %s", ent, eid)
		}
		rec = postSoftphone(t, app, callCardMgrA, uid, "incoming", phone, "end", 9, "no_reach")
		if rec.Code != 200 {
			t.Fatalf("softphone end %d %s", rec.Code, rec.Body.String())
		}
		if got := countLeads(t, ctx, pool); got != before {
			t.Fatalf("hangup created a lead: %d", got)
		}
		dur, disp := callTiming(t, ctx, pool, "softphone:voximplant:"+uid)
		if dur != 9 || disp != "NO_REACH" {
			t.Fatalf("call journal dur=%d disp=%s", dur, disp)
		}
	})

	t.Run("open during the call creates one lead and does not rewrite the call", func(t *testing.T) {
		const phone = "79001000002"
		uid := "live_open_02"
		postSoftphone(t, app, callCardMgrA, uid, "incoming", phone, "start", 0, "RINGING")
		_, _ = pool.Exec(ctx, `
			UPDATE calls SET duration_sec=4, disposition='RINGING'
			WHERE asterisk_uniqueid=$1`, "softphone:voximplant:"+uid)

		var wg sync.WaitGroup
		codes := make([]int, 12)
		bodies := make([]string, 12)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				rec := postOpen(app, callCardMgrA, "", uid, phone)
				codes[i] = rec.Code
				bodies[i] = rec.Body.String()
			}(i)
		}
		// Звонок идёт параллельно с кнопкой и не должен ждать карточку.
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rec := postSoftphone(t, app, callCardMgrA, uid, "incoming", phone, "start", 4, "RINGING")
				if rec.Code != 200 {
					t.Errorf("parallel ring %d %s", rec.Code, rec.Body.String())
				}
			}()
		}
		wg.Wait()

		var leadID string
		for i, code := range codes {
			if code != 200 {
				t.Fatalf("open %d: %d %s", i, code, bodies[i])
			}
			var out struct {
				LeadID string `json:"lead_id"`
			}
			if err := json.Unmarshal([]byte(bodies[i]), &out); err != nil || out.LeadID == "" {
				t.Fatalf("open %d body %s", i, bodies[i])
			}
			if leadID == "" {
				leadID = out.LeadID
			} else if out.LeadID != leadID {
				t.Fatalf("double click made another lead %s vs %s", leadID, out.LeadID)
			}
		}
		if n := countLeadsByPhone(t, ctx, pool, phone); n != 1 {
			t.Fatalf("leads for phone = %d", n)
		}
		owner, source, title, sleeping, stage := leadFacts(t, ctx, pool, leadID)
		if owner != callCardMgrA || source != "call" || title != phone || sleeping || stage != hireNewApplicationStage {
			t.Fatalf("lead owner=%s source=%s title=%s sleeping=%v stage=%s", owner, source, title, sleeping, stage)
		}
		if n := unownedNewLeads(t, ctx, pool); n != 0 {
			t.Fatalf("call card fell into new-lead distribution: %d", n)
		}
		dur, disp := callTiming(t, ctx, pool, "softphone:voximplant:"+uid)
		if dur != 4 || disp != "RINGING" {
			t.Fatalf("open rewrote the live call dur=%d disp=%s", dur, disp)
		}
		ent, eid := callEntity(t, ctx, pool, "softphone:voximplant:"+uid)
		if ent != "deal" || eid == "" {
			t.Fatalf("call not attached: %s %s", ent, eid)
		}

		rec := postSoftphone(t, app, callCardMgrA, uid, "incoming", phone, "end", 15, "reached")
		if rec.Code != 200 {
			t.Fatalf("end after card %d %s", rec.Code, rec.Body.String())
		}
		if n := countLeadsByPhone(t, ctx, pool, phone); n != 1 {
			t.Fatalf("end created another lead: %d", n)
		}
		dur, disp = callTiming(t, ctx, pool, "softphone:voximplant:"+uid)
		if dur != 15 || disp != "REACHED" {
			t.Fatalf("end journal dur=%d disp=%s", dur, disp)
		}
	})

	t.Run("someone elses lead is not duplicated", func(t *testing.T) {
		const phone = "79001000003"
		leadB := insertLiveLead(t, ctx, pool, app, callCardMgrB, phone)
		rec := postOpen(app, callCardMgrA, "", "", phone)
		if rec.Code != 409 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		if n := countLeadsByPhone(t, ctx, pool, phone); n != 1 {
			t.Fatalf("duplicate leads %d", n)
		}
		owner, _, _, _, _ := leadFacts(t, ctx, pool, leadB)
		if owner != callCardMgrB {
			t.Fatalf("owner changed to %s", owner)
		}
	})

	t.Run("sleeping card stays put", func(t *testing.T) {
		const phone = "79001000004"
		sleepID := insertSleepingLead(t, ctx, pool, phone)
		rec := postOpen(app, callCardMgrA, "", "", phone)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var out struct {
			LeadID string `json:"lead_id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out.LeadID == "" || out.LeadID == sleepID {
			t.Fatalf("opened sleeping lead %s", out.LeadID)
		}
		if n := countLeadsByPhone(t, ctx, pool, phone); n != 2 {
			t.Fatalf("want sleeping + new, got %d", n)
		}
		var still bool
		var pipe string
		if err := pool.QueryRow(ctx, `
			SELECT coalesce(d.sleeping,false), coalesce(p.bitrix_id,'')
			FROM deals d
			LEFT JOIN pipelines p ON p.id=d.pipeline_id
			WHERE d.lead_id=$1::uuid`, sleepID).Scan(&still, &pipe); err != nil {
			t.Fatal(err)
		}
		if !still || pipe != oldLeadsPipelineKey {
			t.Fatalf("sleeping card moved sleeping=%v pipe=%s", still, pipe)
		}
		_, _, _, sleeping, stage := leadFacts(t, ctx, pool, out.LeadID)
		if sleeping || stage != hireNewApplicationStage {
			t.Fatalf("new card sleeping=%v stage=%s", sleeping, stage)
		}
	})

	t.Run("lost lead does not block a new card", func(t *testing.T) {
		const phone = "79001000005"
		lostID := insertLostLead(t, ctx, pool, callCardMgrA, phone)
		rec := postOpen(app, callCardMgrA, "", "", phone)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var out struct {
			LeadID string `json:"lead_id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out.LeadID == "" || out.LeadID == lostID {
			t.Fatalf("reopened lost lead %s", out.LeadID)
		}
	})

	t.Run("own live lead is reused", func(t *testing.T) {
		const phone = "79001000006"
		id := insertLiveLead(t, ctx, pool, app, callCardMgrA, phone)
		rec := postOpen(app, callCardMgrA, "", "", phone)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), id) {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), `"created":true`) {
			t.Fatalf("reused lead was marked created: %s", rec.Body.String())
		}
		if n := countLeadsByPhone(t, ctx, pool, phone); n != 1 {
			t.Fatalf("leads %d", n)
		}
	})

	t.Run("does not steal a call already on another card", func(t *testing.T) {
		const phone = "79001000007"
		other := insertLiveLead(t, ctx, pool, app, callCardMgrA, "79001000077")
		var deal string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM deals WHERE lead_id=$1::uuid`, other).Scan(&deal); err != nil {
			t.Fatal(err)
		}
		var callID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO calls (asterisk_uniqueid, direction, from_number, to_number, user_id, entity_type, entity_id, duration_sec, disposition, provider)
			VALUES ('keep-other', 'inbound', $1, '79002000001', $2::uuid, 'deal', $3::uuid, 3, 'ANSWERED', 'voximplant')
			RETURNING id::text`, phone, callCardMgrA, deal).Scan(&callID); err != nil {
			t.Fatal(err)
		}
		rec := postOpen(app, callCardMgrA, callID, "", "")
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var ent, eid string
		if err := pool.QueryRow(ctx, `
			SELECT coalesce(entity_type,''), coalesce(entity_id::text,'')
			FROM calls WHERE id=$1::uuid`, callID).Scan(&ent, &eid); err != nil {
			t.Fatal(err)
		}
		if ent != "deal" || eid != deal {
			t.Fatalf("call moved to %s %s", ent, eid)
		}
	})

	t.Run("list button uses the call row not a spoofed peer", func(t *testing.T) {
		const phone = "79001000008"
		var callID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO calls (asterisk_uniqueid, direction, from_number, to_number, user_id, duration_sec, disposition, provider)
			VALUES ('list-out', 'outbound', '79002000002', $1, $2::uuid, 6, 'ANSWERED', 'voximplant')
			RETURNING id::text`, phone, callCardMgrA).Scan(&callID); err != nil {
			t.Fatal(err)
		}
		rec := postOpen(app, callCardMgrA, callID, "", "79009999999")
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var out struct {
			LeadID string `json:"lead_id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		_, _, title, _, _ := leadFacts(t, ctx, pool, out.LeadID)
		if title != phone {
			t.Fatalf("title %s, spoofed peer must be ignored", title)
		}
		if n := countLeadsByPhone(t, ctx, pool, "79009999999"); n != 0 {
			t.Fatalf("spoofed number created a lead")
		}
		rec = postOpen(app, callCardMgrB, callID, "", "")
		if rec.Code != 404 {
			t.Fatalf("foreign call code=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("short number", func(t *testing.T) {
		rec := postOpen(app, callCardMgrA, "", "", "101")
		if rec.Code != 400 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func startCallCardDB(t *testing.T) (*App, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, "postgres://forge:forge@127.0.0.1:55432/postgres?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if err := admin.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	const dbname = "callcard_hard_test"
	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1 AND pid <> pg_backend_pid()`, dbname)
	_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbname)
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbname); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = admin.Exec(cctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1 AND pid <> pg_backend_pid()`, dbname)
		_, _ = admin.Exec(cctx, `DROP DATABASE IF EXISTS `+dbname)
	})
	pool, err := pgxpool.New(ctx, "postgres://forge:forge@127.0.0.1:55432/"+dbname+"?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, schema.LocalSQL); err != nil {
		t.Fatalf("local schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		ALTER TABLE leads ADD COLUMN IF NOT EXISTS city TEXT NOT NULL DEFAULT '';
		ALTER TABLE leads ADD COLUMN IF NOT EXISTS card_note TEXT NOT NULL DEFAULT '';
		ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
		ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
		ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS short_name TEXT NOT NULL DEFAULT '';
		ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
		ALTER TABLE calls ADD COLUMN IF NOT EXISTS provider TEXT;
		ALTER TABLE deals ADD COLUMN IF NOT EXISTS sleeping BOOLEAN NOT NULL DEFAULT false;
		CREATE UNIQUE INDEX IF NOT EXISTS deals_one_per_lead_uidx ON deals (lead_id) WHERE lead_id IS NOT NULL;
	`); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM roles WHERE name='manager'`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct{ id, email, name string }{
		{callCardMgrA, "a@callcard.test", "Менеджер А"},
		{callCardMgrB, "b@callcard.test", "Менеджер Б"},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, full_name, role_id)
			VALUES ($1::uuid, $2, 'x', $3, $4::uuid)`, u.id, u.email, u.name, role); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewHub()
	go hub.Run()
	return &App{db: pool, hub: hub}, pool
}

func postSoftphone(t *testing.T, app *App, user, uid, dir, peer, phase string, dur int, disp string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"uid":%q,"provider":"vox","direction":%q,"peer":%q,"phase":%q,"duration_sec":%d,"disposition":%q}`,
		uid, dir, peer, phase, dur, disp)
	req := httptest.NewRequest(http.MethodPost, "/telephony/softphone-event", strings.NewReader(body))
	req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxUserID, user), ctxRole, "manager"))
	rec := httptest.NewRecorder()
	app.softphoneEvent(rec, req)
	return rec
}

func postOpen(app *App, user, callID, uid, peer string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"call_id":%q,"uid":%q,"peer":%q}`, callID, uid, peer)
	req := httptest.NewRequest(http.MethodPost, "/calls/open-card", strings.NewReader(body))
	req = req.WithContext(context.WithValue(context.WithValue(req.Context(), ctxUserID, user), ctxRole, "manager"))
	rec := httptest.NewRecorder()
	app.openCallCard(rec, req)
	return rec
}

func countLeads(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM leads`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countContacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contacts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countLeadsByPhone(t *testing.T, ctx context.Context, pool *pgxpool.Pool, phone string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM leads l
		JOIN contacts c ON c.id=l.contact_id
		WHERE right(regexp_replace(coalesce(c.phone,''), '[^0-9]', '', 'g'), 10) = right($1, 10)`, phone).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func callEntity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, uid string) (string, string) {
	t.Helper()
	var ent, eid string
	err := pool.QueryRow(ctx, `
		SELECT coalesce(entity_type,''), coalesce(entity_id::text,'')
		FROM calls WHERE asterisk_uniqueid=$1`, uid).Scan(&ent, &eid)
	if err != nil {
		t.Fatal(err)
	}
	return ent, eid
}

func callTiming(t *testing.T, ctx context.Context, pool *pgxpool.Pool, uid string) (int, string) {
	t.Helper()
	var dur int
	var disp string
	err := pool.QueryRow(ctx, `
		SELECT coalesce(duration_sec,0), coalesce(disposition,'')
		FROM calls WHERE asterisk_uniqueid=$1`, uid).Scan(&dur, &disp)
	if err != nil {
		t.Fatal(err)
	}
	return dur, disp
}

func leadFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, leadID string) (owner, source, title string, sleeping bool, stage string) {
	t.Helper()
	err := pool.QueryRow(ctx, `
		SELECT coalesce(l.owner_id::text,''), coalesce(l.source,''), coalesce(l.title,''),
		       coalesce(d.sleeping,false), coalesce(s.bitrix_id,'')
		FROM leads l
		LEFT JOIN deals d ON d.lead_id=l.id
		LEFT JOIN pipeline_stages s ON s.id=d.stage_id
		WHERE l.id=$1::uuid`, leadID).Scan(&owner, &source, &title, &sleeping, &stage)
	if err != nil {
		t.Fatal(err)
	}
	return owner, source, title, sleeping, stage
}

func unownedNewLeads(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM leads l
		JOIN deals d ON d.lead_id=l.id
		JOIN pipeline_stages s ON s.id=d.stage_id
		WHERE l.owner_id IS NULL
		  AND s.bitrix_id=$1`, hireNewApplicationStage).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func insertLiveLead(t *testing.T, ctx context.Context, pool *pgxpool.Pool, app *App, owner, phone string) string {
	t.Helper()
	id, _, _, err := app.ensureCallLead(ctx, crmScope{UserID: owner, IsManager: true}, phone)
	if err != nil || id == "" {
		t.Fatalf("seed lead %s: %v", phone, err)
	}
	return id
}

func insertSleepingLead(t *testing.T, ctx context.Context, pool *pgxpool.Pool, phone string) string {
	t.Helper()
	var contact, lead, pipe, stage string
	if err := pool.QueryRow(ctx, `
		INSERT INTO contacts (full_name, phone, source) VALUES ($1,$1,'old_excel') RETURNING id::text`, phone).Scan(&contact); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO leads (title, source, status, contact_id, city, card_note)
		VALUES ($1, 'old_excel', 'new', $2::uuid, '', '') RETURNING id::text`, "Спящий "+phone, contact).Scan(&lead); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM pipelines WHERE bitrix_id=$1`, oldLeadsPipelineKey).Scan(&pipe); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, oldLeadsPipelineKey+":1").Scan(&stage); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO deals (title, contact_id, lead_id, pipeline_id, stage_id, amount, sleeping)
		VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 0, true)`,
		"Спящий "+phone, contact, lead, pipe, stage); err != nil {
		t.Fatal(err)
	}
	_, _ = pool.Exec(ctx, `UPDATE leads SET converted_deal_id=(SELECT id FROM deals WHERE lead_id=$1::uuid) WHERE id=$1::uuid`, lead)
	return lead
}

func insertLostLead(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner, phone string) string {
	t.Helper()
	var contact, lead, pipe, stage string
	if err := pool.QueryRow(ctx, `
		INSERT INTO contacts (full_name, phone, source) VALUES ($1,$1,'manual') RETURNING id::text`, phone).Scan(&contact); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO leads (title, source, status, owner_id, contact_id, city, card_note)
		VALUES ($1, 'manual', 'new', $2::uuid, $3::uuid, '', '') RETURNING id::text`, phone, owner, contact).Scan(&lead); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM pipelines WHERE bitrix_id=$1`, hirePipelineKey).Scan(&pipe); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT id::text FROM pipeline_stages
		WHERE pipeline_id=$1::uuid AND is_lost
		ORDER BY sort_order LIMIT 1`, pipe).Scan(&stage); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO deals (title, contact_id, lead_id, pipeline_id, stage_id, amount, owner_id, sleeping)
		VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 0, $6::uuid, false)`,
		phone, contact, lead, pipe, stage, owner); err != nil {
		t.Fatal(err)
	}
	return lead
}
