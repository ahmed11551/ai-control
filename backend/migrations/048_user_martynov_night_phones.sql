-- Лично martynov.night (логист): полные номера клиентов. Остальным логистам нет.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS see_client_phones BOOLEAN NOT NULL DEFAULT false;

UPDATE users
SET see_client_phones = true
WHERE login = 'martynov.night';
