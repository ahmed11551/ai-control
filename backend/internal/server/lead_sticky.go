package server

import (
	"context"
	"fmt"
	"strings"
)

// Липкость лида: как только менеджер-владелец реально поработал с лидом
// (открыл карточку, позвонил, написал коммент, сменил стадию, отправил в логист…),
// лид закреплён за ним. Ни банк, ни РОП-банк, ни скорозвон, ни PATCH owner
// не могут отдать его другому менеджеру.

// ensureStickyIndexes — индексы под EXISTS-подзапросы липкости (без них отзыв/выдача на проде ~2 с).
func (a *App) ensureStickyIndexes(ctx context.Context) {
	if a.db == nil {
		return
	}
	a.stickyIndexOnce.Do(func() {
		for _, q := range []string{
			`CREATE INDEX IF NOT EXISTS idx_calls_entity ON calls (entity_type, entity_id)`,
			`CREATE INDEX IF NOT EXISTS idx_calls_user_entity ON calls (user_id, entity_type, entity_id)`,
			`CREATE INDEX IF NOT EXISTS idx_activity_actor_entity ON activity_log (actor_id, entity_type, entity_id)`,
			`CREATE INDEX IF NOT EXISTS idx_leads_owner ON leads (owner_id)`,
			`CREATE INDEX IF NOT EXISTS idx_leads_converted_deal ON leads (converted_deal_id) WHERE converted_deal_id IS NOT NULL`,
		} {
			_, _ = a.db.Exec(ctx, q)
		}
	})
}

// stickyIgnoredActions — служебные записи, которые не считаются «работой» менеджера.
const stickyIgnoredActions = `('distributed','distributed_manager','recalled_to_bank','recalled_to_rop_bank','owner_changed','created','converted')`

// sqlLeadLinkedDealIDs — подзапрос id сделок лида.
func sqlLeadLinkedDealIDs(alias string) string {
	return `SELECT d.id FROM deals d WHERE d.lead_id=` + alias + `.id OR d.id=` + alias + `.converted_deal_id`
}

// sqlLeadTouchedWhere — четыре EXISTS «от карточки» (индексы (entity_type, entity_id)),
// без OR внутри WHERE — иначе планировщик идёт от пользователей и перебирает всех менеджеров на каждый лид.
// whoActivity/whoCalls — условие на актора: получают выражение колонки (ta.actor_id / tc.user_id).
func sqlLeadTouchedWhere(alias string, who func(col string) string) string {
	if alias == "" {
		alias = "leads"
	}
	dealOf := `(d.lead_id=` + alias + `.id OR d.id=` + alias + `.converted_deal_id)`
	return `(
		EXISTS (
			SELECT 1 FROM activity_log ta
			WHERE ta.entity_type='lead' AND ta.entity_id=` + alias + `.id
			  AND ta.action NOT IN ` + stickyIgnoredActions + `
			  AND ` + who("ta.actor_id") + `
		)
		OR EXISTS (
			SELECT 1 FROM deals d
			JOIN activity_log ta ON ta.entity_type='deal' AND ta.entity_id=d.id
			WHERE ` + dealOf + `
			  AND ta.action NOT IN ` + stickyIgnoredActions + `
			  AND ` + who("ta.actor_id") + `
		)
		OR EXISTS (
			SELECT 1 FROM calls tc
			WHERE tc.entity_type='lead' AND tc.entity_id=` + alias + `.id
			  AND ` + who("tc.user_id") + `
		)
		OR EXISTS (
			SELECT 1 FROM deals d
			JOIN calls tc ON tc.entity_type='deal' AND tc.entity_id=d.id
			WHERE ` + dealOf + `
			  AND ` + who("tc.user_id") + `
		)
	)`
}

// sqlLeadTouchedBy — пользователь userExpr реально работал с лидом (activity или звонок).
func sqlLeadTouchedBy(alias, userExpr string) string {
	return sqlLeadTouchedWhere(alias, func(col string) string {
		return col + ` = ` + userExpr
	})
}

// sqlLeadStickyToOwner — текущий владелец-менеджер (не РОП отдела) уже работал с лидом.
func sqlLeadStickyToOwner(alias string) string {
	if alias == "" {
		alias = "leads"
	}
	return `(
		` + alias + `.owner_id IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM departments sdp WHERE sdp.head_user_id=` + alias + `.owner_id)
		AND ` + sqlLeadTouchedBy(alias, alias+`.owner_id`) + `
	)`
}

// sqlLeadTouchedByManagerOtherThan — с лидом работал какой-то менеджер (не РОП), кроме exceptParam ($N::uuid или ”).
func sqlLeadTouchedByManagerOtherThan(alias, exceptParam string) string {
	if alias == "" {
		alias = "leads"
	}
	except := ""
	if strings.TrimSpace(exceptParam) != "" {
		except = ` AND tu.id <> ` + exceptParam
	}
	// Актор — менеджер (не РОП отдела), кроме exceptParam.
	who := func(col string) string {
		return `EXISTS (
			SELECT 1 FROM users tu
			JOIN roles tr ON tr.id=tu.role_id
			WHERE tu.id=` + col + `
			  AND lower(coalesce(tr.name,'')) IN ('manager','sales')
			  AND NOT EXISTS (SELECT 1 FROM departments tdp WHERE tdp.head_user_id=tu.id)` + except + `
		)`
	}
	return sqlLeadTouchedWhere(alias, who)
}

// leadStickyInfo — владелец лида и признак «закреплён».
func (a *App) leadStickyInfo(ctx context.Context, leadID string) (owner string, sticky bool) {
	leadID = strings.TrimSpace(leadID)
	if leadID == "" || a.db == nil {
		return "", false
	}
	a.ensureStickyIndexes(ctx)
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(l.owner_id::text,''), `+sqlLeadStickyToOwner("l")+`
		FROM leads l WHERE l.id=$1::uuid`, leadID).Scan(&owner, &sticky)
	owner = strings.TrimSpace(owner)
	if owner == "<nil>" {
		owner = ""
	}
	return owner, sticky
}

// leadLastTouchingManager — последний менеджер (не РОП), который реально работал с лидом.
func (a *App) leadLastTouchingManager(ctx context.Context, leadID string) string {
	leadID = strings.TrimSpace(leadID)
	if leadID == "" || a.db == nil {
		return ""
	}
	var id string
	_ = a.db.QueryRow(ctx, `
		WITH ev AS (
			SELECT ta.actor_id AS uid, ta.created_at AS at
			FROM activity_log ta, leads l
			WHERE l.id=$1::uuid
			  AND ta.actor_id IS NOT NULL
			  AND ta.action NOT IN `+stickyIgnoredActions+`
			  AND ((ta.entity_type='lead' AND ta.entity_id=l.id)
			    OR (ta.entity_type='deal' AND ta.entity_id IN (`+sqlLeadLinkedDealIDs("l")+`)))
			UNION ALL
			SELECT tc.user_id, tc.started_at
			FROM calls tc, leads l
			WHERE l.id=$1::uuid
			  AND tc.user_id IS NOT NULL
			  AND ((tc.entity_type='lead' AND tc.entity_id=l.id)
			    OR (tc.entity_type='deal' AND tc.entity_id IN (`+sqlLeadLinkedDealIDs("l")+`)))
		)
		SELECT ev.uid::text
		FROM ev
		JOIN users u ON u.id=ev.uid AND u.is_active
		JOIN roles r ON r.id=u.role_id
		WHERE lower(coalesce(r.name,'')) IN ('manager','sales')
		  AND NOT EXISTS (SELECT 1 FROM departments dp WHERE dp.head_user_id=u.id)
		ORDER BY ev.at DESC
		LIMIT 1`, leadID).Scan(&id)
	return strings.TrimSpace(id)
}

// dealStickyInfo — то же для сделки (через связанный лид; без лида — по самой сделке).
func (a *App) dealStickyInfo(ctx context.Context, dealID string) (owner string, sticky bool) {
	dealID = strings.TrimSpace(dealID)
	if dealID == "" || a.db == nil {
		return "", false
	}
	leadID := a.dealLinkedLeadID(ctx, dealID)
	if leadID != "" {
		return a.leadStickyInfo(ctx, leadID)
	}
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(d.owner_id::text,''),
		       (
		         d.owner_id IS NOT NULL
		         AND NOT EXISTS (SELECT 1 FROM departments sdp WHERE sdp.head_user_id=d.owner_id)
		         AND (
		           EXISTS (SELECT 1 FROM activity_log ta WHERE ta.actor_id=d.owner_id AND ta.entity_type='deal' AND ta.entity_id=d.id
		                   AND ta.action NOT IN `+stickyIgnoredActions+`)
		           OR EXISTS (SELECT 1 FROM calls tc WHERE tc.user_id=d.owner_id AND tc.entity_type='deal' AND tc.entity_id=d.id)
		         )
		       )
		FROM deals d WHERE d.id=$1::uuid`, dealID).Scan(&owner, &sticky)
	owner = strings.TrimSpace(owner)
	if owner == "<nil>" {
		owner = ""
	}
	return owner, sticky
}

// activeLeadByPhone — живой лид (не отказ, сделка не проиграна) с таким телефоном за 60 дней.
func (a *App) activeLeadByPhone(ctx context.Context, phone string) string {
	norm := normalizePhone(phone)
	if len(norm) < 10 || a.db == nil {
		return ""
	}
	var id string
	_ = a.db.QueryRow(ctx, `
		SELECT l.id::text
		FROM leads l
		JOIN contacts c ON c.id=l.contact_id
		WHERE right(regexp_replace(coalesce(c.phone,''), '[^0-9]', '', 'g'), 10) = right($1, 10)
		  AND coalesce(l.status,'') <> 'rejected'
		  AND l.created_at > now() - interval '60 days'
		  AND NOT EXISTS (
		    SELECT 1 FROM deals d
		    JOIN pipeline_stages s ON s.id=d.stage_id
		    WHERE (d.lead_id=l.id OR d.id=l.converted_deal_id) AND s.is_lost
		  )
		ORDER BY l.created_at DESC
		LIMIT 1`, norm).Scan(&id)
	return strings.TrimSpace(id)
}

func (a *App) contactPhone(ctx context.Context, contactID string) string {
	contactID = strings.TrimSpace(contactID)
	if contactID == "" || a.db == nil {
		return ""
	}
	var phone string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(phone,'') FROM contacts WHERE id=$1::uuid`, contactID).Scan(&phone)
	return phone
}

// broadcastDealUpdatedForLead — пнуть канбан владельца, что по лиду есть новая активность.
func (a *App) broadcastDealUpdatedForLead(ctx context.Context, leadID string) {
	rows := a.queryMaps(ctx, `SELECT id::text AS id FROM deals WHERE lead_id=$1::uuid OR id=(SELECT converted_deal_id FROM leads WHERE id=$1::uuid)`, leadID)
	for _, row := range rows {
		if id := strings.TrimSpace(fmt.Sprint(row["id"])); id != "" && id != "<nil>" {
			a.broadcastDealUpdated(ctx, id, nil)
		}
	}
}

// setLeadOwnerGuarded — атомарная смена владельца лида и всех его сделок ОДНИМ statement-ом
// с проверкой липкости внутри UPDATE (закрывает гонку «проверили → менеджер тронул → перезаписали»).
//
//	expectOwner: "" — любой текущий владелец; иначе только если сейчас owner_id=expectOwner.
//	force:       снять закрепление (только admin с force=true).
//
// Возвращает true, если владелец изменён.
func (a *App) setLeadOwnerGuarded(ctx context.Context, leadID, newOwner, expectOwner string, force bool) bool {
	leadID = strings.TrimSpace(leadID)
	newOwner = strings.TrimSpace(newOwner)
	expectOwner = strings.TrimSpace(expectOwner)
	if leadID == "" || a.db == nil {
		return false
	}
	var curOwner string
	_ = a.db.QueryRow(ctx, `SELECT coalesce(owner_id::text,'') FROM leads WHERE id=$1::uuid`, leadID).Scan(&curOwner)
	curOwner = strings.TrimSpace(curOwner)
	if curOwner == "<nil>" {
		curOwner = ""
	}
	// МСК Башня: наружу (в банк / другой отдел) — никогда, даже с force.
	if a.transferLeavesLockedDept(ctx, curOwner, newOwner) {
		return false
	}
	a.ensureStickyIndexes(ctx)
	var n int
	err := a.db.QueryRow(ctx, `
		WITH upd AS (
			UPDATE leads l SET owner_id=nullif($2,'')::uuid, updated_at=now()
			WHERE l.id=$1::uuid
			  AND ($3='' OR l.owner_id=nullif($3,'')::uuid)
			  AND (
			    $4::bool
			    OR l.owner_id IS NOT DISTINCT FROM nullif($2,'')::uuid
			    OR NOT (`+sqlLeadStickyToOwner("l")+`)
			  )
			RETURNING l.id, l.converted_deal_id
		), dl AS (
			UPDATE deals d SET owner_id=nullif($2,'')::uuid, updated_at=now()
			FROM upd
			WHERE d.lead_id=upd.id OR d.id=upd.converted_deal_id
			RETURNING d.id
		)
		SELECT count(*) FROM upd`, leadID, newOwner, expectOwner, force).Scan(&n)
	if err != nil || n == 0 {
		return false
	}
	a.broadcastDealUpdatedForLead(ctx, leadID)
	return true
}

// leadStickyBlocksTransfer — можно ли передать лид newOwner. Возвращает ошибку для ответа клиенту.
func leadStickyBlocksTransfer(currentOwner string, sticky bool, newOwner string) error {
	currentOwner = strings.TrimSpace(currentOwner)
	newOwner = strings.TrimSpace(newOwner)
	if !sticky || currentOwner == "" {
		return nil
	}
	if newOwner == currentOwner {
		return nil
	}
	return fmt.Errorf("лид закреплён за менеджером, который уже работал с ним — передача другому запрещена")
}
