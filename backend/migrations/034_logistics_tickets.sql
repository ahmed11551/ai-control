-- Логистика: очередь логисту + карточка билета (ТЗ §14–15)
ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_needed BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_assigned_to UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_needed_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS deals_logistics_needed_idx
  ON deals (logistics_needed) WHERE logistics_needed = true;

CREATE TABLE IF NOT EXISTS deal_tickets (
    deal_id UUID PRIMARY KEY REFERENCES deals(id) ON DELETE CASCADE,
    transport TEXT NOT NULL DEFAULT '', -- air|rail|bus|other
    flight_or_train TEXT NOT NULL DEFAULT '',
    depart_at TIMESTAMPTZ,
    arrive_at TIMESTAMPTZ,
    route TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    wagon TEXT NOT NULL DEFAULT '',
    seat TEXT NOT NULL DEFAULT '',
    amount NUMERIC(12,2),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS deal_ticket_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'ticket', -- ticket|doc|passport
    filename TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    storage_key TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'upload',
    source_message_id UUID,
    uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS deal_ticket_files_deal_idx ON deal_ticket_files (deal_id);
