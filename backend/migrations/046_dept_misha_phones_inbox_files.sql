-- Отдел Миши: полные номера (как Бауров) и файлы переписки.
-- Файлы чата Баурову не открываем. Билеты логиста этим флагом не управляются.
ALTER TABLE departments
  ADD COLUMN IF NOT EXISTS see_client_phones BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE departments
  ADD COLUMN IF NOT EXISTS see_inbox_files BOOLEAN NOT NULL DEFAULT false;

UPDATE departments
SET see_client_phones = true
WHERE name = 'Отдел Миши';

UPDATE departments
SET see_inbox_files = true
WHERE name = 'Отдел Миши';
