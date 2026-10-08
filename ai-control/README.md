# ai-control — локальный AI-контроль

Изолированный сервис первой итерации из `docs/ai/START_HERE_RU.md`. Сервис принимает только синтетические JSON-переписки, ставит durable job в отдельный PostgreSQL и обрабатывает её mock/local-адаптером. Hermes, рабочая БД, Redis, MinIO, телефония, сообщения и облачные модели не используются; CRM обращается к нему только через отдельный read-only proxy для администратора.

## Раздел в CRM

После запуска AI Control и основной CRM администратор открывает пункт меню **AI Control** в своей учётной записи (`/ai-control`). Раздел показывает состояние локального сервиса, последние jobs и завершённые анализы.

Браузер не получает `AI_CONTROL_TOKEN`: backend CRM проксирует только read-only запросы и проверяет роль `admin`. В основном Compose уже заданы локальные значения по умолчанию:

```text
AI_CONTROL_URL=http://host.docker.internal:8091
AI_CONTROL_TOKEN=local-test-token
```

Если у AI Control выбран другой токен, задайте такой же `AI_CONTROL_TOKEN` в `.env` основной CRM. Если AI Control остановлен, CRM продолжает работать, а раздел показывает «AI Control недоступен».

## Запуск

Требования: Docker Compose и свободный `127.0.0.1:8091`. PostgreSQL порт наружу не публикуется, Compose project и network имеют имя `crm-ai-local`, restart policy отключена.

```sh
cd ai-control
cp .env.example .env
# замените значение AI_CONTROL_TOKEN на локальное значение
AI_CONTROL_TOKEN=change-this-local-token docker compose up --build
```

Открыть `http://127.0.0.1:8091/`, вставить тот же токен и загрузить JSON fixture или готовый `.txt`-транскрипт. Для TXT поддерживаются строки `RFC3339 | author_role | text` и экспорт `RFC3339 speaker: text` (также с timestamp в квадратных скобках); роли `manager/менеджер` и `candidate/кандидат` нормализуются. Пример: `2026-10-05T10:00:00Z | manager | Перезвоню завтра.`. API требует `X-AI-Token`; токен не передаётся в URL. Остановка только этого стенда:

```sh
docker compose down
```

Для восстановления незавершённых jobs достаточно снова выполнить `docker compose up`: jobs в `queued` будут обработаны, а истёкший lease повторно подхватит worker. Не использовать команды `docker compose down -v`, `docker system prune` или общий `docker stop`.

Инструкция для рабочего сервера: [DEPLOYMENT_RU.md](./DEPLOYMENT_RU.md).

## API

`GET /healthz`, `GET /readyz`, `POST /v1/conversations`, `POST /v1/transcripts`, `POST /v1/events`, `GET /v1/events`, `POST /v1/control/evaluate`, `GET /v1/findings`, `POST /v1/findings/{id}/review`, `GET /v1/findings/{id}/reviews`, `GET /v1/jobs`, `GET /v1/jobs/{id}`, `GET /v1/analyses/{id}`, `POST /v1/jobs/{id}/retry`.

`POST /v1/events` принимает события `lead.*`, `task.*`, `call.*`, `deal.*` и `employee.activity` с обязательными версиями aggregate, UTC-временем, trace/correlation IDs, классом данных и JSON payload. События сохраняются durable, digest вычисляется сервером (`sha256:`), повтор того же `event_id` идемпотентен, а другой payload с тем же ID отклоняется как `409 event_conflict`. События пока только импортируются и перечисляются; автоматические действия в CRM не выполняются.

`POST /v1/control/evaluate` выполняет детерминированные правила без LLM: `PROC-TASK-OVERDUE`, `PROC-TASK-UNASSIGNED`, `PROC-TASK-NO-DEADLINE`, `PROC-LEAD-NEXT-ACTION`, `PROC-CALL-RECORDING` и `PROC-EMP-SECURITY-EVENT`. Каждый finding содержит rule ID и `evidence_event_ids`; создание задачи, изменение лида, звонок или другое действие не выполняется автоматически.

Findings сохраняются в durable review queue со статусом `open`, `confirmed`, `dismissed` или `resolved`. Повторная оценка обновляет evidence существующего finding по стабильному ключу правила и агрегата, не создавая дубликат; устранённые автоматически findings получают `resolved`, а при повторном нарушении возвращаются в `open`.

Очередь поддерживает фильтры `?state=open`, `?severity=high` и `?aggregate_type=task`. Каждое ручное подтверждение или отклонение добавляется в append-only историю, доступную через `/v1/findings/{id}/reviews`.

Mock-результат явно помечен `mode=mock`, `model_version=mock-fixture-v1`. Promise имеет `proposed/needs_data`: извлечение не доказывает срок или просрочку. Каждая promise/action/signal ссылается только на `message_id` из принятой переписки. `local` mode не включается автоматически: нужен явно заданный localhost endpoint; для Ollama можно использовать `AI_LOCAL_MODEL_URL=http://host.docker.internal:11434/api/chat` и `AI_LOCAL_MODEL_NAME=qwen3:4b`. Timeout, недоступность или невалидный ответ возвращаются как safe error, без fallback в облако.

## Проверки

```sh
go test ./...
go vet ./...
```

Для end-to-end проверки работающего Docker-стека:

```sh
AI_CONTROL_E2E_TOKEN="$AI_CONTROL_TOKEN" \
AI_CONTROL_E2E_URL=http://127.0.0.1:8091 \
go test ./cmd/ai-control -run TestControlLifecycleE2E -count=1
```

Тест проверяет принятие события, автоматическое создание finding, идемпотентную повторную доставку и переход `open -> resolved`.

12 synthetic fixtures находятся в `fixtures/`. Их классы: normal, callback, conditional, relative date, timezone/needs-data, candidate words, privacy warning, protected request, unknown author, prompt injection, no finding/evidence, repeat import.

## Ресурсные границы

API/worker ограничены 1 CPU и 512 MiB, PostgreSQL — 0.5 CPU и 512 MiB; worker concurrency равна 1, connection pool pgx использует стандартный пул с малым числом idle connections, HTTP body — 1 MiB, conversation — 200 messages / 8,000 chars per message / 100,000 chars total, попытка — 60 секунд с максимумом 2 автоматических retry. У сервиса нет произвольного outbound networking: local adapter принимает только `http://localhost`, `127.0.0.1` или `::1`.

В этой малой реализации хранение идёт в отдельном Postgres volume; прикладной disk quota и полноценный lease-recovery integration test требуют отдельного ресурсного прогона Docker и отмечены в test report как остаток.
