ALTER TABLE calls ADD COLUMN IF NOT EXISTS provider TEXT;
-- TimescaleDB: unique indexes on hypertables must include the partition column (started_at)
CREATE UNIQUE INDEX IF NOT EXISTS calls_uniqueid_started
  ON calls (asterisk_uniqueid, started_at)
  WHERE asterisk_uniqueid IS NOT NULL;
