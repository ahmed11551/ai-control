-- Несколько Voximplant-аккаунтов на сотрудника (username/password)
CREATE TABLE IF NOT EXISTS vox_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS vox_accounts_user_active
    ON vox_accounts (user_id, is_primary DESC, created_at)
    WHERE active;

CREATE UNIQUE INDEX IF NOT EXISTS vox_accounts_user_username_active
    ON vox_accounts (user_id, lower(username))
    WHERE active;
