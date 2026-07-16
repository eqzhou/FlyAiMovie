#!/usr/bin/env bash
set -euo pipefail
pm2 delete flyaimovie >/dev/null 2>&1 || true
pm2 save >/dev/null 2>&1 || true
echo "flyaimovie stopped"
