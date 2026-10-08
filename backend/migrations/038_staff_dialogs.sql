-- Внутренний чат логист ↔ менеджер по кандидату (сделке)
CREATE TABLE IF NOT EXISTS staff_dialogs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
    opened_by UUID REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    closed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    closed_at TIMESTAMPTZ,
    last_body TEXT NOT NULL DEFAULT '',
    last_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS staff_dialogs_one_open_per_deal
    ON staff_dialogs (deal_id) WHERE status = 'open';

CREATE INDEX IF NOT EXISTS staff_dialogs_status_last_idx
    ON staff_dialogs (status, last_at DESC);

CREATE INDEX IF NOT EXISTS staff_dialogs_deal_idx ON staff_dialogs (deal_id);
CREATE INDEX IF NOT EXISTS staff_dialogs_lead_idx ON staff_dialogs (lead_id);

CREATE TABLE IF NOT EXISTS staff_dialog_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dialog_id UUID NOT NULL REFERENCES staff_dialogs(id) ON DELETE CASCADE,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS staff_dialog_messages_dialog_idx
    ON staff_dialog_messages (dialog_id, created_at ASC);
