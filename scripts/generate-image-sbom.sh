#!/usr/bin/env bash
set -euo pipefail

if ! command -v syft >/dev/null 2>&1; then
  echo "syft is required but was not found; install syft to generate the image SBOM" >&2
  exit 127
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OCI_ARCHIVE="${1:-$ROOT/artifacts/flyaimovie-oci.tar}"
OUTPUT_DIR="${2:-$ROOT/artifacts/sbom}"
if [[ "$OCI_ARCHIVE" != /* ]]; then
  OCI_ARCHIVE="$ROOT/$OCI_ARCHIVE"
fi
if [[ "$OUTPUT_DIR" != /* ]]; then
  OUTPUT_DIR="$ROOT/$OUTPUT_DIR"
fi
if [[ ! -f "$OCI_ARCHIVE" ]]; then
  echo "OCI archive not found: $OCI_ARCHIVE (run scripts/build-oci.sh first)" >&2
  exit 2
fi
mkdir -p "$OUTPUT_DIR"

scan_platform() {
  local platform="$1"
  local suffix="${platform//\//-}"
  local output="$OUTPUT_DIR/image-${suffix}.spdx.json"

  syft "oci-archive:$OCI_ARCHIVE" \
    --platform "$platform" \
    --output "spdx-json=$output"
  echo "SPDX JSON image SBOM written to $output"
}

scan_platform linux/amd64
scan_platform linux/arm64
