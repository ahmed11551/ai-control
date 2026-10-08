-- Статусы доставки исходящих MAX/Wappi (галочки: sent / delivered / read)
ALTER TABLE inbox_messages
  ADD COLUMN IF NOT EXISTS delivery_status TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS read_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS inbox_messages_delivery_ext
  ON inbox_messages (external_id)
  WHERE direction = 'outbound' AND coalesce(external_id, '') <> '';
