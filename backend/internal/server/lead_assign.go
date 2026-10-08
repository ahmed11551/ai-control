package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// syncDealsOwnerForLead копирует ответственного лида на связанные сделки,
// чтобы карточка появилась в канбане менеджера (фильтр по deals.owner_id).
func (a *App) syncDealsOwnerForLead(ctx context.Context, leadID, ownerID string) {
	leadID = strings.TrimSpace(leadID)
	ownerID = strings.TrimSpace(ownerID)
	if leadID == "" {
		return
	}
	rows := a.queryMaps(ctx, `
		SELECT id::text AS id FROM deals
		WHERE lead_id=$1::uuid
		   OR id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)`, leadID)
	// Источник истины — leads.owner_id на момент UPDATE (а не переданный ownerID):
	// иначе параллельная выдача/отзыв разводит владельца лида и сделки.
	_, err := a.db.Exec(ctx, `
		UPDATE deals d SET owner_id=l.owner_id, updated_at=now()
		FROM leads l
		WHERE l.id=$1::uuid
		  AND (d.lead_id=l.id OR d.id=l.converted_deal_id)`, leadID)
	if err != nil {
		return
	}
	_ = ownerID
	for _, row := range rows {
		dealID := strings.TrimSpace(fmt.Sprint(row["id"]))
		if dealID == "" || dealID == "<nil>" {
			continue
		}
		a.broadcastDealUpdated(ctx, dealID, nil)
	}
}

// broadcastDealUpdated — WS с актуальным owner_id (клиент режет чужие до GET).
func (a *App) broadcastDealUpdated(ctx context.Context, dealID string, extra map[string]any) {
	dealID = strings.TrimSpace(dealID)
	if dealID == "" || a.hub == nil {
		return
	}
	payload := map[string]any{"id": dealID}
	for k, v := range extra {
		payload[k] = v
	}
	if _, ok := payload["owner_id"]; !ok {
		var owner, dept string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(d.owner_id::text,''), coalesce(u.department_id::text,'')
			FROM deals d
			LEFT JOIN users u ON u.id=d.owner_id
			WHERE d.id=$1::uuid`, dealID).Scan(&owner, &dept)
		payload["owner_id"] = owner
		if dept != "" {
			payload["owner_department_id"] = dept
		}
	} else if _, ok := payload["owner_department_id"]; !ok {
		owner := strings.TrimSpace(fmt.Sprint(payload["owner_id"]))
		if owner != "" && owner != "<nil>" {
			var dept string
			_ = a.db.QueryRow(ctx, `
				SELECT coalesce(department_id::text,'') FROM users WHERE id=$1::uuid`, owner).Scan(&dept)
			if dept != "" {
				payload["owner_department_id"] = dept
			}
		}
	}
	a.hub.Broadcast("deal.updated", payload)
}

// broadcastDealCreated — WS создания сделки с owner.
func (a *App) broadcastDealCreated(ctx context.Context, dealID string, extra map[string]any) {
	dealID = strings.TrimSpace(dealID)
	if dealID == "" || a.hub == nil {
		return
	}
	payload := map[string]any{"id": dealID}
	for k, v := range extra {
		payload[k] = v
	}
	if _, ok := payload["owner_id"]; !ok {
		var owner, dept string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(d.owner_id::text,''), coalesce(u.department_id::text,'')
			FROM deals d
			LEFT JOIN users u ON u.id=d.owner_id
			WHERE d.id=$1::uuid`, dealID).Scan(&owner, &dept)
		payload["owner_id"] = owner
		if dept != "" {
			payload["owner_department_id"] = dept
		}
	}
	a.hub.Broadcast("deal.created", payload)
}

// syncDealsTitleForLead — канбан показывает deals.title; при переименовании лида копируем на сделки.
func (a *App) syncDealsTitleForLead(ctx context.Context, leadID, title string) {
	leadID = strings.TrimSpace(leadID)
	title = strings.TrimSpace(title)
	if leadID == "" || title == "" {
		return
	}
	rows := a.queryMaps(ctx, `
		SELECT id::text AS id FROM deals
		WHERE lead_id=$1::uuid
		   OR id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)`, leadID)
	_, err := a.db.Exec(ctx, `
		UPDATE deals SET title=$2, updated_at=now()
		WHERE lead_id=$1::uuid
		   OR id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)`,
		leadID, title)
	if err != nil {
		return
	}
	for _, row := range rows {
		dealID := strings.TrimSpace(fmt.Sprint(row["id"]))
		if dealID == "" || dealID == "<nil>" {
			continue
		}
		a.broadcastDealUpdated(ctx, dealID, map[string]any{"title": title, "lead_id": leadID})
	}
}

// finalizeDistributedAssignment — после раздачи: сделка в «Адаптации» + owner на канбане.
func (a *App) finalizeDistributedAssignment(ctx context.Context, leadID, dealID, ownerID string) string {
	leadID = strings.TrimSpace(leadID)
	dealID = strings.TrimSpace(dealID)
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" || ownerID == "<nil>" {
		return dealID
	}
	if leadID != "" && leadID != "<nil>" {
		hireID := a.ensureHireDealForLead(ctx, leadID, ownerID)
		if hireID != "" {
			dealID = hireID
		} else if dealID == "" || dealID == "<nil>" {
			dealID = a.ensureDealForLead(ctx, leadID, ownerID, nil)
		}
		a.syncDealsOwnerForLead(ctx, leadID, ownerID)
	} else if dealID != "" && dealID != "<nil>" {
		_, _ = a.db.Exec(ctx, `
			UPDATE deals SET owner_id=$2::uuid, updated_at=now() WHERE id=$1::uuid`, dealID, ownerID)
		a.broadcastDealUpdated(ctx, dealID, map[string]any{"owner_id": ownerID})
	}
	if dealID != "" && dealID != "<nil>" {
		a.broadcastDealUpdated(ctx, dealID, map[string]any{"owner_id": ownerID})
	}
	return dealID
}

// flushROPOwnedToManagers — заявки, зависшие на РОПе, раздать менеджерам по квотам.
func (a *App) flushROPOwnedToManagers(ctx context.Context, deptID, actor string) int {
	deptID = strings.TrimSpace(deptID)
	if deptID == "" {
		return 0
	}
	var head string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(head_user_id::text,'') FROM departments WHERE id=$1::uuid`, deptID).Scan(&head)
	head = strings.TrimSpace(head)
	if head == "" || head == "<nil>" {
		return 0
	}
	rows := a.queryMaps(ctx, `
		SELECT l.id::text AS lead_id,
		       coalesce(
		         (SELECT d.id::text FROM deals d
		          WHERE d.lead_id=l.id OR d.id=l.converted_deal_id
		          ORDER BY d.created_at ASC LIMIT 1),
		         ''
		       ) AS deal_id
		FROM leads l
		WHERE l.owner_id=$1::uuid
		  AND coalesce(l.status,'') <> 'rejected'
		ORDER BY l.created_at ASC
		LIMIT 300`, head)
	n := 0
	for _, row := range rows {
		leadID := strings.TrimSpace(fmt.Sprint(row["lead_id"]))
		dealID := strings.TrimSpace(fmt.Sprint(row["deal_id"]))
		if leadID == "" || leadID == "<nil>" {
			continue
		}
		if dealID == "<nil>" {
			dealID = ""
		}
		if mgr, ok := a.distributeToManagers(ctx, deptID, dealID, leadID, "depts", actor); ok {
			a.finalizeDistributedAssignment(ctx, leadID, dealID, mgr.OwnerID)
			n++
			continue
		}
		// менеджеров нет / лимиты — карточка на канбане ТЕКУЩЕГО владельца лида
		// (лид мог уйти менеджеру параллельно — не перетираем сделку на РОПа).
		cur, _ := a.leadStickyInfo(ctx, leadID)
		if cur == "" {
			cur = head
		}
		a.finalizeDistributedAssignment(ctx, leadID, dealID, cur)
	}
	_ = a.backfillMissingDeals(ctx, 100)
	return n
}

// backfillMissingDeals — у лидов с владельцем без сделки появляется карточка в канбане.
func (a *App) backfillMissingDeals(ctx context.Context, limit int) int {
	if limit <= 0 {
		limit = 300
	}
	// Сделки с пустым/чужим owner на воронке найма — подтянуть owner с лида.
	_, _ = a.db.Exec(ctx, `
		UPDATE deals d
		SET owner_id=l.owner_id, updated_at=now()
		FROM leads l
		JOIN pipelines p ON p.id=d.pipeline_id
		WHERE l.owner_id IS NOT NULL
		  AND (d.lead_id=l.id OR d.id=l.converted_deal_id)
		  AND coalesce(p.bitrix_id,'')=$1
		  AND (d.owner_id IS NULL OR d.owner_id IS DISTINCT FROM l.owner_id)`,
		hirePipelineKey)
	rows := a.queryMaps(ctx, `
		SELECT l.id::text AS lead_id, coalesce(l.owner_id::text,'') AS owner_id
		FROM leads l
		WHERE coalesce(l.status,'') <> 'rejected'
		  AND l.owner_id IS NOT NULL
		  AND NOT EXISTS (
		        SELECT 1 FROM deals d
		        WHERE d.lead_id=l.id OR d.id=l.converted_deal_id
		      )
		ORDER BY l.created_at ASC
		LIMIT $1`, limit)
	n := 0
	for _, row := range rows {
		leadID := strings.TrimSpace(fmt.Sprint(row["lead_id"]))
		ownerID := strings.TrimSpace(fmt.Sprint(row["owner_id"]))
		if leadID == "" || leadID == "<nil>" {
			continue
		}
		if ownerID == "<nil>" {
			ownerID = ""
		}
		// Если есть только escort-сделка — всё равно нужна карточка в «Адаптации».
		dealID := a.ensureHireDealForLead(ctx, leadID, ownerID)
		if dealID != "" {
			n++
		}
	}
	return n
}

// ensureHireDealForLead — сделка строго в воронке «Адаптация» (local:hire).
// Если уже есть escort-сделка (логистика) — не трогаем: один лид = одна сделка.
func (a *App) ensureHireDealForLead(ctx context.Context, leadID, ownerID string) string {
	leadID = strings.TrimSpace(leadID)
	if leadID == "" {
		return ""
	}
	var existing string
	_ = a.db.QueryRow(ctx, `
		SELECT d.id::text FROM deals d
		JOIN pipelines p ON p.id=d.pipeline_id
		WHERE (d.lead_id=$1::uuid OR d.id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid))
		  AND coalesce(p.bitrix_id,'')=$2
		ORDER BY d.created_at ASC LIMIT 1`, leadID, hirePipelineKey).Scan(&existing)
	if existing != "" {
		if ownerID != "" {
			_, _ = a.db.Exec(ctx, `
				UPDATE deals SET owner_id=$2::uuid, updated_at=now()
				WHERE id=$1::uuid AND (owner_id IS NULL OR owner_id IS DISTINCT FROM $2::uuid)`,
				existing, ownerID)
		}
		_, _ = a.db.Exec(ctx, `
			UPDATE leads SET converted_deal_id=coalesce(converted_deal_id, $2::uuid) WHERE id=$1::uuid`, leadID, existing)
		return existing
	}
	var anyDeal string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(nullif(converted_deal_id::text,''), (
			SELECT d.id::text FROM deals d WHERE d.lead_id=$1::uuid ORDER BY d.created_at ASC LIMIT 1
		), '')`, leadID).Scan(&anyDeal)
	if anyDeal != "" {
		// Уже в другой воронке (обычно escort) — не плодим вторую сделку.
		if ownerID != "" {
			_, _ = a.db.Exec(ctx, `
				UPDATE deals SET owner_id=$2::uuid, updated_at=now()
				WHERE id=$1::uuid AND (owner_id IS NULL OR owner_id IS DISTINCT FROM $2::uuid)`,
				anyDeal, ownerID)
		}
		return anyDeal
	}
	return a.ensureDealForLead(ctx, leadID, ownerID, nil)
}

// validateLeadAssignee — РОП назначает только менеджеров своего отдела; админ — менеджера/РОПа.
func (a *App) validateLeadAssignee(ctx context.Context, r *http.Request, _ string, newOwnerID string) error {
	var role, dept string
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(r.name,''), coalesce(u.department_id::text,'')
		FROM users u LEFT JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid AND u.is_active`, newOwnerID).Scan(&role, &dept)
	if err != nil {
		return fmt.Errorf("сотрудник не найден")
	}
	role = strings.ToLower(role)
	if isAdmin(r) {
		if role != "manager" && role != "sales" && role != "rop" {
			return fmt.Errorf("назначать можно только РОПа или менеджера")
		}
		return nil
	}
	if !isROP(r) {
		return fmt.Errorf("недостаточно прав")
	}
	if role != "manager" && role != "sales" {
		return fmt.Errorf("РОП может назначить только менеджера")
	}
	ownDept := a.actorDepartmentID(ctx, actorID(r))
	if ownDept == "" || dept != ownDept {
		return fmt.Errorf("менеджер должен быть из вашего отдела")
	}
	return nil
}

func (a *App) actorDepartmentID(ctx context.Context, userID string) string {
	var dept string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(department_id::text,'') FROM users WHERE id=$1`, userID).Scan(&dept)
	if dept != "" {
		return dept
	}
	_ = a.db.QueryRow(ctx, `
		SELECT id::text FROM departments WHERE head_user_id=$1::uuid LIMIT 1`, userID).Scan(&dept)
	return dept
}

func (a *App) listLeadAssignees(w http.ResponseWriter, r *http.Request) {
	leadID := chi.URLParam(r, "id")
	if hideIfMissing(w, a.canSeeLead(r.Context(), a.crmScope(r), leadID)) {
		return
	}
	if !isAdmin(r) && !isROP(r) {
		writeJSON(w, 403, map[string]string{"error": "недостаточно прав"})
		return
	}
	dept := ""
	if isROP(r) {
		dept = a.actorDepartmentID(r.Context(), actorID(r))
	}
	rows := a.queryMaps(r.Context(), `
		SELECT u.id::text, u.full_name, coalesce(r.name,'') AS role,
		       coalesce(d.name,'') AS department
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN departments d ON d.id=u.department_id
		WHERE u.is_active
		  AND lower(coalesce(r.name,'')) IN ('manager','sales')
		  AND ($1='' OR u.department_id=$1::uuid)
		ORDER BY u.full_name`, dept)
	if rows == nil {
		rows = []map[string]any{}
	}
	var curID, curName string
	_ = a.db.QueryRow(r.Context(), `
		SELECT coalesce(l.owner_id::text,''), coalesce(u.full_name,'')
		FROM leads l
		LEFT JOIN users u ON u.id=l.owner_id
		WHERE l.id=$1::uuid`, leadID).Scan(&curID, &curName)
	curID = strings.TrimSpace(curID)
	if curID != "" && curID != "<nil>" {
		found := false
		for _, row := range rows {
			if strings.TrimSpace(fmt.Sprint(row["id"])) == curID {
				found = true
				break
			}
		}
		if !found {
			rows = append([]map[string]any{{
				"id": curID, "full_name": curName, "role": "manager", "department": "",
			}}, rows...)
		}
	}
	writeJSON(w, 200, map[string]any{"managers": rows})
}
