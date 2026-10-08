-- Доли и дневные лимиты менеджеров внутри отдела (настраивает РОП).
CREATE TABLE IF NOT EXISTS distribution_manager_quotas (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    share NUMERIC(8,2) NOT NULL DEFAULT 0,
    daily_limit INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS distribution_manager_quotas_dept
  ON distribution_manager_quotas (department_id);

CREATE TABLE IF NOT EXISTS distribution_manager_counters (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    assigned INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
);
