-- Ограничение входа сотрудников и РОП по офисным IP. По умолчанию выключено.
CREATE TABLE IF NOT EXISTS office_access (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT false
);
INSERT INTO office_access (id, enabled) VALUES (1, false) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS office_access_ips (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cidr TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS office_access_ips_cidr_idx ON office_access_ips (cidr);
