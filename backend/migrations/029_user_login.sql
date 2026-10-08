-- Логин сотрудника (вход по логину или email)
ALTER TABLE users ADD COLUMN IF NOT EXISTS login TEXT;
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_uidx
  ON users (lower(email))
  WHERE email IS NOT NULL AND email <> '';

-- Бэкфилл логина из локальной части email
DO $$
DECLARE
  r RECORD;
  base TEXT;
  cand TEXT;
  n INT;
BEGIN
  FOR r IN
    SELECT id, coalesce(email, '') AS email
    FROM users
    WHERE login IS NULL OR btrim(login) = ''
  LOOP
    base := lower(split_part(r.email, '@', 1));
    base := regexp_replace(base, '[^a-z0-9._-]', '', 'g');
    IF base = '' THEN
      base := 'user';
    END IF;
    cand := base;
    n := 1;
    WHILE EXISTS (
      SELECT 1 FROM users u
      WHERE lower(coalesce(u.login, '')) = cand AND u.id <> r.id
    ) LOOP
      n := n + 1;
      cand := base || n::text;
    END LOOP;
    UPDATE users SET login = cand WHERE id = r.id;
  END LOOP;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS users_login_lower_uidx
  ON users (lower(login))
  WHERE login IS NOT NULL AND login <> '';
