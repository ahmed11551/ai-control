-- Срез истории MAX: после привязки аккаунта менеджеру старые сообщения не показываем и не принимаем.
ALTER TABLE inbox_lines
  ADD COLUMN IF NOT EXISTS history_from TIMESTAMPTZ;
