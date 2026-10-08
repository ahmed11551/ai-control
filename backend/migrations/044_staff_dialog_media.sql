-- Вложения в диалогах логист ↔ менеджер

CREATE TABLE IF NOT EXISTS staff_dialog_message_files (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id UUID NOT NULL REFERENCES staff_dialog_messages(id) ON DELETE CASCADE,
  sort_order INT NOT NULL DEFAULT 0,
  kind TEXT NOT NULL DEFAULT 'file',
  filename TEXT NOT NULL DEFAULT '',
  mime TEXT NOT NULL DEFAULT '',
  size_bytes BIGINT NOT NULL DEFAULT 0,
  storage_key TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS staff_dialog_message_files_msg
  ON staff_dialog_message_files (message_id, sort_order);
