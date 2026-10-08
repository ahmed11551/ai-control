CREATE TABLE IF NOT EXISTS inbox_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,
    channel TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'personal',
    operator_label TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, channel, name)
);
ALTER TABLE inbox_threads ADD COLUMN IF NOT EXISTS line_id UUID REFERENCES inbox_lines(id);
CREATE INDEX IF NOT EXISTS inbox_lines_channel ON inbox_lines (channel, is_active);
