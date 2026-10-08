-- Exolve recording ref on calls (WAV остаётся в ЛК Exolve; у нас только id).
ALTER TABLE calls ADD COLUMN IF NOT EXISTS exolve_call_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_calls_exolve_call_id
  ON calls (exolve_call_id)
  WHERE exolve_call_id <> '';
