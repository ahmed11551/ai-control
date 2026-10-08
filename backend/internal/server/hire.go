package server

import (
	"context"
	"fmt"
	"strings"
)

const (
	hirePipelineKey     = "local:hire"      // воронка «Адаптация»
	escortPipelineKey   = "local:escort"    // «Оформление и сопровождение»
	oldLeadsPipelineKey = "local:old-leads" // «Старые лиды» отделения Тёмы
	// hireNewApplicationStage — колонка «Новый лид / Новая заявка»; только с неё отзываем в банк.
	hireNewApplicationStage = hirePipelineKey + ":1"
)

type funnelStage struct {
	key, name, short, desc string
	won, lost              bool
}

// Воронка №1 — Адаптация (ТЗ §6.1)
var adaptationStages = []funnelStage{
	{"1", "Новый лид / Новая заявка", "Новый лид", "Все новые заявки из рекламы, почты, форм и других источников. Стартовая точка работы менеджера.", false, false},
	{"2", "Принято в работу", "В работе", "Менеджер открыл карточку и приступил к обработке кандидата.", false, false},
	{"3", "Недозвон 1", "Недозвон 1", "Первая неудачная попытка дозвона. Далее — повторные звонки и сообщения в мессенджер в рамках настраиваемого периода.", false, false},
	{"4", "Недозвон 2", "Недозвон 2", "Вторая неудачная попытка связи с кандидатом.", false, false},
	{"5", "Недозвон 3", "Недозвон 3", "Третья неудачная попытка. После неё по правилам компании может включаться скорозвон.", false, false},
	{"after_sk", "Принято после скорозвона", "После скор.", "Лид вернули первоначальному менеджеру после разговора в скорозвоне. Запись звонка сохранена.", false, false},
	{"call", "Назначен звонок", "Звонок", "С кандидатом согласованы дата и время следующего контакта.", false, false},
	{"6", "Переписка в чате", "Чат", "Основная коммуникация идёт через мессенджер (MAX, Telegram и др.).", false, false},
	{"docs", "Получены документы", "Документы", "Получены необходимые документы. Кандидат готов к переходу во вторую воронку оформления и сопровождения.", false, false},
	{"bilet", "Билет", "Билет", "Менеджер передал сделку логисту. Карточка остаётся у менеджера; логист видит её в «Заявка».", false, false},
	{"departure", "Выезд", "Выезд", "Кандидат выезжает / контроль выезда после оформления билета.", false, false},
	{"selection", "Пункт отбора", "Пункт отбора", "Кандидат на пункте отбора.", false, false},
	{"prescription", "Предписание", "Предписание", "Этап предписания по кандидату.", false, false},
	{"close", "Завершить сделку", "Завершить", "Сделка успешно завершена.", true, false},
	{"bad", "Некачественный лид", "Некачеств.", "Дальнейшая работа не требуется. Причину отказа фиксируют в карточке.", false, true},
	{"other", "Менеджер другой компании", "Чужая комп.", "В процессе общения выяснилось, что обращение связано с сотрудником или представителем другой компании.", false, true},
}

// Воронка №2 — Оформление и сопровождение (ТЗ §14 / §23 логистика)
var escortStages = []funnelStage{
	{"ticket_request", "Заявка на билет", "Заявка", "Нужна организация поездки: логист получил заявку и разрешённые документы.", false, false},
	{"ticket", "Куплен билет", "Билет", "Билет приобретён и загружен в CRM. Детали билета ведёт логист в пределах прав.", false, false},
	{"boarding", "Посадка", "Посадка", "Контроль посадки и выезда кандидата.", false, false},
	{"transit", "В пути", "В пути", "Кандидат выехал и находится в пути к пункту назначения.", false, false},
	{"arrived", "Прибытие", "Прибытие", "Кандидат прибыл в пункт назначения.", false, false},
	{"housing", "Размещение", "Размещение", "Размещение кандидата на месте.", false, false},
	{"vvk", "ВВК", "ВВК", "Этап военно-врачебной комиссии / связанный этап оформления.", false, false},
	{"region", "Изменение региона", "Регион", "Требуется изменить регион или маршрут дальнейшего сопровождения.", false, false},
	{"fail_select", "Не прошел дальнейший этап отбора", "Не прошёл", "Кандидат не прошёл следующий этап отбора. Процесс по этой ветке закрывается.", false, true},
	{"changed_mind", "Передумал продолжать", "Передумал", "Кандидат отказался продолжать процесс (в т.ч. «передумал ехать»). Причину фиксируют в карточке.", false, true},
	{"final_reject", "Отказ на финальном этапе", "Отказ", "Отказ на финальном этапе оформления или сопровождения.", false, true},
	{"success", "Процесс успешно завершен", "Успех", "Финальная успешная стадия: процесс по кандидату завершён положительно.", true, false},
}

func (a *App) ensurePipelineStageMeta(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS short_name TEXT NOT NULL DEFAULT ''`)
	_, _ = a.db.Exec(ctx, `ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT ''`)
}

func (a *App) ensureRecruitFunnels(ctx context.Context) {
	a.recruitFunnelsOnce.Do(func() {
		a.ensurePipelineStageMeta(ctx)
		adaptID := a.ensureNamedPipeline(ctx, hirePipelineKey, "Адаптация")
		escortID := a.ensureNamedPipeline(ctx, escortPipelineKey, "Оформление и сопровождение")
		if adaptID != "" {
			a.syncFunnelStages(ctx, adaptID, hirePipelineKey, adaptationStages)
		}
		if escortID != "" {
			a.syncFunnelStages(ctx, escortID, escortPipelineKey, escortStages)
		}
		oldID := a.ensureNamedPipeline(ctx, oldLeadsPipelineKey, "Спящие лиды")
		if oldID != "" {
			// Только новые стадии этой воронки. Чужие стадии и сделки не удаляем и не переносим.
			a.upsertFunnelStages(ctx, oldID, oldLeadsPipelineKey, adaptationStages)
		}
		a.migrateLegacyHireStages(ctx, adaptID, escortID)
		a.migrateEscortQueueToHireBilet(ctx)
	})
}

// ensureHirePipeline — совместимость со старыми вызовами.
func (a *App) ensureHirePipeline(ctx context.Context) {
	a.ensureRecruitFunnels(ctx)
}

func (a *App) ensureNamedPipeline(ctx context.Context, key, name string) string {
	var id string
	err := a.db.QueryRow(ctx, `SELECT id::text FROM pipelines WHERE bitrix_id=$1`, key).Scan(&id)
	if err != nil {
		err = a.db.QueryRow(ctx, `
			INSERT INTO pipelines (name, kind, bitrix_id) VALUES ($1,'deals',$2)
			RETURNING id::text`, name, key).Scan(&id)
		if err != nil {
			_ = a.db.QueryRow(ctx, `SELECT id::text FROM pipelines WHERE bitrix_id=$1`, key).Scan(&id)
		}
		return id
	}
	_, _ = a.db.Exec(ctx, `UPDATE pipelines SET name=$2 WHERE id=$1::uuid`, id, name)
	return id
}

func (a *App) syncFunnelStages(ctx context.Context, pipeID, pipeKey string, stages []funnelStage) {
	a.upsertFunnelStages(ctx, pipeID, pipeKey, stages)
	a.dropUnusedFunnelStages(ctx, pipeID, pipeKey, stages)
}

// upsertFunnelStages добавляет или обновляет стадии воронки и не удаляет строки.
func (a *App) upsertFunnelStages(ctx context.Context, pipeID, pipeKey string, stages []funnelStage) {
	for i, st := range stages {
		bid := pipeKey + ":" + st.key
		var sid string
		err := a.db.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, bid).Scan(&sid)
		if err != nil {
			_, _ = a.db.Exec(ctx, `
				INSERT INTO pipeline_stages (pipeline_id, name, short_name, description, sort_order, is_won, is_lost, probability, bitrix_id)
				VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9)`,
				pipeID, st.name, st.short, st.desc, i+1, st.won, st.lost, hireProb(st.won, st.lost, i), bid)
			continue
		}
		_, _ = a.db.Exec(ctx, `
			UPDATE pipeline_stages
			SET name=$2, short_name=$3, description=$4, sort_order=$5, is_won=$6, is_lost=$7,
			    probability=$8, pipeline_id=$9::uuid
			WHERE id=$1::uuid`,
			sid, st.name, st.short, st.desc, i+1, st.won, st.lost, hireProb(st.won, st.lost, i), pipeID)
	}
}

func (a *App) dropUnusedFunnelStages(ctx context.Context, pipeID, pipeKey string, stages []funnelStage) {
	keep := make([]string, 0, len(stages))
	for _, st := range stages {
		keep = append(keep, pipeKey+":"+st.key)
	}
	// убрать устаревшие стадии этой воронки без сделок
	if len(keep) == 0 {
		return
	}
	args := []any{pipeID}
	ph := make([]string, 0, len(keep))
	for i, bid := range keep {
		args = append(args, bid)
		ph = append(ph, fmt.Sprintf("$%d", i+2))
	}
	_, _ = a.db.Exec(ctx, fmt.Sprintf(`
		DELETE FROM pipeline_stages s
		WHERE s.pipeline_id=$1::uuid
		  AND s.bitrix_id IS NOT NULL
		  AND s.bitrix_id NOT IN (%s)
		  AND NOT EXISTS (SELECT 1 FROM deals d WHERE d.stage_id=s.id)`, strings.Join(ph, ",")), args...)
}

// Старые стадии найма 7–10 переносим во вторую воронку.
func (a *App) migrateLegacyHireStages(ctx context.Context, adaptID, escortID string) {
	if adaptID == "" || escortID == "" {
		return
	}
	moves := []struct{ fromKey, toKey string }{
		{"7", "ticket"},
		{"8", "transit"},
		{"9", "arrived"},
		{"10", "success"},
	}
	for _, m := range moves {
		fromBid := hirePipelineKey + ":" + m.fromKey
		toBid := escortPipelineKey + ":" + m.toKey
		var fromID, toID string
		_ = a.db.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, fromBid).Scan(&fromID)
		_ = a.db.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, toBid).Scan(&toID)
		if fromID == "" || toID == "" {
			continue
		}
		_, _ = a.db.Exec(ctx, `
			UPDATE deals SET pipeline_id=$1::uuid, stage_id=$2::uuid
			WHERE stage_id=$3::uuid`, escortID, toID, fromID)
		_, _ = a.db.Exec(ctx, `
			DELETE FROM pipeline_stages
			WHERE id=$1::uuid AND NOT EXISTS (SELECT 1 FROM deals WHERE stage_id=$1::uuid)`, fromID)
	}
	// старый «Отказ» → «Некачественный лид»
	var oldLost, badID string
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, hirePipelineKey+":lost").Scan(&oldLost)
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, hirePipelineKey+":bad").Scan(&badID)
	if oldLost != "" && badID != "" && oldLost != badID {
		_, _ = a.db.Exec(ctx, `UPDATE deals SET stage_id=$1::uuid WHERE stage_id=$2::uuid`, badID, oldLost)
		_, _ = a.db.Exec(ctx, `
			DELETE FROM pipeline_stages
			WHERE id=$1::uuid AND NOT EXISTS (SELECT 1 FROM deals WHERE stage_id=$1::uuid)`, oldLost)
	}
}

// migrateEscortQueueToHireBilet — заявки в escort «Заявка» возвращаем менеджеру на «Билет»
// (логист по-прежнему видит их через logistics_needed).
// Не вызывать escortStageID/hireStageID отсюда — они снова зовут ensureRecruitFunnels (deadlock Once).
func (a *App) migrateEscortQueueToHireBilet(ctx context.Context) {
	var hirePipe, hireBilet, escortReq string
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM pipelines WHERE bitrix_id=$1`, hirePipelineKey).Scan(&hirePipe)
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, hirePipelineKey+":bilet").Scan(&hireBilet)
	_ = a.db.QueryRow(ctx, `SELECT id::text FROM pipeline_stages WHERE bitrix_id=$1`, escortPipelineKey+":ticket_request").Scan(&escortReq)
	if hirePipe == "" || hireBilet == "" || escortReq == "" {
		return
	}
	_, _ = a.db.Exec(ctx, `
		UPDATE deals
		SET pipeline_id=$1::uuid,
		    stage_id=$2::uuid,
		    logistics_needed=true,
		    logistics_needed_at=COALESCE(logistics_needed_at, now())
		WHERE stage_id=$3::uuid`, hirePipe, hireBilet, escortReq)
}

func hireProb(won, lost bool, i int) int {
	if won {
		return 100
	}
	if lost {
		return 0
	}
	return 10 + i*8
}

func (a *App) hireStart(ctx context.Context) (pipe, stage string) {
	a.ensureRecruitFunnels(ctx)
	_ = a.db.QueryRow(ctx, `
		SELECT p.id::text, s.id::text
		FROM pipelines p
		JOIN pipeline_stages s ON s.pipeline_id=p.id
		WHERE p.bitrix_id=$1 AND s.is_won=false AND s.is_lost=false
		ORDER BY s.sort_order LIMIT 1`, hirePipelineKey).Scan(&pipe, &stage)
	if pipe != "" {
		return pipe, stage
	}
	_ = a.db.QueryRow(ctx, `
		SELECT p.id::text, s.id::text FROM pipelines p
		JOIN pipeline_stages s ON s.pipeline_id=p.id
		ORDER BY s.sort_order LIMIT 1`).Scan(&pipe, &stage)
	return pipe, stage
}

// attachCallCard только привязывает звонок к уже существующей карточке.
// Новые лиды/сделки/контакты из телефонии не создаём.
func (a *App) attachCallCard(ctx context.Context, callID string, ev callEvent) string {
	var ent, eid string
	_ = a.db.QueryRow(ctx, `
		SELECT coalesce(entity_type,''), coalesce(entity_id::text,'')
		FROM calls WHERE id=$1::uuid`, callID).Scan(&ent, &eid)
	if eid != "" {
		if ent == "deal" {
			return eid
		}
		return ""
	}
	if strings.EqualFold(ev.Entity, "deal") && strings.TrimSpace(ev.EntityID) != "" {
		_, _ = a.db.Exec(ctx, `
			UPDATE calls SET entity_type='deal', entity_id=$1::uuid WHERE id=$2::uuid`,
			ev.EntityID, callID)
		return ev.EntityID
	}
	phone := ev.From
	if strings.EqualFold(ev.Direction, "outbound") {
		phone = ev.To
	}
	digits := normalizePhone(phone)
	if len(digits) < 10 {
		return ""
	}
	var dealID string
	_ = a.db.QueryRow(ctx, `
		SELECT d.id::text FROM deals d
		JOIN contacts c ON c.id=d.contact_id
		JOIN pipeline_stages s ON s.id=d.stage_id
		WHERE s.is_lost=false
		  AND regexp_replace(coalesce(c.phone,''),'[^0-9]','','g') LIKE '%'||$1
		ORDER BY d.created_at DESC LIMIT 1`, digits).Scan(&dealID)
	if dealID == "" {
		return ""
	}
	_, _ = a.db.Exec(ctx, `UPDATE calls SET entity_type='deal', entity_id=$1::uuid WHERE id=$2::uuid`, dealID, callID)
	return dealID
}
