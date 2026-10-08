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
