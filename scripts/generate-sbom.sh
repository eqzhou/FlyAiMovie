#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/artifacts/sbom}"
mkdir -p "$OUT"

{
  echo "# FlyAiMovie SBOM inventory"
  echo
  echo "Generated at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "Host: $(uname -a)"
  echo
  echo "## Backend Go modules"
} > "$OUT/README.md"

(
  cd "$ROOT/backend"
  go list -m -json all > "$OUT/go-modules.json"
  go list -m all > "$OUT/go-modules.txt"
)

(
  cd "$ROOT/frontend"
  if [[ ! -d node_modules ]]; then
    npm ci --ignore-scripts
  fi
  npm ls --all --json > "$OUT/npm-dependency-tree.json" || true
  npm ls --all > "$OUT/npm-dependency-tree.txt" || true
)

if command -v ffmpeg >/dev/null 2>&1; then
  ffmpeg -version > "$OUT/ffmpeg-version.txt" 2>&1 || true
  ffmpeg -buildconf > "$OUT/ffmpeg-buildconf.txt" 2>&1 || true
  ffmpeg -L > "$OUT/ffmpeg-license.txt" 2>&1 || true
else
  echo "ffmpeg not installed on this host" > "$OUT/ffmpeg-version.txt"
fi

{
  echo
  echo "Files:"
  find "$OUT" -maxdepth 1 -type f | sort | sed "s|$OUT/||"
  echo
  echo "This inventory supports commercial release review. It is not a legal opinion."
  echo "Archive model/voice/portrait licenses separately before public distribution."
} >> "$OUT/README.md"

echo "SBOM inventory written to $OUT"
