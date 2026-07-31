#!/usr/bin/env bash
set -euo pipefail

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required but was not found; install Docker with the buildx plugin" >&2
  exit 127
fi
if ! docker buildx version >/dev/null 2>&1; then
  echo "docker buildx is required but is not available" >&2
  exit 127
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="${1:-$ROOT/artifacts/flyaimovie-oci.tar}"
if [[ "$OUTPUT" != /* ]]; then
  OUTPUT="$ROOT/$OUTPUT"
fi
mkdir -p "$(dirname "$OUTPUT")"

docker buildx bake --file "$ROOT/docker-bake.hcl" \
  --set "oci.output=type=oci,dest=$OUTPUT" \
  oci

echo "Multi-architecture OCI archive written to $OUTPUT"
