-- Per-manager Exolve SIP + DID pool (CLI carousel).
-- One number → one manager. Outbound rotates CLI within that manager's pool.

CREATE TABLE IF NOT EXISTS exolve_sips (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    sip_login TEXT NOT NULL,
    sip_resource_id TEXT NOT NULL DEFAULT '',
    sip_password TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS exolve_sips_login_active
    ON exolve_sips (sip_login) WHERE active;

ALTER TABLE exolve_sips ADD COLUMN IF NOT EXISTS carousel_auth TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS exolve_dids (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    forwarding_ok BOOLEAN NOT NULL DEFAULT false,
    last_used_at TIMESTAMPTZ,
    cli_lease_until TIMESTAMPTZ,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS exolve_dids_number_active
    ON exolve_dids (number) WHERE active;
CREATE INDEX IF NOT EXISTS exolve_dids_user_lru
    ON exolve_dids (user_id, last_used_at NULLS FIRST, number)
    WHERE active;
