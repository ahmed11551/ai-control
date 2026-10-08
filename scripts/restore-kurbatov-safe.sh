#!/usr/bin/env bash
# Вернуть Курбатову лиды после отзыва в банк РОПа,
# у которых стадия НЕ «Новый лид» (чат / в работе / недозвон / …).
# Меняет ТОЛЬКО owner_id. Статус и стадию не трогает.
#
#   bash scripts/restore-kurbatov-safe.sh           # dry-run
#   bash scripts/restore-kurbatov-safe.sh apply     # применить
set -euo pipefail

ROOT="${HOME}/crm"
cd "$ROOT"

MODE="${1:-dry-run}"
NAME_FRAG="${2:-Курбатов}"
SINCE="${3:-2026-09-22}"
MIN_N="${4:-1}"
MAX_N="${5:-200}"

# Стадия «Новый лид / Новая заявка» в воронке Адаптация
NEW_STAGE_KEY="local:hire:1"

sql_str() {
  local s=$1
  s=${s//\'/\'\'}
  printf "'%s'" "$s"
}

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
    psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" "$@"
}

NAME_SQL=$(sql_str "$NAME_FRAG")
SINCE_SQL=$(sql_str "${SINCE} 00:00:00")
NEW_KEY_SQL=$(sql_str "$NEW_STAGE_KEY")

# Есть сделка на стадии ≠ Новый лид
NOT_NEW_STAGE="EXISTS (
  SELECT 1 FROM deals d
  JOIN pipeline_stages s ON s.id = d.stage_id
  WHERE (d.lead_id = l.id OR d.id = l.converted_deal_id)
    AND coalesce(s.bitrix_id,'') <> ${NEW_KEY_SQL}
)"

echo "=== менеджер ==="
MGR=$(psql_docker -tAc "
SELECT id::text || '|' || full_name
FROM users
WHERE full_name ILIKE '%' || ${NAME_SQL} || '%'
  AND is_active IS DISTINCT FROM false
ORDER BY CASE WHEN full_name ILIKE ${NAME_SQL} || '%' THEN 0 ELSE 1 END, full_name
LIMIT 1;")
if [[ -z "$MGR" ]]; then
  echo "менеджер не найден: $NAME_FRAG" >&2
  exit 1
fi
MGR_ID="${MGR%%|*}"
MGR_NAME="${MGR#*|}"
MGR_ID_SQL=$(sql_str "$MGR_ID")
MGR_NAME_SQL=$(sql_str "$MGR_NAME")
echo "  $MGR_NAME ($MGR_ID)"
echo "  фильтр: отозваны с $SINCE + стадия ≠ «Новый лид»"
echo "  mode=$MODE  apply-window=$MIN_N…$MAX_N"
echo

echo "=== сводка по стадиям (кандидаты) ==="
psql_docker <<SQL
SELECT coalesce(st.stage, '(нет стадии)') AS stage, count(*) AS n
FROM (
  SELECT DISTINCT ON (a.entity_id) a.entity_id AS lead_id
  FROM activity_log a
  WHERE a.entity_type = 'lead'
    AND a.action = 'recalled_to_rop_bank'
    AND a.created_at >= ${SINCE_SQL}::timestamptz
    AND coalesce(a.payload->>'from_manager','') = ${MGR_ID_SQL}
  ORDER BY a.entity_id, a.created_at DESC
) r
JOIN leads l ON l.id = r.lead_id
LEFT JOIN LATERAL (
  SELECT coalesce(nullif(ps.short_name,''), ps.name) AS stage
  FROM deals d
  JOIN pipeline_stages ps ON ps.id = d.stage_id
  WHERE d.lead_id = l.id OR d.id = l.converted_deal_id
  ORDER BY d.updated_at DESC NULLS LAST
  LIMIT 1
) st ON true
WHERE l.owner_id IS DISTINCT FROM ${MGR_ID_SQL}::uuid
  AND ${NOT_NEW_STAGE}
GROUP BY 1
ORDER BY n DESC;
SQL

echo
echo "=== список (стадия ≠ Новый лид) ==="
psql_docker <<SQL
\\pset format aligned
\\pset border 1
WITH recalls AS (
  SELECT DISTINCT ON (a.entity_id)
    a.entity_id AS lead_id,
    a.created_at AS recalled_at
  FROM activity_log a
  WHERE a.entity_type = 'lead'
    AND a.action = 'recalled_to_rop_bank'
    AND a.created_at >= ${SINCE_SQL}::timestamptz
    AND coalesce(a.payload->>'from_manager','') = ${MGR_ID_SQL}
  ORDER BY a.entity_id, a.created_at DESC
)
SELECT l.id::text AS lead_id,
       left(coalesce(l.title,''), 44) AS title,
       coalesce(st.stage,'(нет)') AS stage,
       coalesce(u.full_name,'(нет)') AS current_owner,
       to_char(r.recalled_at, 'DD.MM HH24:MI') AS recalled
FROM recalls r
JOIN leads l ON l.id = r.lead_id
LEFT JOIN users u ON u.id = l.owner_id
LEFT JOIN LATERAL (
  SELECT coalesce(nullif(ps.short_name,''), ps.name) AS stage,
         coalesce(ps.bitrix_id,'') AS bitrix_id
  FROM deals d
  JOIN pipeline_stages ps ON ps.id = d.stage_id
  WHERE d.lead_id = l.id OR d.id = l.converted_deal_id
  ORDER BY d.updated_at DESC NULLS LAST
  LIMIT 1
) st ON true
WHERE l.owner_id IS DISTINCT FROM ${MGR_ID_SQL}::uuid
  AND ${NOT_NEW_STAGE}
ORDER BY r.recalled_at DESC
LIMIT 150;
SQL

CNT=$(psql_docker -tAc "
WITH recalls AS (
  SELECT DISTINCT ON (a.entity_id) a.entity_id AS lead_id
  FROM activity_log a
  WHERE a.entity_type = 'lead'
    AND a.action = 'recalled_to_rop_bank'
    AND a.created_at >= ${SINCE_SQL}::timestamptz
    AND coalesce(a.payload->>'from_manager','') = ${MGR_ID_SQL}
  ORDER BY a.entity_id, a.created_at DESC
)
SELECT count(*)
FROM recalls r
JOIN leads l ON l.id = r.lead_id
WHERE l.owner_id IS DISTINCT FROM ${MGR_ID_SQL}::uuid
  AND ${NOT_NEW_STAGE};")
CNT=$(echo "$CNT" | tr -d '[:space:]')
echo
echo "Итого к возврату (не «Новый лид»): $CNT"

# сколько остаётся на Новом лиде — не трогаем
LEFT_NEW=$(psql_docker -tAc "
WITH recalls AS (
  SELECT DISTINCT ON (a.entity_id) a.entity_id AS lead_id
  FROM activity_log a
  WHERE a.entity_type = 'lead'
    AND a.action = 'recalled_to_rop_bank'
    AND a.created_at >= ${SINCE_SQL}::timestamptz
    AND coalesce(a.payload->>'from_manager','') = ${MGR_ID_SQL}
  ORDER BY a.entity_id, a.created_at DESC
)
SELECT count(*)
FROM recalls r
JOIN leads l ON l.id = r.lead_id
WHERE l.owner_id IS DISTINCT FROM ${MGR_ID_SQL}::uuid
  AND NOT (${NOT_NEW_STAGE});")
echo "Останутся у текущего владельца (Новый лид / без сделки): $(echo "$LEFT_NEW" | tr -d '[:space:]')"

if [[ "$MODE" != "apply" ]]; then
  echo
  echo "DRY-RUN. Ничего не записано."
  echo "Если сводка ок:"
  echo "  bash scripts/restore-kurbatov-safe.sh apply"
  exit 0
fi

if [[ "$CNT" -lt "$MIN_N" || "$CNT" -gt "$MAX_N" ]]; then
  echo "ABORT: $CNT вне окна $MIN_N…$MAX_N — без изменений." >&2
  exit 1
fi

echo
echo "=== APPLY: только owner_id ==="
psql_docker <<SQL
BEGIN;

CREATE TEMP TABLE _restore_ids ON COMMIT DROP AS
WITH recalls AS (
  SELECT DISTINCT ON (a.entity_id)
    a.entity_id AS lead_id,
    coalesce(l.status,'') AS status_before,
    coalesce(l.owner_id::text,'') AS from_owner
  FROM activity_log a
  JOIN leads l ON l.id = a.entity_id
  WHERE a.entity_type = 'lead'
    AND a.action = 'recalled_to_rop_bank'
    AND a.created_at >= ${SINCE_SQL}::timestamptz
    AND coalesce(a.payload->>'from_manager','') = ${MGR_ID_SQL}
    AND l.owner_id IS DISTINCT FROM ${MGR_ID_SQL}::uuid
    AND ${NOT_NEW_STAGE}
  ORDER BY a.entity_id, a.created_at DESC
)
SELECT * FROM recalls;

CREATE TEMP TABLE _status_snap ON COMMIT DROP AS
SELECT l.id, coalesce(l.status,'') AS status
FROM leads l
JOIN _restore_ids r ON r.lead_id = l.id;

CREATE TEMP TABLE _stage_snap ON COMMIT DROP AS
SELECT l.id,
       (SELECT d.stage_id FROM deals d
        WHERE d.lead_id = l.id OR d.id = l.converted_deal_id
        ORDER BY d.updated_at DESC NULLS LAST LIMIT 1) AS stage_id
FROM leads l
JOIN _restore_ids r ON r.lead_id = l.id;

UPDATE leads l
SET owner_id = ${MGR_ID_SQL}::uuid,
    updated_at = now()
FROM _restore_ids r
WHERE l.id = r.lead_id
  AND l.owner_id IS DISTINCT FROM ${MGR_ID_SQL}::uuid
  AND coalesce(l.status,'') = r.status_before;

UPDATE deals d
SET owner_id = ${MGR_ID_SQL}::uuid,
    updated_at = now()
WHERE d.lead_id IN (SELECT lead_id FROM _restore_ids)
   OR d.id IN (
     SELECT converted_deal_id FROM leads
     WHERE id IN (SELECT lead_id FROM _restore_ids)
       AND converted_deal_id IS NOT NULL
   );

DO \$\$
DECLARE bad_status int;
DECLARE bad_stage int;
BEGIN
  SELECT count(*) INTO bad_status
  FROM leads l
  JOIN _status_snap s ON s.id = l.id
  WHERE coalesce(l.status,'') IS DISTINCT FROM s.status;
  IF bad_status > 0 THEN
    RAISE EXCEPTION 'ABORT: % leads changed status — rolling back', bad_status;
  END IF;

  SELECT count(*) INTO bad_stage
  FROM leads l
  JOIN _stage_snap s ON s.id = l.id
  WHERE (SELECT d.stage_id FROM deals d
         WHERE d.lead_id = l.id OR d.id = l.converted_deal_id
         ORDER BY d.updated_at DESC NULLS LAST LIMIT 1)
        IS DISTINCT FROM s.stage_id;
  IF bad_stage > 0 THEN
    RAISE EXCEPTION 'ABORT: % leads changed stage — rolling back', bad_stage;
  END IF;
END \$\$;

INSERT INTO activity_log (entity_type, entity_id, actor_id, action, payload)
SELECT 'lead', r.lead_id, NULL, 'restored_owner',
       jsonb_build_object(
         'from_owner_id', r.from_owner,
         'to_owner_id', ${MGR_ID_SQL},
         'to_owner_name', ${MGR_NAME_SQL},
         'reason', 'restore_after_rop_bank_recall_not_new_stage',
         'note', 'status/stage not changed; skipped Новый лид'
       )
FROM _restore_ids r;

SELECT count(*) AS restored FROM _restore_ids;

COMMIT;
SQL

echo
LEFT=$(psql_docker -tAc "
WITH recalls AS (
  SELECT DISTINCT ON (a.entity_id) a.entity_id AS lead_id
  FROM activity_log a
  WHERE a.entity_type = 'lead'
    AND a.action = 'recalled_to_rop_bank'
    AND a.created_at >= ${SINCE_SQL}::timestamptz
    AND coalesce(a.payload->>'from_manager','') = ${MGR_ID_SQL}
  ORDER BY a.entity_id, a.created_at DESC
)
SELECT count(*)
FROM recalls r
JOIN leads l ON l.id = r.lead_id
WHERE l.owner_id IS DISTINCT FROM ${MGR_ID_SQL}::uuid
  AND ${NOT_NEW_STAGE};")
echo "осталось чужих (не Новый лид): $(echo "$LEFT" | tr -d '[:space:]') (ожидается 0)"
echo "Готово. У Курбатова в канбане: фильтр «Все», смотреть все стадии кроме пустого «Новый лид»."
