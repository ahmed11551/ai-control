package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Спящие лиды раздаются отдельно от «Новый лид»: свои лимиты, свой счётчик,
// свой автовыключатель. Очередь новых лидов этот код не читает и не пишет.

func sqlRopBankLeadCount() string {
	return `
		SELECT count(*) FROM leads l
		WHERE l.owner_id=$1::uuid
		  AND coalesce(l.status,'') <> 'rejected'
		  AND ` + sqlLeadNotOldLeads("l")
}

// sqlUnownedSleepingLeads — бесхозные карточки только первой колонки «Спящие лиды».
func sqlUnownedSleepingLeads() string {
	stage := oldLeadsPipelineKey + ":1"
	return `
		SELECT l.id::text
		FROM leads l
		WHERE l.owner_id IS NULL
		  AND coalesce(l.status,'') <> 'rejected'
		  AND EXISTS (
		    SELECT 1 FROM deals d
		    JOIN pipelines p ON p.id=d.pipeline_id
		    JOIN pipeline_stages s ON s.id=d.stage_id
		    WHERE coalesce(p.bitrix_id,'')='` + oldLeadsPipelineKey + `'
		      AND coalesce(s.bitrix_id,'')='` + stage + `'
		      AND (d.lead_id=l.id OR d.id=l.converted_deal_id)
		  )
		ORDER BY l.created_at ASC`
}

func (a *App) ensureSleepingDistTables(ctx context.Context) {
	if a == nil || a.db == nil {
		return
	}
	a.sleepingDistOnce.Do(func() {
		_, _ = a.db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS distribution_sleeping_quotas (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    daily_limit INT NOT NULL DEFAULT 0
)`)
		_, _ = a.db.Exec(ctx, `
CREATE INDEX IF NOT EXISTS distribution_sleeping_quotas_dept
  ON distribution_sleeping_quotas (department_id)`)
		_, _ = a.db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS distribution_sleeping_counters (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    assigned INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
)`)
	})
}

func sleepingAutoSettingKey(deptID string) string {
	return "team_auto_sleeping:" + strings.TrimSpace(deptID)
}

func (a *App) distSleepingAutoOn(ctx context.Context, deptID string) bool {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || deptID == "<nil>" || a == nil || a.db == nil {
		return false
	}
	var v string
	_ = a.db.QueryRow(ctx, `SELECT value FROM distribution_settings WHERE key=$1`, sleepingAutoSettingKey(deptID)).Scan(&v)
	if v == "" {
		return true
	}
	return v != "0"
}

func (a *App) setDistSleepingAuto(ctx context.Context, deptID string, on bool) {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || deptID == "<nil>" || a == nil || a.db == nil {
		return
	}
	a.ensureManagerDistTables(ctx)
	val := "1"
	if !on {
		val = "0"
	}
	_, _ = a.db.Exec(ctx, `
		INSERT INTO distribution_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, sleepingAutoSettingKey(deptID), val)
}

func (a *App) departmentIsTema(ctx context.Context, deptID string) bool {
	deptID = strings.TrimSpace(deptID)
	if a == nil || a.db == nil || deptID == "" || deptID == "<nil>" {
		return false
	}
	var name string
	err := a.db.QueryRow(ctx, `SELECT coalesce(name,'') FROM departments WHERE id=$1::uuid`, deptID).Scan(&name)
	return err == nil && isTemaDepartmentName(name)
}

func (a *App) temaDepartmentID(ctx context.Context) string {
	if a == nil || a.db == nil {
		return ""
	}
	rows := a.queryMaps(ctx, `SELECT id::text AS id, coalesce(name,'') AS name FROM departments`)
	for _, row := range rows {
		name := strings.TrimSpace(fmt.Sprint(row["name"]))
		id := strings.TrimSpace(fmt.Sprint(row["id"]))
		if id == "" || id == "<nil>" || !isTemaDepartmentName(name) {
			continue
		}
		return id
	}
	return ""
}

func (a *App) listSleepingDistributionTeam(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	if a == nil || a.db == nil {
		writeJSON(w, 403, map[string]string{"error": "спящие лиды только у отделения Тёмы"})
		return
	}
	deptID, err := a.assertTeamDeptAccess(r, r.URL.Query().Get("department_id"))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if !a.departmentIsTema(r.Context(), deptID) {
		writeJSON(w, 403, map[string]string{"error": "спящие лиды только у отделения Тёмы"})
		return
	}
	a.ensureSleepingDistTables(r.Context())
	day := moscowDay(time.Now())
	var deptName string
	_ = a.db.QueryRow(r.Context(), `SELECT coalesce(name,'') FROM departments WHERE id=$1::uuid`, deptID).Scan(&deptName)
	managers := a.queryMaps(r.Context(), `
		SELECT u.id::text, u.full_name, coalesce(r.name,'') AS role,
		       0::float8 AS share,
		       coalesce(q.daily_limit,0)::int AS daily_limit,
		       coalesce(c.assigned,0)::int AS today,
		       (SELECT count(*)::int FROM leads l
		         WHERE l.owner_id=u.id
		           AND coalesce(l.status,'') <> 'rejected'
		           AND NOT (`+sqlLeadNotOldLeads("l")+`)) AS assigned
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN distribution_sleeping_quotas q ON q.user_id=u.id AND q.department_id=$1::uuid
		LEFT JOIN distribution_sleeping_counters c ON c.user_id=u.id AND c.day=$2::date
		WHERE u.is_active AND u.department_id=$1::uuid
		  AND lower(coalesce(r.name,'')) IN ('manager','sales')
		ORDER BY u.full_name`, deptID, day)
	if managers == nil {
		managers = []map[string]any{}
	}
	var bank int
	_ = a.db.QueryRow(r.Context(), `SELECT count(*) FROM (`+sqlUnownedSleepingLeads()+`) s`).Scan(&bank)
	writeJSON(w, 200, map[string]any{
		"department_id":   deptID,
		"department_name": deptName,
		"managers":        managers,
		"bank":            bank,
		"rop_queue":       0,
		"auto":            a.distSleepingAutoOn(r.Context(), deptID),
		"today":           day,
		"reset":           "00:00 Europe/Moscow",
		"mode":            "daily_quota",
		"pool":            "sleeping",
		"sleeping_tab":    true,
		"mode_note":       "Спящие лиды: лидов в день на менеджера Тёмы. Новые заявки сюда не попадают и эти лимиты их не тратят.",
	})
}

func (a *App) saveSleepingDistributionTeam(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	if a == nil || a.db == nil {
		writeJSON(w, 403, map[string]string{"error": "спящие лиды только у отделения Тёмы"})
		return
	}
	var in struct {
		DepartmentID string `json:"department_id"`
		Auto         *bool  `json:"auto"`
		Quotas       []struct {
			UserID     string `json:"user_id"`
			DailyLimit int    `json:"daily_limit"`
		} `json:"quotas"`
	}
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "json"})
		return
	}
	deptID, err := a.assertTeamDeptAccess(r, in.DepartmentID)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if !a.departmentIsTema(r.Context(), deptID) {
		writeJSON(w, 403, map[string]string{"error": "спящие лиды только у отделения Тёмы"})
		return
	}
	a.ensureSleepingDistTables(r.Context())
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
			INSERT INTO distribution_sleeping_quotas (user_id, department_id, daily_limit)
			VALUES ($1::uuid,$2::uuid,$3)
			ON CONFLICT (user_id) DO UPDATE SET
			  department_id=EXCLUDED.department_id,
			  daily_limit=EXCLUDED.daily_limit`,
			userID, deptID, lim)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось сохранить лимиты спящих лидов"})
			return
		}
	}
	if in.Auto != nil {
		a.setDistSleepingAuto(r.Context(), deptID, *in.Auto)
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "pool": "sleeping", "auto": a.distSleepingAutoOn(r.Context(), deptID),
	})
}

func (a *App) distributionSleepingLoop() {
	time.Sleep(12 * time.Second)
	ticker := time.NewTicker(distOrphanInterval)
	defer ticker.Stop()
	for {
		n, err := a.drainSleepingLeads(context.Background(), distOrphanBatch)
		if err != nil {
			log.Printf("distribution sleeping drain: %v", err)
		} else if n > 0 {
			log.Printf("distribution sleeping drain: assigned=%d", n)
		}
		<-ticker.C
	}
}

// drainSleepingLeads выдаёт бесхозные спящие лиды менеджерам Тёмы по их спящим лимитам.
func (a *App) drainSleepingLeads(ctx context.Context, limit int) (int, error) {
	if a == nil || a.db == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = distOrphanBatch
	}
	deptID := a.temaDepartmentID(ctx)
	if deptID == "" || !a.distSleepingAutoOn(ctx, deptID) {
		return 0, nil
	}
	a.ensureSleepingDistTables(ctx)
	rows := a.queryMaps(ctx, sqlUnownedSleepingLeads()+` LIMIT $1`, limit)
	assigned := 0
	for _, row := range rows {
		id := strings.TrimSpace(fmt.Sprint(row["id"]))
		if id == "" || id == "<nil>" {
			continue
		}
		ok, err := a.assignSleepingLead(ctx, deptID, id)
		if err != nil {
			return assigned, err
		}
		if ok {
			assigned++
		}
	}
	return assigned, nil
}

// assignSleepingLead ставит менеджера только на спящий лид. Чужой лид обновление не затронет.
func (a *App) assignSleepingLead(ctx context.Context, deptID, leadID string) (bool, error) {
	deptID = strings.TrimSpace(deptID)
	leadID = strings.TrimSpace(leadID)
	if deptID == "" || leadID == "" || a == nil || a.db == nil {
		return false, nil
	}
	a.ensureSleepingDistTables(ctx)
	day := moscowDay(time.Now())
	rows := a.queryMaps(ctx, `
		SELECT q.user_id::text AS user_id, q.daily_limit,
		       coalesce(c.assigned,0)::int AS today
		FROM distribution_sleeping_quotas q
		JOIN users u ON u.id=q.user_id AND u.is_active AND u.department_id=$1::uuid
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN distribution_sleeping_counters c ON c.user_id=q.user_id AND c.day=$2::date
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
			Today:  asInt(row["today"]),
			Limit:  asInt(row["daily_limit"]),
			HeadID: uid,
		})
	}
	cands = managerAutoCands(cands)
	picked, err := pickDistribution(cands)
	if err != nil {
		return false, nil
	}
	owner := strings.TrimSpace(picked.HeadID)
	if owner == "" {
		return false, nil
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_sleeping_counters (user_id, day, assigned)
		VALUES ($1::uuid,$2::date,1)
		ON CONFLICT (user_id, day) DO UPDATE SET assigned=distribution_sleeping_counters.assigned+1`,
		owner, day); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE leads l SET owner_id=$2::uuid, updated_at=now()
		WHERE l.id=$1::uuid
		  AND l.owner_id IS NULL
		  AND EXISTS (
		    SELECT 1 FROM deals od
		    JOIN pipelines op ON op.id=od.pipeline_id
		    WHERE coalesce(op.bitrix_id,'')=$3
		      AND (od.lead_id=l.id OR od.id=l.converted_deal_id)
		  )`, leadID, owner, oldLeadsPipelineKey)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE deals d SET owner_id=$2::uuid, updated_at=now()
		FROM pipelines p
		WHERE d.pipeline_id=p.id
		  AND coalesce(p.bitrix_id,'')=$3
		  AND (d.lead_id=$1::uuid OR d.id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid))`,
		leadID, owner, oldLeadsPipelineKey); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO distribution_log (pool, department_id, owner_id, lead_id)
		VALUES ('sleeping',$1::uuid,$2::uuid,$3::uuid)`,
		deptID, owner, leadID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
