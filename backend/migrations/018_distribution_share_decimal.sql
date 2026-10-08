-- decimal shares for catch-up distribution (6.1%, 3.5%, …)
ALTER TABLE distribution_quotas
  ALTER COLUMN share TYPE NUMERIC(8,2) USING share::numeric;
