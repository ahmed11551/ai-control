CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    parent_id UUID REFERENCES departments(id) ON DELETE SET NULL,
    bitrix_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS departments_bitrix_id_uidx ON departments (bitrix_id) WHERE bitrix_id IS NOT NULL;

ALTER TABLE departments ADD COLUMN IF NOT EXISTS access_enabled BOOLEAN NOT NULL DEFAULT true;

ALTER TABLE users ADD COLUMN IF NOT EXISTS department_id UUID REFERENCES departments(id) ON DELETE SET NULL;

ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS short_name TEXT NOT NULL DEFAULT '';
ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS pipelines_bitrix_id_uidx ON pipelines (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS pipeline_stages_bitrix_id_uidx ON pipeline_stages (bitrix_id) WHERE bitrix_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS bitrix_import_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    portal TEXT,
    status TEXT NOT NULL DEFAULT 'idle',
    phase TEXT,
    counts JSONB NOT NULL DEFAULT '{}'::jsonb,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE bitrix_sync_state ADD COLUMN IF NOT EXISTS imported INT NOT NULL DEFAULT 0;
ALTER TABLE bitrix_sync_state ADD COLUMN IF NOT EXISTS total INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_contacts_created ON contacts (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_contacts_phone_trgm ON contacts USING gin (phone gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_leads_created ON leads (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_leads_title_trgm ON leads USING gin (title gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_deals_pipeline_created ON deals (pipeline_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deals_stage_created ON deals (stage_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deals_title_trgm ON deals USING gin (title gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_title_trgm ON tasks USING gin (title gin_trgm_ops);

CREATE TABLE IF NOT EXISTS user_fields (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type TEXT NOT NULL,
    code TEXT NOT NULL,
    label TEXT NOT NULL,
    field_type TEXT NOT NULL DEFAULT 'string',
    field_group TEXT NOT NULL DEFAULT 'прочее',
    multiple BOOLEAN NOT NULL DEFAULT false,
    active BOOLEAN NOT NULL DEFAULT true,
    sort INT NOT NULL DEFAULT 100,
    options JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entity_type, code)
);

CREATE TABLE IF NOT EXISTS entity_field_values (
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    field_id UUID NOT NULL REFERENCES user_fields(id) ON DELETE CASCADE,
    value_text TEXT NOT NULL DEFAULT '',
    value_json JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_type, entity_id, field_id)
);
CREATE INDEX IF NOT EXISTS idx_entity_field_values_entity ON entity_field_values (entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_entity_field_values_field ON entity_field_values (field_id);

ALTER TABLE projects ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS projects_bitrix_id_uidx ON projects (bitrix_id) WHERE bitrix_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS task_members (
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    PRIMARY KEY (task_id, user_id, role)
);
CREATE INDEX IF NOT EXISTS idx_task_members_user_role ON task_members (user_id, role);

INSERT INTO task_members (task_id, user_id, role)
SELECT id, assignee_id, 'assignee' FROM tasks WHERE assignee_id IS NOT NULL
ON CONFLICT DO NOTHING;
INSERT INTO task_members (task_id, user_id, role)
SELECT id, creator_id, 'creator' FROM tasks WHERE creator_id IS NOT NULL
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS task_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    author_label TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    bitrix_id TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_task_comments_task ON task_comments (task_id, created_at);

ALTER TABLE users ADD COLUMN IF NOT EXISTS work_phone TEXT;
ALTER TABLE calls ADD COLUMN IF NOT EXISTS provider TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS calls_uniqueid_started ON calls (asterisk_uniqueid, started_at) WHERE asterisk_uniqueid IS NOT NULL;

CREATE TABLE IF NOT EXISTS inbox_threads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel TEXT NOT NULL,
    external_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    phone TEXT,
    contact_id UUID REFERENCES contacts(id),
    deal_id UUID REFERENCES deals(id),
    last_body TEXT NOT NULL DEFAULT '',
    last_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unread INT NOT NULL DEFAULT 0,
    UNIQUE (channel, external_id)
);
CREATE TABLE IF NOT EXISTS inbox_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id UUID NOT NULL REFERENCES inbox_threads(id) ON DELETE CASCADE,
    direction TEXT NOT NULL,
    body TEXT NOT NULL,
    author_label TEXT NOT NULL DEFAULT '',
    external_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS inbox_messages_ext ON inbox_messages (external_id) WHERE external_id IS NOT NULL;

ALTER TABLE inbox_messages
  ADD COLUMN IF NOT EXISTS media_kind TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_filename TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_mime TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_size BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS media_storage_key TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_wappi_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS author_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS delivery_status TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS read_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS inbox_messages_media_wappi
  ON inbox_messages (media_wappi_id)
  WHERE media_wappi_id <> '';
CREATE INDEX IF NOT EXISTS inbox_messages_author
  ON inbox_messages (author_id)
  WHERE author_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS inbox_threads_last ON inbox_threads (last_at DESC);

CREATE TABLE IF NOT EXISTS inbox_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,
    channel TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'personal',
    operator_label TEXT NOT NULL DEFAULT '',
    external_ref TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, channel, name)
);
ALTER TABLE inbox_threads ADD COLUMN IF NOT EXISTS line_id UUID REFERENCES inbox_lines(id);
ALTER TABLE inbox_lines ADD COLUMN IF NOT EXISTS external_ref TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS inbox_lines_channel ON inbox_lines (channel, is_active);
CREATE INDEX IF NOT EXISTS inbox_lines_ref ON inbox_lines (provider, external_ref);

ALTER TABLE departments ADD COLUMN IF NOT EXISTS head_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS distribution_quotas (
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    pool TEXT NOT NULL,
    share NUMERIC(8,2) NOT NULL DEFAULT 0,
    daily_limit INT NOT NULL DEFAULT 0,
    PRIMARY KEY (department_id, pool)
);
-- уже существующие INT → дробные проценты
DO $$ BEGIN
  ALTER TABLE distribution_quotas ALTER COLUMN share TYPE NUMERIC(8,2) USING share::numeric;
EXCEPTION WHEN others THEN NULL;
END $$;
CREATE TABLE IF NOT EXISTS distribution_counters (
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    assigned INT NOT NULL DEFAULT 0,
    PRIMARY KEY (department_id, day)
);
CREATE TABLE IF NOT EXISTS distribution_manager_quotas (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    share NUMERIC(8,2) NOT NULL DEFAULT 0,
    daily_limit INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS distribution_manager_quotas_dept
  ON distribution_manager_quotas (department_id);
CREATE TABLE IF NOT EXISTS distribution_manager_counters (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    assigned INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
);
CREATE TABLE IF NOT EXISTS distribution_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool TEXT NOT NULL,
    department_id UUID REFERENCES departments(id) ON DELETE SET NULL,
    owner_id UUID REFERENCES users(id) ON DELETE SET NULL,
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS distribution_log_created ON distribution_log (created_at DESC);

CREATE TABLE IF NOT EXISTS distribution_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS distribution_sources (
    source TEXT PRIMARY KEY,
    pool TEXT NOT NULL
);
INSERT INTO distribution_settings (key, value) VALUES ('auto', '1') ON CONFLICT DO NOTHING;
INSERT INTO distribution_sources (source, pool) VALUES
    ('skorozvon', 'skorozvon'),
    ('avito', 'depts'),
    ('google', 'depts'),
    ('hh', 'depts'),
    ('site', 'depts')
ON CONFLICT DO NOTHING;

CREATE UNIQUE INDEX IF NOT EXISTS leads_source_ext_uidx
    ON leads (source, vk_lead_id)
    WHERE vk_lead_id IS NOT NULL AND vk_lead_id <> '';

ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INT NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS extension_quarantine (
    extension TEXT PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    released_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    until_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS extension_quarantine_until ON extension_quarantine (until_at);

ALTER TABLE inbox_lines ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS inbox_lines_owner ON inbox_lines (owner_user_id);

-- Автодозвон Недозвон 3 (Exolve)
CREATE TABLE IF NOT EXISTS autodial_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'started',
    exolve_call_id TEXT NOT NULL DEFAULT '',
    number_code TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    accepted_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    accepted_at TIMESTAMPTZ NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_autodial_attempts_deal
  ON autodial_attempts (deal_id, started_at DESC);
ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS accepted_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_autodial_attempts_accepted
  ON autodial_attempts (accepted_by)
  WHERE accepted_by IS NOT NULL;
ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS original_owner_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS disposition TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS disposition_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS disposition_at TIMESTAMPTZ NULL;

ALTER TABLE deals ADD COLUMN IF NOT EXISTS autodial_status TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_deals_autodial_status
  ON deals (autodial_status)
  WHERE autodial_status <> '';

CREATE TABLE IF NOT EXISTS autodial_schedule (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sort_order INT NOT NULL DEFAULT 0,
    day_from INT NOT NULL,
    day_to INT,
    calls_min INT NOT NULL DEFAULT 0,
    calls_max INT NOT NULL DEFAULT 0,
    interval_minutes INT NOT NULL DEFAULT 60,
    note TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS autodial_schedule_day_from_uidx
  ON autodial_schedule (day_from);

ALTER TABLE users ADD COLUMN IF NOT EXISTS login TEXT;
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_uidx
  ON users (lower(email))
  WHERE email IS NOT NULL AND email <> '';
CREATE UNIQUE INDEX IF NOT EXISTS users_login_lower_uidx
  ON users (lower(login))
  WHERE login IS NOT NULL AND login <> '';

CREATE UNIQUE INDEX IF NOT EXISTS deals_one_per_lead_uidx
  ON deals (lead_id)
  WHERE lead_id IS NOT NULL;
