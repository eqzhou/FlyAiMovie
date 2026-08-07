#!/usr/bin/env bash
# Report whether the running PM2 service matches this checkout.
#
# PM2 runs a copy staged under ~/.local/share/flyaimovie, so `pm2 restart`
# reruns the already-staged binary. That makes a stale deploy easy to miss;
# this compares the revision reported by /api/v1/health against local HEAD.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-8088}"

head_rev="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
dirty=""
if ! git -C "$ROOT" diff --quiet HEAD 2>/dev/null; then
  dirty="-dirty"
fi
expected="${head_rev}${dirty}"

health="$(curl -fsS "http://127.0.0.1:${PORT}/api/v1/health" 2>/dev/null || true)"
if [[ -z "$health" ]]; then
  echo "service not reachable on port ${PORT}"
  echo "  start it with: make deploy"
  exit 1
fi

# Avoid a jq dependency; the field is a plain string.
running="$(printf '%s' "$health" | sed -n 's/.*"revision":"\([^"]*\)".*/\1/p')"
[[ -n "$running" ]] || running="unknown"

echo "local HEAD:       $expected"
echo "running revision: $running"

if [[ "$running" == "unknown" ]]; then
  echo
  echo "STALE: the running binary predates revision reporting."
  echo "  redeploy with: make deploy"
  exit 1
fi

if [[ "$running" != "$expected" ]]; then
  echo
  echo "STALE: the running service is not this checkout."
  echo "  redeploy with: make deploy   (pm2 restart alone will not pick up new code)"
  exit 1
fi

if [[ -n "$dirty" ]]; then
  echo
  echo "IN SYNC, but the working tree has uncommitted changes."
  exit 0
fi

echo
echo "IN SYNC"
