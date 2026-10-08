-- Важные задачи и обязательные события (ТЗ §17)
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS is_important BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'task';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS result TEXT;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS result_note TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS block_work BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_tasks_attention
  ON tasks (assignee_id, is_important, block_work, due_at)
  WHERE status NOT IN ('completed','done','closed') AND resolved_at IS NULL;
