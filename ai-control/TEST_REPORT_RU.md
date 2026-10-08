# Протокол первой итерации

Статус на 05.10.2026: локальная проверка выполнена 05.10.2026 в Docker Desktop на macOS arm64. Основной CRM compose не запускался.

Проверки, предусмотренные первой итерацией:

- структура и изменённые пути: только `ai-control/` на этапе реализации;
- unit-тесты: conversation limits/roles/duplicate message IDs, mock evidence, conservative promise status, DLP/prompt-injection signals, local endpoint allowlist — `go test ./...` успешно;
- статический анализ — `go vet ./...` успешно;
- Docker build — успешно; runtime image дополнен `tzdata` для проверки IANA timezone;
- API smoke: `healthz/readyz`, upload → queued → completed → analysis, повторный импорт возвращает существующую `completed` job, конфликт того же source/version с другим JSON возвращает `409`;
- UI smoke: локальная страница отдаётся, job API возвращает `analysis_id`, страница автоматически опрашивает job и запрашивает analysis для отображения полного результата;
- semantic fixture smoke: условное «Если билет подтвердят, сообщу» распознано как `proposed/needs_data` с `condition` и evidence `m1`; пустые коллекции результата сериализуются как `[]`, а не `null`;
- local adapter contract tests: localhost HTTP endpoint принимается, server-owned metadata переписывается, unknown evidence отвергается; реальные model runtime и веса не устанавливались;
- local Ollama smoke: `qwen3:4b` через Docker → `host.docker.internal:11434` обработала synthetic conditional conversation за 5.5 s, вернула summary, promise, signal и evidence `m1`; строковый evidence нормализован, promise приведён к `proposed/needs_data`; server-owned versions заполнены (`conversation-local-v1`, `qwen3:4b`, `ollama-json-v1`, `conversation-v1`);
- local corpus: 11 валидных fixtures завершились `completed`, fixture `05-unknown-timezone` корректно отклонён на входе `422`; latency успешных inference примерно 1.3–5.7 s;
- local policy overlay: для passport request сервер добавляет `protected_document_request/DLP-C3/high`, для prompt injection — `prompt_injection/SAFE-TEXT-01/high`, даже если модель вернула более мягкую категорию;
- negative cases: invalid JSON, invalid role/timezone, oversized body/message, missing token, disallowed Origin;
- failure cases: local mode without endpoint, retry cap, worker error is persisted as safe code;
- lease recovery: test-only перевод job в `running` с истёкшим lease после чего worker восстановил её до `completed`;
- negative API smoke: missing token `401`, invalid JSON `400`, invalid role `422`, disallowed Origin `403`;
- queue/resource smoke: искусственное заполнение 100 queued jobs вернуло `429 queue_full`; retry при `attempts=3` вернул `409 retry_not_allowed`; snapshot показал около 9 MiB API и 87 MiB PostgreSQL при лимитах 512 MiB;
- restart smoke: queued job пережила restart AI-контейнера и после снятия test-only задержки завершилась `completed`;
- isolation: отдельные compose project/network/volume, PostgreSQL без host port, API только loopback, no CRM file changes.

Команды проверки:

```sh
cd ai-control
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine \
  sh -c 'gofmt -w cmd/ai-control/main.go cmd/ai-control/main_test.go && go test ./... && go vet ./...'
AI_CONTROL_TOKEN=change-this-local-token docker compose config
AI_CONTROL_TOKEN=change-this-local-token docker compose up --build
```

Docker Desktop был установлен заранее; Go на хост не устанавливался, зависимости Go были загружены в одноразовый контейнер `golang:1.23-alpine`. Проверенный стенд оставлен запущенным на `127.0.0.1:8091`; остановка: `AI_CONTROL_TOKEN=... docker compose down`.

Непроверено: disk budget under fill, formal semantic accuracy score/independent human labels и критерий «CRM продолжает работать» — локальная CRM не запускалась в рамках этой задачи, чтобы не менять и не затрагивать её процессы. ASR, готовые аудиотранскрипты, события задач/звонков и интеграция CRM не входят в текущий локальный прототип.
