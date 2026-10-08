package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// bankRoom — сколько ещё можно выдать отделу сегодня.
// limit<=0 в автораздаче значит «без потолка»; для склада потолок обязателен.
func bankRoom(limit, today int) int {
	if limit <= 0 || today >= limit {
		return 0
	}
	return limit - today
}

func clampBankIssue(room, want, bank int) int {
	if room < 0 {
		room = 0
	}
	n := want
	if n <= 0 || n > bank {
		n = bank
	}
	if n > room {
		n = room
	}
	if n < 0 {
		return 0
	}
	return n
}

// sqlLeadOnNewApplicationStage — только колонка «Новый лид / Новая заявка» (не недозвоны / документы / в работе).
// alias — алиас таблицы leads в запросе (например "l").
func sqlLeadOnNewApplicationStage(alias string) string {
	if alias == "" {
		alias = "leads"
	}
	return `(
		EXISTS (
			SELECT 1 FROM deals d
			JOIN pipeline_stages s ON s.id=d.stage_id
			WHERE (d.lead_id=` + alias + `.id OR d.id=` + alias + `.converted_deal_id)
			  AND coalesce(s.bitrix_id,'')='` + hireNewApplicationStage + `'
		)
		OR (
			NOT EXISTS (
				SELECT 1 FROM deals d
				WHERE d.lead_id=` + alias + `.id OR d.id=` + alias + `.converted_deal_id
			)
		)
	)`
}

// sqlLeadNeverOpenedByManager — менеджер-владелец ещё не открывал карточку (activity owner_opened).
// Такие лиды можно отзывать/выдавать; после открытия — нет.
func sqlLeadNeverOpenedByManager(alias string) string {
	if alias == "" {
		alias = "leads"
	}
	// Два EXISTS вместо OR внутри WHERE — чтобы работал индекс (entity_type, entity_id).
	return `(
		NOT EXISTS (
			SELECT 1 FROM activity_log a
			WHERE a.action='owner_opened'
			  AND a.entity_type='lead' AND a.entity_id=` + alias + `.id
		)
		AND NOT EXISTS (
			SELECT 1 FROM deals d
			JOIN activity_log a ON a.entity_type='deal' AND a.entity_id=d.id
			WHERE (d.lead_id=` + alias + `.id OR d.id=` + alias + `.converted_deal_id)
			  AND a.action='owner_opened'
		)
	)`
}

func (a *App) bankLeadCount(ctx context.Context) int {
	var n int
	_ = a.db.QueryRow(ctx, `
		SELECT count(*) FROM leads
		WHERE owner_id IS NULL
		  AND coalesce(status,'') <> 'rejected'`).Scan(&n)
	return n
}

func (a *App) assignLeadToDepartment(ctx context.Context, leadID, deptID, actor string) (distResult, error) {
	if !a.deptAccessEnabled(ctx, deptID) {
		return distResult{}, fmt.Errorf("отдел отключён — выдача недоступна")
	}
	day := moscowDay(time.Now())
	var owner string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(head_user_id::text,'') FROM departments WHERE id=$1::uuid`, deptID).Scan(&owner)
	owner = strings.TrimSpace(owner)
	if owner == "" || owner == "<nil>" {
		_ = a.db.QueryRow(ctx, `
			SELECT id::text FROM users
			WHERE is_active AND department_id=$1::uuid
			ORDER BY full_name LIMIT 1`, deptID).Scan(&owner)
	}
	if owner == "" {
		return distResult{}, fmt.Errorf("в отделе нет РОП и нет сотрудников")
	}
	var dealID string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(converted_deal_id::text, '') FROM leads WHERE id=$1::uuid`, leadID).Scan(&dealID)
	if dealID == "<nil>" {
		dealID = ""
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return distResult{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_counters (department_id, day, assigned)
		VALUES ($1::uuid,$2::date,1)
		ON CONFLICT (department_id, day) DO UPDATE SET assigned=distribution_counters.assigned+1`,
		deptID, day); err != nil {
		return distResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE leads SET owner_id=$2::uuid, updated_at=now() WHERE id=$1::uuid AND owner_id IS NULL`, leadID, owner); err != nil {
		return distResult{}, err
	}
	if dealID != "" {
		_, _ = tx.Exec(ctx, `UPDATE deals SET owner_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, dealID, owner)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_log (pool, department_id, owner_id, deal_id, lead_id)
		VALUES ('depts',$1::uuid,$2::uuid,nullif($3,'')::uuid,$4::uuid)`,
		deptID, owner, dealID, leadID); err != nil {
		return distResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return distResult{}, err
	}
	a.syncDealsOwnerForLead(ctx, leadID, owner)
	var deptName, ownerName string
	_ = a.db.QueryRow(ctx, `SELECT name FROM departments WHERE id=$1::uuid`, deptID).Scan(&deptName)
	_ = a.db.QueryRow(ctx, `SELECT full_name FROM users WHERE id=$1::uuid`, owner).Scan(&ownerName)
	a.logAct(ctx, "lead", leadID, actor, "distributed", map[string]any{
		"pool": "depts", "department": deptName, "owner": ownerName, "from": "bank", "stage": "rop",
	})
	res := distResult{Pool: "depts", DeptID: deptID, DeptName: deptName, OwnerID: owner, OwnerName: ownerName, LeadID: leadID, DealID: dealID}
	if mgr, ok := a.distributeToManagers(ctx, deptID, dealID, leadID, "depts", actor); ok {
		res.OwnerID = mgr.OwnerID
		res.OwnerName = mgr.OwnerName
	}
	res.DealID = a.finalizeDistributedAssignment(ctx, res.LeadID, res.DealID, res.OwnerID)
	return res, nil
}

func (a *App) issueBankToDepartment(ctx context.Context, deptID string, count int, actor string) (int, error) {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || count <= 0 {
		return 0, fmt.Errorf("укажите отдел и количество")
	}
	if !a.deptAccessEnabled(ctx, deptID) {
		return 0, fmt.Errorf("отдел отключён — выдача недоступна")
	}
	day := moscowDay(time.Now())
	var limit, today int
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(q.daily_limit,0), coalesce(c.assigned,0)
		FROM departments d
		LEFT JOIN distribution_quotas q ON q.department_id=d.id AND q.pool='depts'
		LEFT JOIN distribution_counters c ON c.department_id=d.id AND c.day=$2::date
		WHERE d.id=$1::uuid`, deptID, day).Scan(&limit, &today)
	if err != nil {
		return 0, fmt.Errorf("отдел не найден")
	}
	room := bankRoom(limit, today)
	if room == 0 {
		return 0, fmt.Errorf("лимит отдела на сегодня исчерпан — сначала увеличьте лимит")
	}
	take := count
	if take > room {
		take = room
	}
	a.ensureStickyIndexes(ctx)
	rows, err := a.db.Query(ctx, `
		SELECT id::text FROM leads
		WHERE owner_id IS NULL
		  AND coalesce(status,'') <> 'rejected'
		  AND (`+sqlLeadNeverOpenedByManager("leads")+`)
		  AND NOT (`+sqlLeadTouchedByManagerOtherThan("leads", "")+`)
		ORDER BY created_at ASC
		LIMIT $1`, take)
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
	n := clampBankIssue(room, count, len(ids))
	issued := 0
	for _, id := range ids {
		if issued >= n {
			break
		}
		// Только этот свободный лид: банк → РОП → (квота) менеджеру.
		// Не раздаём весь хвост на РОПе после выдачи — иначе «выдать 50»
		// выглядит как съём/перетасовка чужих карточек.
		if _, err = a.assignLeadToDepartment(ctx, id, deptID, actor); err != nil {
			break
		}
		issued++
	}
	return issued, nil
}

func (a *App) recallDepartmentToBank(ctx context.Context, deptID string, count int, actor string) (int, error) {
	n, _, err := a.recallDepartmentLeads(ctx, deptID, count, actor)
	return n, err
}

func (a *App) recallDepartmentLeads(ctx context.Context, deptID string, count int, actor string) (int, []string, error) {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || count <= 0 {
		return 0, nil, fmt.Errorf("укажите отдел и количество")
	}
	if a.departmentLeadsLocked(ctx, deptID) {
		return 0, nil, errMskTowerRecallDenied()
	}
	a.ensureRecruitFunnels(ctx)
	a.ensureStickyIndexes(ctx)
	rows, err := a.db.Query(ctx, `
		SELECT l.id::text
		FROM leads l
		JOIN users u ON u.id=l.owner_id
		WHERE u.department_id=$1::uuid
		  AND coalesce(l.status,'') <> 'rejected'
		  AND (`+sqlLeadOnNewApplicationStage("l")+`)
		  AND (`+sqlLeadNeverOpenedByManager("l")+`)
		  AND NOT (`+sqlLeadStickyToOwner("l")+`)
		ORDER BY l.created_at DESC
		LIMIT $2`, deptID, count)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			ids = append(ids, id)
		}
	}
	var moved []string
	for _, id := range ids {
		// Атомарно: если менеджер успел тронуть лид после SELECT — UPDATE не пройдёт.
		if !a.setLeadOwnerGuarded(ctx, id, "", "", false) {
			continue
		}
		a.logAct(ctx, "lead", id, actor, "recalled_to_bank", map[string]any{"department_id": deptID})
		moved = append(moved, id)
	}
	// Счётчик дня не уменьшаем: отзыв не должен открывать новую автораздачу в этот же отдел.
	return len(moved), moved, nil
}

func (a *App) deleteDepartment(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "нет отдела"})
		return
	}
	ctx := r.Context()
	var name string
	if err := a.db.QueryRow(ctx, `SELECT name FROM departments WHERE id=$1::uuid`, id).Scan(&name); err != nil {
		writeJSON(w, 404, map[string]string{"error": "отдел не найден"})
		return
	}
	// Банк/отзыв отключены: лиды остаются у текущих владельцев, отдел просто снимаем.
	_, _ = a.db.Exec(ctx, `DELETE FROM distribution_quotas WHERE department_id=$1::uuid`, id)
	a.ensureManagerDistTables(ctx)
	_, _ = a.db.Exec(ctx, `DELETE FROM distribution_manager_quotas WHERE department_id=$1::uuid`, id)
	_, _ = a.db.Exec(ctx, `UPDATE users SET department_id=NULL WHERE department_id=$1::uuid`, id)
	_, _ = a.db.Exec(ctx, `UPDATE departments SET head_user_id=NULL WHERE id=$1::uuid`, id)
	_, _ = a.db.Exec(ctx, `DELETE FROM distribution_counters WHERE department_id=$1::uuid`, id)
	tag, err := a.db.Exec(ctx, `DELETE FROM departments WHERE id=$1::uuid`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeJSON(w, 400, map[string]string{"error": "не удалось удалить отдел"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "name": name, "recalled": 0, "redistributed": 0,
	})
}

const distBankOpsDisabledMsg = "банк, выдача и отзыв отключены — только автораздача отдел → РОП → менеджер"

func (a *App) getLeadBank(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "нет доступа"})
		return
	}
	writeJSON(w, 410, map[string]any{"error": distBankOpsDisabledMsg, "count": 0})
}

func (a *App) postLeadBankIssue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 410, map[string]string{"error": distBankOpsDisabledMsg})
}

func (a *App) postLeadBankRecall(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 410, map[string]string{"error": distBankOpsDisabledMsg})
}
