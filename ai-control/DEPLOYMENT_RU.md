# Развёртывание AI Control

## Рекомендованная схема

На первом этапе AI Control и его PostgreSQL можно разместить на том же сервере, что и CRM, но в отдельном Compose-проекте и отдельном volume. Это уменьшает сетевую задержку и упрощает резервное копирование.

Ollama не нужно смешивать с CRM-контейнерами. Если `qwen3:4b` работает на CPU медленно или сервер ограничен по памяти, Ollama выносится на отдельный сервер с GPU; в AI Control задаётся приватный URL Ollama.

## Переменные сервера

Создать `.env` на сервере рядом с `docker-compose.yml` и задать:

```env
AI_CONTROL_TOKEN=<длинный-случайный-токен>
AI_MODEL_MODE=local
AI_LOCAL_MODEL_URL=http://ollama.internal:11434/api/chat
AI_LOCAL_MODEL_NAME=qwen3:4b
```

Тот же `AI_CONTROL_TOKEN` должен быть задан только в backend CRM. В браузер и frontend токен не передаётся.

## Запуск

```sh
cd /opt/bitrix-iso-main/ai-control
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:8091/healthz
curl -fsS http://127.0.0.1:8091/readyz
```

Для CRM в `.env` backend указать URL AI Control, доступный с сервера CRM:

```env
AI_CONTROL_URL=http://127.0.0.1:8091
AI_CONTROL_TOKEN=<тот-же-токен>
```

Если CRM и AI Control находятся на разных серверах, использовать приватный IP/VPN-адрес AI Control и разрешить порт `8091` только от IP CRM. Не открывать PostgreSQL наружу.

## Ollama

На сервере Ollama:

```sh
ollama pull qwen3:4b
ollama list
```

Проверить доступность из контейнера AI Control:

```sh
docker compose exec ai-control wget -qO- http://ollama.internal:11434/api/tags
```

## Обновление и откат

```sh
git pull
docker compose up -d --build
docker compose logs --tail=100 ai-control
```

Перед обновлением сохранить PostgreSQL volume/дамп. Не использовать `docker compose down -v` и `docker system prune` на рабочем сервере.

## Минимальный smoke-тест после релиза

1. `healthz` отвечает `200`.
2. `readyz` показывает `status=ready`.
3. CRM создаёт событие в outbox.
4. В AI Control появляется событие через `GET /v1/events`.
5. Finding появляется в очереди `/ai-control`.
6. После исправления карточки finding переходит в `resolved`.
