package bitrix

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Count struct {
	Total    int    `json:"total"`
	Imported int    `json:"imported"`
	Error    string `json:"error,omitempty"`
}

type Progress struct {
	Status string           `json:"status"`
	Phase  string           `json:"phase"`
	Portal string           `json:"portal"`
	Counts map[string]Count `json:"counts"`
	Error  string           `json:"error,omitempty"`
}

type Importer struct {
	db     *pgxpool.Pool
	client *Client
	want   map[string]bool
	prog   Progress
	hook   func(Progress)

	users     map[string]string
	deps      map[string]string
	companies map[string]string
	contacts  map[string]string
	leads     map[string]string
	deals     map[string]string
	stages    map[string]string
	pipelines map[string]string
	salesRole string
	dummyHash string
}

var defaultEntities = []string{"users", "departments", "pipelines", "companies", "contacts", "leads", "deals", "tasks"}

func Inventory(ctx context.Context, webhook string) (Progress, error) {
	c, err := NewClient(webhook)
	if err != nil {
		return Progress{}, err
	}
	out := Progress{Status: "idle", Portal: c.Portal(), Counts: map[string]Count{}}
	type spec struct {
		key, method string
		params      map[string]any
	}
	jobs := []spec{
		{"users", "user.get", map[string]any{"ADMIN_MODE": true}},
		{"departments", "department.get", nil},
		{"companies", "crm.company.list", map[string]any{"select": []string{"ID"}}},
		{"contacts", "crm.contact.list", map[string]any{"select": []string{"ID"}}},
		{"leads", "crm.lead.list", map[string]any{"select": []string{"ID"}}},
		{"deals", "crm.deal.list", map[string]any{"select": []string{"ID"}}},
		{"tasks", "tasks.task.list", map[string]any{"select": []string{"ID"}}},
		{"pipelines", "crm.dealcategory.list", nil},
		{"fields", "crm.deal.list", map[string]any{"select": []string{"ID"}}},
		{"task_roles", "tasks.task.list", map[string]any{"select": []string{"ID"}}},
		{"task_comments", "tasks.task.list", map[string]any{"select": []string{"ID"}}},
	}
	for _, j := range jobs {
		n, err := c.Count(ctx, j.method, j.params)
		if err != nil && j.key == "users" {
			n, err = c.Count(ctx, "user.get", nil)
		}
		if err != nil {
			if j.key == "users" {
				err = scopeHint(err, "в входящем вебхуке нет права «Пользователи» — откройте вебхук и включите Пользователи, затем повторите")
			}
			out.Counts[j.key] = Count{Error: redactErr(err)}
			continue
		}
		out.Counts[j.key] = Count{Total: n}
	}
	return out, nil
}

func NewImporter(db *pgxpool.Pool, webhook string, entities []string, hook func(Progress)) (*Importer, error) {
	c, err := NewClient(webhook)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	if len(entities) == 0 {
		entities = defaultEntities
	}
	for _, e := range entities {
		want[strings.TrimSpace(strings.ToLower(e))] = true
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(randomPass()), 10)
	im := &Importer{
		db: db, client: c, want: want, hook: hook, dummyHash: string(hash),
		users: map[string]string{}, deps: map[string]string{}, companies: map[string]string{},
		contacts: map[string]string{}, leads: map[string]string{}, deals: map[string]string{},
		stages: map[string]string{}, pipelines: map[string]string{},
		prog: Progress{Status: "running", Portal: c.Portal(), Counts: map[string]Count{}},
	}
	return im, nil
}

func (im *Importer) Progress() Progress { return im.prog }

func (im *Importer) emit(phase string) {
	im.prog.Phase = phase
	if im.hook != nil {
		cp := im.prog
		cp.Counts = cloneCounts(im.prog.Counts)
		im.hook(cp)
	}
}

func (im *Importer) setCount(key string, imported, total int, err error) {
	c := im.prog.Counts[key]
	if imported >= 0 {
		c.Imported = imported
	}
	if total >= 0 {
		c.Total = total
	}
	if err != nil {
		c.Error = redactErr(err)
	}
	im.prog.Counts[key] = c
}

func (im *Importer) Run(ctx context.Context) error {
	im.emit("prepare")
	if err := im.prepare(ctx); err != nil {
		return err
	}
	steps := []struct {
		key string
		fn  func(context.Context) error
	}{
		{"users", im.importUsers},
		{"departments", im.importDepartments},
		{"pipelines", im.importPipelines},
		{"companies", im.importCompanies},
		{"contacts", im.importContacts},
		{"leads", im.importLeads},
		{"deals", im.importDeals},
		{"tasks", im.importTasks},
		{"fields", im.importFields},
		{"task_roles", im.importTaskRoles},
		{"task_comments", im.importTaskComments},
	}
	for _, s := range steps {
		if !im.want[s.key] {
			continue
		}
		im.emit(s.key)
		if err := s.fn(ctx); err != nil {
			im.setCount(s.key, -1, -1, err)
			im.prog.Status = "error"
			im.prog.Error = s.key + ": " + redactErr(err)
			im.emit(s.key)
			return err
		}
		im.emit(s.key)
	}
	im.prog.Status = "done"
	im.prog.Phase = "done"
	im.emit("done")
	return nil
}

func (im *Importer) prepare(ctx context.Context) error {
	if err := im.db.QueryRow(ctx, `SELECT id::text FROM roles WHERE name='sales'`).Scan(&im.salesRole); err != nil {
		return fmt.Errorf("роль sales: %w", err)
	}
	im.loadMap(ctx, `SELECT bitrix_id, id::text FROM users WHERE bitrix_id IS NOT NULL`, im.users)
	im.loadMap(ctx, `SELECT bitrix_id, id::text FROM departments WHERE bitrix_id IS NOT NULL`, im.deps)
	im.loadMap(ctx, `SELECT bitrix_id, id::text FROM companies WHERE bitrix_id IS NOT NULL`, im.companies)
	im.loadMap(ctx, `SELECT bitrix_id, id::text FROM contacts WHERE bitrix_id IS NOT NULL`, im.contacts)
	im.loadMap(ctx, `SELECT bitrix_id, id::text FROM leads WHERE bitrix_id IS NOT NULL`, im.leads)
	im.loadMap(ctx, `SELECT bitrix_id, id::text FROM deals WHERE bitrix_id IS NOT NULL`, im.deals)
	im.loadMap(ctx, `SELECT bitrix_id, id::text FROM pipelines WHERE bitrix_id IS NOT NULL`, im.pipelines)
	im.loadMap(ctx, `SELECT bitrix_id, id::text FROM pipeline_stages WHERE bitrix_id IS NOT NULL`, im.stages)
	return nil
}

func (im *Importer) loadMap(ctx context.Context, q string, dst map[string]string) {
	rows, err := im.db.Query(ctx, q)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var bid, id string
		if rows.Scan(&bid, &id) == nil && bid != "" {
			dst[bid] = id
		}
	}
}

func (im *Importer) importUsers(ctx context.Context) error {
	rows, err := im.client.List(ctx, "user.get", map[string]any{"ADMIN_MODE": true})
	if err != nil {
		rows, err = im.client.List(ctx, "user.get", nil)
	}
	if err != nil {
		return scopeHint(err, "в входящем вебхуке нет права «Пользователи» — откройте вебхук и включите Пользователи, затем повторите")
	}
	n := 0
	for _, row := range rows {
		bid := rowString(row, "ID", "id")
		if bid == "" {
			continue
		}
		email := strings.ToLower(rowString(row, "EMAIL", "email"))
		if email == "" {
			email = fmt.Sprintf("bitrix%s@import.local", bid)
		}
		name := fullName(row)
		if name == "" {
			name = email
		}
		active := isActive(row["ACTIVE"])
		inner, work := bitrixUserPhones(row)
		dept := im.deps[firstDept(row["UF_DEPARTMENT"])]
		id, err := im.upsertUser(ctx, bid, email, name, active, inner, work, dept)
		if err != nil {
			log.Println("import user", bid, err)
			continue
		}
		im.users[bid] = id
		n++
		if n%50 == 0 {
			im.setCount("users", n, len(rows), nil)
			im.emit("users")
		}
	}
	im.setCount("users", n, len(rows), nil)
	return nil
}

func (im *Importer) upsertUser(ctx context.Context, bid, email, name string, active bool, ext, work, dept string) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM users WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		im.applyUserPhones(ctx, id, name, active, ext, work, dept)
		return id, nil
	}
	err = im.db.QueryRow(ctx, `SELECT id::text FROM users WHERE lower(email)=lower($1)`, email).Scan(&id)
	if err == nil {
		_, _ = im.db.Exec(ctx, `UPDATE users SET bitrix_id=$2, full_name=$3, is_active=$4 WHERE id=$1::uuid`, id, bid, name, active)
		im.applyUserPhones(ctx, id, name, active, ext, work, dept)
		return id, nil
	}
	err = im.db.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, full_name, role_id, bitrix_id, is_active, phone_extension, work_phone, department_id)
		VALUES ($1,$2,$3,$4::uuid,$5,$6,nullif($7,''),nullif($8,''),nullif($9,'')::uuid) RETURNING id::text`,
		email, im.dummyHash, name, im.salesRole, bid, active, ext, work, dept).Scan(&id)
	if err != nil && ext != "" {
		err = im.db.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, full_name, role_id, bitrix_id, is_active, work_phone, department_id)
			VALUES ($1,$2,$3,$4::uuid,$5,$6,nullif($7,''),nullif($8,'')::uuid) RETURNING id::text`,
			email, im.dummyHash, name, im.salesRole, bid, active, work, dept).Scan(&id)
	}
	return id, err
}

func (im *Importer) applyUserPhones(ctx context.Context, id, name string, active bool, ext, work, dept string) {
	_, err := im.db.Exec(ctx, `
		UPDATE users SET full_name=$2, is_active=$3,
			phone_extension=coalesce(nullif($4,''), phone_extension),
			work_phone=coalesce(nullif($5,''), work_phone),
			department_id=coalesce(nullif($6,'')::uuid, department_id)
		WHERE id=$1::uuid`, id, name, active, ext, work, dept)
	if err != nil && ext != "" {
		_, _ = im.db.Exec(ctx, `
			UPDATE users SET full_name=$2, is_active=$3,
				work_phone=coalesce(nullif($4,''), work_phone),
				department_id=coalesce(nullif($5,'')::uuid, department_id)
			WHERE id=$1::uuid`, id, name, active, work, dept)
	}
}

func (im *Importer) importDepartments(ctx context.Context) error {
	rows, err := im.client.List(ctx, "department.get", nil)
	if err != nil {
		return err
	}
	type dep struct{ id, name, parent string }
	list := make([]dep, 0, len(rows))
	for _, row := range rows {
		bid := rowString(row, "ID", "id")
		if bid == "" {
			continue
		}
		list = append(list, dep{id: bid, name: nz(rowString(row, "NAME", "name"), "Отдел "+bid), parent: rowString(row, "PARENT", "parent")})
	}
	n := 0
	for _, d := range list {
		id, err := im.upsertDept(ctx, d.id, d.name, "")
		if err != nil {
			continue
		}
		im.deps[d.id] = id
		n++
	}
	for _, d := range list {
		pid := im.deps[d.parent]
		if pid == "" || im.deps[d.id] == "" {
			continue
		}
		_, _ = im.db.Exec(ctx, `UPDATE departments SET parent_id=$2::uuid WHERE id=$1::uuid`, im.deps[d.id], pid)
	}
	im.setCount("departments", n, len(list), nil)
	return nil
}

func (im *Importer) upsertDept(ctx context.Context, bid, name, parentID string) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM departments WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		_, _ = im.db.Exec(ctx, `UPDATE departments SET name=$2 WHERE id=$1::uuid`, id, name)
		return id, nil
	}
	var parent any
	if parentID != "" {
		parent = parentID
	}
	err = im.db.QueryRow(ctx, `INSERT INTO departments (name, parent_id, bitrix_id) VALUES ($1, nullif($2,'')::uuid, $3) RETURNING id::text`,
		name, parent, bid).Scan(&id)
	return id, err
}

func (im *Importer) importPipelines(ctx context.Context) error {
	cats, err := im.client.List(ctx, "crm.dealcategory.list", map[string]any{"select": []string{"ID", "NAME", "SORT"}})
	if err != nil {
		cats = []map[string]any{{"ID": "0", "NAME": "Сделки"}}
	}
	hasZero := false
	for _, c := range cats {
		if rowString(c, "ID") == "0" {
			hasZero = true
		}
	}
	if !hasZero {
		cats = append([]map[string]any{{"ID": "0", "NAME": "Сделки"}}, cats...)
	}
	n := 0
	for _, cat := range cats {
		cid := rowString(cat, "ID", "id")
		name := nz(rowString(cat, "NAME", "name"), "Воронка "+cid)
		pid, err := im.upsertPipeline(ctx, cid, name)
		if err != nil {
			continue
		}
		im.pipelines[cid] = pid
		stages, err := im.client.List(ctx, "crm.dealcategory.stage.list", map[string]any{"id": cid})
		if err != nil && cid == "0" {
			stages, err = im.client.List(ctx, "crm.status.list", map[string]any{"filter": map[string]any{"ENTITY_ID": "DEAL_STAGE"}})
		}
		if err != nil {
			im.setCount("pipelines", n, len(cats), err)
			continue
		}
		for i, st := range stages {
			sid := rowString(st, "STATUS_ID", "statusId", "STATUS", "id")
			if sid == "" {
				continue
			}
			sname := nz(rowString(st, "NAME", "name"), sid)
			won, lost := semanticsWonLost(rowString(st, "SEMANTICS", "semantics"))
			sort := i
			if v := rowString(st, "SORT", "sort"); v != "" {
				fmt.Sscanf(v, "%d", &sort)
			}
			id, err := im.upsertStage(ctx, pid, sid, sname, sort, won, lost)
			if err != nil {
				continue
			}
			im.stages[sid] = id
		}
		n++
	}
	im.setCount("pipelines", n, len(cats), nil)
	return nil
}

func (im *Importer) upsertPipeline(ctx context.Context, bid, name string) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM pipelines WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		_, _ = im.db.Exec(ctx, `UPDATE pipelines SET name=$2 WHERE id=$1::uuid`, id, name)
		return id, nil
	}
	err = im.db.QueryRow(ctx, `INSERT INTO pipelines (name, kind, bitrix_id) VALUES ($1,'deals',$2) RETURNING id::text`, name, bid).Scan(&id)
	return id, err
}

func (im *Importer) upsertStage(ctx context.Context, pipelineID, bid, name string, sort int, won, lost bool) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		_, _ = im.db.Exec(ctx, `UPDATE pipeline_stages SET name=$2, sort_order=$3, is_won=$4, is_lost=$5, pipeline_id=$6::uuid WHERE id=$1::uuid`,
			id, name, sort, won, lost, pipelineID)
		return id, nil
	}
	err = im.db.QueryRow(ctx, `
		INSERT INTO pipeline_stages (pipeline_id, name, sort_order, is_won, is_lost, probability, bitrix_id)
		VALUES ($1::uuid,$2,$3,$4,$5,0,$6) RETURNING id::text`,
		pipelineID, name, sort, won, lost, bid).Scan(&id)
	return id, err
}

func (im *Importer) importCompanies(ctx context.Context) error {
	rows, err := im.client.List(ctx, "crm.company.list", map[string]any{
		"select": []string{"ID", "TITLE", "PHONE", "EMAIL", "WEB", "ADDRESS", "ASSIGNED_BY_ID"},
		"order":  map[string]any{"ID": "ASC"},
	})
	if err != nil {
		return err
	}
	n := 0
	for _, row := range rows {
		bid := rowString(row, "ID")
		if bid == "" {
			continue
		}
		id, err := im.upsertCompany(ctx, bid, nz(rowString(row, "TITLE"), "Компания "+bid),
			firstScalar(row["PHONE"]), firstScalar(row["EMAIL"]), firstScalar(row["WEB"]),
			rowString(row, "ADDRESS"), im.users[rowString(row, "ASSIGNED_BY_ID")])
		if err != nil {
			log.Println("import company", bid, err)
			continue
		}
		im.companies[bid] = id
		n++
		if n%100 == 0 {
			im.setCount("companies", n, len(rows), nil)
			im.emit("companies")
		}
	}
	im.setCount("companies", n, len(rows), nil)
	return nil
}

func (im *Importer) upsertCompany(ctx context.Context, bid, name, phone, email, web, addr, owner string) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM companies WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		_, err = im.db.Exec(ctx, `UPDATE companies SET name=$2, phone=$3, email=$4, website=$5, address=$6, owner_id=nullif($7,'')::uuid, updated_at=now() WHERE id=$1::uuid`,
			id, name, phone, email, web, addr, owner)
		return id, err
	}
	err = im.db.QueryRow(ctx, `
		INSERT INTO companies (name, phone, email, website, address, owner_id, bitrix_id)
		VALUES ($1,$2,$3,$4,$5,nullif($6,'')::uuid,$7) RETURNING id::text`,
		name, phone, email, web, addr, owner, bid).Scan(&id)
	return id, err
}

func (im *Importer) importContacts(ctx context.Context) error {
	return im.eachNew(ctx, "contacts", "crm.contact.list", "contacts", map[string]any{
		"select": []string{"ID", "NAME", "LAST_NAME", "SECOND_NAME", "PHONE", "EMAIL", "POST", "COMPANY_ID", "SOURCE_ID", "ASSIGNED_BY_ID"},
		"order":  map[string]any{"ID": "ASC"},
	}, func(row map[string]any) error {
		bid := rowString(row, "ID")
		if bid == "" {
			return nil
		}
		name := nz(fullName(row), "Контакт "+bid)
		id, err := im.upsertContact(ctx, bid, name, firstScalar(row["PHONE"]), firstScalar(row["EMAIL"]),
			rowString(row, "POST"), sourceID(rowString(row, "SOURCE_ID")),
			im.companies[rowString(row, "COMPANY_ID")], im.users[rowString(row, "ASSIGNED_BY_ID")])
		if err != nil {
			log.Println("import contact", bid, err)
			return nil
		}
		im.contacts[bid] = id
		return nil
	})
}

func (im *Importer) upsertContact(ctx context.Context, bid, name, phone, email, pos, source, company, owner string) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM contacts WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		_, err = im.db.Exec(ctx, `UPDATE contacts SET full_name=$2, email=$3, position=$4, source=$5, company_id=nullif($6,'')::uuid, owner_id=nullif($7,'')::uuid, updated_at=now() WHERE id=$1::uuid`,
			id, name, email, pos, source, company, owner)
		return id, err
	}
	err = im.db.QueryRow(ctx, `
		INSERT INTO contacts (full_name, phone, email, position, source, company_id, owner_id, bitrix_id)
		VALUES ($1,$2,$3,$4,$5,nullif($6,'')::uuid,nullif($7,'')::uuid,$8) RETURNING id::text`,
		name, phone, email, pos, source, company, owner, bid).Scan(&id)
	return id, err
}

func (im *Importer) importLeads(ctx context.Context) error {
	return im.eachNew(ctx, "leads", "crm.lead.list", "leads", map[string]any{
		"select": []string{"ID", "TITLE", "STATUS_ID", "SOURCE_ID", "OPPORTUNITY", "CURRENCY_ID", "CONTACT_ID", "COMPANY_ID", "ASSIGNED_BY_ID"},
		"order":  map[string]any{"ID": "ASC"},
	}, func(row map[string]any) error {
		bid := rowString(row, "ID")
		if bid == "" {
			return nil
		}
		title := nz(rowString(row, "TITLE"), "Лид "+bid)
		id, err := im.upsertLead(ctx, bid, title, leadStatus(rowString(row, "STATUS_ID")),
			sourceID(rowString(row, "SOURCE_ID")), rowString(row, "OPPORTUNITY"),
			nz(rowString(row, "CURRENCY_ID"), "RUB"),
			im.contacts[rowString(row, "CONTACT_ID")], im.companies[rowString(row, "COMPANY_ID")],
			im.users[rowString(row, "ASSIGNED_BY_ID")])
		if err != nil {
			log.Println("import lead", bid, err)
			return nil
		}
		im.leads[bid] = id
		return nil
	})
}

func (im *Importer) upsertLead(ctx context.Context, bid, title, status, source, amount, currency, contact, company, owner string) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM leads WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		_, err = im.db.Exec(ctx, `UPDATE leads SET title=$2, status=$3, source=$4, amount=nullif($5,'')::numeric, currency=$6, contact_id=nullif($7,'')::uuid, company_id=nullif($8,'')::uuid, owner_id=nullif($9,'')::uuid, updated_at=now() WHERE id=$1::uuid`,
			id, title, status, source, amount, currency, contact, company, owner)
		return id, err
	}
	err = im.db.QueryRow(ctx, `
		INSERT INTO leads (title, status, source, amount, currency, contact_id, company_id, owner_id, bitrix_id)
		VALUES ($1,$2,$3,nullif($4,'')::numeric,$5,nullif($6,'')::uuid,nullif($7,'')::uuid,nullif($8,'')::uuid,$9) RETURNING id::text`,
		title, status, source, amount, currency, contact, company, owner, bid).Scan(&id)
	return id, err
}

func (im *Importer) importDeals(ctx context.Context) error {
	fallbackPipe, fallbackStage := im.fallbackStage(ctx)
	return im.eachNew(ctx, "deals", "crm.deal.list", "deals", map[string]any{
		"select": []string{"ID", "TITLE", "STAGE_ID", "CATEGORY_ID", "OPPORTUNITY", "CURRENCY_ID", "CONTACT_ID", "COMPANY_ID", "ASSIGNED_BY_ID", "LEAD_ID", "CLOSEDATE"},
		"order":  map[string]any{"ID": "ASC"},
	}, func(row map[string]any) error {
		bid := rowString(row, "ID")
		if bid == "" {
			return nil
		}
		cat := rowString(row, "CATEGORY_ID")
		if cat == "" {
			cat = "0"
		}
		pipe := im.pipelines[cat]
		stage := im.stages[rowString(row, "STAGE_ID")]
		if pipe == "" {
			pipe = fallbackPipe
		}
		if stage == "" {
			stage = fallbackStage
		}
		if pipe == "" || stage == "" {
			log.Println("import deal: no pipeline/stage", bid)
			return nil
		}
		closed := rowString(row, "CLOSEDATE")
		id, err := im.upsertDeal(ctx, bid, nz(rowString(row, "TITLE"), "Сделка "+bid),
			rowString(row, "OPPORTUNITY"), nz(rowString(row, "CURRENCY_ID"), "RUB"),
			pipe, stage, im.contacts[rowString(row, "CONTACT_ID")], im.companies[rowString(row, "COMPANY_ID")],
			im.leads[rowString(row, "LEAD_ID")], im.users[rowString(row, "ASSIGNED_BY_ID")], closed)
		if err != nil {
			log.Println("import deal", bid, err)
			return nil
		}
		im.deals[bid] = id
		return nil
	})
}

func (im *Importer) fallbackStage(ctx context.Context) (pipe, stage string) {
	for _, id := range im.pipelines {
		pipe = id
		break
	}
	_ = im.db.QueryRow(ctx, `SELECT p.id::text, s.id::text FROM pipelines p JOIN pipeline_stages s ON s.pipeline_id=p.id ORDER BY s.sort_order LIMIT 1`).Scan(&pipe, &stage)
	return pipe, stage
}

func (im *Importer) upsertDeal(ctx context.Context, bid, title, amount, currency, pipe, stage, contact, company, lead, owner, closed string) (string, error) {
	if amount == "" {
		amount = "0"
	}
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM deals WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		_, err = im.db.Exec(ctx, `UPDATE deals SET title=$2, amount=$3::numeric, currency=$4, pipeline_id=$5::uuid, stage_id=$6::uuid, contact_id=nullif($7,'')::uuid, company_id=nullif($8,'')::uuid, lead_id=nullif($9,'')::uuid, owner_id=nullif($10,'')::uuid, closed_at=nullif($11,'')::timestamptz, updated_at=now() WHERE id=$1::uuid`,
			id, title, amount, currency, pipe, stage, contact, company, lead, owner, closed)
		return id, err
	}
	err = im.db.QueryRow(ctx, `
		INSERT INTO deals (title, amount, currency, pipeline_id, stage_id, contact_id, company_id, lead_id, owner_id, closed_at, bitrix_id)
		VALUES ($1,$2::numeric,$3,$4::uuid,$5::uuid,nullif($6,'')::uuid,nullif($7,'')::uuid,nullif($8,'')::uuid,nullif($9,'')::uuid,nullif($10,'')::timestamptz,$11)
		RETURNING id::text`,
		title, amount, currency, pipe, stage, contact, company, lead, owner, closed, bid).Scan(&id)
	return id, err
}

func (im *Importer) importTasks(ctx context.Context) error {
	return im.eachNew(ctx, "tasks", "tasks.task.list", "tasks", map[string]any{
		"select": []string{"ID", "TITLE", "DESCRIPTION", "RESPONSIBLE_ID", "CREATED_BY", "DEADLINE", "STATUS", "UF_CRM_TASK"},
		"order":  map[string]any{"ID": "asc"},
	}, func(row map[string]any) error {
		bid := rowString(row, "ID", "id")
		if bid == "" {
			return nil
		}
		title := nz(rowString(row, "TITLE", "title"), "Задача "+bid)
		desc := rowString(row, "DESCRIPTION", "description")
		deadline := rowString(row, "DEADLINE", "deadline")
		if len(deadline) >= 10 {
			deadline = deadline[:10]
		}
		assignee := im.users[rowString(row, "RESPONSIBLE_ID", "responsibleId")]
		creator := im.users[rowString(row, "CREATED_BY", "createdBy")]
		et, eid := crmBindings(row["UF_CRM_TASK"])
		if et == "" {
			et, eid = crmBindings(row["ufCrmTask"])
		}
		entityID := im.entityUUID(et, eid)
		_, err := im.upsertTask(ctx, bid, title, desc, deadline, taskStatus(rowString(row, "STATUS", "status")), assignee, creator, et, entityID)
		if err != nil {
			log.Println("import task", bid, err)
		}
		return nil
	})
}

func (im *Importer) entityUUID(typ, bid string) string {
	switch typ {
	case "deal":
		return im.deals[bid]
	case "lead":
		return im.leads[bid]
	case "company":
		return im.companies[bid]
	case "contact":
		return im.contacts[bid]
	default:
		return ""
	}
}

func (im *Importer) upsertTask(ctx context.Context, bid, title, desc, deadline, status, assignee, creator, et, eid string) (string, error) {
	var id string
	err := im.db.QueryRow(ctx, `SELECT id::text FROM tasks WHERE bitrix_id=$1`, bid).Scan(&id)
	if err == nil {
		_, err = im.db.Exec(ctx, `UPDATE tasks SET title=$2, description=nullif($3,''), due_at=nullif($4,'')::date, status=$5, assignee_id=nullif($6,'')::uuid, creator_id=nullif($7,'')::uuid, entity_type=nullif($8,''), entity_id=nullif($9,'')::uuid, activity_at=now(), updated_at=now() WHERE id=$1::uuid`,
			id, title, desc, deadline, status, assignee, creator, et, eid)
		return id, err
	}
	err = im.db.QueryRow(ctx, `
		INSERT INTO tasks (title, description, due_at, status, priority, assignee_id, creator_id, entity_type, entity_id, bitrix_id, activity_at)
		VALUES ($1, nullif($2,''), nullif($3,'')::date, $4, 'normal', nullif($5,'')::uuid, nullif($6,'')::uuid, nullif($7,''), nullif($8,'')::uuid, $9, now())
		RETURNING id::text`,
		title, desc, deadline, status, assignee, creator, et, eid, bid).Scan(&id)
	return id, err
}

func LocalImported(ctx context.Context, db *pgxpool.Pool) map[string]Count {
	out := map[string]Count{}
	q := func(key, sql string) {
		var n int
		if db.QueryRow(ctx, sql).Scan(&n) == nil {
			out[key] = Count{Imported: n}
		}
	}
	q("users", `SELECT count(*) FROM users WHERE bitrix_id IS NOT NULL`)
	q("departments", `SELECT count(*) FROM departments WHERE bitrix_id IS NOT NULL`)
	q("companies", `SELECT count(*) FROM companies WHERE bitrix_id IS NOT NULL`)
	q("contacts", `SELECT count(*) FROM contacts WHERE bitrix_id IS NOT NULL`)
	q("leads", `SELECT count(*) FROM leads WHERE bitrix_id IS NOT NULL`)
	q("deals", `SELECT count(*) FROM deals WHERE bitrix_id IS NOT NULL`)
	q("tasks", `SELECT count(*) FROM tasks WHERE bitrix_id IS NOT NULL`)
	q("pipelines", `SELECT count(*) FROM pipelines WHERE bitrix_id IS NOT NULL`)
	q("fields", `SELECT count(DISTINCT entity_id) FROM entity_field_values WHERE entity_type='deal'`)
	q("task_roles", `SELECT count(DISTINCT task_id) FROM task_members`)
	q("task_comments", `SELECT count(*) FROM task_comments`)
	return out
}

func nz(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func randomPass() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func cloneCounts(in map[string]Count) map[string]Count {
	out := make(map[string]Count, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
