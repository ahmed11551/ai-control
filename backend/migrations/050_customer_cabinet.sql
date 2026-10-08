-- Кабинет заказчика: четыре города, анкета и копия документов.
INSERT INTO roles (name, permissions)
VALUES ('customer', '{"customer":true}'::jsonb)
ON CONFLICT (name) DO NOTHING;

CREATE TABLE IF NOT EXISTS customer_sites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

INSERT INTO customer_sites (code, name) VALUES
    ('msk_sao', 'Москва — САО'),
    ('novgorod', 'Великий Новгород'),
    ('saransk', 'Саранск'),
    ('smolensk', 'Смоленск')
ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name;

ALTER TABLE users ADD COLUMN IF NOT EXISTS customer_site_id UUID REFERENCES customer_sites(id);

CREATE TABLE IF NOT EXISTS customer_submissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    site_id UUID NOT NULL REFERENCES customer_sites(id),
    full_name TEXT NOT NULL,
    birth_date DATE NOT NULL,
    passport_number TEXT NOT NULL,
    arrival_date DATE NOT NULL,
    position TEXT NOT NULL,
    sent_by UUID,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    recalled_at TIMESTAMPTZ
);

ALTER TABLE customer_submissions
    ADD COLUMN IF NOT EXISTS arrived_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS arrived_by UUID;

CREATE UNIQUE INDEX IF NOT EXISTS customer_submissions_deal_site
    ON customer_submissions (deal_id, site_id);

CREATE TABLE IF NOT EXISTS customer_submission_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    submission_id UUID NOT NULL REFERENCES customer_submissions(id) ON DELETE CASCADE,
    source_file_id UUID,
    kind TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    storage_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
