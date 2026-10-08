-- Справочник рабочих UF и значения в уже лежащих сущностях.
CREATE TABLE IF NOT EXISTS user_fields (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type TEXT NOT NULL,
    code TEXT NOT NULL,
    label TEXT NOT NULL,
    field_type TEXT NOT NULL DEFAULT 'string',
    field_group TEXT NOT NULL DEFAULT 'прочее',
    multiple BOOLEAN NOT NULL DEFAULT false,
    active BOOLEAN NOT NULL DEFAULT true,
    sort INT NOT NULL DEFAULT 100,
    options JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entity_type, code)
);

CREATE TABLE IF NOT EXISTS entity_field_values (
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    field_id UUID NOT NULL REFERENCES user_fields(id) ON DELETE CASCADE,
    value_text TEXT NOT NULL DEFAULT '',
    value_json JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_type, entity_id, field_id)
);
CREATE INDEX IF NOT EXISTS idx_entity_field_values_entity ON entity_field_values (entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_entity_field_values_field ON entity_field_values (field_id);
