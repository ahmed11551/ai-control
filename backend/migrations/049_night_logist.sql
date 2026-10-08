-- Ночной логист: тот же кабинет, что у логиста, но видит номера клиентов.
INSERT INTO roles (name, permissions)
VALUES ('night_logist', '{"logistics":true}'::jsonb)
ON CONFLICT (name) DO NOTHING;

UPDATE users u
SET role_id = night.id
FROM roles night, roles cur
WHERE night.name = 'night_logist'
  AND u.login = 'martynov.night'
  AND cur.id = u.role_id
  AND cur.name = 'logist';
