#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FAILURES=0

pass() {
  printf 'ok - %s\n' "$1"
}

fail() {
  printf 'not ok - %s\n' "$1" >&2
  FAILURES=$((FAILURES + 1))
}

assert_file() {
  local path="$1"
  if [[ -f "$ROOT/$path" ]]; then
    pass "$path exists"
  else
    fail "$path exists"
  fi
}

assert_contains() {
  local path="$1"
  local pattern="$2"
  local description="$3"
  if [[ -f "$ROOT/$path" ]] && grep -Eq -- "$pattern" "$ROOT/$path"; then
    pass "$description"
  else
    fail "$description"
  fi
}

assert_not_contains() {
  local path="$1"
  local pattern="$2"
  local description="$3"
  if [[ -f "$ROOT/$path" ]] && ! grep -Eiq -- "$pattern" "$ROOT/$path"; then
    pass "$description"
  else
    fail "$description"
  fi
}

assert_missing_tool_message() {
  local script="$1"
  local tool="$2"
  local output status

  set +e
  output="$(PATH=/nonexistent /bin/bash "$ROOT/$script" 2>&1)"
  status=$?
  set -e

  if [[ $status -eq 127 ]] && grep -Eqi "${tool}.*(required|not found|missing|install)" <<<"$output"; then
    pass "$script explains the missing $tool dependency"
  else
    fail "$script explains the missing $tool dependency"
  fi
}

assert_gate_rejects_unapproved_action() {
  local fixture output
  fixture="$(mktemp)"
  cat >"$fixture" <<'EOF'
permissions:
  contents: read
jobs:
  build:
    steps:
      - name: Hidden untrusted publisher
        uses: untrusted/registry-publisher@0123456789012345678901234567890123456789
      - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
        with:
          path: /tmp/image.tar
      - uses: docker/build-push-action@10e90e3645eae34f1e60eeb005ba3a3d33f178e8
        with:
          outputs: type=oci,dest=/tmp/image.tar
EOF
  if output="$(/bin/bash "$ROOT/scripts/check-no-publish.sh" "$fixture" 2>&1)"; then
    fail 'no-publish gate rejects unapproved third-party actions'
  elif grep -Fq 'action is not allowlisted' <<<"$output"; then
    pass 'no-publish gate rejects unapproved third-party actions'
  else
    fail 'no-publish gate rejects unapproved third-party actions'
  fi
  rm -f "$fixture"
}

assert_gate_rejects_inline_write_permission() {
  local fixture output
  fixture="$(mktemp)"
  cat >"$fixture" <<'EOF'
permissions: {contents: read, packages: write}
jobs:
  build:
    steps:
      - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
        with:
          path: /tmp/image.tar
      - uses: docker/build-push-action@10e90e3645eae34f1e60eeb005ba3a3d33f178e8
        with:
          outputs: type=oci,dest=/tmp/image.tar
EOF
  if output="$(/bin/bash "$ROOT/scripts/check-no-publish.sh" "$fixture" 2>&1)"; then
    fail 'no-publish gate rejects inline write permissions'
  elif grep -Fq 'write permission' <<<"$output"; then
    pass 'no-publish gate rejects inline write permissions'
  else
    fail 'no-publish gate rejects inline write permissions'
  fi
  rm -f "$fixture"
}

assert_file docker-bake.hcl
assert_contains docker-bake.hcl 'linux/amd64' 'bake config targets linux/amd64'
assert_contains docker-bake.hcl 'linux/arm64' 'bake config targets linux/arm64'
assert_contains docker-bake.hcl 'type[[:space:]]*=[[:space:]]*"oci"|type=oci' 'bake config writes a local OCI archive'

assert_file .dockerignore
for ignored in '.git' 'frontend/node_modules' 'artifacts' 'data'; do
  assert_contains .dockerignore "^${ignored//./\\.}(/|$)" ".dockerignore excludes $ignored"
done

assert_contains Dockerfile '^# syntax=docker/dockerfile:' 'Dockerfile opts into BuildKit syntax'
assert_contains Dockerfile '--mount=type=cache' 'Dockerfile uses build caches'
assert_contains Dockerfile '^USER[[:space:]]+[^[:space:]]+' 'runtime image declares a non-root user'
assert_contains Dockerfile 'TARGETARCH|TARGETPLATFORM' 'Dockerfile is aware of the target architecture'
assert_contains Dockerfile '^FROM[[:space:]]+node:20-slim@sha256:[0-9a-f]{64}[[:space:]]+AS[[:space:]]+frontend-build$' 'Node builder image is pinned by digest'
assert_contains Dockerfile '^FROM[[:space:]]+golang:1\.26-bookworm@sha256:[0-9a-f]{64}[[:space:]]+AS[[:space:]]+backend-build$' 'Go builder image is pinned by digest'
assert_contains Dockerfile '^FROM[[:space:]]+debian:bookworm-slim@sha256:[0-9a-f]{64}$' 'runtime image is pinned by digest'

for script in scripts/build-oci.sh scripts/generate-image-sbom.sh scripts/cosign-offline.sh scripts/check-no-publish.sh; do
  assert_file "$script"
done
assert_contains scripts/generate-image-sbom.sh 'syft' 'image SBOM generation uses syft'
assert_contains scripts/generate-image-sbom.sh 'spdx-json|cyclonedx-json' 'image SBOM has a standard machine-readable format'
assert_contains scripts/generate-image-sbom.sh '--platform' 'image SBOM selects platforms explicitly'
assert_contains scripts/generate-image-sbom.sh 'linux/amd64' 'image SBOM explicitly scans amd64'
assert_contains scripts/generate-image-sbom.sh 'linux/arm64' 'image SBOM explicitly scans arm64'
assert_contains scripts/cosign-offline.sh 'sign-blob' 'offline signing signs the immutable OCI archive'
assert_contains scripts/cosign-offline.sh 'verify-blob' 'offline verification verifies the immutable OCI archive'
assert_contains scripts/cosign-offline.sh 'tlog-upload=false|insecure-ignore-tlog' 'cosign is explicitly offline'

assert_file .github/workflows/supply-chain-build.yml
assert_contains .github/workflows/supply-chain-build.yml 'linux/amd64' 'supply-chain workflow builds amd64'
assert_contains .github/workflows/supply-chain-build.yml 'linux/arm64' 'supply-chain workflow builds arm64'
assert_contains .github/workflows/supply-chain-build.yml 'actions/upload-artifact@' 'supply-chain workflow uploads a build artifact'
assert_not_contains .github/workflows/supply-chain-build.yml 'uses:[[:space:]]*[^[:space:]#]+@v[0-9]+' 'workflow actions are not pinned to mutable version tags'
assert_contains .github/workflows/supply-chain-build.yml 'uses:[[:space:]]*actions/checkout@[0-9a-f]{40}([[:space:]]|$)' 'checkout action is pinned to a full commit SHA'
assert_contains .github/workflows/supply-chain-build.yml 'uses:[[:space:]]*actions/upload-artifact@[0-9a-f]{40}([[:space:]]|$)' 'artifact upload action is pinned to a full commit SHA'
for build_path in 'backend/\*\*' 'frontend/\*\*' 'configs/\*\*'; do
  assert_contains .github/workflows/supply-chain-build.yml "^[[:space:]]*-[[:space:]]*${build_path}[[:space:]]*$" "workflow watches Docker build context path ${build_path//\\/}"
done
assert_not_contains .github/workflows/supply-chain-build.yml 'docker/login-action|(^|[[:space:]])push:[[:space:]]*true|docker push|packages:[[:space:]]*write|id-token:[[:space:]]*write' 'supply-chain workflow cannot log in, push, or request publishing permissions'

if [[ -f "$ROOT/scripts/check-no-publish.sh" ]]; then
  if /bin/bash "$ROOT/scripts/check-no-publish.sh"; then
    pass 'no-publish static gate accepts the repository workflow'
  else
    fail 'no-publish static gate accepts the repository workflow'
  fi
  assert_gate_rejects_unapproved_action
  assert_gate_rejects_inline_write_permission
fi

assert_missing_tool_message scripts/build-oci.sh docker
assert_missing_tool_message scripts/generate-image-sbom.sh syft
assert_missing_tool_message scripts/cosign-offline.sh cosign

if ((FAILURES > 0)); then
  printf '\n%d supply-chain configuration check(s) failed\n' "$FAILURES" >&2
  exit 1
fi

printf '\nAll supply-chain configuration checks passed\n'
