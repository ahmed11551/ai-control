-- Кто написал исходящее: менеджер видит только свои outbound.
ALTER TABLE inbox_messages
  ADD COLUMN IF NOT EXISTS author_id UUID NULL REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS inbox_messages_author
  ON inbox_messages (author_id)
  WHERE author_id IS NOT NULL;
