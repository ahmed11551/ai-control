#!/bin/zsh
# Перезапуск локального API вне Cursor sandbox (нужен доступ к api.exolve.ru).
set -euo pipefail
cd "$(dirname "$0")"
echo "Stopping :8080…"
kill -9 $(lsof -tiTCP:8080 -sTCP:LISTEN) 2>/dev/null || true
sleep 1
echo "Building…"
go build -o /tmp/bitrix-local ./cmd/local/
echo "Starting API on :8080…"
echo "(если Postgres уже на :55432 — API его переиспользует)"
exec /tmp/bitrix-local
