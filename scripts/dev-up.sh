#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-8088}"
mkdir -p "$ROOT/.run" "$ROOT/data/storage" "$ROOT/logs"

# ensure frontend dist
if [[ ! -f "$ROOT/frontend/dist/index.html" ]]; then
  npm --prefix "$ROOT/frontend" run build
fi

# build backend binary
cd "$ROOT/backend"
go build -o "$ROOT/.run/flyaimovie-server" ./cmd/server

# ensure wrapper/ecosystem exist (idempotent)
if [[ ! -f "$ROOT/.run/pm2-start.js" ]]; then
  echo "missing .run/pm2-start.js"; exit 1
fi

# restart via pm2
pm2 delete flyaimovie >/dev/null 2>&1 || true
PORT="$PORT" pm2 start "$ROOT/ecosystem.config.cjs"
pm2 save
sleep 1
echo "Open: http://127.0.0.1:${PORT}/"
echo "Settings: http://127.0.0.1:${PORT}/settings"
curl -s "http://127.0.0.1:${PORT}/api/v1/health" || true
echo
pm2 status flyaimovie
