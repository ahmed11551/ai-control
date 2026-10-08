-- ForgeCRM schema. Runs once via docker-entrypoint-initdb.d
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT UNIQUE NOT NULL,
    permissions JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    full_name TEXT NOT NULL,
    role_id UUID REFERENCES roles(id),
    phone_extension TEXT UNIQUE,
    can_call BOOLEAN NOT NULL DEFAULT true,
    allowed_call_dest TEXT NOT NULL DEFAULT 'any',
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
    group_id UUID REFERENCES groups(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE companies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    inn TEXT,
    kpp TEXT,
    website TEXT,
    phone TEXT,
    email TEXT,
    address TEXT,
    owner_id UUID REFERENCES users(id),
    search_tsv TSVECTOR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE contacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID REFERENCES companies(id) ON DELETE SET NULL,
    full_name TEXT NOT NULL,
    phone TEXT,
    email TEXT,
    position TEXT,
    source TEXT NOT NULL DEFAULT 'manual',
    vk_user_id TEXT,
    owner_id UUID REFERENCES users(id),
    search_tsv TSVECTOR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE pipelines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'deals'
);

CREATE TABLE pipeline_stages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id UUID REFERENCES pipelines(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    sort_order INT NOT NULL,
    is_won BOOLEAN NOT NULL DEFAULT false,
    is_lost BOOLEAN NOT NULL DEFAULT false,
    probability INT NOT NULL DEFAULT 0
);

CREATE TABLE leads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
    company_id UUID REFERENCES companies(id) ON DELETE SET NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    status TEXT NOT NULL DEFAULT 'new',
    amount NUMERIC(14,2),
    currency TEXT NOT NULL DEFAULT 'RUB',
    vk_ads_campaign_id TEXT,
    vk_click_id TEXT,
    vk_lead_id TEXT,
    owner_id UUID REFERENCES users(id),
    converted_deal_id UUID,
    search_tsv TSVECTOR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE deals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
    company_id UUID REFERENCES companies(id) ON DELETE SET NULL,
    lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
    pipeline_id UUID REFERENCES pipelines(id),
    stage_id UUID REFERENCES pipeline_stages(id),
    amount NUMERIC(14,2) NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'RUB',
    owner_id UUID REFERENCES users(id),
    vk_ads_campaign_id TEXT,
    vk_click_id TEXT,
    closed_at TIMESTAMPTZ,
    search_tsv TSVECTOR,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE leads
    ADD CONSTRAINT leads_converted_deal_fk
    FOREIGN KEY (converted_deal_id) REFERENCES deals(id) ON DELETE SET NULL;

CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    owner_id UUID REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    description TEXT,
    entity_type TEXT,
    entity_id UUID,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    assignee_id UUID REFERENCES users(id),
    creator_id UUID REFERENCES users(id),
    due_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'open',
    priority TEXT NOT NULL DEFAULT 'normal',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE time_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID REFERENCES tasks(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id),
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    seconds INT,
    note TEXT
);

CREATE TABLE calls (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    asterisk_uniqueid TEXT,
    direction TEXT NOT NULL,
    from_number TEXT,
    to_number TEXT,
    user_id UUID REFERENCES users(id),
    entity_type TEXT,
    entity_id UUID,
    duration_sec INT NOT NULL DEFAULT 0,
    disposition TEXT,
    recording_key TEXT,
    exolve_call_id TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ,
    PRIMARY KEY (id, started_at)
);

CREATE TABLE vk_ads_campaigns (
    id TEXT PRIMARY KEY,
    name TEXT,
    budget NUMERIC(14,2),
    status TEXT,
    spend NUMERIC(14,2) DEFAULT 0,
    synced_at TIMESTAMPTZ
);

CREATE TABLE vk_ads_offline_conversions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    vk_ads_campaign_id TEXT,
    vk_click_id TEXT,
    conversion_value NUMERIC(14,2),
    event_name TEXT NOT NULL DEFAULT 'purchase',
    status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL,
    number TEXT,
    title TEXT NOT NULL,
    entity_type TEXT,
    entity_id UUID,
    amount NUMERIC(14,2),
    currency TEXT DEFAULT 'RUB',
    status TEXT NOT NULL DEFAULT 'draft',
    file_key TEXT,
    sign_status TEXT DEFAULT 'unsigned',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE calendar_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    owner_id UUID REFERENCES users(id),
    entity_type TEXT,
    entity_id UUID,
    meet_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    channel TEXT NOT NULL DEFAULT 'in_app',
    title TEXT NOT NULL,
    body TEXT,
    is_read BOOLEAN NOT NULL DEFAULT false,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE automations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    trigger_type TEXT NOT NULL,
    trigger_filter JSONB NOT NULL DEFAULT '{}'::jsonb,
    actions JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE activity_log (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    actor_id UUID REFERENCES users(id),
    action TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
);

CREATE TABLE chat_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel TEXT NOT NULL DEFAULT 'general',
    user_id UUID REFERENCES users(id),
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key TEXT NOT NULL,
    filename TEXT NOT NULL,
    mime TEXT,
    size_bytes BIGINT,
    entity_type TEXT,
    entity_id UUID,
    uploaded_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE inbound_webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source TEXT NOT NULL,
    payload JSONB NOT NULL,
    processed BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_contacts_phone ON contacts(phone);
CREATE INDEX idx_deals_stage ON deals(stage_id);
CREATE INDEX idx_deals_owner ON deals(owner_id);
CREATE INDEX idx_leads_status ON leads(status);
CREATE INDEX idx_tasks_assignee ON tasks(assignee_id, status);
CREATE INDEX idx_calls_started ON calls(started_at DESC);
CREATE INDEX idx_activity_entity ON activity_log(entity_type, entity_id, created_at DESC);
CREATE INDEX idx_notifications_user ON notifications(user_id, is_read, created_at DESC);
CREATE INDEX idx_vk_conv_status ON vk_ads_offline_conversions(status);


-- Timescale requires unique constraints to include partition key.
-- asterisk_uniqueid unique is enough via btree on non-hypertable; calls uniqueid index:
CREATE UNIQUE INDEX IF NOT EXISTS calls_uniqueid_started ON calls (asterisk_uniqueid, started_at) WHERE asterisk_uniqueid IS NOT NULL;

CREATE OR REPLACE FUNCTION forge_tsv_contacts() RETURNS trigger AS $$
BEGIN
  NEW.search_tsv := to_tsvector('simple', coalesce(NEW.full_name,'') || ' ' || coalesce(NEW.phone,'') || ' ' || coalesce(NEW.email,''));
  RETURN NEW;
END; $$ LANGUAGE plpgsql;
CREATE TRIGGER t_contacts_tsv BEFORE INSERT OR UPDATE ON contacts FOR EACH ROW EXECUTE FUNCTION forge_tsv_contacts();

CREATE OR REPLACE FUNCTION forge_tsv_companies() RETURNS trigger AS $$
BEGIN
  NEW.search_tsv := to_tsvector('simple', coalesce(NEW.name,'') || ' ' || coalesce(NEW.inn,'') || ' ' || coalesce(NEW.phone,''));
  RETURN NEW;
END; $$ LANGUAGE plpgsql;
CREATE TRIGGER t_companies_tsv BEFORE INSERT OR UPDATE ON companies FOR EACH ROW EXECUTE FUNCTION forge_tsv_companies();

CREATE OR REPLACE FUNCTION forge_tsv_deals() RETURNS trigger AS $$
BEGIN
  NEW.search_tsv := to_tsvector('simple', coalesce(NEW.title,''));
  NEW.updated_at := now();
  RETURN NEW;
END; $$ LANGUAGE plpgsql;
CREATE TRIGGER t_deals_tsv BEFORE INSERT OR UPDATE ON deals FOR EACH ROW EXECUTE FUNCTION forge_tsv_deals();

CREATE OR REPLACE FUNCTION forge_tsv_leads() RETURNS trigger AS $$
BEGIN
  NEW.search_tsv := to_tsvector('simple', coalesce(NEW.title,''));
  NEW.updated_at := now();
  RETURN NEW;
END; $$ LANGUAGE plpgsql;
CREATE TRIGGER t_leads_tsv BEFORE INSERT OR UPDATE ON leads FOR EACH ROW EXECUTE FUNCTION forge_tsv_leads();

INSERT INTO roles (name, permissions) VALUES
('admin', '{"*": true}'::jsonb),
('rop', '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"staff.create":true}'::jsonb),
('manager', '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"phones.hide":true}'::jsonb),
('logist', '{"logistics":true}'::jsonb),
('sales', '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"phones.hide":true}'::jsonb),
('support', '{"tasks":true,"contacts":true,"calls.make":true}'::jsonb),
('marketing', '{"vk":true,"leads":true,"reports":true}'::jsonb);

INSERT INTO pipelines (id, name, kind) VALUES
('00000000-0000-0000-0000-000000000001', 'Продажи', 'deals');

INSERT INTO pipeline_stages (pipeline_id, name, sort_order, is_won, is_lost, probability) VALUES
('00000000-0000-0000-0000-000000000001', 'Новая', 10, false, false, 10),
('00000000-0000-0000-0000-000000000001', 'Квалификация', 20, false, false, 30),
('00000000-0000-0000-0000-000000000001', 'Предложение', 30, false, false, 50),
('00000000-0000-0000-0000-000000000001', 'Переговоры', 40, false, false, 70),
('00000000-0000-0000-0000-000000000001', 'Счёт', 50, false, false, 85),
('00000000-0000-0000-0000-000000000001', 'Выиграна', 60, true, false, 100),
('00000000-0000-0000-0000-000000000001', 'Проиграна', 70, false, true, 0);

INSERT INTO automations (name, trigger_type, trigger_filter, actions) VALUES
('VK лид → задача менеджеру', 'lead.created', '{"source":"vk_ads"}',
 '[{"type":"create_task","title":"Обработать лид VK Ads","priority":"high"}]'),
('Сделка выиграна → офлайн-конверсия VK', 'deal.won', '{}',
 '[{"type":"vk_offline_conversion","event":"purchase"}]');
