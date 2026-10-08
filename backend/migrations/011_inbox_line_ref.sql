ALTER TABLE inbox_lines ADD COLUMN IF NOT EXISTS external_ref TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS inbox_lines_ref ON inbox_lines (provider, external_ref);
