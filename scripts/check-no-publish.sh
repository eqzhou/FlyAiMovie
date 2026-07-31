#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKFLOW="${1:-$ROOT/.github/workflows/supply-chain-build.yml}"

if [[ ! -f "$WORKFLOW" ]]; then
  echo "supply-chain workflow not found: $WORKFLOW" >&2
  exit 2
fi

FORBIDDEN='docker/login-action|^[[:space:]]*push:|docker[[:space:]]+push|podman[[:space:]]+push|buildx[^#]*--push|type=registry|permissions:[[:space:]]*write-all|[a-z-]+:[[:space:]]*write([[:space:],}]|$)|\$\{\{[[:space:]]*secrets\.'
if grep -Eiq -- "$FORBIDDEN" "$WORKFLOW"; then
  echo "no-publish gate rejected $WORKFLOW: login, registry push, write permission, or secret usage detected" >&2
  exit 1
fi

if ! grep -Eq 'contents:[[:space:]]*read([[:space:],}]|$)' "$WORKFLOW"; then
  echo "no-publish gate rejected $WORKFLOW: explicit contents: read permission is required" >&2
  exit 1
fi
if ! grep -Eq 'actions/upload-artifact@' "$WORKFLOW"; then
  echo "no-publish gate rejected $WORKFLOW: the local OCI archive must be uploaded as an artifact" >&2
  exit 1
fi
if ! grep -Eq 'outputs:[[:space:]]*type=oci,dest=' "$WORKFLOW"; then
  echo "no-publish gate rejected $WORKFLOW: build output must be a local OCI archive" >&2
  exit 1
fi

while IFS= read -r action; do
  if [[ ! "$action" =~ ^(actions/checkout|actions/upload-artifact|docker/setup-qemu-action|docker/setup-buildx-action|docker/build-push-action)@[0-9a-f]{40}$ ]]; then
    echo "no-publish gate rejected $WORKFLOW: action is not allowlisted: $action" >&2
    exit 1
  fi
done < <(grep -Eo 'uses:[[:space:]]*[^[:space:]},#]+' "$WORKFLOW" | sed -E 's/^uses:[[:space:]]*//')

while IFS= read -r command; do
  if [[ "$command" != "bash scripts/check-no-publish.sh" ]]; then
    echo "no-publish gate rejected $WORKFLOW: run command is not allowlisted: $command" >&2
    exit 1
  fi
done < <(sed -nE 's/^[[:space:]]*run:[[:space:]]*(.*)$/\1/p' "$WORKFLOW")

echo "No-publish gate passed: workflow has read-only permissions and no registry publication path"
