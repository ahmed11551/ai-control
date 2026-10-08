-- Идемпотентные поля для UI задач и будущего импорта Bitrix24
ALTER TABLE users     ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE contacts  ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE companies ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE leads     ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE deals     ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS planned_at TIMESTAMPTZ;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS activity_at TIMESTAMPTZ;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS assignee_label TEXT;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS project_label TEXT;

UPDATE tasks SET activity_at = COALESCE(activity_at, updated_at, created_at, now()) WHERE activity_at IS NULL;
ALTER TABLE tasks ALTER COLUMN activity_at SET DEFAULT now();
ALTER TABLE tasks ALTER COLUMN activity_at SET NOT NULL;

UPDATE tasks SET status = 'in_progress' WHERE status IN ('open', 'wait', 'deferred');
UPDATE tasks SET status = 'completed' WHERE status IN ('done', 'closed');

CREATE UNIQUE INDEX IF NOT EXISTS users_bitrix_id_uidx ON users (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS contacts_bitrix_id_uidx ON contacts (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS companies_bitrix_id_uidx ON companies (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS leads_bitrix_id_uidx ON leads (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS deals_bitrix_id_uidx ON deals (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS tasks_bitrix_id_uidx ON tasks (bitrix_id) WHERE bitrix_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS bitrix_sync_state (
    entity TEXT PRIMARY KEY,
    last_id BIGINT NOT NULL DEFAULT 0,
    last_sync_at TIMESTAMPTZ
);
