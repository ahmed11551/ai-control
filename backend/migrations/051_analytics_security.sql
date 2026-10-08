-- Аналитика и контур безопасности: инциденты, карантин сессии, проверка звонков.
-- Тот же SQL применяется при старте из internal/schema/compat.sql.

CREATE TABLE IF NOT EXISTS security_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    rule_code TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'medium',
    status TEXT NOT NULL DEFAULT 'open',
    object_type TEXT NOT NULL DEFAULT '',
    object_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    comment TEXT NOT NULL DEFAULT '',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS security_events_created_idx ON security_events (created_at DESC);
CREATE INDEX IF NOT EXISTS security_events_actor_idx ON security_events (actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS security_events_status_idx ON security_events (status, created_at DESC);

ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS call_reviews (
    call_id UUID PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    transcript TEXT NOT NULL DEFAULT '',
    matched_rule TEXT NOT NULL DEFAULT '',
    incident_id UUID REFERENCES security_events(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS call_reviews_status_idx ON call_reviews (status, created_at DESC);

ALTER TABLE calls ADD COLUMN IF NOT EXISTS transcript TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS card_views (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS card_views_actor_time ON card_views (actor_id, viewed_at DESC);
