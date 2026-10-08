-- Скорозвон: кто первым принял (exclusive claim).

ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS accepted_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_autodial_attempts_accepted
  ON autodial_attempts (accepted_by)
  WHERE accepted_by IS NOT NULL;
