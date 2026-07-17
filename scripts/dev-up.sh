#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-8088}"
RUNTIME_DIR="${FLYAIMOVIE_RUNTIME_DIR:-$HOME/.local/share/flyaimovie}"
SERVER_BIN="$RUNTIME_DIR/.run/flyaimovie-server"

mkdir -p \
  "$ROOT/.run" \
  "$RUNTIME_DIR/.run" \
  "$RUNTIME_DIR/backend/skills" \
  "$RUNTIME_DIR/configs" \
  "$RUNTIME_DIR/data/storage" \
  "$RUNTIME_DIR/frontend/dist" \
  "$RUNTIME_DIR/logs"
chmod 700 "$RUNTIME_DIR" "$RUNTIME_DIR/configs" "$RUNTIME_DIR/data" "$RUNTIME_DIR/logs"

if [[ ! -f "$ROOT/configs/config.yaml" ]]; then
  cp "$ROOT/configs/config.example.yaml" "$ROOT/configs/config.yaml"
  chmod 600 "$ROOT/configs/config.yaml"
  echo "Created local config: $ROOT/configs/config.yaml"
  echo "Set database.dsn and set auth.secure_cookies=false for local HTTP, then run this command again." >&2
  exit 2
fi

npm --prefix "$ROOT/frontend" run build

cd "$ROOT/backend"
go build -o "$ROOT/.run/flyaimovie-server.next" ./cmd/server

# PM2 launched outside an interactive terminal may not have macOS permission to
# read Documents. Stage a self-contained runtime under the user's home directory.
mv "$ROOT/.run/flyaimovie-server.next" "$SERVER_BIN"
chmod 755 "$SERVER_BIN"
cp "$ROOT/configs/config.yaml" "$RUNTIME_DIR/configs/config.yaml"
chmod 600 "$RUNTIME_DIR/configs/config.yaml"
cp -R "$ROOT/frontend/dist/." "$RUNTIME_DIR/frontend/dist/"
cp -R "$ROOT/backend/skills/." "$RUNTIME_DIR/backend/skills/"
if [[ -d "$ROOT/data/storage" ]]; then
  cp -R "$ROOT/data/storage/." "$RUNTIME_DIR/data/storage/"
fi

pm2 delete flyaimovie >/dev/null 2>&1 || true
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "port ${PORT} is already occupied; stop the existing service before starting FlyAiMovie" >&2
  lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >&2 || true
  exit 1
fi
PORT="$PORT" CONFIG_PATH="$RUNTIME_DIR/configs/config.yaml" \
  pm2 start "$SERVER_BIN" \
    --name flyaimovie \
    --cwd "$RUNTIME_DIR" \
    --interpreter none \
    --time \
    --output "$RUNTIME_DIR/logs/server.out.log" \
    --error "$RUNTIME_DIR/logs/server.error.log"

ready=0
for _ in $(seq 1 30); do
  server_pid="$(pm2 pid flyaimovie 2>/dev/null || true)"
  if [[ "$server_pid" =~ ^[1-9][0-9]*$ ]] && kill -0 "$server_pid" 2>/dev/null && \
    curl -fsS "http://127.0.0.1:${PORT}/api/v1/health" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done

if [[ "$ready" -ne 1 ]]; then
  echo "flyaimovie failed to become healthy on port ${PORT}" >&2
  pm2 logs flyaimovie --lines 80 --nostream >&2 || true
  exit 1
fi

pm2 save >/dev/null

echo "Open: http://127.0.0.1:${PORT}/"
echo "Settings: http://127.0.0.1:${PORT}/settings"
curl -fsS "http://127.0.0.1:${PORT}/api/v1/health"
echo
pm2 status flyaimovie
echo "Runtime: $RUNTIME_DIR"
