-- Флаг: вложение из чата уже передано логисту (менеджер не видит файл)
ALTER TABLE inbox_messages
  ADD COLUMN IF NOT EXISTS media_sent_to_logist_at TIMESTAMPTZ;
