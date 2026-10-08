-- Идемпотентные поля для UI задач и будущего импорта Bitrix24
ALTER TABLE users     ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE contacts  ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE companies ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE leads     ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE deals     ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS bitrix_id TEXT;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS planned_at TIMESTAMPTZ;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS activity_at TIMESTAMPTZ;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS assignee_label TEXT;
ALTER TABLE tasks     ADD COLUMN IF NOT EXISTS project_label TEXT;

UPDATE tasks SET activity_at = COALESCE(activity_at, updated_at, created_at, now()) WHERE activity_at IS NULL;
ALTER TABLE tasks ALTER COLUMN activity_at SET DEFAULT now();
ALTER TABLE tasks ALTER COLUMN activity_at SET NOT NULL;

UPDATE tasks SET status = 'in_progress' WHERE status IN ('open', 'wait', 'deferred');
UPDATE tasks SET status = 'completed' WHERE status IN ('done', 'closed');

CREATE UNIQUE INDEX IF NOT EXISTS users_bitrix_id_uidx ON users (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS contacts_bitrix_id_uidx ON contacts (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS companies_bitrix_id_uidx ON companies (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS leads_bitrix_id_uidx ON leads (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS deals_bitrix_id_uidx ON deals (bitrix_id) WHERE bitrix_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS tasks_bitrix_id_uidx ON tasks (bitrix_id) WHERE bitrix_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS bitrix_sync_state (
    entity TEXT PRIMARY KEY,
    last_id BIGINT NOT NULL DEFAULT 0,
    last_sync_at TIMESTAMPTZ
);

-- Exolve DID carousel (1 SIP + N DID per manager)
CREATE TABLE IF NOT EXISTS exolve_sips (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    sip_login TEXT NOT NULL,
    sip_resource_id TEXT NOT NULL DEFAULT '',
    sip_password TEXT NOT NULL,
    carousel_auth TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS exolve_sips_login_active
    ON exolve_sips (sip_login) WHERE active;

ALTER TABLE exolve_sips ADD COLUMN IF NOT EXISTS carousel_auth TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS exolve_dids (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    forwarding_ok BOOLEAN NOT NULL DEFAULT false,
    last_used_at TIMESTAMPTZ,
    cli_lease_until TIMESTAMPTZ,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS exolve_dids_number_active
    ON exolve_dids (number) WHERE active;
CREATE INDEX IF NOT EXISTS exolve_dids_user_lru
    ON exolve_dids (user_id, last_used_at NULLS FIRST, number)
    WHERE active;

-- Несколько Voximplant-аккаунтов на сотрудника
CREATE TABLE IF NOT EXISTS vox_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS vox_accounts_user_active
    ON vox_accounts (user_id, is_primary DESC, created_at)
    WHERE active;
CREATE UNIQUE INDEX IF NOT EXISTS vox_accounts_user_username_active
    ON vox_accounts (user_id, lower(username))
    WHERE active;

-- Иерархия: admin / rop / manager / logist
INSERT INTO roles (name, permissions) VALUES
    ('rop', '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"staff.create":true}'::jsonb),
    ('manager', '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"phones.hide":true}'::jsonb),
    ('logist', '{"logistics":true}'::jsonb)
ON CONFLICT (name) DO NOTHING;
UPDATE roles SET permissions = '{"deals":true,"leads":true,"contacts":true,"calls.make":true,"phones.hide":true}'::jsonb
WHERE name = 'sales';

-- Дробные доли распределения (6.1%, 3.5%)
DO $$ BEGIN
  ALTER TABLE distribution_quotas ALTER COLUMN share TYPE NUMERIC(8,2) USING share::numeric;
EXCEPTION WHEN undefined_table THEN NULL;
           WHEN others THEN NULL;
END $$;

-- Пул «Барс» убран: источники и квоты → depts
DO $$ BEGIN
  DELETE FROM distribution_sources WHERE source = 'bars' OR pool = 'bars';
  UPDATE distribution_quotas SET pool = 'depts' WHERE pool = 'bars'
    AND NOT EXISTS (
      SELECT 1 FROM distribution_quotas q2
      WHERE q2.department_id = distribution_quotas.department_id AND q2.pool = 'depts'
    );
  DELETE FROM distribution_quotas WHERE pool = 'bars';
  DELETE FROM distribution_log WHERE pool = 'bars';
EXCEPTION WHEN undefined_table THEN NULL;
END $$;

-- Скорозвон (создаём таблицу до ALTER — иначе bootstrap падает на старых БД)
CREATE TABLE IF NOT EXISTS autodial_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'started',
    exolve_call_id TEXT NOT NULL DEFAULT '',
    number_code TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    accepted_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    accepted_at TIMESTAMPTZ NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_autodial_attempts_deal
  ON autodial_attempts (deal_id, started_at DESC);

ALTER TABLE deals ADD COLUMN IF NOT EXISTS autodial_status TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_deals_autodial_status
  ON deals (autodial_status)
  WHERE autodial_status <> '';

ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS accepted_by UUID NULL REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_autodial_attempts_accepted
  ON autodial_attempts (accepted_by)
  WHERE accepted_by IS NOT NULL;

ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS original_owner_id UUID NULL REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS disposition TEXT NOT NULL DEFAULT '';
ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS disposition_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE autodial_attempts
  ADD COLUMN IF NOT EXISTS disposition_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_autodial_attempts_original_owner
  ON autodial_attempts (original_owner_id)
  WHERE original_owner_id IS NOT NULL;

-- Таблицы из import.sql, которые compat меняет ниже. На пустой базе их ещё нет.
CREATE TABLE IF NOT EXISTS departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    parent_id UUID REFERENCES departments(id) ON DELETE SET NULL,
    bitrix_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS inbox_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,
    channel TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'personal',
    operator_label TEXT NOT NULL DEFAULT '',
    external_ref TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS inbox_threads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel TEXT NOT NULL,
    external_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    phone TEXT,
    contact_id UUID REFERENCES contacts(id),
    deal_id UUID REFERENCES deals(id),
    last_body TEXT NOT NULL DEFAULT '',
    last_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unread INT NOT NULL DEFAULT 0,
    UNIQUE (channel, external_id)
);
ALTER TABLE inbox_threads ADD COLUMN IF NOT EXISTS line_id UUID REFERENCES inbox_lines(id);
CREATE TABLE IF NOT EXISTS inbox_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    thread_id UUID NOT NULL REFERENCES inbox_threads(id) ON DELETE CASCADE,
    direction TEXT NOT NULL,
    body TEXT NOT NULL,
    author_label TEXT NOT NULL DEFAULT '',
    external_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Медиа во входящих
ALTER TABLE inbox_messages
  ADD COLUMN IF NOT EXISTS media_kind TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_filename TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_mime TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_size BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS media_storage_key TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_wappi_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_sent_to_logist_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS inbox_messages_media_wappi
  ON inbox_messages (media_wappi_id)
  WHERE media_wappi_id <> '';

-- Исходящие мессенджера: видимость только автору (менеджеру)
ALTER TABLE inbox_messages
  ADD COLUMN IF NOT EXISTS author_id UUID NULL REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS inbox_messages_author
  ON inbox_messages (author_id)
  WHERE author_id IS NOT NULL;

-- Галочки доставки/прочтения MAX (Wappi delivery_status)
ALTER TABLE inbox_messages
  ADD COLUMN IF NOT EXISTS delivery_status TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS read_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS inbox_messages_delivery_ext
  ON inbox_messages (external_id)
  WHERE direction = 'outbound' AND coalesce(external_id, '') <> '';

-- Расписание частоты скорозвона
CREATE TABLE IF NOT EXISTS autodial_schedule (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sort_order INT NOT NULL DEFAULT 0,
    day_from INT NOT NULL,
    day_to INT,
    calls_min INT NOT NULL DEFAULT 0,
    calls_max INT NOT NULL DEFAULT 0,
    interval_minutes INT NOT NULL DEFAULT 60,
    note TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS autodial_schedule_day_from_uidx
  ON autodial_schedule (day_from);

INSERT INTO autodial_schedule (sort_order, day_from, day_to, calls_min, calls_max, interval_minutes, note)
SELECT * FROM (VALUES
  (1, 1, 1, 5, 15, 60, 'Максимальная активность в первый день'),
  (2, 2, 2, 1, 7, 120, 'Снижаем частоту, но сохраняем активность'),
  (3, 3, 3, 1, 5, 180, 'Продолжаем работу с теплыми лидами'),
  (4, 4, 7, 3, 4, 240, 'Умеренная активность'),
  (5, 8, 30, 1, 1, 1440, 'Разное время суток'),
  (6, 31, NULL::int, 0, 0, 1440, 'Можно настроить индивидуально')
) AS v(sort_order, day_from, day_to, calls_min, calls_max, interval_minutes, note)
WHERE NOT EXISTS (SELECT 1 FROM autodial_schedule LIMIT 1);


-- Логин сотрудника (вход по логину или email)
ALTER TABLE users ADD COLUMN IF NOT EXISTS login TEXT;
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_uidx
  ON users (lower(email))
  WHERE email IS NOT NULL AND email <> '';
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

-- Один лид — одна сделка
DELETE FROM deals a
USING deals b
WHERE a.lead_id IS NOT NULL
  AND a.lead_id = b.lead_id
  AND a.created_at > b.created_at;
UPDATE leads l
SET converted_deal_id = d.id
FROM deals d
WHERE d.lead_id = l.id
  AND (l.converted_deal_id IS NULL OR l.converted_deal_id <> d.id);
CREATE UNIQUE INDEX IF NOT EXISTS deals_one_per_lead_uidx
  ON deals (lead_id)
  WHERE lead_id IS NOT NULL;

-- Распределение РОП → менеджеры отдела
CREATE TABLE IF NOT EXISTS distribution_manager_quotas (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    department_id UUID NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    share NUMERIC(8,2) NOT NULL DEFAULT 0,
    daily_limit INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS distribution_manager_quotas_dept
  ON distribution_manager_quotas (department_id);

CREATE TABLE IF NOT EXISTS distribution_manager_counters (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    assigned INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
);

-- MAX: тред = line × phone (см. migrations/032_inbox_max_per_line.sql)
-- Безопасно при коллизиях: один победитель на line+phone, сообщения дублей сливаются.
DO $$
DECLARE
  r RECORD;
  phone_norm TEXT;
  tgt TEXT;
  keeper UUID;
BEGIN
  FOR r IN
    SELECT t.id, t.line_id, coalesce(t.phone,'') AS phone, t.external_id, t.last_at
    FROM inbox_threads t
    WHERE t.channel = 'max'
      AND t.line_id IS NOT NULL
      AND position(':' in t.external_id) = 0
    ORDER BY t.last_at DESC NULLS LAST, t.id
  LOOP
    phone_norm := regexp_replace(coalesce(nullif(r.phone,''), r.external_id), '\D', '', 'g');
    IF length(phone_norm) = 11 AND left(phone_norm, 1) = '8' THEN
      phone_norm := '7' || substr(phone_norm, 2);
    END IF;
    IF length(phone_norm) < 10 THEN
      CONTINUE;
    END IF;
    tgt := r.line_id::text || ':' || phone_norm;

    SELECT id INTO keeper
    FROM inbox_threads
    WHERE channel = 'max' AND external_id = tgt AND id <> r.id
    LIMIT 1;

    IF keeper IS NOT NULL THEN
      UPDATE inbox_messages SET thread_id = keeper WHERE thread_id = r.id;
      DELETE FROM inbox_threads WHERE id = r.id;
    ELSE
      BEGIN
        UPDATE inbox_threads
        SET external_id = tgt, phone = phone_norm
        WHERE id = r.id;
      EXCEPTION WHEN unique_violation THEN
        SELECT id INTO keeper
        FROM inbox_threads
        WHERE channel = 'max' AND external_id = tgt AND id <> r.id
        LIMIT 1;
        IF keeper IS NOT NULL THEN
          UPDATE inbox_messages SET thread_id = keeper WHERE thread_id = r.id;
          DELETE FROM inbox_threads WHERE id = r.id;
        END IF;
      END;
    END IF;
  END LOOP;
END $$;

UPDATE inbox_threads
SET phone = split_part(external_id, ':', 2)
WHERE channel = 'max'
  AND position(':' in external_id) > 0
  AND (phone IS NULL OR phone = '' OR phone = external_id OR position(':' in coalesce(phone,'')) > 0
       OR phone <> split_part(external_id, ':', 2));

-- Exolve: ссылка на запись звонка (файл в ЛК, у нас только call_id)
ALTER TABLE calls ADD COLUMN IF NOT EXISTS exolve_call_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_calls_exolve_call_id
  ON calls (exolve_call_id)
  WHERE exolve_call_id <> '';

-- Логистика / билеты
ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_needed BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_assigned_to UUID;
ALTER TABLE deals ADD COLUMN IF NOT EXISTS logistics_needed_at TIMESTAMPTZ;
CREATE TABLE IF NOT EXISTS deal_tickets (
    deal_id UUID PRIMARY KEY REFERENCES deals(id) ON DELETE CASCADE,
    transport TEXT NOT NULL DEFAULT '',
    flight_or_train TEXT NOT NULL DEFAULT '',
    depart_at TIMESTAMPTZ,
    arrive_at TIMESTAMPTZ,
    route TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    wagon TEXT NOT NULL DEFAULT '',
    seat TEXT NOT NULL DEFAULT '',
    amount NUMERIC(12,2),
    updated_by UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS deal_ticket_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'ticket',
    filename TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    storage_key TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'upload',
    source_message_id UUID,
    uploaded_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Доступ сотрудников только с офисных IP (по умолчанию выключено)
CREATE TABLE IF NOT EXISTS office_access (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT false
);
INSERT INTO office_access (id, enabled) VALUES (1, false) ON CONFLICT (id) DO NOTHING;
CREATE TABLE IF NOT EXISTS office_access_ips (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cidr TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS office_access_ips_cidr_idx ON office_access_ips (cidr);

-- Срез истории линии MAX после смены владельца
ALTER TABLE inbox_lines ADD COLUMN IF NOT EXISTS history_from TIMESTAMPTZ;

-- Внутренний чат логист ↔ менеджер по кандидату
CREATE TABLE IF NOT EXISTS staff_dialogs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id UUID NOT NULL REFERENCES deals(id) ON DELETE CASCADE,
    lead_id UUID REFERENCES leads(id) ON DELETE SET NULL,
    opened_by UUID REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    closed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    closed_at TIMESTAMPTZ,
    last_body TEXT NOT NULL DEFAULT '',
    last_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS staff_dialogs_one_open_per_deal
    ON staff_dialogs (deal_id) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS staff_dialogs_status_last_idx
    ON staff_dialogs (status, last_at DESC);
CREATE INDEX IF NOT EXISTS staff_dialogs_deal_idx ON staff_dialogs (deal_id);
CREATE INDEX IF NOT EXISTS staff_dialogs_lead_idx ON staff_dialogs (lead_id);

CREATE TABLE IF NOT EXISTS staff_dialog_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dialog_id UUID NOT NULL REFERENCES staff_dialogs(id) ON DELETE CASCADE,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS staff_dialog_messages_dialog_idx
    ON staff_dialog_messages (dialog_id, created_at ASC);

-- Город у лида + «МСК Башня» у РОПа
ALTER TABLE leads ADD COLUMN IF NOT EXISTS city TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS msk_tower BOOLEAN NOT NULL DEFAULT false;

-- Комментарий кандидата (вместо «Компания» на карточке/канбане)
ALTER TABLE leads ADD COLUMN IF NOT EXISTS card_note TEXT NOT NULL DEFAULT '';

-- Инциденты безопасности, карантин сессии и проверка звонков
CREATE TABLE IF NOT EXISTS security_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    rule_code TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'medium',
    status TEXT NOT NULL DEFAULT 'open',
    object_type TEXT NOT NULL DEFAULT '',
    object_id TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    comment TEXT NOT NULL DEFAULT '',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS security_events_created_idx ON security_events (created_at DESC);
CREATE INDEX IF NOT EXISTS security_events_actor_idx ON security_events (actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS security_events_status_idx ON security_events (status, created_at DESC);

ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_restricted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS call_reviews (
    call_id UUID PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    transcript TEXT NOT NULL DEFAULT '',
    matched_rule TEXT NOT NULL DEFAULT '',
    incident_id UUID REFERENCES security_events(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS call_reviews_status_idx ON call_reviews (status, created_at DESC);

ALTER TABLE calls ADD COLUMN IF NOT EXISTS transcript TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS card_views (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    entity_id UUID NOT NULL,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS card_views_actor_time ON card_views (actor_id, viewed_at DESC);

-- ТЗ v6.0: единая доменная модель кандидата и сквозные пакеты передачи.
CREATE TABLE IF NOT EXISTS candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legacy_contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
    display_name TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'crm',
    status TEXT NOT NULL DEFAULT 'active',
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS candidates_legacy_contact_uidx
    ON candidates (legacy_contact_id) WHERE legacy_contact_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS candidates_status_idx ON candidates (status, updated_at DESC);
ALTER TABLE leads ADD COLUMN IF NOT EXISTS candidate_id UUID REFERENCES candidates(id) ON DELETE SET NULL;
ALTER TABLE deals ADD COLUMN IF NOT EXISTS candidate_id UUID REFERENCES candidates(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS leads_candidate_idx ON leads (candidate_id);
CREATE INDEX IF NOT EXISTS deals_candidate_idx ON deals (candidate_id);
CREATE OR REPLACE FUNCTION ensure_candidate_link()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    candidate_uuid UUID;
    linked_candidate UUID;
    bound_contact UUID;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.candidate_id IS NOT NULL AND NEW.candidate_id IS DISTINCT FROM OLD.candidate_id THEN
            RAISE EXCEPTION 'candidate identity is immutable' USING ERRCODE = '23514';
        END IF;
    END IF;
    candidate_uuid := NEW.candidate_id;
    IF TG_TABLE_NAME = 'leads' THEN
        IF NEW.converted_deal_id IS NOT NULL THEN
            SELECT candidate_id INTO linked_candidate FROM deals WHERE id = NEW.converted_deal_id;
            IF linked_candidate IS NOT NULL THEN
                IF candidate_uuid IS NOT NULL AND candidate_uuid <> linked_candidate THEN
                    RAISE EXCEPTION 'lead and converted deal identities differ' USING ERRCODE = '23514';
                END IF;
                candidate_uuid := linked_candidate;
            END IF;
        END IF;
    END IF;
    IF TG_TABLE_NAME = 'deals' THEN
        IF NEW.lead_id IS NOT NULL THEN
            SELECT candidate_id INTO linked_candidate FROM leads WHERE id = NEW.lead_id FOR UPDATE;
            IF NOT FOUND THEN
                RAISE EXCEPTION 'linked lead does not exist' USING ERRCODE = '23503';
            END IF;
            IF linked_candidate IS NULL THEN
                UPDATE leads SET candidate_id = candidate_uuid WHERE id = NEW.lead_id
                RETURNING candidate_id INTO linked_candidate;
            END IF;
            IF candidate_uuid IS NOT NULL AND candidate_uuid <> linked_candidate THEN
                RAISE EXCEPTION 'deal and lead candidate identities differ' USING ERRCODE = '23514';
            END IF;
            candidate_uuid := linked_candidate;
        END IF;
    END IF;
    IF candidate_uuid IS NULL THEN
        INSERT INTO candidates (legacy_contact_id, display_name, source)
        VALUES (NEW.contact_id, coalesce(NEW.title, ''), TG_TABLE_NAME)
        ON CONFLICT (legacy_contact_id) WHERE legacy_contact_id IS NOT NULL
        DO UPDATE SET updated_at = now()
        RETURNING id INTO candidate_uuid;
    END IF;
    SELECT legacy_contact_id INTO bound_contact FROM candidates WHERE id = candidate_uuid FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'candidate does not exist' USING ERRCODE = '23503';
    END IF;
    IF NEW.contact_id IS NOT NULL THEN
        IF bound_contact IS NOT NULL AND bound_contact <> NEW.contact_id THEN
            RAISE EXCEPTION 'candidate belongs to another contact' USING ERRCODE = '23514';
        END IF;
        -- The unique index also serializes competing claims on the same contact.
        IF bound_contact IS NULL THEN
            UPDATE candidates SET legacy_contact_id = NEW.contact_id WHERE id = candidate_uuid;
        END IF;
    END IF;
    NEW.candidate_id := candidate_uuid;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION protect_candidate_identity()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR
       (OLD.legacy_contact_id IS NOT NULL AND NEW.legacy_contact_id IS DISTINCT FROM OLD.legacy_contact_id) THEN
        RAISE EXCEPTION 'candidate identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS candidates_identity ON candidates;
CREATE TRIGGER candidates_identity BEFORE UPDATE OF id, legacy_contact_id ON candidates
FOR EACH ROW EXECUTE FUNCTION protect_candidate_identity();
DROP TRIGGER IF EXISTS leads_candidate_link ON leads;
CREATE TRIGGER leads_candidate_link
BEFORE INSERT OR UPDATE OF contact_id, title, candidate_id, converted_deal_id ON leads
FOR EACH ROW EXECUTE FUNCTION ensure_candidate_link();
DROP TRIGGER IF EXISTS deals_candidate_link ON deals;
CREATE TRIGGER deals_candidate_link
BEFORE INSERT OR UPDATE OF contact_id, title, candidate_id, lead_id ON deals
FOR EACH ROW EXECUTE FUNCTION ensure_candidate_link();

-- Preserve already assigned identities. Conflicts require explicit reconciliation;
-- never silently merge candidates that may own documents or approval history.
-- First propagate existing deal identities to linked, unassigned leads.
UPDATE leads l SET candidate_id = d.candidate_id FROM deals d
WHERE (d.lead_id = l.id OR l.converted_deal_id = d.id)
  AND l.candidate_id IS NULL AND d.candidate_id IS NOT NULL;
UPDATE leads SET candidate_id = candidate_id WHERE candidate_id IS NULL;
UPDATE deals SET candidate_id = candidate_id;
-- Validate existing contact bindings as well as new ones.
UPDATE leads SET candidate_id = candidate_id;
ALTER TABLE leads ALTER COLUMN candidate_id SET NOT NULL;
ALTER TABLE deals ALTER COLUMN candidate_id SET NOT NULL;

-- Identity links must reject deletion, not attempt to NULL an immutable,
-- mandatory identity. Replace legacy FK actions on upgrades as well.
ALTER TABLE candidates DROP CONSTRAINT IF EXISTS candidates_legacy_contact_id_fkey;
ALTER TABLE candidates ADD CONSTRAINT candidates_legacy_contact_id_fkey
    FOREIGN KEY (legacy_contact_id) REFERENCES contacts(id) ON DELETE RESTRICT;
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_candidate_id_fkey;
ALTER TABLE leads ADD CONSTRAINT leads_candidate_id_fkey
    FOREIGN KEY (candidate_id) REFERENCES candidates(id) ON DELETE RESTRICT;
ALTER TABLE deals DROP CONSTRAINT IF EXISTS deals_candidate_id_fkey;
ALTER TABLE deals ADD CONSTRAINT deals_candidate_id_fkey
    FOREIGN KEY (candidate_id) REFERENCES candidates(id) ON DELETE RESTRICT;
CREATE TABLE IF NOT EXISTS candidate_trips (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'planned',
    transport_type TEXT NOT NULL DEFAULT '',
    origin_city TEXT NOT NULL DEFAULT '',
    destination_city TEXT NOT NULL DEFAULT '',
    depart_at TIMESTAMPTZ,
    arrive_at TIMESTAMPTZ,
    ticket_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS candidate_trips_candidate_idx ON candidate_trips (candidate_id, arrive_at DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS candidate_trips_arrival_idx ON candidate_trips (destination_city, arrive_at);
CREATE TABLE IF NOT EXISTS candidate_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    document_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'uploaded',
    original_filename TEXT NOT NULL DEFAULT '',
    encrypted_storage_key TEXT NOT NULL,
    encrypted_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS candidate_documents_candidate_idx ON candidate_documents (candidate_id, document_type, created_at DESC);
CREATE TABLE IF NOT EXISTS approval_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    version INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    prepared_by UUID REFERENCES users(id) ON DELETE SET NULL,
    manager_comment TEXT NOT NULL DEFAULT '',
    return_reason TEXT NOT NULL DEFAULT '',
    submitted_at TIMESTAMPTZ,
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (candidate_id, version)
);
CREATE INDEX IF NOT EXISTS approval_requests_queue_idx ON approval_requests (status, created_at DESC);
CREATE INDEX IF NOT EXISTS approval_requests_candidate_idx ON approval_requests (candidate_id, version DESC);
CREATE TABLE IF NOT EXISTS approval_request_documents (
    approval_request_id UUID NOT NULL REFERENCES approval_requests(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES candidate_documents(id) ON DELETE RESTRICT,
    PRIMARY KEY (approval_request_id, document_id)
);
CREATE TABLE IF NOT EXISTS customer_sites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS customer_packages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_request_id UUID NOT NULL REFERENCES approval_requests(id) ON DELETE RESTRICT,
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    customer_site_id UUID REFERENCES customer_sites(id) ON DELETE RESTRICT,
    object_id TEXT NOT NULL DEFAULT '',
    version INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'published',
    approved_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    published_by UUID REFERENCES users(id) ON DELETE SET NULL,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (approval_request_id, version)
);
CREATE INDEX IF NOT EXISTS customer_packages_scope_idx ON customer_packages (customer_site_id, object_id, status, published_at DESC);
