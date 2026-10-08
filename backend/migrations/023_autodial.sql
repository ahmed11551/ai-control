-- Автодозвон при Недозвон 3 (Exolve MakeCallback)

CREATE TABLE IF NOT EXISTS autodial_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'started',
    exolve_call_id TEXT NOT NULL DEFAULT '',
    number_code TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_autodial_attempts_deal
  ON autodial_attempts (deal_id, started_at DESC);

ALTER TABLE deals ADD COLUMN IF NOT EXISTS autodial_status TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_deals_autodial_status
  ON deals (autodial_status)
  WHERE autodial_status <> '';
