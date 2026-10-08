ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INT NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS extension_quarantine (
    extension TEXT PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    released_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    until_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS extension_quarantine_until ON extension_quarantine (until_at);

ALTER TABLE inbox_lines ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS inbox_lines_owner ON inbox_lines (owner_user_id);
