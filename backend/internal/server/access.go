package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type crmScope struct {
	All       bool
	UserID    string
	DeptID    string
	IsManager bool
	IsLogist  bool
}

func (a *App) crmScope(r *http.Request) crmScope {
	uid := actorID(r)
	role := roleName(r)
	s := crmScope{
		UserID:    uid,
		All:       role == "admin",
		IsManager: role == "manager" || role == "sales",
		IsLogist:  isLogistRole(role),
	}
	if s.All || uid == "" {
		return s
	}
	// РОП видит весь свой отдел (department_id или head_user_id).
	if role == "rop" {
		s.DeptID = a.actorDepartmentID(r.Context(), uid)
	}
	return s
}

func ownerCol(alias string) string {
	if alias == "" {
		return "owner_id"
	}
	return alias + ".owner_id"
}

func (s crmScope) applyOwner(alias string, where []string, next func(any) string) []string {
	if s.All {
		return where
	}
	col := ownerCol(alias)
	uid := next(s.UserID)
	if s.DeptID != "" {
		dept := next(s.DeptID)
		where = append(where, fmt.Sprintf(
			"(%s = %s::uuid OR %s IN (SELECT id FROM users WHERE department_id=%s::uuid))",
			col, uid, col, dept))
		return where
	}
	return append(where, col+" = "+uid+"::uuid")
}

// applyDealListVisibility — только свои (или отдела) сделки в канбане/списке.
// Воронка «Оформление и сопровождение» менеджеру в списке недоступна.
// Логист видит escort / logistics_needed (owner не важен).
func (a *App) applyDealListVisibility(r *http.Request, where []string, next func(any) string) []string {
	s := a.crmScope(r)
	if s.All {
		return where
	}
	if s.IsLogist {
		// Пользовательский фильтр стадии не является разрешением доступа.
		where = append(where, sqlLogistBoardVisible("d"))
		return where
	}
	// Менеджер и РОП не видят escort, кроме колонок логиста «Заявка» и «Билет»:
	// их карточки остаются в «Адаптация → Билет».
	if s.IsManager || roleName(r) == "rop" {
		esc := next(escortPipelineKey)
		ticket := next(escortPipelineKey + ":ticket")
		req := next(escortPipelineKey + ":ticket_request")
		where = append(where, fmt.Sprintf(`(
			NOT EXISTS (SELECT 1 FROM pipelines p WHERE p.id=d.pipeline_id AND coalesce(p.bitrix_id,'')=%s)
			OR EXISTS (
				SELECT 1 FROM pipeline_stages es
				WHERE es.id=d.stage_id AND coalesce(es.bitrix_id,'') IN (%s, %s)
			)
		)`, esc, ticket, req))
	}
	uid := next(s.UserID)
	if s.DeptID != "" {
		dept := next(s.DeptID)
		where = append(where, fmt.Sprintf(
			`(d.owner_id = %s::uuid OR d.owner_id IN (SELECT id FROM users WHERE department_id=%s::uuid))`,
			uid, dept))
		return where
	}
	where = append(where, fmt.Sprintf(`d.owner_id = %s::uuid`, uid))
	return where
}

// dealOnBoardForUser — как applyDealListVisibility: канбан без исключений недозвон/autodial.
// Менеджер видит только свои; РОП — отдел; логист — escort/logistics_needed; admin — всё.
func (a *App) dealOnBoardForUser(ctx context.Context, s crmScope, dealID string) bool {
	dealID = strings.TrimSpace(dealID)
	if dealID == "" {
		return false
	}
	if s.All {
		return true
	}
	if s.UserID == "" {
		return false
	}
	if a.dealOnOldLeadsPipeline(ctx, dealID) {
		if s.IsLogist && a.dealLogisticsNeeded(ctx, dealID) {
			return true
		}
		return a.temaSeesSleepingDeal(ctx, s, dealID)
	}
	if a.dealIsSleeping(ctx, dealID) {
		if s.IsLogist {
			return true
		}
		return a.temaSeesSleepingDeal(ctx, s, dealID)
	}
	if s.IsLogist {
		var n int
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM deals d
			WHERE d.id=$1::uuid
			  AND `+sqlLogistBoardVisible("d")+`
			LIMIT 1`, dealID).Scan(&n)
		return err == nil
	}
	// Менеджер и РОП: escort не на их канбане, кроме «Заявка» и «Куплен билет».
	// Своя колонка после «Билет» карточку на доске оставляет, стадию логиста не трогает.
	if (s.IsManager || s.DeptID != "") && a.dealOnEscortPipeline(ctx, dealID) &&
		!a.dealStageIn(ctx, dealID, escortPipelineKey+":ticket", escortPipelineKey+":ticket_request") &&
		!a.dealHasManagerPostBilet(ctx, dealID) {
		return false
	}
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM deals WHERE id=$1::uuid AND (
				owner_id=$2::uuid OR owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
			)`, dealID, s.UserID, s.DeptID).Scan(&n)
		return err == nil
	}
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals WHERE id=$1::uuid AND owner_id=$2::uuid`, dealID, s.UserID).Scan(&n)
	return err == nil
}

func (a *App) dealStageIn(ctx context.Context, dealID string, keys ...string) bool {
	if a == nil || a.db == nil || dealID == "" || len(keys) == 0 {
		return false
	}
	var key string
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(s.bitrix_id,'')
		FROM deals d
		JOIN pipeline_stages s ON s.id=d.stage_id
		WHERE d.id=$1::uuid`, dealID).Scan(&key)
	if err != nil {
		return false
	}
	for _, k := range keys {
		if key == k {
			return true
		}
	}
	return false
}

func (a *App) dealOnHireLogisticsQueue(ctx context.Context, dealID string) bool {
	if a == nil || a.db == nil || dealID == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals d
		JOIN pipelines p ON p.id=d.pipeline_id
		WHERE d.id=$1::uuid
		  AND coalesce(d.logistics_needed,false)=true
		  AND coalesce(p.bitrix_id,'')=$2
		LIMIT 1`, dealID, hirePipelineKey).Scan(&n)
	return err == nil
}

func (a *App) dealOnEscortPipeline(ctx context.Context, dealID string) bool {
	if dealID == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals d
		JOIN pipelines p ON p.id=d.pipeline_id
		WHERE d.id=$1::uuid AND coalesce(p.bitrix_id,'')=$2
		LIMIT 1`, dealID, escortPipelineKey).Scan(&n)
	return err == nil
}

func (a *App) dealExists(ctx context.Context, id string) bool {
	id = strings.TrimSpace(id)
	if a == nil || a.db == nil || id == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `SELECT 1 FROM deals WHERE id=$1::uuid LIMIT 1`, id).Scan(&n)
	return err == nil
}

func (a *App) canSeeDeal(ctx context.Context, s crmScope, id string) bool {
	if s.All {
		return true
	}
	// Прямая ссылка не расширяет логистическую область доступа.
	if s.IsLogist {
		if a == nil || a.db == nil || id == "" {
			return false
		}
		var n int
		err := a.db.QueryRow(ctx, `SELECT 1 FROM deals d
			LEFT JOIN pipelines p ON p.id=d.pipeline_id
			WHERE d.id=$1::uuid AND (coalesce(d.logistics_needed,false)
			OR `+sqlDealOnLogistLinkStage("d")+`
			OR coalesce(p.bitrix_id,'')=$2)`, id, escortPipelineKey).Scan(&n)
		return err == nil
	}
	if a.dealOnOldLeadsPipeline(ctx, id) || a.dealIsSleeping(ctx, id) {
		return a.temaSeesSleepingDeal(ctx, s, id)
	}
	// Менеджер не видит чужой escort; свою сделку после передачи — может (билет кандидату).
	if s.IsManager && a.dealOnEscortPipeline(ctx, id) {
		var own int
		_ = a.db.QueryRow(ctx, `SELECT 1 FROM deals WHERE id=$1::uuid AND owner_id=$2::uuid`, id, s.UserID).Scan(&own)
		if own == 0 {
			return false
		}
		return true
	}
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM deals WHERE id=$1::uuid AND (
				owner_id=$2::uuid OR owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
			)`, id, s.UserID, s.DeptID).Scan(&n)
		if err == nil {
			return true
		}
	} else {
		err := a.db.QueryRow(ctx, `SELECT 1 FROM deals WHERE id=$1::uuid AND owner_id=$2::uuid`, id, s.UserID).Scan(&n)
		if err == nil {
			return true
		}
	}
	// Скорозвон: менеджеры на линии видят карточку до claim (иначе 404 «not found»).
	if a.dealOpenForAutodialManager(ctx, id, s.UserID) {
		return true
	}
	// Недозвон 1/2/3 — общий пул для менеджеров на линии (без полного номера в API).
	return a.managerCanSeeNedozvonPool(ctx, s.UserID) && a.dealOnNedozvonPool(ctx, id)
}

// dealOpenForAutodialManager — активный MakeCallback + caller manager/sales с can_call.
func (a *App) dealOpenForAutodialManager(ctx context.Context, dealID, userID string) bool {
	if dealID == "" || userID == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1
		FROM autodial_attempts a
		JOIN users u ON u.id=$2::uuid AND u.is_active AND coalesce(u.can_call, true)
		JOIN roles r ON r.id=u.role_id AND lower(r.name) IN ('manager','sales')
		WHERE a.deal_id=$1::uuid
		  AND a.finished_at IS NULL
		  AND a.status IN ('started','ringing','connected')
		  AND a.started_at > now() - interval '15 minutes'
		LIMIT 1`, dealID, userID).Scan(&n)
	return err == nil
}

func (a *App) managerCanSeeNedozvonPool(ctx context.Context, userID string) bool {
	if userID == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM users u
		JOIN roles r ON r.id=u.role_id
		WHERE u.id=$1::uuid AND u.is_active AND coalesce(u.can_call, true)
		  AND lower(r.name) IN ('manager','sales')`, userID).Scan(&n)
	return err == nil
}

func (a *App) dealOnNedozvonPool(ctx context.Context, dealID string) bool {
	var bid string
	err := a.db.QueryRow(ctx, `
		SELECT coalesce(ps.bitrix_id,'')
		FROM deals d
		JOIN pipeline_stages ps ON ps.id=d.stage_id
		WHERE d.id=$1::uuid`, dealID).Scan(&bid)
	if err != nil {
		return false
	}
	for _, id := range nedozvonPoolBitrixIDs {
		if bid == id {
			return true
		}
	}
	return false
}

func (a *App) leadLinkedToNedozvonPool(ctx context.Context, leadID string) bool {
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals d
		JOIN pipeline_stages ps ON ps.id=d.stage_id
		WHERE coalesce(ps.bitrix_id,'') IN (`+nedozvonPoolSQLIn()+`)
		  AND (
			d.lead_id=$1::uuid
			OR d.id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
		  )
		LIMIT 1`, leadID).Scan(&n)
	return err == nil
}

func (a *App) canSeeLead(ctx context.Context, s crmScope, id string) bool {
	if s.All {
		return true
	}
	// Ссылка на лид доступна только в логистическом процессе.
	if s.IsLogist {
		return a.logisticsDealIDForLead(ctx, id) != ""
	}
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM leads WHERE id=$1::uuid AND (
				owner_id=$2::uuid OR owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
			)`, id, s.UserID, s.DeptID).Scan(&n)
		if err == nil {
			return true
		}
	} else {
		err := a.db.QueryRow(ctx, `SELECT 1 FROM leads WHERE id=$1::uuid AND owner_id=$2::uuid`, id, s.UserID).Scan(&n)
		if err == nil {
			return true
		}
	}
	if a.managerCanSeeNedozvonPool(ctx, s.UserID) && a.leadLinkedToNedozvonPool(ctx, id) {
		return true
	}
	// Видит связанную сделку (владелец карточки на канбане) — может открыть лид и оставить комментарий.
	if dealID := a.anyDealIDForLead(ctx, id); dealID != "" && a.canSeeDeal(ctx, s, dealID) {
		return true
	}
	return false
}

// anyDealIDForLead — сделка, связанная с лидом (без создания новой).
func (a *App) anyDealIDForLead(ctx context.Context, leadID string) string {
	leadID = strings.TrimSpace(leadID)
	if leadID == "" {
		return ""
	}
	var dealID string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(
			nullif((SELECT converted_deal_id::text FROM leads WHERE id=$1::uuid), ''),
			(SELECT d.id::text FROM deals d WHERE d.lead_id=$1::uuid ORDER BY d.created_at ASC LIMIT 1),
			''
		)`, leadID).Scan(&dealID)
	dealID = strings.TrimSpace(dealID)
	if dealID == "<nil>" {
		return ""
	}
	return dealID
}

// leadLinkedToLogistics — лид связан со сделкой в очереди логистики / escort.
func (a *App) leadLinkedToLogistics(ctx context.Context, leadID string) bool {
	return a.logisticsDealIDForLead(ctx, leadID) != ""
}

// logisticsDealIDForLead — сделка escort / logistics_needed для карточки логиста.
func (a *App) logisticsDealIDForLead(ctx context.Context, leadID string) string {
	if a == nil || a.db == nil || leadID == "" {
		return ""
	}
	var id string
	err := a.db.QueryRow(ctx, `
		SELECT d.id::text FROM deals d
		LEFT JOIN pipelines p ON p.id=d.pipeline_id
		WHERE (
			d.lead_id=$1::uuid
			OR d.id = (SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
		)
		  AND (coalesce(d.logistics_needed,false)=true
		    OR `+sqlDealOnLogistLinkStage("d")+`
		    OR coalesce(p.bitrix_id,'')=$2)
		ORDER BY d.updated_at DESC NULLS LAST, d.created_at DESC
		LIMIT 1`, leadID, escortPipelineKey).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

func (a *App) contactOnNedozvonPool(ctx context.Context, contactID string) bool {
	if contactID == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals d
		JOIN pipeline_stages ps ON ps.id=d.stage_id
		WHERE d.contact_id=$1::uuid AND coalesce(ps.bitrix_id,'') IN (`+nedozvonPoolSQLIn()+`)
		LIMIT 1`, contactID).Scan(&n)
	return err == nil
}

func (a *App) contactVisible(ctx context.Context, s crmScope, id string) bool {
	if s.All {
		return true
	}
	if s.IsLogist {
		return a.contactLinkedToLogistics(ctx, id)
	}
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM contacts c WHERE c.id=$1::uuid AND (
				c.owner_id=$2::uuid
				OR c.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
				OR EXISTS (
					SELECT 1 FROM deals d WHERE d.contact_id=c.id AND (
						d.owner_id=$2::uuid OR d.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
					)
				)
				OR EXISTS (
					SELECT 1 FROM leads l WHERE l.contact_id=c.id AND (
						l.owner_id=$2::uuid OR l.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
					)
				)
			)`, id, s.UserID, s.DeptID).Scan(&n)
		if err == nil {
			return true
		}
	} else {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM contacts c WHERE c.id=$1::uuid AND (
				c.owner_id=$2::uuid
				OR EXISTS (SELECT 1 FROM deals d WHERE d.contact_id=c.id AND d.owner_id=$2::uuid)
				OR EXISTS (SELECT 1 FROM leads l WHERE l.contact_id=c.id AND l.owner_id=$2::uuid)
			)`, id, s.UserID).Scan(&n)
		if err == nil {
			return true
		}
	}
	return a.managerCanSeeNedozvonPool(ctx, s.UserID) && a.contactOnNedozvonPool(ctx, id)
}

func (a *App) contactOnAnyDeal(ctx context.Context, contactID string) bool {
	if a == nil || a.db == nil || strings.TrimSpace(contactID) == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals d
		WHERE d.contact_id=$1::uuid
		   OR d.lead_id IN (SELECT id FROM leads WHERE contact_id=$1::uuid)
		   OR d.id IN (
		        SELECT converted_deal_id FROM leads
		        WHERE contact_id=$1::uuid AND converted_deal_id IS NOT NULL
		   )
		LIMIT 1`, contactID).Scan(&n)
	return err == nil
}

func (a *App) contactLinkedToLogistics(ctx context.Context, contactID string) bool {
	if a == nil || a.db == nil || contactID == "" {
		return false
	}
	var n int
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM deals d
		LEFT JOIN pipelines p ON p.id=d.pipeline_id
		WHERE d.contact_id=$1::uuid
		  AND (coalesce(d.logistics_needed,false)=true
		    OR `+sqlDealOnLogistLinkStage("d")+`
		    OR coalesce(p.bitrix_id,'')=$2)
		LIMIT 1`, contactID, escortPipelineKey).Scan(&n)
	return err == nil
}

// ownsDealOrDept — владелец/отдел, без пула Недозвон 3.
func (a *App) ownsDealOrDept(ctx context.Context, s crmScope, id string) bool {
	if s.All || id == "" {
		return s.All
	}
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM deals WHERE id=$1::uuid AND (
				owner_id=$2::uuid OR owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
			)`, id, s.UserID, s.DeptID).Scan(&n)
		return err == nil
	}
	err := a.db.QueryRow(ctx, `SELECT 1 FROM deals WHERE id=$1::uuid AND owner_id=$2::uuid`, id, s.UserID).Scan(&n)
	return err == nil
}

func (a *App) ownsLeadOrDept(ctx context.Context, s crmScope, id string) bool {
	if s.All || id == "" {
		return s.All
	}
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM leads WHERE id=$1::uuid AND (
				owner_id=$2::uuid OR owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
			)`, id, s.UserID, s.DeptID).Scan(&n)
		return err == nil
	}
	err := a.db.QueryRow(ctx, `SELECT 1 FROM leads WHERE id=$1::uuid AND owner_id=$2::uuid`, id, s.UserID).Scan(&n)
	return err == nil
}

// canDialEntity — полный номер только своим/отделу или во время активного скорозвона.
func (a *App) canDialEntity(ctx context.Context, s crmScope, leadID, dealID, contactID string) bool {
	if s.All {
		return true
	}
	if dealID != "" {
		if a.ownsDealOrDept(ctx, s, dealID) {
			return true
		}
		return a.dealOpenForAutodialManager(ctx, dealID, s.UserID)
	}
	if leadID != "" {
		if a.ownsLeadOrDept(ctx, s, leadID) {
			return true
		}
		var linkedDeal string
		_ = a.db.QueryRow(ctx, `
			SELECT coalesce(d.id::text,'') FROM deals d
			WHERE d.lead_id=$1::uuid OR d.id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid)
			ORDER BY d.updated_at DESC NULLS LAST LIMIT 1`, leadID).Scan(&linkedDeal)
		if linkedDeal != "" && a.dealOpenForAutodialManager(ctx, linkedDeal, s.UserID) {
			return true
		}
		return false
	}
	if contactID != "" {
		return a.ownsDealOrDeptContact(ctx, s, contactID)
	}
	return false
}

func (a *App) ownsDealOrDeptContact(ctx context.Context, s crmScope, contactID string) bool {
	if s.All || contactID == "" {
		return s.All
	}
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM contacts c WHERE c.id=$1::uuid AND (
				c.owner_id=$2::uuid
				OR c.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
				OR EXISTS (
					SELECT 1 FROM deals d WHERE d.contact_id=c.id AND (
						d.owner_id=$2::uuid OR d.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
					)
				)
				OR EXISTS (
					SELECT 1 FROM leads l WHERE l.contact_id=c.id AND (
						l.owner_id=$2::uuid OR l.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
					)
				)
			)`, contactID, s.UserID, s.DeptID).Scan(&n)
		return err == nil
	}
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM contacts c WHERE c.id=$1::uuid AND (
			c.owner_id=$2::uuid
			OR EXISTS (SELECT 1 FROM deals d WHERE d.contact_id=c.id AND d.owner_id=$2::uuid)
			OR EXISTS (SELECT 1 FROM leads l WHERE l.contact_id=c.id AND l.owner_id=$2::uuid)
		)`, contactID, s.UserID).Scan(&n)
	return err == nil
}

func (a *App) companyVisible(ctx context.Context, s crmScope, id string) bool {
	if s.All {
		return true
	}
	var n int
	if s.DeptID != "" {
		err := a.db.QueryRow(ctx, `
			SELECT 1 FROM companies c WHERE c.id=$1::uuid AND (
				c.owner_id=$2::uuid
				OR c.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
				OR EXISTS (
					SELECT 1 FROM deals d WHERE d.company_id=c.id AND (
						d.owner_id=$2::uuid OR d.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)
					)
				)
			)`, id, s.UserID, s.DeptID).Scan(&n)
		return err == nil
	}
	err := a.db.QueryRow(ctx, `
		SELECT 1 FROM companies c WHERE c.id=$1::uuid AND (
			c.owner_id=$2::uuid
			OR EXISTS (SELECT 1 FROM deals d WHERE d.company_id=c.id AND d.owner_id=$2::uuid)
		)`, id, s.UserID).Scan(&n)
	return err == nil
}

func hideIfMissing(w http.ResponseWriter, ok bool) bool {
	if ok {
		return false
	}
	writeJSON(w, 404, map[string]string{"error": "not found"})
	return true
}

func ownerOrDept(alias string, s crmScope) string {
	if s.DeptID != "" {
		return alias + ".owner_id=$2::uuid OR " + alias + ".owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)"
	}
	return alias + ".owner_id=$2::uuid"
}

func contactDeptOr(s crmScope) string {
	if s.DeptID != "" {
		return " OR c.owner_id IN (SELECT id FROM users WHERE department_id=$3::uuid)"
	}
	return ""
}

func searchACLArgs(like string, s crmScope) []any {
	if s.DeptID != "" {
		return []any{like, s.UserID, s.DeptID}
	}
	return []any{like, s.UserID}
}

func parseListACL(r *http.Request, defLimit, maxLimit, staffMax int) (q string, limit, offset int) {
	q, limit, offset = parseList(r, defLimit, maxLimit)
	if !isAdmin(r) && limit > staffMax {
		limit = staffMax
	}
	return q, limit, offset
}

func jwtClaimVer(claims map[string]any) int {
	v, ok := claims["ver"]
	if !ok || v == nil {
		return 1
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return 1
	}
}

func jwtTTLHours(role string, adminHours int) int {
	if role == "admin" {
		if adminHours <= 0 {
			return 12
		}
		return adminHours
	}
	return 4
}
