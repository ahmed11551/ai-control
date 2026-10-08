package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	distOrphanBatch    = 25
	distOrphanInterval = 20 * time.Second
)

// sqlUnownedNewApplicationLeads — бесхозные только из колонки «Новый лид».
func sqlUnownedNewApplicationLeads() string {
	return `
		SELECT id::text, coalesce(nullif(source,''),'site') AS source
		FROM leads
		WHERE owner_id IS NULL
		  AND coalesce(status,'') <> 'rejected'
		  AND (` + sqlLeadOnNewApplicationStage("leads") + `)
		ORDER BY created_at ASC`
}

// sqlRopHeldNewApplicationLeads — «Новый лид», висящие на РОПе отдела (ещё не у менеджера).
// held_on_rop — ручной возврат ропу, такие лиды обратно менеджерам не уходят.
// Отдел без свободного дневного места и отдел с выключенной автораздачей в выборку не входят:
// иначе их старые лиды навсегда занимают пачку и не пускают отделы, куда ещё можно раздать.
// $2 — московский день для счётчика менеджера.
func sqlRopHeldNewApplicationLeads() string {
	return `
		SELECT l.id::text, coalesce(d.id::text,'') AS dept_id,
		       coalesce(nullif(l.source,''),'site') AS source
		FROM leads l
		JOIN users u ON u.id=l.owner_id
		JOIN roles r ON r.id=u.role_id
		JOIN departments d ON d.head_user_id=u.id
		WHERE coalesce(l.status,'') <> 'rejected'
		  AND lower(coalesce(r.name,'')) IN ('rop','admin')
		  AND (` + sqlLeadOnNewApplicationStage("l") + `)
		  AND NOT EXISTS (
		    SELECT 1 FROM activity_log a
		    WHERE a.entity_type='lead' AND a.entity_id=l.id
		      AND a.action='held_on_rop'
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM distribution_settings s
		    WHERE s.key = 'team_auto:' || d.id::text
		      AND s.value = '0'
		  )
		  AND ` + sqlDeptManagerHasRoom("d", "$2::date") + `
		ORDER BY l.updated_at ASC`
}

// sqlDeptManagerHasRoom — в отделе есть активный менеджер с планом > 0 и ещё не выбранным лимитом на день.
func sqlDeptManagerHasRoom(deptAlias, dayExpr string) string {
	return `EXISTS (
		    SELECT 1
		    FROM distribution_manager_quotas q
		    JOIN users mu ON mu.id=q.user_id AND mu.is_active AND mu.department_id=` + deptAlias + `.id
		    LEFT JOIN roles mr ON mr.id=mu.role_id
		    LEFT JOIN distribution_manager_counters c
		      ON c.user_id=q.user_id AND c.day=` + dayExpr + `
		    WHERE q.department_id=` + deptAlias + `.id
		      AND lower(coalesce(mr.name,'')) IN ('manager','sales')
		      AND q.daily_limit > 0
		      AND coalesce(c.assigned,0) < q.daily_limit
		  )`
}

// distDrainScanLimit — сколько лидов смотреть за заход, чтобы набрать пачку.
// Пачка назначений маленькая, а впереди могут лежать отделы, которым сегодня уже некуда.
func distDrainScanLimit(assignLimit int) int {
	if assignLimit < 1 {
		assignLimit = distOrphanBatch
	}
	scan := assignLimit * 16
	if scan < 400 {
		scan = 400
	}
	if scan > 1000 {
		scan = 1000
	}
	return scan
}

// distributionOrphanLoop — догоняет бесхозные и зависшие на РОПе «Новый лид».
func (a *App) distributionOrphanLoop() {
	time.Sleep(8 * time.Second)
	ticker := time.NewTicker(distOrphanInterval)
	defer ticker.Stop()
	for {
		n1, err1 := a.drainUnownedLeads(context.Background(), distOrphanBatch)
		n2, err2 := a.drainRopHeldLeads(context.Background(), distOrphanBatch)
		if err1 != nil {
			log.Printf("distribution orphan drain: %v", err1)
		}
		if err2 != nil {
			log.Printf("distribution rop-held drain: %v", err2)
		}
		if n1+n2 > 0 {
			log.Printf("distribution drain: unowned=%d rop_held→manager=%d", n1, n2)
		}
		<-ticker.C
	}
}

// drainUnownedLeads раздаёт до limit бесхозных лидов из «Новый лид».
func (a *App) drainUnownedLeads(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = distOrphanBatch
	}
	if !a.distAutoOn(ctx) {
		return 0, nil
	}
	rows := a.queryMaps(ctx, sqlUnownedNewApplicationLeads()+` LIMIT $1`, distDrainScanLimit(limit))
	assigned := 0
	blockedPool := map[string]bool{}
	for _, row := range rows {
		if assigned >= limit {
			break
		}
		id := strings.TrimSpace(fmt.Sprint(row["id"]))
		if id == "" || id == "<nil>" {
			continue
		}
		source := strings.TrimSpace(fmt.Sprint(row["source"]))
		if source == "<nil>" {
			source = "site"
		}
		pool := a.poolForSource(ctx, source)
		if pool == "" {
			pool = "depts"
		}
		if blockedPool[pool] {
			continue
		}
		if _, err := a.distributeOne(ctx, "", id, pool, "system:orphan-drain"); err != nil {
			msg := err.Error()
			if strings.Contains(msg, "уже назначен") || strings.Contains(msg, "Новый лид") {
				continue
			}
			if strings.Contains(msg, "дневные лимиты исчерпаны") || strings.Contains(msg, "нет отделов") {
				// Этот пул на сегодня выбран. Лиды другого пула, которым ещё есть куда, не держим.
				blockedPool[pool] = true
				continue
			}
			log.Printf("distribution orphan lead=%s: %v", id, err)
			continue
		}
		assigned++
	}
	return assigned, nil
}

// drainRopHeldLeads — лиды на РОПе в «Новый лид» → менеджеру отдела (не трогает чужих менеджеров).
// Отдел, которому сегодня уже некуда, пропускается. Его лиды остаются на РОПе и не держат чужую очередь.
func (a *App) drainRopHeldLeads(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = distOrphanBatch
	}
	if !a.distAutoOn(ctx) {
		return 0, nil
	}
	day := moscowDay(time.Now())
	rows := a.queryMaps(ctx, sqlRopHeldNewApplicationLeads()+` LIMIT $1`, distDrainScanLimit(limit), day)
	moved := 0
	known := map[string]bool{}
	room := map[string]bool{}
	hasRoom := func(deptID string) bool {
		if known[deptID] {
			return room[deptID]
		}
		v := a.deptHasManagerRoom(ctx, deptID, day)
		known[deptID] = true
		room[deptID] = v
		return v
	}
	forgetRoom := func(deptID string) {
		delete(known, deptID)
		delete(room, deptID)
	}
	for _, row := range rows {
		if moved >= limit {
			break
		}
		id := strings.TrimSpace(fmt.Sprint(row["id"]))
		deptID := strings.TrimSpace(fmt.Sprint(row["dept_id"]))
		if id == "" || id == "<nil>" || deptID == "" || deptID == "<nil>" {
			continue
		}
		if !a.distTeamAutoOn(ctx, deptID) {
			known[deptID] = true
			room[deptID] = false
			continue
		}
		if !hasRoom(deptID) {
			continue
		}
		// ещё раз: owner всё ещё РОП этого отдела (не менеджер)
		var stillRop bool
		_ = a.db.QueryRow(ctx, `
			SELECT true FROM leads l
			JOIN users u ON u.id=l.owner_id
			JOIN roles r ON r.id=u.role_id
			JOIN departments d ON d.id=$2::uuid AND d.head_user_id=u.id
			WHERE l.id=$1::uuid
			  AND lower(coalesce(r.name,'')) IN ('rop','admin')`, id, deptID).Scan(&stillRop)
		if !stillRop {
			continue
		}
		if _, ok := a.distributeToManagers(ctx, deptID, "", id, "depts", "system:rop-held-drain"); ok {
			moved++
			forgetRoom(deptID)
			continue
		}
		forgetRoom(deptID)
		if !hasRoom(deptID) {
			known[deptID] = true
			room[deptID] = false
		}
	}
	return moved, nil
}

func (a *App) deptHasManagerRoom(ctx context.Context, deptID, day string) bool {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" || deptID == "<nil>" || a == nil || a.db == nil {
		return false
	}
	var ok bool
	err := a.db.QueryRow(ctx, `SELECT `+sqlDeptManagerHasRoom("d", "$2::date")+`
		FROM departments d WHERE d.id=$1::uuid`, deptID, day).Scan(&ok)
	return err == nil && ok
}

// postDrainUnownedLeads — админ: сразу раздать бесхозные + зависшие на РОПе.
func (a *App) postDrainUnownedLeads(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(r) {
		writeJSON(w, 403, map[string]string{"error": "только для администратора"})
		return
	}
	n1, err1 := a.drainUnownedLeads(r.Context(), 200)
	n2, err2 := a.drainRopHeldLeads(r.Context(), 200)
	if err1 != nil {
		writeJSON(w, 400, map[string]string{"error": err1.Error()})
		return
	}
	if err2 != nil {
		writeJSON(w, 400, map[string]string{"error": err2.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "unowned_assigned": n1, "rop_held_to_manager": n2})
}
