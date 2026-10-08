-- Скорозвон: исходный владелец + решение после разговора (ТЗ §8.2)

ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS original_owner_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS disposition TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS disposition_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS disposition_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_autodial_attempts_original_owner
  ON autodial_attempts (original_owner_id)
  WHERE original_owner_id IS NOT NULL;
