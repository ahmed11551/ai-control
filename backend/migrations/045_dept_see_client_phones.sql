-- Полные номера клиентов: флаг на отделе (не на роли).
-- По умолчанию false; Отдел Баурова включается явно.
ALTER TABLE departments
  ADD COLUMN IF NOT EXISTS see_client_phones BOOLEAN NOT NULL DEFAULT false;

UPDATE departments
SET see_client_phones = true
WHERE name = 'Отдел Баурова';
