CREATE TABLE IF NOT EXISTS inbox_threads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel TEXT NOT NULL,
    external_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    phone TEXT,
    contact_id UUID REFERENCES contacts(id),
    deal_id UUID REFERENCES deals(id),
    last_body TEXT NOT NULL DEFAULT '',
    last_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unread INT NOT NULL DEFAULT 0,
    UNIQUE (channel, external_id)
);
CREATE TABLE IF NOT EXISTS inbox_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id UUID NOT NULL REFERENCES inbox_threads(id) ON DELETE CASCADE,
    direction TEXT NOT NULL,
    body TEXT NOT NULL,
    author_label TEXT NOT NULL DEFAULT '',
    external_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS inbox_messages_ext ON inbox_messages (external_id) WHERE external_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS inbox_threads_last ON inbox_threads (last_at DESC);
