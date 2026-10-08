CREATE TABLE IF NOT EXISTS distribution_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS distribution_sources (
    source TEXT PRIMARY KEY,
    pool TEXT NOT NULL
);
INSERT INTO distribution_settings (key, value) VALUES ('auto', '1') ON CONFLICT DO NOTHING;
INSERT INTO distribution_sources (source, pool) VALUES
    ('skorozvon', 'skorozvon'),
    ('avito', 'depts'),
    ('google', 'depts'),
    ('hh', 'depts'),
    ('site', 'depts')
ON CONFLICT DO NOTHING;
