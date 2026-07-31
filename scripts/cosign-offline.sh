#!/usr/bin/env bash
set -euo pipefail

if ! command -v cosign >/dev/null 2>&1; then
  echo "cosign is required but was not found; install cosign for optional offline signing" >&2
  exit 127
fi

usage() {
  cat >&2 <<'EOF'
Usage:
  scripts/cosign-offline.sh sign   <oci-archive> <private-key> [signature]
  scripts/cosign-offline.sh verify <oci-archive> <public-key>  [signature]

The OCI archive is signed as an immutable blob. No registry or transparency-log
connection is used. Keep the archive, signature, and public key together.
EOF
  exit 2
}

MODE="${1:-}"
ARTIFACT="${2:-}"
KEY="${3:-}"
SIGNATURE="${4:-${ARTIFACT}.sig}"
[[ "$MODE" == "sign" || "$MODE" == "verify" ]] || usage
[[ -n "$ARTIFACT" && -n "$KEY" ]] || usage
[[ -f "$ARTIFACT" ]] || { echo "OCI archive not found: $ARTIFACT" >&2; exit 2; }
[[ -f "$KEY" ]] || { echo "cosign key not found: $KEY" >&2; exit 2; }

case "$MODE" in
  sign)
    cosign sign-blob \
      --key "$KEY" \
      --tlog-upload=false \
      --output-signature "$SIGNATURE" \
      "$ARTIFACT"
    echo "Offline signature written to $SIGNATURE"
    ;;
  verify)
    [[ -f "$SIGNATURE" ]] || { echo "signature not found: $SIGNATURE" >&2; exit 2; }
    cosign verify-blob \
      --key "$KEY" \
      --signature "$SIGNATURE" \
      --insecure-ignore-tlog \
      "$ARTIFACT"
    ;;
esac
