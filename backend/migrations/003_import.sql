-- Применяется и через bootstrap (schema.ImportSQL), и при чистой установке Docker.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    parent_id UUID REFERENCES departments(id) ON DELETE SET NULL,
    bitrix_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS departments_bitrix_id_uidx ON departments (bitrix_id) WHERE bitrix_id IS NOT NULL;

ALTER TABLE users ADD COLUMN IF NOT EXISTS department_id UUID REFERENCES departments(id) ON DELETE SET NULL;

ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
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
