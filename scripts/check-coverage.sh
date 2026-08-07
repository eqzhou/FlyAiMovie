#!/usr/bin/env bash
# Run the backend suite and enforce the same coverage floor as CI.
#
# CI fails the build below 80%, which is easy to trip by adding code without
# tests. Running this locally surfaces the breach before the push, and prints
# the least-covered functions so it is clear what to test.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
THRESHOLD="${COVERAGE_THRESHOLD:-80}"
PROFILE="${COVERAGE_PROFILE:-$ROOT/backend/coverage.out}"

cd "$ROOT/backend"
go test ./... -coverprofile="$PROFILE"

total="$(go tool cover -func="$PROFILE" | awk '/^total:/ {gsub("%", "", $3); print $3}')"
if [[ -z "$total" ]]; then
  echo "could not read total coverage from $PROFILE" >&2
  exit 1
fi

printf 'total coverage: %s%%  (floor %s%%)\n' "$total" "$THRESHOLD"

if awk -v total="$total" -v floor="$THRESHOLD" 'BEGIN { exit !(total + 0 < floor + 0) }'; then
  echo
  echo "BELOW FLOOR — CI will fail this build."
  echo "Least-covered functions:"
  go tool cover -func="$PROFILE" \
    | grep -vE '^total:|100\.0%$' \
    | awk '{ gsub("%", "", $NF); if ($NF + 0 < 70) print }' \
    | sort -t$'\t' -k3 -n \
    | head -15
  exit 1
fi

# Warn while the buffer is thin enough that one untested change breaks CI.
if awk -v total="$total" -v floor="$THRESHOLD" 'BEGIN { exit !(total + 0 < floor + 1) }'; then
  echo
  printf 'WARNING: only %.1fpp above the floor. Add tests with new code.\n' \
    "$(awk -v t="$total" -v f="$THRESHOLD" 'BEGIN { print t - f }')"
fi
