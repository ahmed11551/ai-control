-- Иерархия ролей: admin, rop, manager, logist (+ legacy sales)
INSERT INTO roles (name, permissions) VALUES
    ('rop', '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"staff.create":true}'::jsonb),
    ('manager', '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"phones.hide":true}'::jsonb),
    ('logist', '{"logistics":true}'::jsonb)
ON CONFLICT (name) DO NOTHING;
UPDATE roles SET permissions = '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"phones.hide":true}'::jsonb
WHERE name = 'sales';
