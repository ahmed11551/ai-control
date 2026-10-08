package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (a *App) ensureManagerDistTables(ctx context.Context) {
	a.managerDistOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS distribution_manager_quotas (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    share NUMERIC(8,2) NOT NULL DEFAULT 0,
    daily_limit INT NOT NULL DEFAULT 0
)`)
		_, _ = a.db.Exec(ctx, `
CREATE INDEX IF NOT EXISTS distribution_manager_quotas_dept
  ON distribution_manager_quotas (department_id)`)
		_, _ = a.db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS distribution_manager_counters (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    assigned INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
)`)
		_, _ = a.db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS distribution_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
)`)
	})
}

func teamAutoSettingKey(deptID string) string {
	return "team_auto:" + strings.TrimSpace(deptID)
}

// distTeamAutoOn — автораздача РОП → менеджеры отдела (по умолчанию вкл.).
func (a *App) distTeamAutoOn(ctx context.Context, deptID string) bool {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || deptID == "<nil>" {
		return true
	}
	var v string
	_ = a.db.QueryRow(ctx, `SELECT value FROM distribution_settings WHERE key=$1`, teamAutoSettingKey(deptID)).Scan(&v)
	if v == "" {
		return true
	}
	return v != "0"
}

func (a *App) setDistTeamAuto(ctx context.Context, deptID string, on bool) {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || deptID == "<nil>" {
		return
	}
	val := "1"
	if !on {
		val = "0"
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO distribution_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, teamAutoSettingKey(deptID), val)
}

// managerAutoCands — в автораздаче РОП → менеджер участвует только дневной план > 0.
// Ноль на экране это «выкл», не «плана нет, раздай по доле».
func managerAutoCands(cands []distCand) []distCand {
	out := make([]distCand, 0, len(cands))
	for _, c := range cands {
		if c.Limit > 0 {
			out = append(out, c)
		}
	}
	return out
}

// distributeToManagers — этап 2: заявка уже на РОПе отдела → менеджеру по дневному плану.
// Автораздача выключена, план 0 или лимит исчерпан — заявка остаётся у РОПа.
func (a *App) distributeToManagers(ctx context.Context, deptID, dealID, leadID, pool, actor string) (distResult, bool) {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || deptID == "<nil>" {
		return distResult{}, false
	}
	if !a.distTeamAutoOn(ctx, deptID) {
		return distResult{}, false
	}
	a.ensureManagerDistTables(ctx)
	day := moscowDay(time.Now())
	rows := a.queryMaps(ctx, `
		SELECT q.user_id::text AS user_id, q.share::float8 AS share, q.daily_limit,
		       coalesce(c.assigned,0)::int AS today
		FROM distribution_manager_quotas q
		JOIN users u ON u.id=q.user_id AND u.is_active AND u.department_id=$1::uuid
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN distribution_manager_counters c ON c.user_id=q.user_id AND c.day=$2::date
		WHERE q.department_id=$1::uuid
		  AND lower(coalesce(r.name,'')) IN ('manager','sales')`, deptID, day)
	var cands []distCand
	for _, row := range rows {
		uid := strings.TrimSpace(fmt.Sprint(row["user_id"]))
		if uid == "" || uid == "<nil>" {
			continue
		}
		cands = append(cands, distCand{
			DeptID: deptID,
			Share:  asFloat(row["share"]),
			Today:  asInt(row["today"]),
			Limit:  asInt(row["daily_limit"]),
			HeadID: uid,
		})
	}
	cands = managerAutoCands(cands)
	if leadID == "" && dealID != "" {
		leadID = a.dealLinkedLeadID(ctx, dealID)
	}
	// Лид, с которым уже работал менеджер, идёт только к нему (возврат), а не по квотам другому.
	owner := ""
	if leadID != "" {
		if cur, sticky := a.leadStickyInfo(ctx, leadID); sticky && cur != "" {
			if a.managerInDepartment(ctx, cur, deptID) {
				owner = cur
			}
			// sticky-менеджер другого отдела — не блокируем, идём по квотам отдела
		} else if prev := a.leadLastTouchingManager(ctx, leadID); prev != "" {
			if a.managerInDepartment(ctx, prev, deptID) {
				owner = prev
			}
		}
	} else if dealID != "" {
		if cur, sticky := a.dealStickyInfo(ctx, dealID); sticky && cur != "" {
			if a.managerInDepartment(ctx, cur, deptID) {
				owner = cur
			}
		}
	}
	if owner == "" {
		picked, err := pickDistribution(cands)
		if err != nil {
			return distResult{}, false
		}
		owner = strings.TrimSpace(picked.HeadID)
	}
	if owner == "" {
		return distResult{}, false
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return distResult{}, false
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_manager_counters (user_id, day, assigned)
		VALUES ($1::uuid,$2::date,1)
		ON CONFLICT (user_id, day) DO UPDATE SET assigned=distribution_manager_counters.assigned+1`,
		owner, day); err != nil {
		return distResult{}, false
	}
	if dealID != "" {
		if _, err := tx.Exec(ctx, `UPDATE deals SET owner_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, dealID, owner); err != nil {
			return distResult{}, false
		}
	}
	if leadID != "" {
		if _, err := tx.Exec(ctx, `UPDATE leads SET owner_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, leadID, owner); err != nil {
			return distResult{}, false
		}
	}
	// если есть только лид — подтянуть связанную сделку
	if dealID == "" && leadID != "" {
		_, _ = tx.Exec(ctx, `
			UPDATE deals SET owner_id=$2::uuid, updated_at=now()
			WHERE lead_id=$1::uuid
			   OR id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)`, leadID, owner)
	}
	if leadID == "" && dealID != "" {
		_, _ = tx.Exec(ctx, `
			UPDATE leads SET owner_id=$2::uuid, updated_at=now()
			WHERE id = (SELECT lead_id FROM deals WHERE id=$1::uuid)
			   OR converted_deal_id=$1::uuid`, dealID, owner)
	}
	logPool := normPool(pool)
	if logPool == "" {
		logPool = "depts"
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_log (pool, department_id, owner_id, deal_id, lead_id)
		VALUES ($1,$2::uuid,$3::uuid,nullif($4,'')::uuid,nullif($5,'')::uuid)`,
		logPool, deptID, owner, dealID, leadID); err != nil {
		return distResult{}, false
	}
	if err := tx.Commit(ctx); err != nil {
		return distResult{}, false
	}
	var ownerName, deptName string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(full_name,'') FROM users WHERE id=$1::uuid`, owner).Scan(&ownerName)
	_ = a.db.QueryRow(ctx, `SELECT coalesce(name,'') FROM departments WHERE id=$1::uuid`, deptID).Scan(&deptName)
	ent, eid := "deal", dealID
	if dealID == "" {
		ent, eid = "lead", leadID
	}
	payload := map[string]any{
		"pool": logPool, "department": deptName, "owner": ownerName, "stage": "manager",
	}
	if ent == "deal" {
		a.logDealLeadAct(ctx, eid, actor, "distributed_manager", payload)
		a.broadcastDealUpdated(ctx, dealID, map[string]any{"owner_id": owner})
	} else {
		a.logAct(ctx, ent, eid, actor, "distributed_manager", payload)
		if leadID != "" {
			a.syncDealsOwnerForLead(ctx, leadID, owner)
		}
	}
	return distResult{
		Pool: logPool, DeptID: deptID, DeptName: deptName,
		OwnerID: owner, OwnerName: ownerName, DealID: dealID, LeadID: leadID,
	}, true
}

func (a *App) ropDepartmentID(ctx context.Context, userID string) string {
	return a.actorDepartmentID(ctx, userID)
}

// mskTowerFlagForCreate — галочка «МСК Башня» сохраняется только у РОПа.
func mskTowerFlagForCreate(role string, raw any) bool {
	return strings.EqualFold(strings.TrimSpace(role), "rop") && truthy(raw)
}

// shouldDistributeMskTower — лиды РОПа с флагом уходят менеджерам его отдела.
func shouldDistributeMskTower(tower bool, role string) bool {
	return tower && strings.EqualFold(strings.TrimSpace(role), "rop")
}

// maybeDistributeMskTowerLead — РОП с флагом «МСК Башня»: лид сразу менеджеру своего отдела.
func (a *App) maybeDistributeMskTowerLead(ctx context.Context, leadID, ownerID string) {
	leadID = strings.TrimSpace(leadID)
	ownerID = strings.TrimSpace(ownerID)
	if leadID == "" || ownerID == "" {
		return
	}
	var tower bool
	var role string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(u.msk_tower,false), lower(coalesce(r.name,''))
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid`, ownerID).Scan(&tower, &role)
	if !shouldDistributeMskTower(tower, role) {
		return
	}
	deptID := a.ropDepartmentID(ctx, ownerID)
	if deptID == "" {
		return
	}
	if !a.distTeamAutoOn(ctx, deptID) {
		return
	}
	_, _ = a.distributeToManagers(ctx, deptID, "", leadID, "depts", ownerID)
}

func (a *App) listDistributionTeam(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("pool")) == "sleeping" {
		a.listSleepingDistributionTeam(w, r)
		return
	}
	a.ensureManagerDistTables(r.Context())
	_ = a.backfillMissingDeals(r.Context(), 300)
	uid := actorID(r)
	deptID := a.ropDepartmentID(r.Context(), uid)
	if isAdmin(r) {
		// админ может смотреть любой отдел через ?department_id=
		if q := strings.TrimSpace(r.URL.Query().Get("department_id")); q != "" {
			deptID = q
		}
	}
	if deptID == "" {
		writeJSON(w, 400, map[string]string{"error": "отдел РОПа не задан"})
		return
	}
	if isROP(r) && !isAdmin(r) {
		own := a.ropDepartmentID(r.Context(), uid)
		if own == "" || own != deptID {
			writeJSON(w, 403, map[string]string{"error": "только свой отдел"})
			return
		}
	}
	day := moscowDay(time.Now())
	var deptName string
	_ = a.db.QueryRow(r.Context(), `SELECT coalesce(name,'') FROM departments WHERE id=$1::uuid`, deptID).Scan(&deptName)
	managers := a.queryMaps(r.Context(), `
		SELECT u.id::text, u.full_name, coalesce(r.name,'') AS role,
		       coalesce(q.share,0)::float8 AS share,
		       coalesce(q.daily_limit,0)::int AS daily_limit,
		       coalesce(c.assigned,0)::int AS today,
		       (SELECT count(*)::int FROM leads l
		         WHERE l.owner_id=u.id
		           AND coalesce(l.status,'') <> 'rejected'
		           AND `+sqlLeadNotOldLeads("l")+`) AS assigned
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN distribution_manager_quotas q ON q.user_id=u.id AND q.department_id=$1::uuid
		LEFT JOIN distribution_manager_counters c ON c.user_id=u.id AND c.day=$2::date
		WHERE u.is_active AND u.department_id=$1::uuid
		  AND lower(coalesce(r.name,'')) IN ('manager','sales')
		ORDER BY u.full_name`, deptID, day)
	if managers == nil {
		managers = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{
		"department_id":   deptID,
		"department_name": deptName,
		"managers":        managers,
		"bank":            0,
		"rop_queue":       a.ropBankLeadCount(r.Context(), deptID),
		"auto":            a.distTeamAutoOn(r.Context(), deptID),
		"today":           day,
		"reset":           "00:00 Europe/Moscow",
		"mode":            "daily_quota",
		"mode_note":       "Лидов в день на менеджера. Выдача тому, у кого меньше % от плана (today/limit). Банк и отзыв отключены.",
		"sleeping_tab":    isTemaDepartmentName(deptName),
	})
}

func (a *App) saveDistributionTeam(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("pool")) == "sleeping" {
		a.saveSleepingDistributionTeam(w, r)
		return
	}
	a.ensureManagerDistTables(r.Context())
	uid := actorID(r)
	deptID := a.ropDepartmentID(r.Context(), uid)
	var in struct {
		DepartmentID string `json:"department_id"`
		Auto         *bool  `json:"auto"`
		Quotas       []struct {
			UserID     string  `json:"user_id"`
			Share      float64 `json:"share"`
			DailyLimit int     `json:"daily_limit"`
		} `json:"quotas"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	if isAdmin(r) && strings.TrimSpace(in.DepartmentID) != "" {
		deptID = strings.TrimSpace(in.DepartmentID)
	}
	if deptID == "" {
		writeJSON(w, 400, map[string]string{"error": "отдел РОПа не задан"})
		return
	}
	if isROP(r) && !isAdmin(r) {
		own := a.ropDepartmentID(r.Context(), uid)
		if own == "" || own != deptID {
			writeJSON(w, 403, map[string]string{"error": "только свой отдел"})
			return
		}
	}
	for _, q := range in.Quotas {
		userID := strings.TrimSpace(q.UserID)
		if userID == "" {
			continue
		}
		var role, userDept string
		err := a.db.QueryRow(r.Context(), `
			SELECT lower(coalesce(r.name,'')), coalesce(u.department_id::text,'')
			FROM users u LEFT JOIN roles r ON r.id=u.role_id
			WHERE u.id=$1::uuid AND u.is_active`, userID).Scan(&role, &userDept)
		if err != nil || userDept != deptID || (role != "manager" && role != "sales") {
			writeJSON(w, 400, map[string]string{"error": "можно назначать лимиты только менеджерам своего отдела"})
			return
		}
		lim := q.DailyLimit
		if lim < 0 {
			lim = 0
		}
		_, err = a.db.Exec(r.Context(), `
			INSERT INTO distribution_manager_quotas (user_id, department_id, share, daily_limit)
			VALUES ($1::uuid,$2::uuid,0,$3)
			ON CONFLICT (user_id) DO UPDATE SET
			  department_id=EXCLUDED.department_id,
			  share=0,
			  daily_limit=EXCLUDED.daily_limit`,
			userID, deptID, lim)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить лимиты"})
			return
		}
	}
	if in.Auto != nil {
		a.setDistTeamAuto(r.Context(), deptID, *in.Auto)
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "flushed_to_managers": 0, "auto": a.distTeamAutoOn(r.Context(), deptID),
	})
}

// ropBankLeadCount — лиды на РОПе отдела (ещё не у менеджеров).
func (a *App) ropBankLeadCount(ctx context.Context, deptID string) int {
	head := a.departmentHeadID(ctx, deptID)
	if head == "" {
		return 0
	}
	var n int
	_ = a.db.QueryRow(ctx, sqlRopBankLeadCount(), head).Scan(&n)
	return n
}

func (a *App) departmentHeadID(ctx context.Context, deptID string) string {
	var head string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(head_user_id::text,'') FROM departments WHERE id=$1::uuid`, deptID).Scan(&head)
	head = strings.TrimSpace(head)
	if head == "" || head == "<nil>" {
		return ""
	}
	return head
}

func (a *App) assertTeamDeptAccess(r *http.Request, deptID string) (string, error) {
	deptID = strings.TrimSpace(deptID)
	uid := actorID(r)
	if isAdmin(r) {
		if deptID == "" {
			deptID = a.ropDepartmentID(r.Context(), uid)
		}
		if deptID == "" {
			return "", fmt.Errorf("укажите отдел")
		}
		return deptID, nil
	}
	if !isROP(r) {
		return "", fmt.Errorf("только для РОПа")
	}
	own := a.ropDepartmentID(r.Context(), uid)
	if own == "" {
		return "", fmt.Errorf("отдел РОПа не задан")
	}
	if deptID != "" && deptID != own {
		return "", fmt.Errorf("только свой отдел")
	}
	return own, nil
}

func (a *App) managerInDepartment(ctx context.Context, userID, deptID string) bool {
	var role, userDept string
	err := a.db.QueryRow(ctx, `
		SELECT lower(coalesce(r.name,'')), coalesce(u.department_id::text,'')
		FROM users u LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid AND u.is_active`, userID).Scan(&role, &userDept)
	return err == nil && userDept == deptID && (role == "manager" || role == "sales")
}

// assignLeadToManagerForced — выдать конкретную заявку с РОПа конкретному менеджеру.
func (a *App) assignLeadToManagerForced(ctx context.Context, leadID, deptID, managerID, actor string) error {
	a.ensureManagerDistTables(ctx)
	head := a.departmentHeadID(ctx, deptID)
	if head == "" {
		return fmt.Errorf("у отдела нет РОПа")
	}
	day := moscowDay(time.Now())
	var dealID string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(
			(SELECT d.id::text FROM deals d
			 WHERE d.lead_id=$1::uuid OR d.id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
			 ORDER BY d.created_at ASC LIMIT 1),
			''
		)`, leadID).Scan(&dealID)
	if dealID == "<nil>" {
		dealID = ""
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE leads SET owner_id=$2::uuid, updated_at=now()
		WHERE id=$1::uuid AND owner_id=$3::uuid`, leadID, managerID, head)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("лид уже не на РОПе")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_manager_counters (user_id, day, assigned)
		VALUES ($1::uuid,$2::date,1)
		ON CONFLICT (user_id, day) DO UPDATE SET assigned=distribution_manager_counters.assigned+1`,
		managerID, day); err != nil {
		return err
	}
	if dealID != "" {
		_, _ = tx.Exec(ctx, `UPDATE deals SET owner_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, dealID, managerID)
	} else {
		_, _ = tx.Exec(ctx, `
			UPDATE deals SET owner_id=$2::uuid, updated_at=now()
			WHERE lead_id=$1::uuid OR id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid)`, leadID, managerID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_log (pool, department_id, owner_id, deal_id, lead_id)
		VALUES ('depts',$1::uuid,$2::uuid,nullif($3,'')::uuid,$4::uuid)`,
		deptID, managerID, dealID, leadID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	var ownerName, deptName string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(full_name,'') FROM users WHERE id=$1::uuid`, managerID).Scan(&ownerName)
	_ = a.db.QueryRow(ctx, `SELECT coalesce(name,'') FROM departments WHERE id=$1::uuid`, deptID).Scan(&deptName)
	a.logAct(ctx, "lead", leadID, actor, "distributed_manager", map[string]any{
		"pool": "depts", "department": deptName, "owner": ownerName, "stage": "manager", "from": "rop_bank",
	})
	a.finalizeDistributedAssignment(ctx, leadID, dealID, managerID)
	return nil
}

func (a *App) issueRopBankToManager(ctx context.Context, deptID, managerID string, count int, actor string) (int, error) {
	deptID = strings.TrimSpace(deptID)
	managerID = strings.TrimSpace(managerID)
	if deptID == "" || managerID == "" || count <= 0 {
		return 0, fmt.Errorf("укажите менеджера и количество")
	}
	if !a.managerInDepartment(ctx, managerID, deptID) {
		return 0, fmt.Errorf("можно выдавать только менеджерам своего отдела")
	}
	head := a.departmentHeadID(ctx, deptID)
	if head == "" {
		return 0, fmt.Errorf("у отдела нет РОПа")
	}
	a.ensureManagerDistTables(ctx)
	day := moscowDay(time.Now())
	var limit, today int
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(q.daily_limit,0), coalesce(c.assigned,0)
		FROM users u
		LEFT JOIN distribution_manager_quotas q ON q.user_id=u.id AND q.department_id=$2::uuid
		LEFT JOIN distribution_manager_counters c ON c.user_id=u.id AND c.day=$3::date
		WHERE u.id=$1::uuid`, managerID, deptID, day).Scan(&limit, &today)
	room := count
	if limit > 0 {
		room = bankRoom(limit, today)
		if room == 0 {
			return 0, fmt.Errorf("лимит менеджера на сегодня исчерпан — сначала увеличьте лимит")
		}
	}
	take := count
	if take > room {
		take = room
	}
	a.ensureRecruitFunnels(ctx)
	a.ensureStickyIndexes(ctx)
	rows, err := a.db.Query(ctx, `
		SELECT id::text FROM leads
		WHERE owner_id=$1::uuid
		  AND coalesce(status,'') <> 'rejected'
		  AND (`+sqlLeadOnNewApplicationStage("leads")+`)
		  AND (`+sqlLeadNeverOpenedByManager("leads")+`)
		  AND NOT (`+sqlLeadTouchedByManagerOtherThan("leads", "$3::uuid")+`)
		ORDER BY created_at ASC
		LIMIT $2`, head, take, managerID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			ids = append(ids, id)
		}
	}
	issued := 0
	for _, id := range ids {
		if err := a.assignLeadToManagerForced(ctx, id, deptID, managerID, actor); err != nil {
			break
		}
		issued++
	}
	return issued, nil
}

func (a *App) recallManagerToRopBank(ctx context.Context, deptID, managerID string, count int, actor string) (int, error) {
	deptID = strings.TrimSpace(deptID)
	managerID = strings.TrimSpace(managerID)
	if deptID == "" || managerID == "" || count <= 0 {
		return 0, fmt.Errorf("укажите менеджера и количество")
	}
	if a.departmentLeadsLocked(ctx, deptID) {
		return 0, errMskTowerRecallDenied()
	}
	if !a.managerInDepartment(ctx, managerID, deptID) {
		return 0, fmt.Errorf("можно отзывать только у менеджеров своего отдела")
	}
	head := a.departmentHeadID(ctx, deptID)
	if head == "" {
		return 0, fmt.Errorf("у отдела нет РОПа")
	}
	a.ensureRecruitFunnels(ctx)
	a.ensureStickyIndexes(ctx)
	rows, err := a.db.Query(ctx, `
		SELECT id::text FROM leads
		WHERE owner_id=$1::uuid
		  AND coalesce(status,'') <> 'rejected'
		  AND (`+sqlLeadOnNewApplicationStage("leads")+`)
		  AND (`+sqlLeadNeverOpenedByManager("leads")+`)
		  AND NOT (`+sqlLeadStickyToOwner("leads")+`)
		ORDER BY created_at DESC
		LIMIT $2`, managerID, count)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			ids = append(ids, id)
		}
	}
	moved := 0
	for _, id := range ids {
		// Атомарно с проверкой липкости в том же UPDATE (гонка с работой менеджера).
		if !a.setLeadOwnerGuarded(ctx, id, head, managerID, false) {
			continue
		}
		a.logAct(ctx, "lead", id, actor, "recalled_to_rop_bank", map[string]any{
			"department_id": deptID, "from_manager": managerID,
		})
		a.finalizeDistributedAssignment(ctx, id, "", head)
		moved++
	}
	return moved, nil
}

// POST /distribution/team/bank/issue — отключено.
func (a *App) postRopTeamBankIssue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 410, map[string]string{"error": distBankOpsDisabledMsg})
}

// POST /distribution/team/bank/recall — отключено.
func (a *App) postRopTeamBankRecall(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 410, map[string]string{"error": distBankOpsDisabledMsg})
}
