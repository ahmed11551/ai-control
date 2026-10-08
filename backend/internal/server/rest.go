package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var openLeads, openDeals, openTasks, calls24 int64
	var pipeline float64
	_ = a.db.QueryRow(ctx, `SELECT count(*) FROM leads WHERE status NOT IN ('qualified','rejected')`).Scan(&openLeads)
	_ = a.db.QueryRow(ctx, `SELECT count(*) FROM deals d JOIN pipeline_stages s ON s.id=d.stage_id WHERE s.is_won=false AND s.is_lost=false`).Scan(&openDeals)
	_ = a.db.QueryRow(ctx, `SELECT coalesce(sum(amount),0) FROM deals d JOIN pipeline_stages s ON s.id=d.stage_id WHERE s.is_won=false AND s.is_lost=false`).Scan(&pipeline)
	_ = a.db.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE status <> 'completed'`).Scan(&openTasks)
	_ = a.db.QueryRow(ctx, `SELECT count(*) FROM calls WHERE started_at > now() - interval '24 hours'`).Scan(&calls24)
	writeJSON(w, 200, map[string]any{
		"open_leads": openLeads, "open_deals": openDeals, "pipeline_amount": pipeline,
		"open_tasks": openTasks, "calls_24h": calls24,
	})
}

func (a *App) listPipelines(w http.ResponseWriter, r *http.Request) {
	a.ensureRecruitFunnels(r.Context())
	a.ensureLogisticsSchema(r.Context())
	rows, err := a.db.Query(r.Context(), `
		SELECT p.id::text, p.name, p.kind, coalesce(p.bitrix_id,''),
		       coalesce(json_agg(json_build_object(
		         'id', s.id::text,
		         'name', s.name,
		         'short_name', coalesce(nullif(s.short_name,''), s.name),
		         'description', coalesce(s.description,''),
		         'sort_order', s.sort_order,
		         'is_won', s.is_won,
		         'is_lost', s.is_lost,
		         'probability', s.probability
		       ) ORDER BY s.sort_order) FILTER (WHERE s.id IS NOT NULL), '[]')
		FROM pipelines p
		LEFT JOIN pipeline_stages s ON s.pipeline_id=p.id
		GROUP BY p.id
		ORDER BY CASE
			WHEN p.bitrix_id='local:hire' THEN 0
			WHEN p.bitrix_id='local:escort' THEN 1
			ELSE 2
		END, p.name`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []map[string]any{}
	// Escort — кабинет логиста; РОП/менеджер работают только в «Адаптации».
	hideEscort := isManager(r) || isROP(r)
	hideHire := isLogist(r)
	for rows.Next() {
		var id, name, kind, key, stages string
		if err := rows.Scan(&id, &name, &kind, &key, &stages); err != nil {
			continue
		}
		if hideEscort && (key == escortPipelineKey || name == "Оформление и сопровождение") {
			continue
		}
		if hideHire && (key == hirePipelineKey || name == "Адаптация" || name == "Найм") {
			continue
		}
		if key == oldLeadsPipelineKey && !a.userCanSeeOldLeads(r) {
			continue
		}
		list = append(list, map[string]any{"id": id, "name": name, "kind": kind, "key": key, "stages": jsonRaw(stages)})
	}
	writeJSON(w, 200, list)
}

func jsonRaw(s string) any {
	if s == "" {
		return []any{}
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return v
}

func (a *App) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 200, map[string]any{"contacts": []any{}, "companies": []any{}, "deals": []any{}, "leads": []any{}})
		return
	}
	like := "%" + q + "%"
	ctx := r.Context()
	out := map[string]any{}
	s := a.crmScope(r)
	if s.All {
		out["contacts"] = a.queryMaps(ctx, `SELECT id::text, full_name AS title, phone, email FROM contacts WHERE full_name ILIKE $1 OR phone ILIKE $1 OR email ILIKE $1 LIMIT 10`, like)
		out["companies"] = a.queryMaps(ctx, `SELECT id::text, name AS title, inn, phone FROM companies WHERE name ILIKE $1 OR inn ILIKE $1 LIMIT 10`, like)
		out["deals"] = a.queryMaps(ctx, `SELECT id::text, title, amount::text FROM deals WHERE title ILIKE $1 LIMIT 10`, like)
		out["leads"] = a.queryMaps(ctx, `SELECT id::text, title, status FROM leads WHERE title ILIKE $1 LIMIT 10`, like)
	} else {
		out["contacts"] = a.queryMaps(ctx, `
			SELECT c.id::text, c.full_name AS title, c.phone, c.email FROM contacts c
			WHERE (c.full_name ILIKE $1 OR c.phone ILIKE $1 OR c.email ILIKE $1)
			  AND (
				c.owner_id=$2::uuid`+contactDeptOr(s)+`
				OR EXISTS (SELECT 1 FROM deals d WHERE d.contact_id=c.id AND (`+ownerOrDept("d", s)+`))
				OR EXISTS (SELECT 1 FROM leads l WHERE l.contact_id=c.id AND (`+ownerOrDept("l", s)+`))
			  ) LIMIT 10`, searchACLArgs(like, s)...)
		out["companies"] = a.queryMaps(ctx, `
			SELECT c.id::text, c.name AS title, c.inn, c.phone FROM companies c
			WHERE (c.name ILIKE $1 OR c.inn ILIKE $1) AND (`+ownerOrDept("c", s)+`
				OR EXISTS (SELECT 1 FROM deals d WHERE d.company_id=c.id AND (`+ownerOrDept("d", s)+`))
			) LIMIT 10`, searchACLArgs(like, s)...)
		dealSQL := `SELECT d.id::text, d.title, d.amount::text FROM deals d WHERE d.title ILIKE $1 AND (` + ownerOrDept("d", s) + `)`
		dealArgs := searchACLArgs(like, s)
		if s.IsManager {
			dealSQL = `SELECT d.id::text, d.title, d.amount::text FROM deals d
				JOIN pipelines p ON p.id=d.pipeline_id
				WHERE d.title ILIKE $1 AND (` + ownerOrDept("d", s) + `)
				  AND coalesce(p.bitrix_id,'') <> $` + strconv.Itoa(len(dealArgs)+1)
			dealArgs = append(dealArgs, escortPipelineKey)
		}
		out["deals"] = a.queryMaps(ctx, dealSQL+` LIMIT 10`, dealArgs...)
		out["leads"] = a.queryMaps(ctx, `SELECT id::text, title, status FROM leads l WHERE title ILIKE $1 AND (`+ownerOrDept("l", s)+`) LIMIT 10`, searchACLArgs(like, s)...)
	}
	a.writePhones(w, r, 200, out)
}

func (a *App) queryMaps(ctx context.Context, sql string, args ...any) []map[string]any {
	rows, err := a.db.Query(ctx, sql, args...)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	fds := rows.FieldDescriptions()
	var res []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		m := map[string]any{}
		for i, f := range fds {
			m[string(f.Name)] = stringify(vals[i])
		}
		res = append(res, m)
	}
	if res == nil {
		res = []map[string]any{}
	}
	return res
}

func stringify(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	default:
		return t
	}
}

func (a *App) listContacts(w http.ResponseWriter, r *http.Request) {
	q, limit, offset := parseListACL(r, 20, 200, 50)
	order := parseSort(r, map[string]string{
		"full_name": "c.full_name", "phone": "c.phone", "email": "c.email",
		"company": "co.name", "source": "c.source", "created_at": "c.created_at",
	}, "c.created_at")
	ctx := r.Context()
	from := ` FROM contacts c LEFT JOIN companies co ON co.id=c.company_id`
	selectSQL := `SELECT c.id::text, c.full_name, c.phone, c.email, c.source, c.position,
		c.company_id::text, coalesce(co.name,'') AS company, c.created_at, c.owner_id::text` + from
	where, args := []string{}, []any{}
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	where = a.crmScope(r).applyOwner("c", where, next)
	if wantMine(r) {
		where = append(where, "c.owner_id = "+next(actorID(r))+"::uuid")
	}
	if q != "" {
		p := next(likePattern(q))
		where = append(where, "(c.full_name ILIKE "+p+" OR c.phone ILIKE "+p+" OR coalesce(c.email,'') ILIKE "+p+" OR coalesce(co.name,'') ILIKE "+p+")")
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	countArgs := append([]any{}, args...)
	args = append(args, limit, offset)
	a.writePage(w, r, a.queryMaps(ctx, selectSQL+clause+" ORDER BY "+order+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...),
		a.queryInt(ctx, `SELECT count(*)`+from+clause, countArgs...), limit, offset)
}

func (a *App) createContact(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO contacts (full_name, phone, email, position, source, company_id, owner_id)
		VALUES ($1,$2,$3,$4,coalesce(nullif($5,''),'manual'), nullif($6,'')::uuid, $7::uuid)
		RETURNING id::text`,
		str(in, "full_name"), str(in, "phone"), str(in, "email"), str(in, "position"), str(in, "source"), str(in, "company_id"), actorID(r),
	).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.logAct(r.Context(), "contact", id, actorID(r), "created", in)
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) ensureContactByPhone(ctx context.Context, phone, name string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	norm := normalizePhone(phone)
	var id string
	if norm != "" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM contacts
			WHERE regexp_replace(coalesce(phone,''),'[^0-9]','','g') LIKE '%'||$1
			ORDER BY created_at DESC LIMIT 1`, norm).Scan(&id)
		if id != "" {
			return id
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = phone
	}
	_ = a.db.QueryRow(ctx, `
		INSERT INTO contacts (full_name, phone, source)
		VALUES ($1,$2,'auto')
		RETURNING id::text`, name, phone).Scan(&id)
	return id
}

func (a *App) patchContact(w http.ResponseWriter, r *http.Request) {
	if hideIfMissing(w, a.contactVisible(r.Context(), a.crmScope(r), chi.URLParam(r, "id"))) {
		return
	}
	// phone нет в списке: номер контакта меняется только правкой в базе.
	a.patchTable(w, r, "contacts", []string{"full_name", "email", "position", "source", "company_id"})
}

func (a *App) listCompanies(w http.ResponseWriter, r *http.Request) {
	q, limit, offset := parseListACL(r, 20, 200, 50)
	order := parseSort(r, map[string]string{
		"name": "name", "inn": "inn", "phone": "phone", "email": "email",
		"website": "website", "created_at": "created_at",
	}, "created_at")
	ctx := r.Context()
	selectSQL := `SELECT id::text, name, inn, website, phone, email, address, created_at, owner_id::text FROM companies`
	where, args := []string{}, []any{}
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	where = a.crmScope(r).applyOwner("", where, next)
	if wantMine(r) {
		where = append(where, "owner_id = "+next(actorID(r))+"::uuid")
	}
	if q != "" {
		p := next(likePattern(q))
		where = append(where, "(name ILIKE "+p+" OR inn ILIKE "+p+" OR coalesce(phone,'') ILIKE "+p+" OR coalesce(email,'') ILIKE "+p+")")
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	countArgs := append([]any{}, args...)
	args = append(args, limit, offset)
	a.writePage(w, r, a.queryMaps(ctx, selectSQL+clause+" ORDER BY "+order+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...),
		a.queryInt(ctx, `SELECT count(*) FROM companies`+clause, countArgs...), limit, offset)
}

func (a *App) createCompany(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = readJSON(r, &in)
	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO companies (name, inn, website, phone, email, address, owner_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7::uuid) RETURNING id::text`,
		str(in, "name"), str(in, "inn"), str(in, "website"), str(in, "phone"), str(in, "email"), str(in, "address"), actorID(r)).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) patchCompany(w http.ResponseWriter, r *http.Request) {
	if hideIfMissing(w, a.companyVisible(r.Context(), a.crmScope(r), chi.URLParam(r, "id"))) {
		return
	}
	a.patchTable(w, r, "companies", []string{"name", "inn", "website", "phone", "email", "address"})
}

func (a *App) listLeads(w http.ResponseWriter, r *http.Request) {
	a.ensureLogisticsSchema(r.Context())
	q, limit, offset := parseListACL(r, 20, 100, 50)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	order := parseSort(r, map[string]string{
		"title": "l.title", "status": "l.status", "source": "l.source", "amount": "l.amount",
		"contact": "c.full_name", "company": "co.name", "created_at": "l.created_at",
	}, "l.created_at")
	ctx := r.Context()
	from := `
		FROM leads l
		LEFT JOIN contacts c ON c.id=l.contact_id
		LEFT JOIN companies co ON co.id=l.company_id
		LEFT JOIN users u ON u.id=l.owner_id`
	selectSQL := `SELECT l.id::text, l.title, l.status, l.source, l.amount::text, l.vk_ads_campaign_id, l.created_at,
		       l.contact_id::text, l.company_id::text, l.converted_deal_id::text, l.owner_id::text,
		       coalesce(l.city,'') AS city, coalesce(l.card_note,'') AS card_note,
		       coalesce(c.full_name,'') AS contact, coalesce(c.phone,'') AS contact_phone,
		       coalesce(co.name,'') AS company, coalesce(u.full_name,'') AS owner` + from
	where, args := []string{}, []any{}
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	where = a.crmScope(r).applyOwner("l", where, next)
	if wantMine(r) {
		where = append(where, "l.owner_id = "+next(actorID(r))+"::uuid")
	}
	if strings.TrimSpace(r.URL.Query().Get("queue")) == "1" {
		// лиды ещё у РОПа — не назначены менеджеру
		where = append(where, `l.owner_id IN (
			SELECT u.id FROM users u JOIN roles r ON r.id=u.role_id WHERE lower(r.name)='rop'
		)`)
	}
	if status != "" {
		where = append(where, "l.status = "+next(status))
	}
	if days := periodDays(r); days > 0 {
		where = append(where, "l.created_at >= now() - ("+next(days)+"::int * interval '1 day')")
	}
	if q != "" {
		p := next(likePattern(q))
		where = append(where, "(l.title ILIKE "+p+" OR coalesce(c.full_name,'') ILIKE "+p+" OR coalesce(co.name,'') ILIKE "+p+" OR coalesce(c.phone,'') ILIKE "+p+")")
	}
	if !isAdmin(r) {
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM deals vd
			WHERE coalesce(vd.visual_only,false)
			  AND (vd.lead_id=l.id OR vd.id=l.converted_deal_id)
		)`)
	}
	where = append(where, sqlLeadNotOldLeads("l"))
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	countArgs := append([]any{}, args...)
	args = append(args, limit, offset)
	a.writePage(w, r, a.queryMaps(ctx, selectSQL+clause+" ORDER BY "+order+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...),
		a.queryInt(ctx, `SELECT count(*) `+from+clause, countArgs...), limit, offset)
}

func (a *App) createLead(w http.ResponseWriter, r *http.Request) {
	if isManager(r) {
		writeJSON(w, 403, map[string]string{"error": "менеджер не может создавать лиды — их назначает РОП"})
		return
	}
	var in map[string]any
	_ = readJSON(r, &in)
	id, err := a.insertLead(r.Context(), in, actorID(r))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) insertLead(ctx context.Context, in map[string]any, ownerID string) (string, error) {
	title := str(in, "title")
	if title == "" {
		title = str(in, "full_name")
		if title == "" {
			title = "Лид " + time.Now().Format("02.01 15:04")
		}
	}
	candidate := strings.TrimSpace(nz(str(in, "candidate"), nz(str(in, "contact_name"), str(in, "full_name"))))
	if candidate == "" {
		candidate = title
	}
	city := strings.TrimSpace(str(in, "city"))
	cardNote := strings.TrimSpace(nz(str(in, "card_note"), str(in, "comment")))
	contactID := str(in, "contact_id")
	// Один телефон — один живой лид. Повторная заявка не создаёт второй лид у другого менеджера,
	// а пишется в историю существующего (кроме backfill лида под уже существующую сделку).
	if !truthy(in["skip_deal"]) {
		if dupID := a.activeLeadByPhone(ctx, nz(str(in, "phone"), a.contactPhone(ctx, contactID))); dupID != "" {
			a.logAct(ctx, "lead", dupID, ownerID, "duplicate_application", map[string]any{
				"title":  title,
				"source": nz(str(in, "source"), "manual"),
				"note":   cardNote,
			})
			// Повторная заявка на бесхозный лид — сразу в раздачу.
			var own string
			_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1::uuid`, dupID).Scan(&own)
			if own == "" {
				a.maybeAutoDistribute(ctx, "", dupID, nz(str(in, "source"), "site"), ownerID)
			}
			a.broadcastDealUpdatedForLead(ctx, dupID)
			return dupID, nil
		}
	}
	if contactID == "" {
		if phone := str(in, "phone"); phone != "" {
			contactID = a.ensureContactByPhone(ctx, phone, candidate)
		}
	} else if candidate != "" && candidate != title {
		_, _ = a.db.Exec(ctx, `
			UPDATE contacts SET full_name=$2, updated_at=now()
			WHERE id=$1::uuid AND (full_name='' OR full_name=phone OR full_name=$3)`,
			contactID, candidate, title)
	}
	// The INSERT trigger assigns identity atomically.
	candidateID := ""
	var id string
	err := a.db.QueryRow(ctx, `
		INSERT INTO leads (title, source, status, amount, vk_ads_campaign_id, vk_click_id, vk_lead_id, owner_id, contact_id, company_id, candidate_id, city, card_note)
		VALUES ($1, coalesce(nullif($2,''),'manual'), coalesce(nullif($3,''),'new'), nullif($4,'')::numeric, nullif($5,''), nullif($6,''), nullif($7,''), nullif($8,'')::uuid, nullif($9,'')::uuid, nullif($10,'')::uuid, nullif($11,'')::uuid, coalesce($12,''), coalesce($13,''))
		RETURNING id::text`,
		title, str(in, "source"), str(in, "status"), str(in, "amount"),
		str(in, "vk_ads_campaign_id"), str(in, "vk_click_id"), str(in, "vk_lead_id"),
		nz(ownerID, str(in, "owner_id")), contactID, str(in, "company_id"), candidateID, city, cardNote,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	a.logAct(ctx, "lead", id, ownerID, "created", in)
	a.runAutomations(ctx, "lead.created", map[string]any{"id": id, "source": str(in, "source")})
	a.hub.Broadcast("lead.created", map[string]any{"id": id, "title": title})
	if ownerID == "" {
		a.maybeAutoDistribute(ctx, "", id, str(in, "source"), ownerID)
	} else {
		a.maybeDistributeMskTowerLead(ctx, id, ownerID)
	}
	// После раздачи owner уже в БД — сделку сразу в канбан с этим owner.
	var finalOwner string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1::uuid`, id).Scan(&finalOwner)
	if finalOwner == "<nil>" {
		finalOwner = ""
	}
	a.enqueueLeadAIEvent(ctx, "lead.created", id, ownerID, 1, map[string]any{
		"title":       title,
		"status":      nz(str(in, "status"), "new"),
		"source":      nz(str(in, "source"), "manual"),
		"city":        city,
		"owner_id":    finalOwner,
		"next_action": "",
		"card_note":   cardNote,
	})
	if !truthy(in["skip_deal"]) {
		dealID := a.ensureDealForLead(ctx, id, finalOwner, in)
		if finalOwner != "" {
			a.syncDealsOwnerForLead(ctx, id, finalOwner)
		}
		if dealID != "" {
			a.broadcastDealCreated(ctx, dealID, map[string]any{"owner_id": finalOwner, "lead_id": id})
		}
	}
	return id, nil
}

// ensureDealForLead — сделка в «Адаптации» для лида (строго одна на лид).
func (a *App) ensureDealForLead(ctx context.Context, leadID, ownerID string, in map[string]any) string {
	leadID = strings.TrimSpace(leadID)
	if leadID == "" {
		return ""
	}

	// Быстрый путь без транзакции: уже есть сделка
	var existing string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(
			nullif((SELECT converted_deal_id::text FROM leads WHERE id=$1::uuid), ''),
			(SELECT d.id::text FROM deals d WHERE d.lead_id=$1::uuid ORDER BY d.created_at ASC LIMIT 1),
			''
		)`, leadID).Scan(&existing)
	if existing != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE leads SET converted_deal_id=coalesce(converted_deal_id, $2::uuid) WHERE id=$1::uuid`, leadID, existing)
		if ownerID != "" {
			_, _ = a.db.Exec(ctx, `
				UPDATE deals SET owner_id=$2::uuid, updated_at=now()
				WHERE id=$1::uuid AND (owner_id IS NULL OR owner_id IS DISTINCT FROM $2::uuid)`,
				existing, ownerID)
		}
		return existing
	}

	pipe, stage := a.hireStart(ctx)
	if pipe == "" || stage == "" {
		return ""
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return ""
	}
	defer tx.Rollback(ctx)

	// Блокировка по лиду — abs(int4) падает на INT_MIN, поэтому сначала ::bigint.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(abs(hashtext($1::text)::bigint))`, leadID); err != nil {
		return ""
	}

	existing = ""
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(
			nullif((SELECT converted_deal_id::text FROM leads WHERE id=$1::uuid), ''),
			(SELECT d.id::text FROM deals d WHERE d.lead_id=$1::uuid ORDER BY d.created_at ASC LIMIT 1),
			''
		)`, leadID).Scan(&existing); err != nil {
		return ""
	}
	if existing != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE leads SET converted_deal_id=coalesce(converted_deal_id, $2::uuid) WHERE id=$1::uuid`, leadID, existing); err != nil {
			return ""
		}
		if err := tx.Commit(ctx); err != nil {
			return ""
		}
		return existing
	}

	var title, candidateID string
	var amount *float64
	var contactID, companyID, campaign, click, owner *string
	err = tx.QueryRow(ctx, `
		SELECT title, amount, contact_id::text, company_id::text, vk_ads_campaign_id, vk_click_id, owner_id::text, coalesce(candidate_id::text,'')
		FROM leads WHERE id=$1::uuid FOR UPDATE`, leadID).
		Scan(&title, &amount, &contactID, &companyID, &campaign, &click, &owner, &candidateID)
	if err != nil {
		return ""
	}
	if title == "" {
		title = str(in, "title")
	}
	if ownerID == "" && owner != nil {
		ownerID = *owner
	}
	if candidateID == "" {
		candidateID, err = ensureCandidateForLead(ctx, tx, leadID)
		if err != nil {
			return ""
		}
	}
	dealAmount := 0.0
	if amount != nil {
		dealAmount = *amount
	}
	var dealID string
	err = tx.QueryRow(ctx, `
		INSERT INTO deals (title, contact_id, company_id, lead_id, candidate_id, pipeline_id, stage_id, amount, owner_id, vk_ads_campaign_id, vk_click_id)
		VALUES ($1, nullif($2,'')::uuid, nullif($3,'')::uuid, $4::uuid, nullif($5,'')::uuid, $6::uuid, $7::uuid, $8, nullif($9,'')::uuid, $10, $11)
		ON CONFLICT (lead_id) WHERE lead_id IS NOT NULL DO NOTHING
		RETURNING id::text`,
		title, deref(contactID), deref(companyID), leadID, candidateID, pipe, stage, dealAmount, nz(ownerID, deref(owner)), deref(campaign), deref(click)).Scan(&dealID)
	if err != nil || dealID == "" {
		_ = tx.QueryRow(ctx, `
			SELECT id::text FROM deals WHERE lead_id=$1::uuid ORDER BY created_at ASC LIMIT 1`, leadID).Scan(&dealID)
		if dealID == "" {
			return ""
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE leads SET converted_deal_id=$1::uuid WHERE id=$2::uuid`, dealID, leadID); err != nil {
		return ""
	}
	if err := tx.Commit(ctx); err != nil {
		return ""
	}
	a.logAct(ctx, "lead", leadID, ownerID, "converted", map[string]any{"deal_id": dealID})
	a.broadcastDealCreated(ctx, dealID, map[string]any{"owner_id": nz(ownerID, deref(owner)), "lead_id": leadID})
	return dealID
}

func (a *App) getLead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.ensureLogisticsSchema(r.Context())
	items := a.queryMaps(r.Context(), `
		SELECT l.id::text, l.title, l.status, l.source, l.amount::text, l.currency,
		       l.contact_id::text, l.company_id::text, l.converted_deal_id::text,
		       l.owner_id::text, coalesce(u.full_name,'') AS owner,
		       coalesce(u.department_id::text,'') AS owner_department_id,
		       coalesce(dep.name,'') AS owner_department,
		       coalesce(dep.head_user_id::text,'') AS rop_id,
		       coalesce(
		         nullif(coalesce(rop.full_name,''), ''),
		         CASE WHEN lower(coalesce(orole.name,''))='rop' THEN coalesce(u.full_name,'') ELSE '' END,
		         ''
		       ) AS rop_name,
		       coalesce(l.city,'') AS city, coalesce(l.card_note,'') AS card_note,
		       l.vk_ads_campaign_id, l.created_at,
		       coalesce(c.full_name,'') AS contact, c.phone AS contact_phone,
		       coalesce(co.name,'') AS company
		FROM leads l
		LEFT JOIN contacts c ON c.id=l.contact_id
		LEFT JOIN companies co ON co.id=l.company_id
		LEFT JOIN users u ON u.id=l.owner_id
		LEFT JOIN roles orole ON orole.id=u.role_id
		LEFT JOIN departments dep ON dep.id=u.department_id
		LEFT JOIN users rop ON rop.id=dep.head_user_id
		WHERE l.id=$1`, id)
	if len(items) == 0 || hideIfMissing(w, a.canSeeLead(r.Context(), a.crmScope(r), id)) {
		if len(items) == 0 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
		}
		return
	}
	if rejectVisualOnly(w, r, a.leadVisualOnly(r.Context(), id)) {
		return
	}
	a.noteCardView(r, "lead", id)
	ownerID := strings.TrimSpace(fmt.Sprint(items[0]["owner_id"]))
	if ownerID == "<nil>" {
		ownerID = ""
	}
	var dealID string
	// Логист: не создаём сделку «Найм» — берём escort / logistics_needed.
	if isLogist(r) {
		dealID = a.logisticsDealIDForLead(r.Context(), id)
	} else {
		dealID = a.ensureDealForLead(r.Context(), id, ownerID, nil)
	}
	if dealID == "" {
		dealID = strings.TrimSpace(fmt.Sprint(items[0]["converted_deal_id"]))
		if dealID == "<nil>" {
			dealID = ""
		}
	}
	if dealID == "" {
		_ = a.db.QueryRow(r.Context(), `
			SELECT id::text FROM deals WHERE lead_id=$1::uuid ORDER BY created_at ASC LIMIT 1`, id).Scan(&dealID)
	}
	if dealID != "" {
		items[0]["converted_deal_id"] = dealID
		items[0]["deal_id"] = dealID
		var stageID, pipeID, stageName, stageShort, pipeKey, stageKey string
		var logisticsNeeded, sleeping bool
		err := a.db.QueryRow(r.Context(), `
			SELECT d.stage_id::text, d.pipeline_id::text,
			       coalesce(s.name,''), coalesce(nullif(s.short_name,''), s.name,''),
			       coalesce(d.logistics_needed,false),
			       coalesce(p.bitrix_id,''), coalesce(s.bitrix_id,''),
			       coalesce(d.sleeping,false) OR coalesce(p.bitrix_id,'')=$2
			FROM deals d
			JOIN pipeline_stages s ON s.id=d.stage_id
			JOIN pipelines p ON p.id=d.pipeline_id
			WHERE d.id=$1::uuid`, dealID, oldLeadsPipelineKey).Scan(
			&stageID, &pipeID, &stageName, &stageShort, &logisticsNeeded, &pipeKey, &stageKey, &sleeping)
		if err == nil && stageID != "" {
			items[0]["stage_id"] = stageID
			items[0]["pipeline_id"] = pipeID
			items[0]["stage"] = stageName
			items[0]["stage_short"] = stageShort
			items[0]["logistics_needed"] = logisticsNeeded
			escortTicket := stageKey == escortPipelineKey+":ticket" || stageKey == escortPipelineKey+":ticket_request"
			if (isManager(r) || isROP(r)) && sleeping && escortTicket {
				if oldPipe, oldBilet := a.oldLeadsStageID(r.Context(), "bilet"); oldPipe != "" && oldBilet != "" {
					pipeID = oldPipe
					stageID = oldBilet
					items[0]["pipeline_id"] = pipeID
					items[0]["stage_id"] = stageID
					items[0]["stage"] = "Билет"
					items[0]["stage_short"] = "Билет"
				}
			} else if (isManager(r) || isROP(r)) && !sleeping && a.applyManagerPostBiletStage(r.Context(), dealID, items[0]) {
				pipeID = fmt.Sprint(items[0]["pipeline_id"])
				stageID = fmt.Sprint(items[0]["stage_id"])
			} else if (isManager(r) || isROP(r)) && !sleeping && dealMirrorsToManagerBilet(stageKey, pipeKey, logisticsNeeded) {
				// Менеджер и РОП: «Заявка» и «Куплен билет» логиста — одна колонка «Билет».
				if hirePipe, hireBilet := a.hireStageID(r.Context(), "bilet"); hirePipe != "" && hireBilet != "" {
					pipeID = hirePipe
					stageID = hireBilet
					items[0]["pipeline_id"] = pipeID
					items[0]["stage_id"] = stageID
					items[0]["stage"] = "Билет"
					items[0]["stage_short"] = "Билет"
				}
			}
			// Логист видит воронку «Оформление…», а не «Адаптацию» менеджера
			// (очередь logistics_needed на hire → колонка «Заявка», как на канбане).
			if isLogist(r) && logisticsNeeded && (pipeKey == hirePipelineKey || pipeKey == oldLeadsPipelineKey) {
				escortPipe, escortReq := a.escortStageID(r.Context(), "ticket_request")
				if escortPipe != "" && escortReq != "" {
					pipeID = escortPipe
					stageID = escortReq
					items[0]["pipeline_id"] = pipeID
					items[0]["stage_id"] = stageID
					items[0]["stage"] = "Заявка на билет"
					items[0]["stage_short"] = "Заявка"
					items[0]["logist_view"] = "ticket_request"
				}
			}
		}
		if pipeID != "" {
			var stagesJSON string
			_ = a.db.QueryRow(r.Context(), `
				SELECT coalesce(json_agg(json_build_object(
				  'id', s.id::text,
				  'name', s.name,
				  'short_name', coalesce(nullif(s.short_name,''), s.name),
				  'sort_order', s.sort_order,
				  'is_won', s.is_won,
				  'is_lost', s.is_lost
				) ORDER BY s.sort_order), '[]')
				FROM pipeline_stages s
				WHERE s.pipeline_id=$1::uuid`, pipeID).Scan(&stagesJSON)
			items[0]["stages"] = jsonRaw(stagesJSON)
		}
	}
	items[0]["activity"] = a.queryMaps(r.Context(), `
		SELECT a.id::text, a.entity_type, a.action, a.payload, a.created_at,
		       coalesce(u.full_name,'') AS actor
		FROM activity_log a
		LEFT JOIN users u ON u.id=a.actor_id
		WHERE (a.entity_type='lead' AND a.entity_id=$1::uuid)
		   OR (a.entity_type='deal' AND a.entity_id IN (
		         SELECT d.id FROM deals d
		         WHERE d.lead_id=$1::uuid
		            OR d.id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
		       ))
		ORDER BY a.created_at DESC LIMIT 80`, id)
	items[0]["calls"] = a.queryMaps(r.Context(), `
		SELECT id::text, direction, from_number, to_number, duration_sec, disposition, started_at, provider,
		       coalesce(recording_key,'') AS recording_key, coalesce(exolve_call_id,'') AS exolve_call_id
		FROM calls
		WHERE (entity_type='lead' AND entity_id=$1::uuid)
		   OR (entity_type='deal' AND entity_id IN (
		         SELECT d.id FROM deals d
		         WHERE d.lead_id=$1::uuid
		            OR d.id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
		       ))
		ORDER BY started_at DESC LIMIT 50`, id)
	a.ensureTaskAttentionSchema(r.Context())
	items[0]["planned_tasks"] = a.queryMaps(r.Context(), `
		SELECT t.id::text, t.title, coalesce(t.description,'') AS description,
		       t.due_at, t.status,
		       coalesce(t.is_important,false) AS is_important,
		       coalesce(t.block_work,false) AS block_work,
		       coalesce(t.kind,'task') AS kind,
		       coalesce(t.entity_type,'') AS entity_type,
		       coalesce(t.entity_id::text,'') AS entity_id,
		       t.created_at,
		       t.resolved_at
		FROM tasks t
		WHERE (
		    (t.entity_type='lead' AND t.entity_id=$1::uuid)
		    OR (t.entity_type='deal' AND t.entity_id IN (
		          SELECT d.id FROM deals d
		          WHERE d.lead_id=$1::uuid
		             OR d.id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
		        ))
		  )
		  AND (
		    t.status NOT IN ('completed','done','closed')
		    OR t.resolved_at > now() - interval '14 days'
		    OR (t.status IN ('completed','done','closed') AND t.activity_at > now() - interval '14 days')
		  )
		ORDER BY
		  CASE WHEN t.status IN ('completed','done','closed') THEN 1 ELSE 0 END,
		  t.due_at NULLS LAST,
		  t.created_at ASC
		LIMIT 40`, id)
	items[0]["fields"] = a.entityFields(r.Context(), "lead", id)
	a.maybeMarkOwnerOpened(r.Context(), r, id, ownerID)
	a.writePhones(w, r, 200, items[0])
}

func (a *App) patchLead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeLead(r.Context(), a.crmScope(r), id)) {
		return
	}
	var peek map[string]any
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	_ = json.Unmarshal(body, &peek)

	var syncOwner string
	syncOwnerSet := false
	if ownerRaw, ok := peek["owner_id"]; ok {
		newOwner := strings.TrimSpace(fmt.Sprint(ownerRaw))
		if newOwner == "<nil>" {
			newOwner = ""
		}
		if isManager(r) {
			writeJSON(w, 403, map[string]string{"error": "менеджер не может менять ответственного"})
			return
		}
		if newOwner != "" && (isROP(r) || isAdmin(r)) {
			if err := a.validateLeadAssignee(r.Context(), r, id, newOwner); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
		}
		// Закреплённый лид: передача другому запрещена. Админ может только с force=true.
		force := isAdmin(r) && truthy(peek["force"])
		cur, sticky := a.leadStickyInfo(r.Context(), id)
		if a.transferLeavesLockedDept(r.Context(), cur, newOwner) {
			writeJSON(w, 409, map[string]any{
				"error":    mskTowerRecallDeniedMsg,
				"owner_id": cur,
			})
			return
		}
		if leadStickyBlocksTransfer(cur, sticky, newOwner) != nil && !force {
			writeJSON(w, 409, map[string]any{
				"error":    "лид закреплён за менеджером, который уже работал с ним — смена ответственного запрещена",
				"owner_id": cur,
			})
			return
		}
		// Атомарно (лид+сделки, проверка липкости в самом UPDATE) — против гонки с работой менеджера.
		if !a.setLeadOwnerGuarded(r.Context(), id, newOwner, "", force) {
			cur, _ = a.leadStickyInfo(r.Context(), id)
			errMsg := "лид закреплён за менеджером, который уже работал с ним — смена ответственного запрещена"
			if a.transferLeavesLockedDept(r.Context(), cur, newOwner) {
				errMsg = mskTowerRecallDeniedMsg
			}
			writeJSON(w, 409, map[string]any{
				"error":    errMsg,
				"owner_id": cur,
			})
			return
		}
		if force && leadStickyBlocksTransfer(cur, sticky, newOwner) != nil {
			a.logAct(r.Context(), "lead", id, actorID(r), "owner_changed_forced", map[string]any{"from": cur, "to": newOwner})
		}
		delete(peek, "owner_id")
		delete(peek, "force")
		body, _ = json.Marshal(peek)
		syncOwner = newOwner
		syncOwnerSet = true
	}

	r.Body = io.NopCloser(bytes.NewReader(body))
	if err := a.patchTableExec(r, "leads", []string{"title", "status", "source", "amount", "vk_ads_campaign_id", "contact_id", "company_id", "city", "card_note"}); err != nil && !(syncOwnerSet && err.Error() == "no fields") {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	// ФИО кандидата можно поправить. Номер контакта из карточки не пишем —
	// иначе маска 7••••••6790 сохраняется как 76790. Номер меняется только в базе.
	cand := strings.TrimSpace(fmt.Sprint(peek["candidate"]))
	if cand == "<nil>" {
		cand = ""
	}
	if cand != "" {
		var contactID string
		_ = a.db.QueryRow(r.Context(), `SELECT coalesce(contact_id::text,'') FROM leads WHERE id=$1::uuid`, id).Scan(&contactID)
		if contactID != "" && contactID != "<nil>" {
			_, _ = a.db.Exec(r.Context(), `UPDATE contacts SET full_name=$2, updated_at=now() WHERE id=$1::uuid`, contactID, cand)
		}
	}
	if syncOwnerSet {
		a.syncDealsOwnerForLead(r.Context(), id, syncOwner)
	}
	if titleRaw, ok := peek["title"]; ok {
		newTitle := strings.TrimSpace(fmt.Sprint(titleRaw))
		if newTitle != "" && newTitle != "<nil>" {
			a.syncDealsTitleForLead(r.Context(), id, newTitle)
		}
	}
	var leadTitle, leadStatus, leadSource, leadCity, leadOwner, leadNote string
	if err := a.db.QueryRow(r.Context(), `
		SELECT title, status, source, city, coalesce(owner_id::text,''), coalesce(card_note,'')
		FROM leads WHERE id=$1::uuid`, id).
		Scan(&leadTitle, &leadStatus, &leadSource, &leadCity, &leadOwner, &leadNote); err == nil {
		a.enqueueLeadAIEvent(r.Context(), "lead.updated", id, actorID(r), int(time.Now().Unix()), map[string]any{
			"title":       leadTitle,
			"status":      leadStatus,
			"source":      leadSource,
			"city":        leadCity,
			"owner_id":    leadOwner,
			"next_action": "",
			"card_note":   leadNote,
		})
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) deleteLead(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "удалять заявки может только администратор"})
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	if hideIfMissing(w, a.canSeeLead(ctx, a.crmScope(r), id)) {
		return
	}
	// заявка = лид + связанная сделка в «Найм»; удаляем оба, иначе карточка остаётся на доске
	dealRows := a.queryMaps(ctx, `
		SELECT id::text AS id FROM deals
		WHERE lead_id=$1::uuid
		   OR id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)`, id)
	dealIDs := make([]string, 0, len(dealRows))
	for _, row := range dealRows {
		if idStr := strings.TrimSpace(fmt.Sprint(row["id"])); idStr != "" && idStr != "<nil>" {
			dealIDs = append(dealIDs, idStr)
		}
	}
	_, _ = a.db.Exec(ctx, `UPDATE leads SET converted_deal_id=NULL WHERE id=$1::uuid`, id)
	_, _ = a.db.Exec(ctx, `UPDATE deals SET lead_id=NULL WHERE lead_id=$1::uuid`, id)
	for _, dealID := range dealIDs {
		_, _ = a.db.Exec(ctx, `DELETE FROM entity_field_values WHERE entity_type='deal' AND entity_id=$1::uuid`, dealID)
		_, _ = a.db.Exec(ctx, `DELETE FROM activity_log WHERE entity_type='deal' AND entity_id=$1::uuid`, dealID)
		_, _ = a.db.Exec(ctx, `DELETE FROM distribution_log WHERE deal_id=$1::uuid`, dealID)
		_, _ = a.db.Exec(ctx, `UPDATE inbox_threads SET deal_id=NULL WHERE deal_id=$1::uuid`, dealID)
		_, _ = a.db.Exec(ctx, `UPDATE projects SET deal_id=NULL WHERE deal_id=$1::uuid`, dealID)
		_, err := a.db.Exec(ctx, `DELETE FROM deals WHERE id=$1::uuid`, dealID)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось удалить сделку: " + err.Error()})
			return
		}
		a.hub.Broadcast("deal.deleted", map[string]any{"id": dealID})
	}
	_, _ = a.db.Exec(ctx, `DELETE FROM entity_field_values WHERE entity_type='lead' AND entity_id=$1::uuid`, id)
	_, _ = a.db.Exec(ctx, `DELETE FROM activity_log WHERE entity_type='lead' AND entity_id=$1::uuid`, id)
	_, _ = a.db.Exec(ctx, `DELETE FROM distribution_log WHERE lead_id=$1::uuid`, id)
	tag, err := a.db.Exec(ctx, `DELETE FROM leads WHERE id=$1::uuid`, id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "не удалось удалить: " + err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, 404, map[string]string{"error": "лид не найден"})
		return
	}
	a.logAct(ctx, "lead", id, actorID(r), "deleted", nil)
	a.hub.Broadcast("lead.deleted", map[string]any{"id": id})
	writeJSON(w, 200, map[string]any{"ok": true, "deleted_deals": dealIDs})
}

func (a *App) convertLead(w http.ResponseWriter, r *http.Request) {
	leadID := chi.URLParam(r, "id")
	ctx := r.Context()
	if hideIfMissing(w, a.canSeeLead(ctx, a.crmScope(r), leadID)) {
		return
	}
	var owner string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1::uuid`, leadID).Scan(&owner)
	dealID := a.ensureDealForLead(ctx, leadID, nz(owner, actorID(r)), nil)
	if dealID == "" {
		writeJSON(w, 400, map[string]string{"error": "не удалось создать сделку в воронке"})
		return
	}
	writeJSON(w, 200, map[string]any{"deal_id": dealID, "already": true})
}

func (a *App) dealListWhere(r *http.Request) (clause string, args []any) {
	where := []string{}
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	ctx := r.Context()
	pipe := strings.TrimSpace(r.URL.Query().Get("pipeline_id"))
	oldBoard := pipe != "" && a.isOldLeadsPipeline(ctx, pipe)
	escortBoard := pipe != "" && (isLogist(r) || isAdmin(r)) && a.isEscortPipeline(ctx, pipe)
	if oldBoard {
		if !a.userCanSeeOldLeads(r) {
			where = append(where, "1=0")
		} else {
			where = append(where, `(d.pipeline_id = `+next(pipe)+`::uuid OR (
				coalesce(d.sleeping,false)=true
				AND EXISTS (
					SELECT 1 FROM pipeline_stages es
					WHERE es.id=d.stage_id
					  AND coalesce(es.bitrix_id,'') IN ('`+escortPipelineKey+`:ticket','`+escortPipelineKey+`:ticket_request')
				)
			))`)
		}
	} else if pipe != "" {
		if isManager(r) && a.isEscortPipeline(ctx, pipe) {
			where = append(where, "1=0")
		} else if (isManager(r) || isROP(r)) && a.isHirePipeline(ctx, pipe) {
			// «Адаптация» плюс карточки логиста «Заявка» и «Куплен билет».
			ticket := next(escortPipelineKey + ":ticket")
			req := next(escortPipelineKey + ":ticket_request")
			where = append(where, fmt.Sprintf(`(
				d.pipeline_id = %s::uuid
				OR EXISTS (
					SELECT 1 FROM pipeline_stages es
					WHERE es.id=d.stage_id AND coalesce(es.bitrix_id,'') IN (%s, %s)
				)
				OR EXISTS (
					SELECT 1 FROM pipeline_stages mps
					WHERE mps.id=d.manager_stage_id
					  AND coalesce(mps.bitrix_id,'') IN (%s)
				)
			)`, next(pipe), ticket, req, sqlHirePostBiletIn()))
		} else if escortBoard {
			// escort + очередь логиста на hire, включая «Выезд» и колонки после него.
			// У логиста пустая «Заявка» (нет документа и заполненного билета) в доску не входит.
			// Отдельная колонка сопровождения читается по стадии, без обхода всей очереди.
			stagePeek := strings.TrimSpace(r.URL.Query().Get("stage_id"))
			if isLogist(r) && stagePeek != "" && !a.isEscortTicketRequestStage(ctx, stagePeek) {
				where = append(where, "d.pipeline_id = "+next(pipe)+"::uuid")
			} else if isLogist(r) && stagePeek != "" {
				// «Заявка» собирается отдельным фильтром стадии.
			} else if isLogist(r) {
				where = append(where, fmt.Sprintf(
					`(
						d.pipeline_id = %s::uuid
						AND `+sqlLogistEscortExceptEmptyRequest("d")+`
					) OR d.id IN (`+sqlLogistQueueAndHandoffIDs()+`)`, next(pipe)))
			} else {
				where = append(where, fmt.Sprintf(
					`(d.pipeline_id = %s::uuid OR (
						coalesce(d.logistics_needed,false)=true
						AND `+sqlDealOnLogisticsSourcePipeline("d")+`
					) OR `+sqlDealOnManagerHandoffStage("d")+`)`, next(pipe)))
			}
		} else {
			where = append(where, "d.pipeline_id = "+next(pipe)+"::uuid")
		}
	}
	if stage := strings.TrimSpace(r.URL.Query().Get("stage_id")); stage != "" {
		if oldBoard && a.isOldLeadsBiletStage(ctx, stage) {
			where = append(where, `(d.stage_id = `+next(stage)+`::uuid OR (
				coalesce(d.sleeping,false)=true
				AND EXISTS (
					SELECT 1 FROM pipeline_stages es
					WHERE es.id=d.stage_id
					  AND coalesce(es.bitrix_id,'') IN ('`+escortPipelineKey+`:ticket','`+escortPipelineKey+`:ticket_request')
				)
			))`)
		} else if (isLogist(r) || isAdmin(r)) && a.isEscortTicketRequestStage(ctx, stage) {
			// «Заявка»: реальные escort, очередь logistics_needed и колонки менеджера после «Билет».
			// Логист не видит карточку без документа и без заполненного билета.
			if isLogist(r) {
				where = append(where, sqlLogistRequestColumn("d", next(stage)))
			} else {
				where = append(where, fmt.Sprintf(
					`(d.stage_id = %s::uuid OR (
						coalesce(d.logistics_needed,false)=true
						AND `+sqlDealOnLogisticsSourcePipeline("d")+`
					) OR `+sqlDealOnManagerHandoffStage("d")+`)`, next(stage)))
			}
		} else if (isManager(r) || isROP(r)) && a.isHireBiletStage(ctx, stage) {
			// Одна колонка «Билет»: свои на стадии и всё, что у логиста в «Заявка» или «Билет».
			ticket := next(escortPipelineKey + ":ticket")
			req := next(escortPipelineKey + ":ticket_request")
			hire := next(hirePipelineKey)
			where = append(where, fmt.Sprintf(`(
				(d.stage_id = %s::uuid AND d.manager_stage_id IS NULL)
				OR (
					d.manager_stage_id IS NULL
					AND EXISTS (
						SELECT 1 FROM pipeline_stages es
						WHERE es.id=d.stage_id AND coalesce(es.bitrix_id,'') IN (%s, %s)
					)
				)
				OR (
					d.manager_stage_id IS NULL
					AND coalesce(d.logistics_needed,false)=true
					AND EXISTS (
						SELECT 1 FROM pipelines p
						WHERE p.id=d.pipeline_id AND coalesce(p.bitrix_id,'')=%s
					)
					AND NOT EXISTS (
						SELECT 1 FROM pipeline_stages hs
						WHERE hs.id=d.stage_id AND coalesce(hs.bitrix_id,'') IN (%s)
					)
				)
			)`, next(stage), ticket, req, hire, sqlHirePostBiletIn()))
		} else if (isManager(r) || isROP(r)) && a.isHirePostBiletStage(ctx, stage) {
			// Колонка менеджера. Стадия логиста может остаться «Куплен билет».
			where = append(where, `(d.stage_id = `+next(stage)+`::uuid OR d.manager_stage_id = `+next(stage)+`::uuid)`)
		} else if isManager(r) || isROP(r) {
			// Уже отданные логисту не дублируем в других колонках адаптации.
			hire := next(hirePipelineKey)
			where = append(where, fmt.Sprintf(`(
				d.stage_id = %s::uuid
				AND d.manager_stage_id IS NULL
				AND NOT (
					coalesce(d.logistics_needed,false)=true
					AND EXISTS (
						SELECT 1 FROM pipelines p
						WHERE p.id=d.pipeline_id AND coalesce(p.bitrix_id,'')=%s
					)
				)
			)`, next(stage), hire))
		} else {
			where = append(where, "d.stage_id = "+next(stage)+"::uuid")
		}
	}
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	if scope == "archive" {
		if !isAdmin(r) {
			scope = "active"
		} else {
			where = append(where, "s.is_lost = true")
		}
	}
	// scope=all — вся воронка включая отказы (канбан найма: «Некачеств.» / «Чужая комп.»)
	if scope != "archive" && scope != "all" {
		if escortBoard {
			// Очередь логиста не прячем, если менеджер случайно кинул в отказ hire.
			where = append(where, fmt.Sprintf(`(
				s.is_lost = false
				OR (
					coalesce(d.logistics_needed,false)=true
					AND `+sqlDealOnLogisticsSourcePipeline("d")+`
				)
			)`))
		} else {
			where = append(where, "s.is_lost = false")
		}
	}
	if !oldBoard {
		where = a.applyDealListVisibility(r, where, next)
		if !escortBoard {
			where = append(where, sqlDealNotOldLeads("d"))
		}
	} else if isManager(r) {
		where = append(where, "d.owner_id = "+next(actorID(r))+"::uuid")
	}
	if wantMine(r) {
		where = append(where, "d.owner_id = "+next(actorID(r))+"::uuid")
	}
	// Админ: канбан/список по отделу владельца. Банк спящих ему не режем: карточки ещё без ответственного.
	if dept := strings.TrimSpace(r.URL.Query().Get("department_id")); dept != "" && isAdmin(r) && !oldBoard {
		where = append(where,
			"d.owner_id IN (SELECT id FROM users WHERE department_id="+next(dept)+"::uuid)")
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		p := next(likePattern(q))
		where = append(where, "(d.title ILIKE "+p+" OR coalesce(c.full_name,'') ILIKE "+p+" OR coalesce(co.name,'') ILIKE "+p+" OR coalesce(c.phone,'') ILIKE "+p+")")
	}
	if days := periodDays(r); days > 0 {
		where = append(where, "d.created_at >= now() - ("+next(days)+"::int * interval '1 day')")
	}
	// Источник лида: вручную / VK Ads (канбан).
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source"))) {
	case "manual":
		where = append(where, `(
			EXISTS (
				SELECT 1 FROM leads ld
				WHERE ld.id=d.lead_id AND coalesce(nullif(ld.source,''), 'manual') = 'manual'
			)
			OR (
				d.lead_id IS NULL
				AND coalesce(d.vk_ads_campaign_id,'') = ''
			)
		)`)
	case "vk", "vk_ads":
		where = append(where, `(
			EXISTS (
				SELECT 1 FROM leads ld
				WHERE ld.id=d.lead_id AND coalesce(ld.source,'') = 'vk_ads'
			)
			OR coalesce(d.vk_ads_campaign_id,'') <> ''
		)`)
	}
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	return clause, args
}

func (a *App) listDeals(w http.ResponseWriter, r *http.Request) {
	a.ensureLogisticsSchema(r.Context())
	_, limit, offset := parseListACL(r, 20, 100, 50)
	order := parseSort(r, map[string]string{
		"title": "d.title", "amount": "d.amount", "stage": "s.sort_order",
		"contact": "c.full_name", "company": "co.name", "created_at": "d.created_at",
	}, "d.created_at")
	ctx := r.Context()
	from := `
		FROM deals d
		JOIN pipeline_stages s ON s.id=d.stage_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN companies co ON co.id=d.company_id
		LEFT JOIN leads ld ON ld.id=d.lead_id
		LEFT JOIN users ou ON ou.id=d.owner_id
		LEFT JOIN departments odep ON odep.id=ou.department_id
		LEFT JOIN pipeline_stages ms ON ms.id=d.manager_stage_id`
	selectSQL := `SELECT d.id::text, d.title, d.amount::text, d.currency, d.stage_id::text, s.name AS stage, s.sort_order,
		       s.is_won, s.is_lost, d.pipeline_id::text, d.contact_id::text, d.company_id::text, d.lead_id::text,
		       coalesce(c.full_name,'') AS contact, coalesce(c.phone,'') AS contact_phone,
		       coalesce(co.name,'') AS company, coalesce(ld.card_note,'') AS card_note,
		       d.vk_ads_campaign_id, d.created_at, d.owner_id::text,
		       coalesce(ou.full_name,'') AS owner,
		       coalesce(ou.department_id::text,'') AS owner_department_id,
		       coalesce(odep.name,'') AS owner_department,
		       coalesce(d.autodial_status,'') AS autodial_status,
		       coalesce(d.logistics_needed,false) AS logistics_needed,
		       coalesce(d.sleeping,false) AS sleeping,
		       coalesce(d.visual_only,false) AS visual_only,
		       coalesce(s.bitrix_id,'') AS stage_key,
		       coalesce(d.manager_stage_id::text,'') AS manager_stage_id,
		       coalesce(ms.bitrix_id,'') AS manager_stage_key,
		       coalesce(ms.name,'') AS manager_stage_name,
		       coalesce(nullif(ms.short_name,''), ms.name, '') AS manager_stage_short,
		       coalesce(ms.pipeline_id::text,'') AS manager_pipeline_id` + from
	clause, args := a.dealListWhere(r)
	countArgs := append([]any{}, args...)
	args = append(args, limit, offset)
	items := a.queryMaps(ctx, selectSQL+clause+" ORDER BY "+order+" LIMIT $"+strconv.Itoa(len(args)-1)+" OFFSET $"+strconv.Itoa(len(args)), args...)
	listPipe := strings.TrimSpace(r.URL.Query().Get("pipeline_id"))
	if listPipe != "" && a.isOldLeadsPipeline(ctx, listPipe) {
		a.remapSleepingEscortAsOwnBilet(ctx, items)
	} else {
		if isLogist(r) || isAdmin(r) {
			a.remapHireBiletAsLogistRequest(ctx, items, isLogist(r) || a.isEscortPipeline(ctx, listPipe))
		}
		if isManager(r) || isROP(r) {
			a.remapLogistQueueAsManagerBilet(ctx, items)
		}
	}
	a.writePage(w, r, items,
		a.queryInt(ctx, `SELECT count(*) `+from+clause, countArgs...), limit, offset)
}

// remapHireBiletAsLogistRequest — у логиста очередь logistics_needed на hire
// всегда в колонке «Заявка», даже если менеджер ушёл со стадии «Билет».
// handoffWithoutFlag — «Выезд» и колонки после него тоже «Заявка», если флаг ещё не стоял.
func (a *App) remapHireBiletAsLogistRequest(ctx context.Context, items []map[string]any, handoffWithoutFlag bool) {
	if a == nil || a.db == nil || len(items) == 0 {
		return
	}
	escortPipe, escortReq := a.escortStageID(ctx, "ticket_request")
	if escortPipe == "" || escortReq == "" {
		return
	}
	for _, it := range items {
		key := fmt.Sprint(it["stage_key"])
		if !logistPaintsHireStageAsRequest(key, truthy(it["logistics_needed"]), handoffWithoutFlag) {
			continue
		}
		it["pipeline_id"] = escortPipe
		it["stage_id"] = escortReq
		it["stage"] = "Заявка на билет"
		it["logist_view"] = "ticket_request"
	}
}

// dealMirrorsToManagerBilet — карточка из колонок логиста «Заявка» или «Билет».
// У менеджера и РОПа она остаётся в «Адаптация → Билет».
func dealMirrorsToManagerBilet(stageKey, pipeKey string, logisticsNeeded bool) bool {
	if isHirePostBiletKey(stageKey) {
		return false
	}
	switch stageKey {
	case escortPipelineKey + ":ticket", escortPipelineKey + ":ticket_request":
		return true
	}
	return logisticsNeeded && (pipeKey == hirePipelineKey || strings.HasPrefix(stageKey, hirePipelineKey))
}

func (a *App) isHirePostBiletStage(ctx context.Context, stageID string) bool {
	if stageID == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipeline_stages WHERE id=$1::uuid`, stageID).Scan(&key)
	return isHirePostBiletKey(key)
}

func (a *App) dealHasManagerPostBilet(ctx context.Context, dealID string) bool {
	if a == nil || a.db == nil || dealID == "" {
		return false
	}
	a.ensureLogisticsSchema(ctx)
	var key string
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(ms.bitrix_id,'')
		FROM deals d
		JOIN pipeline_stages ms ON ms.id=d.manager_stage_id
		WHERE d.id=$1::uuid`, dealID).Scan(&key)
	return err == nil && isHirePostBiletKey(key)
}

// applyManagerPostBiletStage показывает менеджеру его колонку после «Билет».
// stage_id сделки (стадия логиста) не читается как текущая колонка.
func (a *App) applyManagerPostBiletStage(ctx context.Context, dealID string, row map[string]any) bool {
	if !a.dealHasManagerPostBilet(ctx, dealID) {
		return false
	}
	var pipe, stage, name, short, key string
	err := a.db.QueryRow(ctx, `
		SELECT ms.pipeline_id::text, ms.id::text, coalesce(ms.name,''),
		       coalesce(nullif(ms.short_name,''), ms.name, ''),
		       coalesce(ms.bitrix_id,'')
		FROM deals d
		JOIN pipeline_stages ms ON ms.id=d.manager_stage_id
		WHERE d.id=$1::uuid`, dealID).Scan(&pipe, &stage, &name, &short, &key)
	if err != nil || !isHirePostBiletKey(key) {
		return false
	}
	row["pipeline_id"] = pipe
	row["stage_id"] = stage
	row["stage"] = name
	row["stage_short"] = short
	row["stage_key"] = key
	return true
}

// remapLogistQueueAsManagerBilet рисует очередь логиста в одной колонке «Билет».
func (a *App) remapLogistQueueAsManagerBilet(ctx context.Context, items []map[string]any) {
	if a == nil || a.db == nil || len(items) == 0 {
		return
	}
	hirePipe, hireBilet := a.hireStageID(ctx, "bilet")
	if hirePipe == "" || hireBilet == "" {
		return
	}
	for _, it := range items {
		if truthy(it["sleeping"]) || strings.HasPrefix(fmt.Sprint(it["stage_key"]), oldLeadsPipelineKey) {
			continue
		}
		if isHirePostBiletKey(fmt.Sprint(it["manager_stage_key"])) {
			it["pipeline_id"] = it["manager_pipeline_id"]
			it["stage_id"] = it["manager_stage_id"]
			it["stage"] = it["manager_stage_name"]
			it["stage_short"] = it["manager_stage_short"]
			it["stage_key"] = it["manager_stage_key"]
			continue
		}
		key := fmt.Sprint(it["stage_key"])
		if !dealMirrorsToManagerBilet(key, "", truthy(it["logistics_needed"])) {
			continue
		}
		it["pipeline_id"] = hirePipe
		it["stage_id"] = hireBilet
		it["stage"] = "Билет"
		it["stage_short"] = "Билет"
		it["stage_key"] = hirePipelineKey + ":bilet"
	}
}

// remapSleepingEscortAsOwnBilet — у Тёмы карточка логиста остаётся в «Спящие лиды → Билет», не в адаптации.
func (a *App) remapSleepingEscortAsOwnBilet(ctx context.Context, items []map[string]any) {
	if a == nil || a.db == nil || len(items) == 0 {
		return
	}
	pipe, stage := a.oldLeadsStageID(ctx, "bilet")
	if pipe == "" || stage == "" {
		return
	}
	for _, it := range items {
		key := fmt.Sprint(it["stage_key"])
		if key != escortPipelineKey+":ticket" && key != escortPipelineKey+":ticket_request" {
			continue
		}
		if !truthy(it["sleeping"]) {
			continue
		}
		it["pipeline_id"] = pipe
		it["stage_id"] = stage
		it["stage"] = "Билет"
		it["stage_short"] = "Билет"
		it["stage_key"] = oldLeadsPipelineKey + ":bilet"
	}
}

func (a *App) dealStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a.ensureLogisticsSchema(ctx)
	from := `
		FROM deals d
		JOIN pipeline_stages s ON s.id=d.stage_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN companies co ON co.id=d.company_id`
	clause, args := a.dealListWhere(r)
	pipe := strings.TrimSpace(r.URL.Query().Get("pipeline_id"))
	oldBoard := pipe != "" && a.isOldLeadsPipeline(ctx, pipe)
	escortPipe, escortReq := "", ""
	if isLogist(r) || isAdmin(r) {
		escortPipe, escortReq = a.escortStageID(ctx, "ticket_request")
	}
	queue := `(coalesce(d.logistics_needed,false)=true AND ` + sqlDealOnLogisticsSourcePipeline("d") + `)`
	if isLogist(r) {
		// Отбор уже в WHERE. Здесь только раскладка по колонке, без повторной проверки билета.
		queue = `(
			coalesce(s.bitrix_id,'') IN (` + sqlHirePostBiletIn() + `)
			OR (
				coalesce(d.logistics_needed,false)=true
				AND ` + sqlDealOnLogisticsSourcePipeline("d") + `
			)
		)`
	} else if pipe != "" && a.isEscortPipeline(ctx, pipe) {
		queue = `(` + queue + ` OR ` + sqlDealOnManagerHandoffStage("d") + `)`
	}
	var items []map[string]any
	if oldBoard {
		oldPipe, oldBilet := a.oldLeadsStageID(ctx, "bilet")
		if oldPipe != "" && oldBilet != "" {
			op := strconv.Itoa(len(args) + 1)
			ob := strconv.Itoa(len(args) + 2)
			items = a.queryMaps(ctx, `
				SELECT
				  CASE
				    WHEN coalesce(d.sleeping,false)=true
				     AND coalesce(s.bitrix_id,'') IN ('`+escortPipelineKey+`:ticket','`+escortPipelineKey+`:ticket_request')
				      THEN $`+op+`::text
				    ELSE d.pipeline_id::text
				  END AS pipeline_id,
				  CASE
				    WHEN coalesce(d.sleeping,false)=true
				     AND coalesce(s.bitrix_id,'') IN ('`+escortPipelineKey+`:ticket','`+escortPipelineKey+`:ticket_request')
				      THEN $`+ob+`::text
				    ELSE d.stage_id::text
				  END AS stage_id,
				  count(*)::int AS count,
				  coalesce(sum(d.amount),0)::text AS amount`+from+clause+`
				GROUP BY 1, 2`, append(append([]any{}, args...), oldPipe, oldBilet)...)
		} else {
			items = a.queryMaps(ctx, `
				SELECT d.pipeline_id::text AS pipeline_id, d.stage_id::text AS stage_id,
				       count(*)::int AS count, coalesce(sum(d.amount),0)::text AS amount`+from+clause+`
				GROUP BY d.pipeline_id, d.stage_id`, args...)
		}
	} else if escortPipe != "" && escortReq != "" {
		// Очередь logistics_needed на hire → колонка логиста «Заявка»
		// (независимо от того, ушёл ли менеджер со стадии «Билет»).
		escP := strconv.Itoa(len(args) + 1)
		escS := strconv.Itoa(len(args) + 2)
		items = a.queryMaps(ctx, `
			SELECT
			  CASE
			    WHEN `+queue+`
			      THEN $`+escP+`::text
			    ELSE d.pipeline_id::text
			  END AS pipeline_id,
			  CASE
			    WHEN `+queue+`
			      THEN $`+escS+`::text
			    ELSE d.stage_id::text
			  END AS stage_id,
			  count(*)::int AS count,
			  coalesce(sum(d.amount),0)::text AS amount`+from+clause+`
			GROUP BY 1, 2`, append(append([]any{}, args...), escortPipe, escortReq)...)
	} else if isManager(r) || isROP(r) {
		hirePipe, hireBilet := a.hireStageID(ctx, "bilet")
		if hirePipe != "" && hireBilet != "" {
			tKey := strconv.Itoa(len(args) + 1)
			rKey := strconv.Itoa(len(args) + 2)
			hKey := strconv.Itoa(len(args) + 3)
			hp := strconv.Itoa(len(args) + 4)
			hs := strconv.Itoa(len(args) + 5)
			mirror := `d.manager_stage_id IS NULL AND (
			      coalesce(s.bitrix_id,'') IN ($` + tKey + `, $` + rKey + `)
			      OR (
			        coalesce(d.logistics_needed,false)=true
			        AND coalesce(s.bitrix_id,'') NOT IN (` + sqlHirePostBiletIn() + `)
			        AND EXISTS (
			          SELECT 1 FROM pipelines p
			          WHERE p.id=d.pipeline_id AND coalesce(p.bitrix_id,'')=$` + hKey + `
			        )
			      )
			    )`
			mgrFrom := from + ` LEFT JOIN pipeline_stages ms ON ms.id=d.manager_stage_id`
			items = a.queryMaps(ctx, `
				SELECT
				  CASE
				    WHEN coalesce(ms.bitrix_id,'') IN (`+sqlHirePostBiletIn()+`) THEN ms.pipeline_id::text
				    WHEN `+mirror+` THEN $`+hp+`::text
				    ELSE d.pipeline_id::text
				  END AS pipeline_id,
				  CASE
				    WHEN coalesce(ms.bitrix_id,'') IN (`+sqlHirePostBiletIn()+`) THEN ms.id::text
				    WHEN `+mirror+` THEN $`+hs+`::text
				    ELSE d.stage_id::text
				  END AS stage_id,
				  count(*)::int AS count,
				  coalesce(sum(d.amount),0)::text AS amount`+mgrFrom+clause+`
				GROUP BY 1, 2`, append(append([]any{}, args...),
				escortPipelineKey+":ticket", escortPipelineKey+":ticket_request", hirePipelineKey, hirePipe, hireBilet)...)
		} else {
			items = a.queryMaps(ctx, `
				SELECT d.pipeline_id::text AS pipeline_id, d.stage_id::text AS stage_id,
				       count(*)::int AS count, coalesce(sum(d.amount),0)::text AS amount`+from+clause+`
				GROUP BY d.pipeline_id, d.stage_id`, args...)
		}
	} else {
		items = a.queryMaps(ctx, `
			SELECT d.pipeline_id::text AS pipeline_id, d.stage_id::text AS stage_id,
			       count(*)::int AS count, coalesce(sum(d.amount),0)::text AS amount`+from+clause+`
			GROUP BY d.pipeline_id, d.stage_id`, args...)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (a *App) createDeal(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = readJSON(r, &in)
	ctx := r.Context()
	pipeIn := str(in, "pipeline_id")
	// Менеджер не создаёт кандидатов (воронки найма / create_lead) — только РОП/admin.
	if isManager(r) && (a.isRecruitPipeline(ctx, pipeIn) || truthy(in["create_lead"]) || str(in, "phone") != "") {
		writeJSON(w, 403, map[string]string{"error": "менеджер не может создавать кандидатов — их назначает РОП"})
		return
	}
	var pipe, stage string
	if pipeIn == "" {
		pipe, stage = a.hireStart(ctx)
	} else {
		pipe = pipeIn
		stage = str(in, "stage_id")
	}
	contactID := str(in, "contact_id")
	if contactID == "" {
		if phone := str(in, "phone"); phone != "" {
			cand := nz(str(in, "candidate"), nz(str(in, "contact_name"), str(in, "title")))
			contactID = a.ensureContactByPhone(ctx, phone, cand)
		}
	}
	// The INSERT trigger assigns identity atomically.
	candidateID := ""
	var id string
	err := a.db.QueryRow(ctx, `
		INSERT INTO deals (title, amount, currency, pipeline_id, stage_id, contact_id, company_id, candidate_id, owner_id, vk_ads_campaign_id, vk_click_id)
		VALUES ($1, coalesce(nullif($2,'')::numeric,0), coalesce(nullif($3,''),'RUB'), $4::uuid, $5::uuid, nullif($6,'')::uuid, nullif($7,'')::uuid, nullif($8,'')::uuid, $9::uuid, nullif($10,''), nullif($11,''))
		RETURNING id::text`,
		str(in, "title"), str(in, "amount"), str(in, "currency"), pipe, nz(stage, str(in, "stage_id")),
		contactID, str(in, "company_id"), candidateID, actorID(r), str(in, "vk_ads_campaign_id"), str(in, "vk_click_id"),
	).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.logAct(ctx, "deal", id, actorID(r), "created", in)

	wantLead := truthy(in["create_lead"]) || str(in, "phone") != "" || a.isHirePipeline(ctx, pipe)
	leadID := ""
	if wantLead {
		leadID, err = a.ensureLeadForDeal(ctx, id, actorID(r), in)
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error(), "id": id})
			return
		}
	}
	a.enqueueDealAIEvent(ctx, "deal.created", id, actorID(r), 1, map[string]any{
		"title":       str(in, "title"),
		"amount":      str(in, "amount"),
		"currency":    nz(str(in, "currency"), "RUB"),
		"pipeline_id": pipe,
		"stage_id":    nz(stage, str(in, "stage_id")),
		"owner_id":    actorID(r),
		"lead_id":     leadID,
	})
	writeJSON(w, 201, map[string]any{"id": id, "lead_id": leadID, "contact_id": contactID})
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes"
	case float64:
		return t != 0
	default:
		return false
	}
}

func (a *App) isHirePipeline(ctx context.Context, pipeID string) bool {
	if pipeID == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipelines WHERE id=$1::uuid`, pipeID).Scan(&key)
	return key == hirePipelineKey
}

func (a *App) isEscortPipeline(ctx context.Context, pipeID string) bool {
	if pipeID == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipelines WHERE id=$1::uuid`, pipeID).Scan(&key)
	return key == escortPipelineKey
}

func (a *App) isEscortTicketRequestStage(ctx context.Context, stageID string) bool {
	if stageID == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipeline_stages WHERE id=$1::uuid`, stageID).Scan(&key)
	return key == escortPipelineKey+":ticket_request"
}

func (a *App) isHireBiletStage(ctx context.Context, stageID string) bool {
	if stageID == "" {
		return false
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipeline_stages WHERE id=$1::uuid`, stageID).Scan(&key)
	return key == hirePipelineKey+":bilet"
}

func (a *App) isRecruitPipeline(ctx context.Context, pipeID string) bool {
	if pipeID == "" {
		return true // createDeal без pipeline → hireStart
	}
	var key string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(bitrix_id,'') FROM pipelines WHERE id=$1::uuid`, pipeID).Scan(&key)
	return key == hirePipelineKey || key == escortPipelineKey
}

// ensureLeadForDeal создаёт/возвращает лид, привязанный к сделке найма.
func (a *App) ensureLeadForDeal(ctx context.Context, dealID, ownerID string, in map[string]any) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var existing, title, contactID, companyID, candidateID string
	err = tx.QueryRow(ctx, `
		SELECT coalesce(lead_id::text,''), coalesce(title,''), coalesce(contact_id::text,''),
		       coalesce(company_id::text,''), candidate_id::text
		FROM deals WHERE id=$1::uuid FOR UPDATE`, dealID).
		Scan(&existing, &title, &contactID, &companyID, &candidateID)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, tx.Commit(ctx)
	}
	// Serialize repeated requests and reuse the deal identity even without a contact.
	var leadID string
	err = tx.QueryRow(ctx, `
		INSERT INTO leads (title, source, status, owner_id, contact_id, company_id,
		                   candidate_id, city, card_note, converted_deal_id)
		VALUES ($1, $2, $3, nullif($4,'')::uuid, nullif($5,'')::uuid, nullif($6,'')::uuid,
		        $7::uuid, $8, $9, $10::uuid)
		RETURNING id::text`, nz(title, str(in, "title")), nz(str(in, "source"), "manual"),
		nz(str(in, "status"), "new"), ownerID, contactID, companyID, candidateID,
		str(in, "city"), nz(str(in, "card_note"), str(in, "comment")), dealID).Scan(&leadID)
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE deals SET lead_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, dealID, leadID); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	a.logAct(ctx, "lead", leadID, ownerID, "created", in)
	a.hub.Broadcast("lead.created", map[string]any{"id": leadID, "title": title})
	return leadID, nil
}

func (a *App) ensureDealLead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	a.ensureLogisticsSchema(ctx)
	if hideIfMissing(w, a.canSeeDeal(ctx, a.crmScope(r), id)) {
		return
	}
	if rejectVisualOnly(w, r, a.dealVisualOnly(ctx, id)) {
		return
	}
	// Уже есть связанный лид — отдать его (менеджеру тоже: иначе не открыть карточку/комментарий).
	if leadID := a.dealLinkedLeadID(ctx, id); leadID != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE deals SET lead_id=coalesce(lead_id, $2::uuid), updated_at=now()
			WHERE id=$1::uuid`, id, leadID)
		writeJSON(w, 200, map[string]any{"ok": true, "lead_id": leadID, "deal_id": id})
		return
	}
	if isManager(r) {
		writeJSON(w, 403, map[string]string{"error": "менеджер не может создавать кандидатов — их назначает РОП"})
		return
	}
	ownerID := actorID(r)
	// Логист открывает карточку — владельцем лида остаётся менеджер сделки, не логист.
	if isLogist(r) {
		var dealOwner string
		_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM deals WHERE id=$1::uuid`, id).Scan(&dealOwner)
		if dealOwner != "" && dealOwner != "<nil>" {
			ownerID = dealOwner
		}
	}
	leadID, err := a.ensureLeadForDeal(ctx, id, ownerID, map[string]any{})
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "lead_id": leadID, "deal_id": id})
}

func (a *App) getDeal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.ensureLogisticsSchema(r.Context())
	items := a.queryMaps(r.Context(), `
		SELECT d.id::text, d.title, d.amount::text, d.currency, d.stage_id::text, d.pipeline_id::text,
		       d.contact_id::text, d.company_id::text, d.lead_id::text, d.created_at, d.closed_at,
		       d.owner_id::text, coalesce(u.full_name,'') AS owner,
		       coalesce(u.department_id::text,'') AS owner_department_id,
		       coalesce(dep.name,'') AS owner_department,
		       s.name AS stage, coalesce(s.short_name,'') AS stage_short, s.is_won, s.is_lost,
		       coalesce(s.bitrix_id,'') AS stage_key,
		       coalesce(d.logistics_needed,false) AS logistics_needed,
		       coalesce(d.sleeping,false) AS sleeping,
		       coalesce(c.full_name,'') AS contact, coalesce(c.phone,'') AS contact_phone,
		       coalesce(co.name,'') AS company, coalesce(ld.card_note,'') AS card_note,
		       coalesce(d.autodial_status,'') AS autodial_status,
		       coalesce(d.visual_only,false) AS visual_only
		FROM deals d
		JOIN pipeline_stages s ON s.id=d.stage_id
		LEFT JOIN contacts c ON c.id=d.contact_id
		LEFT JOIN companies co ON co.id=d.company_id
		LEFT JOIN leads ld ON ld.id=d.lead_id
		LEFT JOIN users u ON u.id=d.owner_id
		LEFT JOIN departments dep ON dep.id=u.department_id
		WHERE d.id=$1`, id)
	scope := a.crmScope(r)
	onBoard := r.URL.Query().Get("for_board") == "1"
	visible := a.canSeeDeal(r.Context(), scope, id)
	if onBoard {
		visible = a.dealOnBoardForUser(r.Context(), scope, id)
	}
	if len(items) == 0 || hideIfMissing(w, visible) {
		if len(items) == 0 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
		}
		return
	}
	if rejectVisualOnly(w, r, truthy(items[0]["visual_only"])) {
		return
	}
	a.noteCardView(r, "deal", id)
	ownerID := strings.TrimSpace(fmt.Sprint(items[0]["owner_id"]))
	if ownerID == "<nil>" {
		ownerID = ""
	}
	leadID := strings.TrimSpace(fmt.Sprint(items[0]["lead_id"]))
	if leadID == "<nil>" {
		leadID = ""
	}
	if leadID == "" {
		leadID = a.dealLinkedLeadID(r.Context(), id)
	}
	if leadID != "" && !onBoard {
		a.maybeMarkOwnerOpened(r.Context(), r, leadID, ownerID)
	}
	items[0]["activity"] = a.queryMaps(r.Context(), `
		SELECT a.id::text, a.entity_type, a.action, a.payload, a.created_at,
		       coalesce(u.full_name,'') AS actor
		FROM activity_log a
		LEFT JOIN users u ON u.id=a.actor_id
		WHERE (a.entity_type='deal' AND a.entity_id=$1::uuid)
		   OR (a.entity_type='lead' AND a.entity_id IN (
		         SELECT coalesce(d.lead_id, l.id)
		         FROM deals d
		         LEFT JOIN leads l ON l.converted_deal_id=d.id
		         WHERE d.id=$1::uuid
		       ))
		ORDER BY a.created_at DESC LIMIT 80`, id)
	items[0]["tasks"] = a.queryMaps(r.Context(), `SELECT id::text, title, status, due_at FROM tasks WHERE entity_type='deal' AND entity_id=$1 ORDER BY created_at DESC`, id)
	items[0]["calls"] = a.queryMaps(r.Context(), `
		SELECT id::text, direction, from_number, to_number, duration_sec, disposition, started_at,
		       coalesce(recording_key,'') AS recording_key, coalesce(exolve_call_id,'') AS exolve_call_id
		FROM calls
		WHERE (entity_type='deal' AND entity_id=$1::uuid)
		   OR (entity_type='lead' AND entity_id IN (
		         SELECT coalesce(d.lead_id, l.id)
		         FROM deals d
		         LEFT JOIN leads l ON l.converted_deal_id=d.id
		         WHERE d.id=$1::uuid
		       ))
		ORDER BY started_at DESC LIMIT 50`, id)
	items[0]["fields"] = a.entityFields(r.Context(), "deal", id)
	a.attachTicketToDealResponse(r.Context(), r, items[0])
	stageKey := fmt.Sprint(items[0]["stage_key"])
	sleepingCard := truthy(items[0]["sleeping"]) || strings.HasPrefix(stageKey, oldLeadsPipelineKey)
	if sleepingCard && (stageKey == escortPipelineKey+":ticket" || stageKey == escortPipelineKey+":ticket_request") {
		if pipe, stage := a.oldLeadsStageID(r.Context(), "bilet"); pipe != "" && stage != "" {
			items[0]["pipeline_id"] = pipe
			items[0]["stage_id"] = stage
			items[0]["stage"] = "Билет"
			items[0]["stage_short"] = "Билет"
			items[0]["stage_key"] = oldLeadsPipelineKey + ":bilet"
		}
	} else if (isManager(r) || isROP(r)) && !sleepingCard && a.applyManagerPostBiletStage(r.Context(), id, items[0]) {
		// колонка менеджера после «Билет»; стадия логиста в stage_id не подменяется для него
	} else if (isManager(r) || isROP(r)) && !sleepingCard && dealMirrorsToManagerBilet(
		stageKey, "", truthy(items[0]["logistics_needed"])) {
		if hirePipe, hireBilet := a.hireStageID(r.Context(), "bilet"); hirePipe != "" && hireBilet != "" {
			items[0]["pipeline_id"] = hirePipe
			items[0]["stage_id"] = hireBilet
			items[0]["stage"] = "Билет"
			items[0]["stage_short"] = "Билет"
			items[0]["stage_key"] = hirePipelineKey + ":bilet"
		}
	} else if isLogist(r) && !sleepingCard {
		a.remapHireBiletAsLogistRequest(r.Context(), items, true)
	}
	a.writePhones(w, r, 200, items[0])
}

func (a *App) patchDeal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.ensureLogisticsSchema(r.Context())
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if rejectVisualOnlyChange(w, a.dealVisualOnly(r.Context(), id)) {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	var peek map[string]any
	_ = json.Unmarshal(body, &peek)
	if ownerRaw, ok := peek["owner_id"]; ok {
		newOwner := strings.TrimSpace(fmt.Sprint(ownerRaw))
		if newOwner == "<nil>" {
			newOwner = ""
		}
		if isManager(r) {
			writeJSON(w, 403, map[string]string{"error": "менеджер не может менять ответственного"})
			return
		}
		force := isAdmin(r) && truthy(peek["force"])
		cur, sticky := a.dealStickyInfo(r.Context(), id)
		deny := func(msg string) {
			writeJSON(w, 409, map[string]any{
				"error":    msg,
				"owner_id": cur,
			})
		}
		if a.transferLeavesLockedDept(r.Context(), cur, newOwner) {
			deny(mskTowerRecallDeniedMsg)
			return
		}
		if leadStickyBlocksTransfer(cur, sticky, newOwner) != nil && !force {
			deny("сделка закреплена за менеджером, который уже работал с ней — смена ответственного запрещена")
			return
		}
		// Владелец сделки = владелец лида: меняем атомарно через лид (лид + все сделки одним UPDATE).
		if leadID := a.dealLinkedLeadID(r.Context(), id); leadID != "" {
			if !a.setLeadOwnerGuarded(r.Context(), leadID, newOwner, "", force) {
				cur, _ = a.leadStickyInfo(r.Context(), leadID)
				msg := "сделка закреплена за менеджером, который уже работал с ней — смена ответственного запрещена"
				if a.transferLeavesLockedDept(r.Context(), cur, newOwner) {
					msg = mskTowerRecallDeniedMsg
				}
				deny(msg)
				return
			}
		} else if a.transferLeavesLockedDept(r.Context(), cur, newOwner) {
			deny(mskTowerRecallDeniedMsg)
			return
		} else {
			_, _ = a.db.Exec(r.Context(), `UPDATE deals SET owner_id=nullif($2,'')::uuid, updated_at=now() WHERE id=$1::uuid`, id, newOwner)
		}
		if force && leadStickyBlocksTransfer(cur, sticky, newOwner) != nil {
			a.logDealLeadAct(r.Context(), id, actorID(r), "owner_changed_forced", map[string]any{"from": cur, "to": newOwner})
		}
		delete(peek, "owner_id")
		delete(peek, "force")
		body, _ = json.Marshal(peek)
		a.broadcastDealUpdated(r.Context(), id, nil)
		if len(peek) == 0 {
			a.enqueueDealAIEventFromDB(r.Context(), "deal.updated", id, actorID(r), int(time.Now().Unix()))
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err := a.patchTableExec(r, "deals", []string{"title", "amount", "currency", "contact_id", "company_id"}); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.enqueueDealAIEventFromDB(r.Context(), "deal.updated", id, actorID(r), int(time.Now().Unix()))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) moveDeal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.ensureLogisticsSchema(r.Context())
	if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), id)) {
		return
	}
	if rejectVisualOnlyChange(w, a.dealVisualOnly(r.Context(), id)) {
		return
	}
	var in struct {
		StageID string `json:"stage_id"`
	}
	_ = readJSON(r, &in)
	var pipeID string
	_ = a.db.QueryRow(r.Context(), `SELECT pipeline_id::text FROM pipeline_stages WHERE id=$1`, in.StageID).Scan(&pipeID)
	if pipeID == "" {
		writeJSON(w, 400, map[string]string{"error": "стадия не найдена"})
		return
	}
	onOld := a.dealOnOldLeadsPipeline(r.Context(), id)
	targetOld := a.isOldLeadsPipeline(r.Context(), pipeID)
	targetEscort := a.isEscortPipeline(r.Context(), pipeID)
	targetHire := a.isHirePipeline(r.Context(), pipeID)
	sleeping := onOld || a.dealIsSleeping(r.Context(), id)
	targetKey := ""
	switch {
	case targetOld:
		targetKey = oldLeadsPipelineKey
	case targetEscort:
		targetKey = escortPipelineKey
	case targetHire:
		targetKey = hirePipelineKey
	}
	if !sleepingHandoffAllowed(sleeping, targetKey) {
		if targetOld {
			writeJSON(w, 400, map[string]string{"error": "обычную карточку нельзя перенести в спящие лиды"})
		} else if sleeping && targetHire {
			writeJSON(w, 400, map[string]string{"error": "спящая карточка остаётся вне адаптации"})
		} else {
			writeJSON(w, 400, map[string]string{"error": "карточка остаётся в спящих лидах"})
		}
		return
	}
	if (isManager(r) || isROP(r)) && sleeping && !a.isOldLeadsBiletStage(r.Context(), in.StageID) {
		var needed bool
		_ = a.db.QueryRow(r.Context(), `SELECT coalesce(logistics_needed,false) FROM deals WHERE id=$1::uuid`, id).Scan(&needed)
		if needed || a.dealOnEscortPipeline(r.Context(), id) {
			writeJSON(w, 400, map[string]string{"error": "карточка остаётся в «Билет»"})
			return
		}
	}
	if isManager(r) && a.isEscortPipeline(r.Context(), pipeID) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа к воронке «Оформление и сопровождение»"})
		return
	}
	if a.biletCitiesRequired(r.Context(), id, in.StageID) {
		writeJSON(w, 400, map[string]string{"error": errBiletCitiesRequired})
		return
	}
	// Карточка уже у логиста: его stage_id не переписываем. Менеджер двигает только свою колонку после «Билет».
	if (isManager(r) || isROP(r)) && a.dealOnEscortPipeline(r.Context(), id) {
		if a.isHireBiletStage(r.Context(), in.StageID) {
			a.finishManagerStage(w, r, id, "")
			return
		}
		if a.isHirePostBiletStage(r.Context(), in.StageID) {
			a.finishManagerStage(w, r, id, in.StageID)
			return
		}
		writeJSON(w, 400, map[string]string{"error": "карточка остаётся в «Билет»"})
		return
	}
	if (isManager(r) || isROP(r)) && !a.isHireBiletStage(r.Context(), in.StageID) && !a.isHirePostBiletStage(r.Context(), in.StageID) &&
		(a.dealOnHireLogisticsQueue(r.Context(), id) || a.dealStageIn(r.Context(), id, hirePostBiletKeys()...)) {
		writeJSON(w, 400, map[string]string{"error": "карточка остаётся в «Билет»"})
		return
	}
	var prevStage string
	_ = a.db.QueryRow(r.Context(), `SELECT coalesce(stage_id::text,'') FROM deals WHERE id=$1::uuid`, id).Scan(&prevStage)
	var err error
	if isManager(r) || isROP(r) {
		_, err = a.db.Exec(r.Context(), `UPDATE deals SET stage_id=$1, pipeline_id=$2::uuid, manager_stage_id=NULL, updated_at=now() WHERE id=$3`, in.StageID, pipeID, id)
	} else if targetEscort {
		// Логист забирает карточку в свою воронку и оставляет менеджеру колонку после «Билет».
		_, err = a.db.Exec(r.Context(), `
			UPDATE deals SET stage_id=$1, pipeline_id=$2::uuid,
			    manager_stage_id=CASE
			      WHEN manager_stage_id IS NULL AND EXISTS (
			        SELECT 1 FROM pipeline_stages cs
			        WHERE cs.id=deals.stage_id
			          AND coalesce(cs.bitrix_id,'') IN (`+sqlHirePostBiletIn()+`)
			      ) THEN deals.stage_id
			      ELSE manager_stage_id
			    END,
			    updated_at=now()
			WHERE id=$3`, in.StageID, pipeID, id)
	} else {
		_, err = a.db.Exec(r.Context(), `UPDATE deals SET stage_id=$1, pipeline_id=$2::uuid WHERE id=$3`, in.StageID, pipeID, id)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	stageName, stageShort, stageDesc := a.stageDisplay(r.Context(), in.StageID)
	a.logDealLeadAct(r.Context(), id, actorID(r), "stage_changed", map[string]any{
		"stage_id":    in.StageID,
		"pipeline_id": pipeID,
		"stage_name":  stageName,
		"stage_short": stageShort,
		"stage_desc":  stageDesc,
	})
	var won, lost bool
	_ = a.db.QueryRow(r.Context(), `SELECT is_won, is_lost FROM pipeline_stages WHERE id=$1`, in.StageID).Scan(&won, &lost)
	if won {
		_, _ = a.db.Exec(r.Context(), `UPDATE deals SET closed_at=now() WHERE id=$1`, id)
		a.enqueueOfflineConversion(r.Context(), id)
		a.runAutomations(r.Context(), "deal.won", map[string]any{"id": id})
	}
	a.maybeStartNedozvonAutodial(r.Context(), id, in.StageID)
	enteredBilet := (a.isHireBiletStageID(r.Context(), in.StageID) || a.isOldLeadsBiletStage(r.Context(), in.StageID)) && prevStage != in.StageID
	enteredPost := a.isHirePostBiletStage(r.Context(), in.StageID) && prevStage != in.StageID
	if enteredBilet || enteredPost {
		var wasQueued bool
		_ = a.db.QueryRow(r.Context(), `SELECT coalesce(logistics_needed,false) FROM deals WHERE id=$1::uuid`, id).Scan(&wasQueued)
		_, _ = a.db.Exec(r.Context(), `
			UPDATE deals SET logistics_needed=true,
			    logistics_needed_at=COALESCE(logistics_needed_at, now())
			WHERE id=$1::uuid`, id)
		if enteredBilet || !wasQueued {
			go a.notifyLogistsDealToBilet(context.Background(), id)
		}
	}
	a.broadcastDealUpdated(r.Context(), id, map[string]any{"stage_id": in.StageID})
	a.enqueueDealAIEventFromDB(r.Context(), "deal.stage_changed", id, actorID(r), int(time.Now().Unix()))
	writeJSON(w, 200, map[string]any{"ok": true, "won": won})
}

// finishManagerStage пишет колонку менеджера и не меняет stage_id логиста.
// stageID пустой — вернуть карточку в «Билет» на доске менеджера.
func (a *App) finishManagerStage(w http.ResponseWriter, r *http.Request, dealID, stageID string) {
	ctx := r.Context()
	var err error
	if stageID == "" {
		_, err = a.db.Exec(ctx, `UPDATE deals SET manager_stage_id=NULL, updated_at=now() WHERE id=$1::uuid`, dealID)
		_, stageID = a.hireStageID(ctx, "bilet")
	} else {
		_, err = a.db.Exec(ctx, `UPDATE deals SET manager_stage_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, dealID, stageID)
	}
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	stageName, stageShort, stageDesc := "", "", ""
	var pipeID string
	if stageID != "" {
		stageName, stageShort, stageDesc = a.stageDisplay(ctx, stageID)
		_ = a.db.QueryRow(ctx, `SELECT coalesce(pipeline_id::text,'') FROM pipeline_stages WHERE id=$1::uuid`, stageID).Scan(&pipeID)
	}
	a.logDealLeadAct(ctx, dealID, actorID(r), "stage_changed", map[string]any{
		"stage_id":    stageID,
		"pipeline_id": pipeID,
		"stage_name":  stageName,
		"stage_short": stageShort,
		"stage_desc":  stageDesc,
	})
	var realStage string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(stage_id::text,'') FROM deals WHERE id=$1::uuid`, dealID).Scan(&realStage)
	a.broadcastDealUpdated(ctx, dealID, map[string]any{"stage_id": realStage})
	a.enqueueDealAIEventFromDB(ctx, "deal.stage_changed", dealID, actorID(r), int(time.Now().Unix()))
	writeJSON(w, 200, map[string]any{"ok": true, "won": false})
}

func (a *App) timerStart(w http.ResponseWriter, r *http.Request) {
	_, err := a.db.Exec(r.Context(), `INSERT INTO time_entries (task_id, user_id, started_at) VALUES ($1::uuid,$2::uuid,now())`, chi.URLParam(r, "id"), actorID(r))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) timerStop(w http.ResponseWriter, r *http.Request) {
	_, err := a.db.Exec(r.Context(), `
		UPDATE time_entries SET ended_at=now(), seconds=extract(epoch from (now()-started_at))::int
		WHERE id=(SELECT id FROM time_entries WHERE task_id=$1::uuid AND user_id=$2::uuid AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1)`,
		chi.URLParam(r, "id"), actorID(r))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) listProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.queryMaps(r.Context(), `SELECT id::text, name, status, created_at FROM projects ORDER BY created_at DESC`))
}

func (a *App) createProject(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = readJSON(r, &in)
	var id string
	err := a.db.QueryRow(r.Context(), `INSERT INTO projects (name, deal_id, owner_id) VALUES ($1, nullif($2,'')::uuid, $3::uuid) RETURNING id::text`,
		str(in, "name"), str(in, "deal_id"), actorID(r)).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) listCalls(w http.ResponseWriter, r *http.Request) {
	args := []any{}
	where := ""
	if !isAdmin(r) {
		uid := actorID(r)
		if uid == "" {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		args = append(args, uid)
		where = ` WHERE user_id=$1::uuid`
	}
	a.writePhones(w, r, 200, a.queryMaps(r.Context(), `
		SELECT c.id::text, c.direction, c.from_number, c.to_number, c.duration_sec, c.disposition, c.entity_type, c.entity_id::text, c.started_at,
		       coalesce(c.recording_key,'') AS recording_key, coalesce(c.exolve_call_id,'') AS exolve_call_id,
		       coalesce(cr.status,'') AS review_status
		FROM calls c
		LEFT JOIN call_reviews cr ON cr.call_id=c.id`+strings.Replace(where, "user_id=", "c.user_id=", 1)+` ORDER BY c.started_at DESC LIMIT 200`, args...))
}

func (a *App) liveCalls(w http.ResponseWriter, r *http.Request) {
	live := []map[string]string{}
	if a.ami != nil {
		live = a.ami.Snapshot()
	}
	a.writePhones(w, r, 200, map[string]any{"live": len(live), "channels": live})
}

func (a *App) listDocuments(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.queryMaps(r.Context(), `SELECT id::text, kind, number, title, amount::text, status, sign_status, created_at FROM documents ORDER BY created_at DESC LIMIT 200`))
}

func (a *App) createDocument(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = readJSON(r, &in)
	num := fmt.Sprintf("%s-%d", strings.ToUpper(str(in, "kind")), time.Now().Unix()%100000)
	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO documents (kind, number, title, entity_type, entity_id, amount, created_by)
		VALUES ($1,$2,$3,nullif($4,''),nullif($5,'')::uuid, nullif($6,'')::numeric, $7::uuid) RETURNING id::text`,
		nz(str(in, "kind"), "invoice"), num, nz(str(in, "title"), "Документ"), str(in, "entity_type"), str(in, "entity_id"), str(in, "amount"), actorID(r)).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "number": num})
}

func (a *App) listEvents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.queryMaps(r.Context(), `SELECT id::text, title, starts_at, ends_at, meet_url FROM calendar_events ORDER BY starts_at DESC LIMIT 200`))
}

func (a *App) createEvent(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	_ = readJSON(r, &in)
	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO calendar_events (title, starts_at, ends_at, owner_id, entity_type, entity_id, meet_url)
		VALUES ($1,$2::timestamptz,$3::timestamptz,$4::uuid,nullif($5,''),nullif($6,'')::uuid,$7) RETURNING id::text`,
		str(in, "title"), str(in, "starts_at"), str(in, "ends_at"), actorID(r), str(in, "entity_type"), str(in, "entity_id"), str(in, "meet_url")).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (a *App) listNotifications(w http.ResponseWriter, r *http.Request) {
	uid := actorID(r)
	// Диалоги staff + пропущенные + MAX. Без «новый лид» / autodial_return и т.п.
	items := a.queryMaps(r.Context(), `
		SELECT id::text, title, coalesce(body,'') AS body, is_read, channel, payload, created_at
		FROM notifications
		WHERE user_id=$1
		  AND channel IN ('staff_dialog','missed_call','inbox_max')
		ORDER BY created_at DESC LIMIT 40`, uid)
	if items == nil {
		items = []map[string]any{}
	}
	unread := a.queryInt(r.Context(), `
		SELECT count(*) FROM notifications
		WHERE user_id=$1 AND is_read=false
		  AND channel IN ('staff_dialog','missed_call','inbox_max')`, uid)
	writeJSON(w, 200, map[string]any{"items": items, "unread": unread})
}

func (a *App) readNotification(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(), `UPDATE notifications SET is_read=true WHERE id=$1 AND user_id=$2`, chi.URLParam(r, "id"), actorID(r))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) listChat(w http.ResponseWriter, r *http.Request) {
	ch := r.URL.Query().Get("channel")
	if ch == "" {
		ch = "general"
	}
	writeJSON(w, 200, a.queryMaps(r.Context(), `
		SELECT m.id::text, m.body, m.created_at, u.full_name FROM chat_messages m JOIN users u ON u.id=m.user_id
		WHERE m.channel=$1 ORDER BY m.created_at DESC LIMIT 100`, ch))
}

func (a *App) postChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Channel string `json:"channel"`
		Body    string `json:"body"`
	}
	_ = readJSON(r, &in)
	if in.Channel == "" {
		in.Channel = "general"
	}
	if a.gateManagerOutbound(w, r, "chat", in.Channel, in.Body, false) {
		return
	}
	_, err := a.db.Exec(r.Context(), `INSERT INTO chat_messages (channel, user_id, body) VALUES ($1,$2::uuid,$3)`, in.Channel, actorID(r), in.Body)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	a.hub.Broadcast("chat", map[string]any{"channel": in.Channel, "body": in.Body, "user_id": actorID(r)})
	writeJSON(w, 201, map[string]any{"ok": true})
}

func (a *App) listActivity(w http.ResponseWriter, r *http.Request) {
	entityType := strings.TrimSpace(chi.URLParam(r, "type"))
	entityID := strings.TrimSpace(chi.URLParam(r, "id"))
	if entityType == "" || entityID == "" {
		writeJSON(w, 400, map[string]string{"error": "bad entity"})
		return
	}
	a.ensureLogisticsSchema(r.Context())
	switch entityType {
	case "lead":
		if hideIfMissing(w, a.canSeeLead(r.Context(), a.crmScope(r), entityID)) {
			return
		}
		if rejectVisualOnly(w, r, a.leadVisualOnly(r.Context(), entityID)) {
			return
		}
	case "deal":
		if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), entityID)) {
			return
		}
		if rejectVisualOnly(w, r, a.dealVisualOnly(r.Context(), entityID)) {
			return
		}
	case "contact":
		if hideIfMissing(w, a.contactVisible(r.Context(), a.crmScope(r), entityID)) {
			return
		}
	default:
		if !isAdmin(r) {
			writeJSON(w, 403, map[string]string{"error": "нет доступа"})
			return
		}
	}
	a.writePhones(w, r, 200, a.queryMaps(r.Context(), `
		SELECT action, payload, created_at FROM activity_log
		WHERE entity_type=$1 AND entity_id=$2
		ORDER BY created_at DESC LIMIT 100`, entityType, entityID))
}

func (a *App) addComment(w http.ResponseWriter, r *http.Request) {
	entityType := strings.TrimSpace(chi.URLParam(r, "type"))
	entityID := strings.TrimSpace(chi.URLParam(r, "id"))
	if entityType == "" || entityID == "" {
		writeJSON(w, 400, map[string]string{"error": "bad entity"})
		return
	}
	a.ensureLogisticsSchema(r.Context())
	switch entityType {
	case "lead":
		if hideIfMissing(w, a.canSeeLead(r.Context(), a.crmScope(r), entityID)) {
			return
		}
		if rejectVisualOnly(w, r, a.leadVisualOnly(r.Context(), entityID)) {
			return
		}
	case "deal":
		if hideIfMissing(w, a.canSeeDeal(r.Context(), a.crmScope(r), entityID)) {
			return
		}
		if rejectVisualOnly(w, r, a.dealVisualOnly(r.Context(), entityID)) {
			return
		}
	default:
		writeJSON(w, 400, map[string]string{"error": "unsupported entity"})
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	_ = readJSON(r, &in)
	body := strings.TrimSpace(in.Body)
	if body == "" {
		writeJSON(w, 400, map[string]string{"error": "пустой комментарий"})
		return
	}
	if len([]rune(body)) > 8000 {
		body = string([]rune(body)[:8000])
	}
	a.logAct(r.Context(), entityType, entityID, actorID(r), "comment", map[string]any{"body": body})
	writeJSON(w, 201, map[string]any{"ok": true})
}

func (a *App) listUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.scopedEmployeeRows(r.Context(), r))
}

const employeeSelectSQL = `
		SELECT u.id::text, coalesce(u.email,'') AS email, coalesce(u.login,'') AS login,
		       u.full_name, coalesce(r.name,'') AS role,
		       coalesce(u.phone_extension,'') AS phone_extension,
		       coalesce(u.phone_extension,'') AS extension,
		       coalesce(u.work_phone,'') AS work_phone,
		       u.can_call, u.is_active,
		       coalesce(u.department_id::text,'') AS department_id,
		       coalesce(d.name,'') AS department,
		       EXISTS (
		         SELECT 1 FROM vox_accounts v
		         WHERE v.user_id = u.id AND v.active
		           AND coalesce(v.username,'') <> ''
		           AND coalesce(v.password,'') <> ''
		       ) AS has_vox
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN departments d ON d.id=u.department_id`

func (a *App) employeeRows(ctx context.Context) []map[string]any {
	rows := a.queryMaps(ctx, employeeSelectSQL+`
		WHERE lower(coalesce(r.name,'')) <> 'customer'
		ORDER BY u.full_name`)
	if rows == nil {
		return []map[string]any{}
	}
	return rows
}

// scopedEmployeeRows — admin видит всех; РОП — только менеджеров своего отдела; остальные — себя.
func (a *App) scopedEmployeeRows(ctx context.Context, r *http.Request) []map[string]any {
	if isAdmin(r) {
		return a.employeeRows(ctx)
	}
	if isROP(r) {
		dept := a.actorDepartmentID(ctx, actorID(r))
		if dept == "" {
			return []map[string]any{}
		}
		rows := a.queryMaps(ctx, employeeSelectSQL+`
			WHERE u.department_id=$1::uuid
			  AND lower(coalesce(r.name,'')) IN ('manager', 'sales')
			ORDER BY u.full_name`, dept)
		if rows == nil {
			return []map[string]any{}
		}
		return rows
	}
	rows := a.queryMaps(ctx, employeeSelectSQL+`
		WHERE u.id=$1::uuid
		ORDER BY u.full_name`, actorID(r))
	if rows == nil {
		return []map[string]any{}
	}
	return rows
}

func (a *App) listEmployees(w http.ResponseWriter, r *http.Request) {
	users := a.scopedEmployeeRows(r.Context(), r)
	deps := a.queryMaps(r.Context(), `
		SELECT d.id::text, d.name, d.parent_id::text AS parent_id,
		       coalesce(d.head_user_id::text,'') AS head_id,
		       coalesce(h.full_name,'') AS head_name,
		       (SELECT count(*) FROM users u WHERE u.department_id=d.id AND u.is_active) AS people
		FROM departments d
		LEFT JOIN users h ON h.id=d.head_user_id
		ORDER BY d.name`)
	if deps == nil {
		deps = []map[string]any{}
	}

	if isROP(r) {
		deptID := a.actorDepartmentID(r.Context(), actorID(r))
		if deptID != "" {
			own := make([]map[string]any, 0)
			for _, d := range deps {
				if fmt.Sprint(d["id"]) == deptID {
					own = append(own, d)
				}
			}
			deps = own
		} else {
			deps = []map[string]any{}
		}
	} else if !isAdmin(r) {
		deps = []map[string]any{}
	}

	active, total := 0, len(users)
	for _, u := range users {
		if fmt.Sprint(u["is_active"]) == "true" {
			active++
		}
	}
	out := map[string]any{
		"users": users, "departments": deps, "active": active, "total": total,
		"can_offboard":        isAdmin(r),
		"can_edit_department": isAdmin(r),
		"can_create":          canManageStaff(r),
		"creatable_roles":     creatableRoles(r),
	}
	if isAdmin(r) {
		out["numbers"] = a.numberRegistry(r.Context(), true)
	} else {
		out["numbers"] = map[string]any{
			"people": []any{}, "lines": []any{}, "quarantine": []any{}, "log": []any{},
		}
	}
	writeJSON(w, 200, out)
}

func (a *App) createUser(w http.ResponseWriter, r *http.Request) {
	if !canManageStaff(r) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	var in map[string]any
	_ = readJSON(r, &in)
	role := normStaffRole(nz(str(in, "role"), "manager"))
	if role == "admin" && !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "нельзя создать администратора"})
		return
	}
	if !canCreateRole(r, role) {
		writeJSON(w, 403, map[string]string{"error": "эту роль создать нельзя"})
		return
	}
	email := strings.TrimSpace(str(in, "email"))
	fullName := strings.TrimSpace(str(in, "full_name"))
	pass := str(in, "password")
	if fullName == "" || pass == "" {
		writeJSON(w, 400, map[string]string{"error": "укажите ФИО и пароль"})
		return
	}
	login := a.resolveStaffLogin(r.Context(), str(in, "login"), fullName, email)
	if login == "" {
		writeJSON(w, 400, map[string]string{"error": "не удалось сформировать логин"})
		return
	}
	ext := str(in, "phone_extension")
	if a.extensionBlocked(r.Context(), ext) {
		writeJSON(w, 409, map[string]string{"error": "внутренний номер в карантине после увольнения, подождите 14 дней"})
		return
	}
	hash, _ := bcryptHash(pass)
	var roleID string
	if err := a.db.QueryRow(r.Context(), `SELECT id FROM roles WHERE name=$1`, role).Scan(&roleID); err != nil || roleID == "" {
		writeJSON(w, 400, map[string]string{"error": "роль не найдена"})
		return
	}
	deptID := strings.TrimSpace(str(in, "department_id"))
	if isROP(r) {
		var own string
		_ = a.db.QueryRow(r.Context(), `
			SELECT coalesce(department_id::text,'') FROM users WHERE id=$1`, actorID(r)).Scan(&own)
		if own == "" {
			_ = a.db.QueryRow(r.Context(), `
				SELECT id::text FROM departments WHERE head_user_id=$1::uuid LIMIT 1`, actorID(r)).Scan(&own)
		}
		deptID = own
		if deptID == "" {
			writeJSON(w, 400, map[string]string{"error": "у вас нет отдела — обратитесь к администратору"})
			return
		}
	}
	mskTower := mskTowerFlagForCreate(role, in["msk_tower"])
	var id string
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO users (email, login, password_hash, full_name, role_id, phone_extension, can_call, department_id, msk_tower)
		VALUES (nullif($1,''), $2, $3, $4, $5::uuid, nullif($6,''), coalesce($7,true), nullif($8,'')::uuid, $9)
		RETURNING id::text`,
		email, login, hash, fullName, roleID, ext, true, deptID, mskTower).Scan(&id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{
		"id": id, "email": email, "login": login, "full_name": fullName, "role": role, "role_label": fmtRoleLabel(role),
		"msk_tower": mskTower,
	})
}

func (a *App) listRoles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.queryMaps(r.Context(), `SELECT id::text, name, permissions FROM roles`))
}

func (a *App) listAutomations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.queryMaps(r.Context(), `SELECT id::text, name, is_active, trigger_type, trigger_filter, actions FROM automations`))
}

func (a *App) patchAutomation(w http.ResponseWriter, r *http.Request) {
	a.patchTable(w, r, "automations", []string{"name", "is_active"})
}

func (a *App) reportSummary(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "выгрузки только для администратора"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"deals_by_stage":  a.queryMaps(r.Context(), `SELECT s.name, count(*) AS n, coalesce(sum(d.amount),0)::text AS amount FROM deals d JOIN pipeline_stages s ON s.id=d.stage_id GROUP BY s.name, s.sort_order ORDER BY s.sort_order`),
		"leads_by_source": a.queryMaps(r.Context(), `SELECT source, count(*) AS n FROM leads GROUP BY source ORDER BY n DESC`),
		"calls":           a.queryMaps(r.Context(), `SELECT direction, count(*) AS n, coalesce(avg(duration_sec),0)::int AS avg_sec FROM calls GROUP BY direction`),
	})
}

func (a *App) getContact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	items := a.queryMaps(r.Context(), `
		SELECT c.id::text, c.full_name, c.phone, c.email, c.position, c.source,
		       c.company_id::text, coalesce(co.name,'') AS company, c.created_at
		FROM contacts c LEFT JOIN companies co ON co.id=c.company_id WHERE c.id=$1`, id)
	if len(items) == 0 || hideIfMissing(w, a.contactVisible(r.Context(), a.crmScope(r), id)) {
		if len(items) == 0 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
		}
		return
	}
	items[0]["deals"] = a.queryMaps(r.Context(), `SELECT id::text, title, amount::text, currency FROM deals WHERE contact_id=$1 ORDER BY created_at DESC`, id)
	items[0]["leads"] = a.queryMaps(r.Context(), `SELECT id::text, title, status FROM leads WHERE contact_id=$1 ORDER BY created_at DESC`, id)
	items[0]["fields"] = a.entityFields(r.Context(), "contact", id)
	a.writePhones(w, r, 200, items[0])
}

func (a *App) getCompany(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	items := a.queryMaps(r.Context(), `
		SELECT id::text, name, inn, website, phone, email, address, created_at FROM companies WHERE id=$1`, id)
	if len(items) == 0 || hideIfMissing(w, a.companyVisible(r.Context(), a.crmScope(r), id)) {
		if len(items) == 0 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
		}
		return
	}
	items[0]["contacts"] = a.queryMaps(r.Context(), `SELECT id::text, full_name, phone, email, position FROM contacts WHERE company_id=$1 ORDER BY created_at DESC`, id)
	items[0]["deals"] = a.queryMaps(r.Context(), `SELECT id::text, title, amount::text, currency FROM deals WHERE company_id=$1 ORDER BY created_at DESC`, id)
	items[0]["leads"] = a.queryMaps(r.Context(), `SELECT id::text, title, status FROM leads WHERE company_id=$1 ORDER BY created_at DESC`, id)
	a.writePhones(w, r, 200, items[0])
}

func (a *App) getByID(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items := a.queryMaps(r.Context(), "SELECT * FROM "+table+" WHERE id=$1", chi.URLParam(r, "id"))
		if len(items) == 0 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, items[0])
	}
}

func (a *App) patchTable(w http.ResponseWriter, r *http.Request, table string, allowed []string) {
	if err := a.patchTableExec(r, table, allowed); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) patchTableExec(r *http.Request, table string, allowed []string) error {
	var in map[string]any
	if err := readJSON(r, &in); err != nil {
		return fmt.Errorf("json")
	}
	sets := []string{}
	args := []any{}
	i := 1
	allow := map[string]bool{}
	for _, k := range allowed {
		allow[k] = true
	}
	for k, v := range in {
		if !allow[k] {
			continue
		}
		if strings.HasSuffix(k, "_id") {
			if s, ok := v.(string); ok && s == "" {
				v = nil
			}
		}
		if k == "amount" {
			if s, ok := v.(string); ok && s == "" {
				v = nil
			}
		}
		sets = append(sets, fmt.Sprintf("%s=$%d", k, i))
		args = append(args, v)
		i++
	}
	if len(sets) == 0 {
		return fmt.Errorf("no fields")
	}
	args = append(args, chi.URLParam(r, "id"))
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id=$%d", table, strings.Join(sets, ","), i)
	if _, err := a.db.Exec(r.Context(), q, args...); err != nil {
		return err
	}
	return nil
}

func (a *App) logAct(ctx context.Context, typ, id, actor, action string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = a.db.Exec(ctx, `INSERT INTO activity_log (entity_type, entity_id, actor_id, action, payload) VALUES ($1,$2::uuid,nullif($3,'')::uuid,$4,$5::jsonb)`,
		typ, id, actor, action, string(b))
}

// userIsDepartmentHead — РОП по departments.head_user_id (роль может быть manager).
func (a *App) userIsDepartmentHead(ctx context.Context, userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM departments WHERE head_user_id=$1::uuid LIMIT 1`, userID).Scan(&n)
	return err == nil
}

// maybeMarkOwnerOpened — менеджер-владелец открыл карточку → лид больше не отзываем/не выдаём.
// Admin/РОП/head отдела не пишут флаг (просмотр банка не блокирует раздачу).
func (a *App) maybeMarkOwnerOpened(ctx context.Context, r *http.Request, leadID, ownerID string) {
	leadID = strings.TrimSpace(leadID)
	ownerID = strings.TrimSpace(ownerID)
	actor := strings.TrimSpace(actorID(r))
	if !shouldRecordOwnerOpened(roleName(r), actor, ownerID, a.userIsDepartmentHead(ctx, actor)) {
		return
	}
	if leadID == "" {
		return
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM activity_log a
		WHERE a.action='owner_opened'
		  AND (
		    (a.entity_type='lead' AND a.entity_id=$1::uuid)
		    OR (a.entity_type='deal' AND a.entity_id IN (
		          SELECT d.id FROM deals d
		          WHERE d.lead_id=$1::uuid
		             OR d.id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
		        ))
		  )
		LIMIT 1`, leadID).Scan(&n)
	if err == nil {
		return
	}
	a.logAct(ctx, "lead", leadID, actor, "owner_opened", nil)
}

// shouldRecordOwnerOpened — чистый гейт без БД (для жёстких тестов).
func shouldRecordOwnerOpened(role, actor, owner string, isDeptHead bool) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	actor = strings.TrimSpace(actor)
	owner = strings.TrimSpace(owner)
	if actor == "" || owner == "" || actor != owner {
		return false
	}
	if role == "admin" || role == "rop" || isDeptHead {
		return false
	}
	switch role {
	case "manager", "sales":
		return true
	default:
		return false
	}
}

// logDealLeadAct пишет activity на сделку и связанный лид (карточка открывается как lead).
func (a *App) logDealLeadAct(ctx context.Context, dealID, actor, action string, payload any) {
	dealID = strings.TrimSpace(dealID)
	if dealID == "" {
		return
	}
	a.logAct(ctx, "deal", dealID, actor, action, payload)
	leadID := a.dealLinkedLeadID(ctx, dealID)
	if leadID != "" {
		a.logAct(ctx, "lead", leadID, actor, action, payload)
	}
}

func (a *App) dealLinkedLeadID(ctx context.Context, dealID string) string {
	var leadID string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(nullif(lead_id::text, ''), '')
		FROM deals WHERE id=$1::uuid`, dealID).Scan(&leadID)
	if leadID != "" {
		return leadID
	}
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM leads WHERE converted_deal_id=$1::uuid LIMIT 1`, dealID).Scan(&leadID)
	return strings.TrimSpace(leadID)
}

func (a *App) userFullName(ctx context.Context, userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	var name string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(full_name,'') FROM users WHERE id=$1::uuid`, userID).Scan(&name)
	return strings.TrimSpace(name)
}

func (a *App) stageDisplay(ctx context.Context, stageID string) (name, short, desc string) {
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(name,''),
		       coalesce(nullif(short_name,''), name),
		       coalesce(description,'')
		FROM pipeline_stages WHERE id=$1`, stageID).Scan(&name, &short, &desc)
	return
}

func (a *App) runAutomations(ctx context.Context, trigger string, payload map[string]any) {
	rows, err := a.db.Query(ctx, `SELECT id::text, actions, trigger_filter FROM automations WHERE is_active AND trigger_type=$1`, trigger)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, actions, filter string
		if err := rows.Scan(&id, &actions, &filter); err != nil {
			continue
		}
		if strings.Contains(filter, "vk_ads") && str(payload, "source") != "vk_ads" && trigger == "lead.created" {
			continue
		}
		if strings.Contains(actions, "create_task") {
			entityID := str(payload, "id")
			if entityID == "" {
				continue
			}
			title := "Автозадача"
			entityType := strings.TrimSuffix(trigger, ".created")
			if trigger == "lead.created" {
				title = "Обработать лид"
				if str(payload, "source") == "vk_ads" {
					title = "Обработать лид VK Ads"
				}
			}
			// Без исполнителя задача светится у всех — создаём только на владельца сущности.
			var assigneeID string
			switch entityType {
			case "lead":
				_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1::uuid`, entityID).Scan(&assigneeID)
			case "deal":
				_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM deals WHERE id=$1::uuid`, entityID).Scan(&assigneeID)
			}
			assigneeID = strings.TrimSpace(assigneeID)
			if assigneeID == "" {
				continue
			}
			_, _ = a.db.Exec(ctx, `
				INSERT INTO tasks (title, entity_type, entity_id, status, priority, assignee_id, creator_id, activity_at)
				VALUES ($1,$2,$3::uuid,'in_progress','high',$4::uuid,$4::uuid, now())`,
				title, entityType, entityID, assigneeID)
		}
		if strings.Contains(actions, "vk_offline_conversion") {
			if did, ok := payload["id"].(string); ok {
				a.enqueueOfflineConversion(ctx, did)
			}
		}
	}
}

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	switch v := m[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

func (a *App) listUserFields(w http.ResponseWriter, r *http.Request) {
	entity := strings.TrimSpace(r.URL.Query().Get("entity"))
	if entity == "" {
		entity = "deal"
	}
	writeJSON(w, 200, map[string]any{
		"items": a.queryMaps(r.Context(), `
			SELECT id::text, entity_type, code, label, field_type, field_group, multiple, sort
			FROM user_fields WHERE entity_type=$1 AND active
			ORDER BY sort, label`, entity),
	})
}

func (a *App) entityFields(ctx context.Context, entity, id string) []map[string]any {
	return a.queryMaps(ctx, `
		SELECT uf.code, uf.label, uf.field_type, uf.field_group, uf.multiple,
		       v.value_text AS value
		FROM entity_field_values v
		JOIN user_fields uf ON uf.id = v.field_id
		WHERE v.entity_type=$1 AND v.entity_id=$2::uuid
		ORDER BY uf.sort, uf.label`, entity, id)
}

func nz(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func bcryptHash(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), 12)
	return string(b), err
}
