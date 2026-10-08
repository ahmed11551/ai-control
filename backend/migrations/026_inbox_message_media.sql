-- Вложения входящих сообщений MAX (байты на диске, без URL в БД)

ALTER TABLE inbox_messages
  ADD COLUMN IF NOT EXISTS media_kind TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_filename TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_mime TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_size BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS media_storage_key TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_wappi_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS inbox_messages_media_wappi
  ON inbox_messages (media_wappi_id)
  WHERE media_wappi_id <> '';
