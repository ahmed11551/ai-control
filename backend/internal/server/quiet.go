package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	quietDailyCap   = 50
	quietSettingKey = "quiet_flow"
)

var (
	quietSchemaOnce sync.Once
	quietUserMu     sync.Mutex
	quietUserCached string
)

// quietRowHidden — кого не пускаем к помеченной карточке.
// Админ видит. Логист видит по своим правилам доски (очередь билета), не по отделу.
// Сама сотрудница видит помеченные только с нажатой «Мои». Без кнопки их на доске нет.
// РОП и остальные помеченные карточки не видят ни так, ни так.
func quietRowHidden(admin, logist bool, viewer, mark string, mine bool) bool {
	mark = strings.TrimSpace(mark)
	viewer = strings.TrimSpace(viewer)
	if mark == "" || admin || logist {
		return false
	}
	if viewer == mark {
		return !mine
	}
	return true
}

func quietNameMatch(full string) bool {
	n := strings.ToLower(strings.Join(strings.Fields(full), " "))
	return strings.Contains(n, "завьялов") && strings.Contains(n, "екатер") && strings.Contains(n, "сергеев")
}

func (a *App) ensureQuietSchema(ctx context.Context) {
	if a == nil || a.db == nil {
		return
	}
	quietSchemaOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE leads ADD COLUMN IF NOT EXISTS quiet_for UUID
		`)
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE leads ADD COLUMN IF NOT EXISTS quiet_day DATE
		`)
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE deals ADD COLUMN IF NOT EXISTS quiet_for UUID
		`)
		_, _ = a.db.Exec(ctx, `
			ALTER TABLE deals ADD COLUMN IF NOT EXISTS quiet_day DATE
		`)
		_, _ = a.db.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS leads_quiet_for_idx ON leads (quiet_for) WHERE quiet_for IS NOT NULL
		`)
		_, _ = a.db.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS deals_quiet_for_idx ON deals (quiet_for) WHERE quiet_for IS NOT NULL
		`)
		_, _ = a.db.Exec(ctx, `
			CREATE OR REPLACE FUNCTION quiet_clear_on_owner_move() RETURNS trigger AS $$
			BEGIN
			  IF NEW.owner_id IS DISTINCT FROM OLD.owner_id
			     AND NEW.quiet_for IS NOT NULL
			     AND (NEW.owner_id IS NULL OR NEW.owner_id IS DISTINCT FROM NEW.quiet_for) THEN
			    NEW.quiet_for := NULL;
			    NEW.quiet_day := NULL;
			  END IF;
			  RETURN NEW;
			END;
			$$ LANGUAGE plpgsql
		`)
		_, _ = a.db.Exec(ctx, `DROP TRIGGER IF EXISTS quiet_clear_leads ON leads`)
		_, _ = a.db.Exec(ctx, `
			CREATE TRIGGER quiet_clear_leads
			BEFORE UPDATE OF owner_id ON leads
			FOR EACH ROW EXECUTE FUNCTION quiet_clear_on_owner_move()
		`)
		_, _ = a.db.Exec(ctx, `DROP TRIGGER IF EXISTS quiet_clear_deals ON deals`)
		_, _ = a.db.Exec(ctx, `
			CREATE TRIGGER quiet_clear_deals
			BEFORE UPDATE OF owner_id ON deals
			FOR EACH ROW EXECUTE FUNCTION quiet_clear_on_owner_move()
		`)
	})
}

func (a *App) quietUserID(ctx context.Context) string {
	if a == nil || a.db == nil {
		return ""
	}
	quietUserMu.Lock()
	cached := quietUserCached
	quietUserMu.Unlock()
	if cached != "" {
		return cached
	}
	a.ensureQuietSchema(ctx)
	rows := a.queryMaps(ctx, `
		SELECT u.id::text AS id, u.full_name
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.is_active
		  AND lower(coalesce(r.name,'')) IN ('manager','sales')
		  AND lower(u.full_name) LIKE '%завьялов%'
		  AND lower(u.full_name) LIKE '%екатер%'
		  AND lower(u.full_name) LIKE '%сергеев%'`)
	if len(rows) != 1 {
		return ""
	}
	name := strings.TrimSpace(fmt.Sprint(rows[0]["full_name"]))
	id := strings.TrimSpace(fmt.Sprint(rows[0]["id"]))
	if id == "" || id == "<nil>" || !quietNameMatch(name) {
		return ""
	}
	quietUserMu.Lock()
	quietUserCached = id
	quietUserMu.Unlock()
	return id
}

func (a *App) isQuietUser(ctx context.Context, userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	return userID == a.quietUserID(ctx)
}

func (a *App) quietFlowOn(ctx context.Context) bool {
	if a == nil || a.db == nil {
		return false
	}
	a.ensureManagerDistTables(ctx)
	var v string
	_ = a.db.QueryRow(ctx, `SELECT value FROM distribution_settings WHERE key=$1`, quietSettingKey).Scan(&v)
	return v == "1"
}

func (a *App) setQuietFlow(ctx context.Context, on bool) {
	if a == nil || a.db == nil {
		return
	}
	a.ensureManagerDistTables(ctx)
	val := "0"
	if on {
		val = "1"
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO distribution_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, quietSettingKey, val)
}

// quietContactKeepSQL — контакт виден, только если есть обычная карточка
// или владелец контакта не она. Один только скрытый лид контакт не показывает.
func quietContactKeepSQL(viewerRef, deptRef, quietRef string, hasDept, ownPass bool) string {
	deptOwner := "FALSE"
	dealScope := "d.owner_id=" + viewerRef + "::uuid"
	leadScope := "l.owner_id=" + viewerRef + "::uuid"
	if hasDept && deptRef != "" {
		deptOwner = "c.owner_id IN (SELECT id FROM users WHERE department_id=" + deptRef + "::uuid)"
		dealScope = "(" + dealScope + " OR d.owner_id IN (SELECT id FROM users WHERE department_id=" + deptRef + "::uuid))"
		leadScope = "(" + leadScope + " OR l.owner_id IN (SELECT id FROM users WHERE department_id=" + deptRef + "::uuid))"
	}
	own := ""
	dealMark := "d.quiet_for IS NULL"
	leadMark := "l.quiet_for IS NULL"
	if ownPass {
		own = "c.owner_id = " + viewerRef + "::uuid OR "
		dealMark = "(d.quiet_for IS NULL OR d.quiet_for=" + viewerRef + "::uuid)"
		leadMark = "(l.quiet_for IS NULL OR l.quiet_for=" + viewerRef + "::uuid)"
	}
	return `(
		` + own + `EXISTS (
			SELECT 1 FROM deals d WHERE d.contact_id=c.id
			  AND ` + dealMark + `
			  AND ` + dealScope + `
		)
		OR EXISTS (
			SELECT 1 FROM leads l WHERE l.contact_id=c.id
			  AND ` + leadMark + `
			  AND ` + leadScope + `
		)
		OR (
			c.owner_id IS NOT NULL
			AND c.owner_id IS DISTINCT FROM ` + quietRef + `::uuid
			AND (` + deptOwner + ` OR c.owner_id = ` + viewerRef + `::uuid)
		)
	)`
}

func (a *App) quietContactHidden(ctx context.Context, contactID string, s crmScope) bool {
	if a == nil || a.db == nil || s.All || s.IsLogist || strings.TrimSpace(contactID) == "" {
		return false
	}
	if a.isQuietUser(ctx, s.UserID) {
		return false
	}
	quietID := a.quietUserID(ctx)
	if quietID == "" {
		return false
	}
	var n int
	var err error
	if s.DeptID != "" {
		err = a.db.QueryRow(ctx, `
			SELECT 1 FROM contacts c WHERE c.id=$1::uuid AND `+quietContactKeepSQL("$2", "$3", "$4", true, true),
			contactID, s.UserID, s.DeptID, quietID).Scan(&n)
	} else {
		err = a.db.QueryRow(ctx, `
			SELECT 1 FROM contacts c WHERE c.id=$1::uuid AND `+quietContactKeepSQL("$2", "", "$3", false, true),
			contactID, s.UserID, quietID).Scan(&n)
	}
	return err != nil
}

// quietCompanyClause прячет компанию, которая числится только на ней и не имеет обычной сделки.
func (a *App) quietCompanyClause(r *http.Request, alias string, next func(any) string) string {
	if a == nil || r == nil || isAdmin(r) || isLogist(r) {
		return ""
	}
	quietID := a.quietUserID(r.Context())
	if quietID == "" {
		return ""
	}
	owner := "companies.owner_id"
	idCol := "companies.id"
	if alias != "" {
		owner = alias + ".owner_id"
		idCol = alias + ".id"
	}
	viewer := next(actorID(r))
	quiet := next(quietID)
	holdOwn := a.isQuietUser(r.Context(), actorID(r)) && !wantMine(r)
	distinct := ""
	if !holdOwn {
		distinct = " AND " + owner + " IS DISTINCT FROM " + viewer + "::uuid"
	}
	dealScope := "d.owner_id=" + viewer + "::uuid"
	if d := a.crmScope(r).DeptID; d != "" {
		dept := next(d)
		dealScope = "(d.owner_id=" + viewer + "::uuid OR d.owner_id IN (SELECT id FROM users WHERE department_id=" + dept + "::uuid))"
	}
	dealMark := "(d.quiet_for IS NULL OR d.quiet_for=" + viewer + "::uuid)"
	if holdOwn {
		dealMark = "d.quiet_for IS NULL"
	}
	return `NOT (
		` + owner + ` = ` + quiet + `::uuid
		` + distinct + `
		AND NOT EXISTS (
			SELECT 1 FROM deals d
			WHERE d.company_id=` + idCol + `
			  AND ` + dealMark + `
			  AND ` + dealScope + `
		)
	)`
}

func (a *App) quietCompanyBlocked(ctx context.Context, companyID string, s crmScope) bool {
	if a == nil || a.db == nil || s.All || s.IsLogist || strings.TrimSpace(companyID) == "" {
		return false
	}
	if a.isQuietUser(ctx, s.UserID) {
		return false
	}
	quietID := a.quietUserID(ctx)
	if quietID == "" {
		return false
	}
	var dept any
	if s.DeptID != "" {
		dept = s.DeptID
	}
	var blocked bool
	err := a.db.QueryRow(ctx, `
		SELECT
			coalesce(c.owner_id=$3::uuid, false)
			AND c.owner_id IS DISTINCT FROM $2::uuid
			AND NOT EXISTS (
				SELECT 1 FROM deals d
				WHERE d.company_id=c.id
				  AND (d.quiet_for IS NULL OR d.quiet_for=$2::uuid)
				  AND (
					d.owner_id=$2::uuid
					OR ($4::uuid IS NOT NULL AND d.owner_id IN (SELECT id FROM users WHERE department_id=$4::uuid))
				  )
			)
		FROM companies c WHERE c.id=$1::uuid`, companyID, s.UserID, quietID, dept).Scan(&blocked)
	return err == nil && blocked
}

func (a *App) quietContactClause(r *http.Request, next func(any) string) string {
	if a == nil || r == nil || isAdmin(r) || isLogist(r) {
		return ""
	}
	quietID := a.quietUserID(r.Context())
	if quietID == "" {
		return ""
	}
	viewer := next(actorID(r))
	dept := ""
	hasDept := false
	if d := a.crmScope(r).DeptID; d != "" {
		dept = next(d)
		hasDept = true
	}
	ownPass := !(a.isQuietUser(r.Context(), actorID(r)) && !wantMine(r))
	return quietContactKeepSQL(viewer, dept, next(quietID), hasDept, ownPass)
}

// quietSearchFilter — кусок AND для поиска. У неё без «Мои» помеченные не ищутся.
func (a *App) quietSearchFilter(r *http.Request, alias string) string {
	if a == nil || r == nil || isAdmin(r) || isLogist(r) {
		return ""
	}
	if a.isQuietUser(r.Context(), actorID(r)) && !wantMine(r) {
		return " AND " + alias + ".quiet_for IS NULL"
	}
	return " AND (" + alias + ".quiet_for IS NULL OR " + alias + ".quiet_for=$2::uuid)"
}

// hideQuietRows прячет помеченные карточки у всех, кроме админа и логиста.
// У неё они появляются только с нажатой «Мои». Без кнопки на доске остаются обычные.
func (a *App) hideQuietRows(r *http.Request, alias string, where []string, next func(any) string) []string {
	if a == nil || r == nil || isAdmin(r) || isLogist(r) {
		return where
	}
	a.ensureQuietSchema(r.Context())
	if a.isQuietUser(r.Context(), actorID(r)) && !wantMine(r) {
		where = append(where, alias+".quiet_for IS NULL")
		return where
	}
	uid := next(actorID(r))
	where = append(where, fmt.Sprintf("(%s.quiet_for IS NULL OR %s.quiet_for = %s::uuid)", alias, alias, uid))
	return where
}

// quietMarkedFor — карточка помечена именно на этого человека.
func (a *App) quietMarkedFor(ctx context.Context, table, id, viewer string) bool {
	if a == nil || a.db == nil {
		return false
	}
	id = strings.TrimSpace(id)
	viewer = strings.TrimSpace(viewer)
	if id == "" || viewer == "" || (table != "deals" && table != "leads") {
		return false
	}
	a.ensureQuietSchema(ctx)
	var mark string
	err := a.db.QueryRow(ctx, `SELECT coalesce(quiet_for::text,'') FROM `+table+` WHERE id=$1::uuid`, id).Scan(&mark)
	return err == nil && strings.TrimSpace(mark) == viewer
}

func (a *App) dealQuietBlocked(ctx context.Context, dealID, viewer string) bool {
	return a.rowQuietBlocked(ctx, "deals", dealID, viewer)
}

func (a *App) leadQuietBlocked(ctx context.Context, leadID, viewer string) bool {
	if a.rowQuietBlocked(ctx, "leads", leadID, viewer) {
		return true
	}
	if a == nil || a.db == nil || strings.TrimSpace(leadID) == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals d
		WHERE d.quiet_for IS NOT NULL
		  AND d.quiet_for IS DISTINCT FROM $2::uuid
		  AND (d.lead_id=$1::uuid OR d.id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid))
		LIMIT 1`, leadID, viewer).Scan(&n)
	return err == nil
}

func (a *App) rowQuietBlocked(ctx context.Context, table, id, viewer string) bool {
	if a == nil || a.db == nil {
		return false
	}
	id = strings.TrimSpace(id)
	viewer = strings.TrimSpace(viewer)
	if id == "" || (table != "deals" && table != "leads") {
		return false
	}
	a.ensureQuietSchema(ctx)
	var mark string
	err := a.db.QueryRow(ctx, `SELECT coalesce(quiet_for::text,'') FROM `+table+` WHERE id=$1::uuid`, id).Scan(&mark)
	if err != nil {
		return false
	}
	// Ссылку на свою карточку она открывает. С доски помеченные снимает «Мои».
	return quietRowHidden(false, false, viewer, mark, true)
}

// tryQuietAssign забирает ещё ничью заявку из «Новый лид» напрямую ей.
// В счётчики отдела и дневной план менеджера она не попадает и в журнал раздачи не пишется.
func (a *App) tryQuietAssign(ctx context.Context, dealID, leadID string) (distResult, bool) {
	if a == nil || a.db == nil || !a.quietFlowOn(ctx) {
		return distResult{}, false
	}
	userID := a.quietUserID(ctx)
	if userID == "" {
		return distResult{}, false
	}
	leadID = strings.TrimSpace(leadID)
	dealID = strings.TrimSpace(dealID)
	if leadID == "" && dealID != "" {
		_ = a.db.QueryRow(ctx, `SELECT coalesce(lead_id::text,'') FROM deals WHERE id=$1::uuid`, dealID).Scan(&leadID)
		leadID = strings.TrimSpace(leadID)
	}
	if leadID == "" || leadID == "<nil>" {
		return distResult{}, false
	}
	day := moscowDay(time.Now())
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return distResult{}, false
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('quiet-flow'))`); err != nil {
		return distResult{}, false
	}
	var today int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int FROM leads
		WHERE quiet_for=$1::uuid AND quiet_day=$2::date`, userID, day).Scan(&today); err != nil {
		return distResult{}, false
	}
	if today >= quietDailyCap {
		return distResult{}, false
	}
	tag, err := tx.Exec(ctx, `
		UPDATE leads
		SET owner_id=$2::uuid, quiet_for=$2::uuid, quiet_day=$3::date, updated_at=now()
		WHERE id=$1::uuid
		  AND owner_id IS NULL
		  AND coalesce(status,'') <> 'rejected'
		  AND (`+sqlLeadOnNewApplicationStage("leads")+`)`, leadID, userID, day)
	if err != nil || tag.RowsAffected() == 0 {
		return distResult{}, false
	}
	_, _ = tx.Exec(ctx, `
		UPDATE deals
		SET owner_id=$2::uuid, quiet_for=$2::uuid, quiet_day=$3::date, updated_at=now()
		WHERE quiet_for IS NULL
		  AND (lead_id=$1::uuid OR id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid))`,
		leadID, userID, day)
	if err := tx.Commit(ctx); err != nil {
		return distResult{}, false
	}
	dealID = a.finalizeDistributedAssignment(ctx, leadID, dealID, userID)
	_, _ = a.db.Exec(ctx, `
		UPDATE deals d
		SET quiet_for=l.quiet_for, quiet_day=l.quiet_day, updated_at=now()
		FROM leads l
		WHERE l.id=$1::uuid
		  AND l.quiet_for IS NOT NULL
		  AND (d.lead_id=l.id OR d.id=l.converted_deal_id)
		  AND (d.quiet_for IS NULL OR d.quiet_for IS DISTINCT FROM l.quiet_for)`, leadID)
	return distResult{OwnerID: userID, LeadID: leadID, DealID: dealID}, true
}

func (a *App) drainQuietLeads(ctx context.Context, limit int) (int, error) {
	if a == nil || a.db == nil || !a.quietFlowOn(ctx) {
		return 0, nil
	}
	if limit < 1 {
		limit = 25
	}
	rows := a.queryMaps(ctx, sqlUnownedNewApplicationLeads()+` LIMIT $1`, limit)
	n := 0
	for _, row := range rows {
		if n >= limit {
			break
		}
		id := strings.TrimSpace(fmt.Sprint(row["id"]))
		if id == "" || id == "<nil>" {
			continue
		}
		if _, ok := a.tryQuietAssign(ctx, "", id); ok {
			n++
		}
	}
	return n, nil
}

func (a *App) putQuietFlow(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	quietID := a.quietUserID(r.Context())
	if quietID == "" || id != quietID {
		writeJSON(w, 404, map[string]string{"error": "не найдено"})
		return
	}
	var in struct {
		On bool `json:"on"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	a.setQuietFlow(r.Context(), in.On)
	writeJSON(w, 200, map[string]any{"on": in.On})
}

