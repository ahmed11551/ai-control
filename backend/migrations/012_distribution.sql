ALTER TABLE departments ADD COLUMN IF NOT EXISTS head_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS distribution_quotas (
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    pool TEXT NOT NULL,
    share NUMERIC(8,2) NOT NULL DEFAULT 0,
    daily_limit INT NOT NULL DEFAULT 0,
    PRIMARY KEY (department_id, pool)
);
CREATE TABLE IF NOT EXISTS distribution_counters (
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    assigned INT NOT NULL DEFAULT 0,
    PRIMARY KEY (department_id, day)
);
CREATE TABLE IF NOT EXISTS distribution_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool TEXT NOT NULL,
    department_id UUID REFERENCES departments(id) ON DELETE SET NULL,
    owner_id UUID REFERENCES users(id) ON DELETE SET NULL,
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS distribution_log_created ON distribution_log (created_at DESC);
