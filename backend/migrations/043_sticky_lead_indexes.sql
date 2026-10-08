-- Индексы под проверку «липкости» лида (EXISTS по calls/activity_log на каждого кандидата).
-- Дублируются в рантайме (ensureStickyIndexes) — здесь для истории схемы.
CREATE INDEX IF NOT EXISTS idx_calls_entity ON calls (entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_calls_user_entity ON calls (user_id, entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_activity_actor_entity ON activity_log (actor_id, entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_leads_owner ON leads (owner_id);
CREATE INDEX IF NOT EXISTS idx_leads_converted_deal ON leads (converted_deal_id) WHERE converted_deal_id IS NOT NULL;
