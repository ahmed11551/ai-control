#!/usr/bin/env bash
# У менеджеров забрать неуспешный MAX и выдать свободный успешный.
# Успешный номер, уже привязанный кому-либо, не берётся.
# Один менеджер — одна замена. Если свободные кончились, остальные неуспешные остаются.
#
#   bash scripts/swap-max-accounts.sh plan  /path/check.txt
#   bash scripts/swap-max-accounts.sh apply /path/check.txt
#   bash scripts/swap-max-accounts.sh revert
#
# Откат возвращает последний снимок: новую линию отвязывает, старую возвращает
# менеджеру. Строку не трогает, если новую уже перепривязали или старую кто-то занял.
set -euo pipefail

ROOT="${HOME}/crm"
cd "$ROOT"

MODE="${1:-plan}"
CHECK="${2:-}"

if [[ ! -f .env ]]; then
  echo "нет $ROOT/.env" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
source .env
set +a

SVC=$(docker compose ps --status running --services 2>/dev/null | grep -E '^(postgres|db)$' | head -1 || true)
if [[ -z "$SVC" ]]; then
  echo "контейнер postgres/db не запущен" >&2
  docker compose ps >&2 || true
  exit 1
fi

psql_docker() {
  docker compose exec -T "$SVC" \
    env PGPASSWORD="$POSTGRES_PASSWORD" \
    psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -v ON_ERROR_STOP=1 "$@"
}

if [[ "$MODE" == "revert" ]]; then
  psql_docker <<'SQL'
BEGIN;

DO $$
DECLARE
  batch uuid;
  r record;
  cleared int;
  restored int;
  ok int := 0;
  skipped int := 0;
BEGIN
  SELECT b.batch_id INTO batch
  FROM max_bind_swap_backup b
  WHERE b.reverted_at IS NULL
  ORDER BY b.created_at DESC
  LIMIT 1;

  IF batch IS NULL THEN
    RAISE EXCEPTION 'нет снимка для отката';
  END IF;

  FOR r IN
    SELECT b.id, b.manager_id, b.old_line_id, b.new_line_id, b.new_phone,
           coalesce(u.full_name, '') AS full_name
    FROM max_bind_swap_backup b
    LEFT JOIN users u ON u.id = b.manager_id
    WHERE b.batch_id = batch AND b.reverted_at IS NULL
    ORDER BY b.created_at, b.new_phone
  LOOP
    IF NOT EXISTS (
      SELECT 1 FROM inbox_lines
      WHERE id = r.new_line_id AND owner_user_id = r.manager_id
    ) OR EXISTS (
      SELECT 1 FROM inbox_lines
      WHERE id = r.old_line_id
        AND owner_user_id IS NOT NULL
        AND owner_user_id <> r.manager_id
    ) THEN
      skipped := skipped + 1;
      RAISE NOTICE 'пропуск %: новая линия уже не у менеджера или старую занял другой (% )', r.full_name, r.new_phone;
      CONTINUE;
    END IF;

    UPDATE inbox_lines
    SET owner_user_id = NULL
    WHERE id = r.new_line_id AND owner_user_id = r.manager_id;
    GET DIAGNOSTICS cleared = ROW_COUNT;

    UPDATE inbox_lines
    SET owner_user_id = r.manager_id
    WHERE id = r.old_line_id AND (owner_user_id IS NULL OR owner_user_id = r.manager_id);
    GET DIAGNOSTICS restored = ROW_COUNT;

    IF cleared <> 1 OR restored <> 1 THEN
      RAISE EXCEPTION 'откат % не сошёлся (новая %, старая %)', r.full_name, cleared, restored;
    END IF;

    UPDATE max_bind_swap_backup
    SET reverted_at = now()
    WHERE id = r.id AND reverted_at IS NULL;
    ok := ok + 1;
    RAISE NOTICE 'вернул %: снова старый номер, % отвязан', r.full_name, r.new_phone;
  END LOOP;

  RAISE NOTICE 'откат: вернули %, пропустили %', ok, skipped;
END $$;

COMMIT;

SELECT coalesce(u.full_name, '') AS manager,
       b.new_phone,
       b.reverted_at IS NOT NULL AS reverted
FROM max_bind_swap_backup b
LEFT JOIN users u ON u.id = b.manager_id
WHERE b.batch_id = (
  SELECT batch_id FROM max_bind_swap_backup ORDER BY created_at DESC LIMIT 1
)
ORDER BY u.full_name, b.new_phone;
SQL
  exit 0
fi

if [[ "$MODE" != "plan" && "$MODE" != "apply" ]]; then
  echo "режим: plan, apply или revert" >&2
  exit 1
fi
if [[ -z "$CHECK" || ! -f "$CHECK" ]]; then
  echo "нужен файл проверки: bash scripts/swap-max-accounts.sh plan /path/wappi-max-contact-check.txt" >&2
  exit 1
fi

if [[ "$MODE" == "apply" ]]; then
  pending=$(psql_docker -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='max_bind_swap_backup';" || true)
  if [[ "${pending:-0}" != "0" ]]; then
    open_n=$(psql_docker -tAc "SELECT count(*) FROM max_bind_swap_backup WHERE reverted_at IS NULL;" || echo 0)
    if [[ "${open_n:-0}" != "0" ]]; then
      echo "есть незакрытый снимок ($open_n строк). Сначала revert." >&2
      exit 1
    fi
  fi
fi

END_SQL="ROLLBACK;"
if [[ "$MODE" == "apply" ]]; then
  END_SQL="COMMIT;"
fi

{
  echo "BEGIN;"
  echo "CREATE TEMP TABLE max_check (ord int, phone text, profile text, result text) ON COMMIT DROP;"
  echo "COPY max_check (ord, phone, profile, result) FROM STDIN WITH (FORMAT text, DELIMITER E'\\t');"
  awk -F '\t' '
    $1 ~ /^[0-9]{10,15}$/ && $2 != "" && $3 != "" {
      n++
      gsub(/\r/, "", $3)
      printf "%d\t%s\t%s\t%s\n", n, $1, $2, $3
    }
  ' "$CHECK"
  echo '\.'
  cat <<SQL
CREATE TEMP TABLE swap_plan ON COMMIT DROP AS
WITH bad AS (
  SELECT DISTINCT ON (l.owner_user_id)
    l.id AS old_line_id,
    l.owner_user_id AS manager_id,
    u.full_name,
    regexp_replace(coalesce(l.operator_label, ''), '\\D', '', 'g') AS old_phone,
    l.external_ref AS old_profile,
    (
      SELECT c.result FROM max_check c
      WHERE l.external_ref = c.profile
         OR l.external_ref LIKE c.profile || '%'
      ORDER BY length(c.profile) DESC, c.ord
      LIMIT 1
    ) AS old_result
  FROM inbox_lines l
  JOIN users u ON u.id = l.owner_user_id
  JOIN roles r ON r.id = u.role_id AND r.name = 'manager'
  WHERE l.channel = 'max'
    AND l.provider = 'wappi'
    AND l.is_active
    AND l.owner_user_id IS NOT NULL
    AND regexp_replace(coalesce(l.operator_label, ''), '\\D', '', 'g') <> '79811180019'
    AND EXISTS (
      SELECT 1 FROM max_check c
      WHERE c.result <> 'успешно'
        AND (l.external_ref = c.profile OR l.external_ref LIKE c.profile || '%')
    )
    AND NOT EXISTS (
      SELECT 1 FROM max_check c
      WHERE c.result = 'успешно'
        AND (l.external_ref = c.profile OR l.external_ref LIKE c.profile || '%')
    )
  ORDER BY l.owner_user_id, regexp_replace(coalesce(l.operator_label, ''), '\\D', '', 'g')
),
free AS (
  SELECT DISTINCT ON (c.phone)
    c.phone AS new_phone,
    c.profile AS new_profile,
    c.ord
  FROM max_check c
  WHERE c.result = 'успешно'
    AND NOT EXISTS (
      SELECT 1 FROM inbox_lines l
      WHERE l.channel = 'max'
        AND l.provider = 'wappi'
        AND l.owner_user_id IS NOT NULL
        AND (
          l.external_ref = c.profile
          OR l.external_ref LIKE c.profile || '%'
          OR regexp_replace(coalesce(l.operator_label, ''), '\\D', '', 'g') = c.phone
        )
    )
  ORDER BY c.phone, c.ord
),
bad_n AS (
  SELECT b.*, row_number() OVER (ORDER BY b.full_name, b.old_phone) AS n
  FROM bad b
),
free_n AS (
  SELECT f.*, row_number() OVER (ORDER BY f.ord) AS n
  FROM free f
)
SELECT b.n, b.old_line_id, b.manager_id, b.full_name, b.old_phone, b.old_profile, b.old_result,
       f.new_phone, f.new_profile
FROM bad_n b
JOIN free_n f ON f.n = b.n
ORDER BY b.n;

SELECT
  (SELECT count(*) FROM (
    SELECT DISTINCT l.owner_user_id
    FROM inbox_lines l
    JOIN users u ON u.id = l.owner_user_id
    JOIN roles r ON r.id = u.role_id AND r.name = 'manager'
    WHERE l.channel = 'max' AND l.provider = 'wappi' AND l.is_active AND l.owner_user_id IS NOT NULL
      AND regexp_replace(coalesce(l.operator_label, ''), '\\D', '', 'g') <> '79811180019'
      AND EXISTS (
        SELECT 1 FROM max_check c
        WHERE c.result <> 'успешно'
          AND (l.external_ref = c.profile OR l.external_ref LIKE c.profile || '%')
      )
      AND NOT EXISTS (
        SELECT 1 FROM max_check c
        WHERE c.result = 'успешно'
          AND (l.external_ref = c.profile OR l.external_ref LIKE c.profile || '%')
      )
  ) s) AS managers_with_bad,
  (SELECT count(*) FROM (
    SELECT DISTINCT ON (c.phone) c.phone
    FROM max_check c
    WHERE c.result = 'успешно'
      AND NOT EXISTS (
        SELECT 1 FROM inbox_lines l
        WHERE l.channel = 'max'
          AND l.provider = 'wappi'
          AND l.owner_user_id IS NOT NULL
          AND (
            l.external_ref = c.profile
            OR l.external_ref LIKE c.profile || '%'
            OR regexp_replace(coalesce(l.operator_label, ''), '\\D', '', 'g') = c.phone
          )
      )
  ) fs) AS free_success,
  (SELECT count(*) FROM swap_plan) AS swaps;

SELECT n AS "#", full_name AS manager, old_phone AS take_away, old_result AS was, new_phone AS give
FROM swap_plan
ORDER BY n;
SQL

  if [[ "$MODE" == "apply" ]]; then
    cat <<'SQL'
CREATE TABLE IF NOT EXISTS max_bind_swap_backup (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  batch_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  manager_id uuid NOT NULL,
  old_line_id uuid NOT NULL,
  old_owner uuid NOT NULL,
  new_line_id uuid NOT NULL,
  new_phone text NOT NULL,
  new_profile text NOT NULL,
  reverted_at timestamptz
);

DO $$
DECLARE
  r record;
  new_id uuid;
  n int;
  batch uuid := gen_random_uuid();
  has_hist boolean;
BEGIN
  IF (SELECT count(*) FROM swap_plan) = 0 THEN
    RAISE EXCEPTION 'нет пар для замены';
  END IF;

  SELECT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'inbox_lines' AND column_name = 'history_from'
  ) INTO has_hist;

  FOR r IN SELECT * FROM swap_plan ORDER BY n LOOP
    UPDATE inbox_lines
    SET owner_user_id = NULL
    WHERE id = r.old_line_id
      AND owner_user_id = r.manager_id
      AND channel = 'max'
      AND provider = 'wappi'
      AND is_active;
    GET DIAGNOSTICS n = ROW_COUNT;
    IF n <> 1 THEN
      RAISE EXCEPTION 'не сняли % у %', r.old_phone, r.full_name;
    END IF;

    IF EXISTS (
      SELECT 1 FROM inbox_lines l
      WHERE l.channel = 'max'
        AND l.provider = 'wappi'
        AND (
          l.external_ref = r.new_profile
          OR l.external_ref LIKE r.new_profile || '%'
          OR regexp_replace(coalesce(l.operator_label, ''), '\D', '', 'g') = r.new_phone
        )
    ) THEN
      RAISE EXCEPTION 'успешный % уже есть в линиях, не дублируем', r.new_phone;
    END IF;

    IF has_hist THEN
      INSERT INTO inbox_lines (
        provider, channel, name, kind, operator_label, external_ref, owner_user_id, is_active, history_from
      ) VALUES (
        'wappi', 'max', 'MAX · ' || r.new_phone, 'personal', r.new_phone, r.new_profile, r.manager_id, true, now()
      )
      RETURNING id INTO new_id;
    ELSE
      INSERT INTO inbox_lines (
        provider, channel, name, kind, operator_label, external_ref, owner_user_id, is_active
      ) VALUES (
        'wappi', 'max', 'MAX · ' || r.new_phone, 'personal', r.new_phone, r.new_profile, r.manager_id, true
      )
      RETURNING id INTO new_id;
    END IF;

    INSERT INTO max_bind_swap_backup (
      batch_id, manager_id, old_line_id, old_owner, new_line_id, new_phone, new_profile
    ) VALUES (
      batch, r.manager_id, r.old_line_id, r.manager_id, new_id, r.new_phone, r.new_profile
    );
  END LOOP;
END $$;

SELECT coalesce(u.full_name, '') AS manager,
       regexp_replace(coalesce(old.operator_label, ''), '\D', '', 'g') AS old_phone_now_free,
       b.new_phone AS given,
       new.owner_user_id = b.manager_id AS given_ok,
       old.owner_user_id IS NULL AS old_cleared
FROM max_bind_swap_backup b
JOIN inbox_lines new ON new.id = b.new_line_id
JOIN inbox_lines old ON old.id = b.old_line_id
LEFT JOIN users u ON u.id = b.manager_id
WHERE b.reverted_at IS NULL
  AND b.batch_id = (SELECT batch_id FROM max_bind_swap_backup ORDER BY created_at DESC LIMIT 1)
ORDER BY u.full_name;
SQL
  fi

  echo "$END_SQL"
} | psql_docker
