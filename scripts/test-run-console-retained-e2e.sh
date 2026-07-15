#!/usr/bin/env bash
# Static lifecycle contract for the retained browser-session console proof.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="$repo_root/scripts/run-console-retained-e2e.sh"

[[ -f "$target" ]] || {
  echo "missing retained console proof lifecycle helper: $target" >&2
  exit 1
}
bash -n "$target"

for required in \
  'CREATE SCHEMA' \
  'API_INPUT_HASH' \
  'ESHU_VERSION=proof-${API_INPUT_HASH}' \
  'RUNNER_INPUT_HASH' \
  'package.json' \
  'package-lock.json' \
  'ESHU_E2E_NODE_VERSION' \
  'ESHU_E2E_PLAYWRIGHT_VERSION' \
  'ESHU_E2E_API_IMAGE_DIGEST' \
  'ESHU_E2E_NORNIC_IMAGE_DIGEST' \
  'ESHU_E2E_CORPUS_ATTESTATION' \
  'ESHU_E2E_CORPUS_IDENTITY' \
  'ESHU_E2E_CORPUS_REPOSITORY_COUNT' \
  '_public_identity_snapshots' \
  'row_digest' \
  'current_digest <> baseline.row_digest' \
  'search_path' \
  'docker compose' \
  'run -d --no-deps' \
  'npm run console:e2e' \
  'DROP SCHEMA' \
  'ESHU_KEEP_RETAINED_PROOF'; do
  rg -q --fixed-strings "$required" "$target" || {
    echo "retained console proof helper missing lifecycle contract: $required" >&2
    exit 1
  }
done

# A content mutation must fail even when cardinality is unchanged. This small
# model guards the invariant while the SQL contract checks above bind the
# production helper to the same count+digest comparison.
identity_snapshot_matches() {
  [[ "$1" == "$3" && "$2" == "$4" ]]
}
if identity_snapshot_matches 1 before-digest 1 after-digest; then
  echo "retained console proof accepted a same-count public identity mutation" >&2
  exit 1
fi

# The production manifest hashes path and content. Reproduce that exact file
# set in an isolated Git repository and prove a lockfile-only content change
# changes the runner identity.
hash_repo="$(mktemp -d)"
tmp=""
cleanup() {
  rm -rf "$hash_repo"
  if [[ -n "$tmp" ]]; then
    rm -rf "$tmp"
  fi
}
trap cleanup EXIT
mkdir -p "$hash_repo/apps/console" "$hash_repo/scripts"
printf '%s\n' '{"name":"proof"}' >"$hash_repo/package.json"
printf '%s\n' '{"lockfileVersion":3}' >"$hash_repo/package-lock.json"
printf '%s\n' 'runner' >"$hash_repo/apps/console/runner.ts"
for input in \
  run-console-live-e2e.sh \
  run-console-retained-e2e.sh \
  console-live-e2e-runtime.mjs; do
  printf '%s\n' "$input" >"$hash_repo/scripts/$input"
done
git -C "$hash_repo" init -q
git -C "$hash_repo" add .
runner_hash() {
  (
    cd "$hash_repo"
    {
      printf '%s\0' package.json package-lock.json
      git ls-files -z -co --exclude-standard -- \
        apps/console \
        scripts/run-console-live-e2e.sh \
        scripts/run-console-retained-e2e.sh \
        scripts/console-live-e2e-runtime.mjs
    } | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}'
  )
}
before_lock_hash="$(runner_hash)"
printf '%s\n' '{"changed":true}' >>"$hash_repo/package-lock.json"
after_lock_hash="$(runner_hash)"
if [[ "$before_lock_hash" == "$after_lock_hash" ]]; then
  echo "runner identity ignored a package-lock-only change" >&2
  exit 1
fi

if rg -q --fixed-strings 'down -v' "$target"; then
  echo "retained console proof helper must never remove retained volumes" >&2
  exit 1
fi

if rg -q 'source[[:space:]]+.*compose_env_file' "$target"; then
  echo "retained console proof helper must not execute Compose env files as shell code" >&2
  exit 1
fi

for ownership_guard in 'schema_created=false' 'container_created=false'; do
  rg -q --fixed-strings "$ownership_guard" "$target" || {
    echo "retained console proof helper missing ownership guard: $ownership_guard" >&2
    exit 1
  }
done

tmp="$(mktemp -d)"
mkdir -p "$tmp/bin"
cat >"$tmp/bin/docker" <<'MOCK_DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$ESHU_TEST_DOCKER_LOG"
if [[ "$*" == "container inspect"* ]]; then
  [[ "${ESHU_TEST_COLLISION:-container}" == "container" ]] && exit 0
  exit 1
fi
if [[ "${ESHU_TEST_COLLISION:-container}" == "schema" && "$*" == *"exec -T postgres psql"* ]]; then
  printf '%s\n' "$(cat)" >>"$ESHU_TEST_DOCKER_LOG"
  exit 1
fi
exit 0
MOCK_DOCKER
chmod +x "$tmp/bin/docker"

docker_log="$tmp/docker.log"
set +e
PATH="$tmp/bin:$PATH" \
ESHU_TEST_DOCKER_LOG="$docker_log" \
ESHU_TEST_COLLISION=container \
ESHU_E2E_RETAINED_PROOF_ID=existing_proof \
ESHU_E2E_WIZARD_NEW_PASSWORD=not-a-real-secret \
ESHU_E2E_CORPUS_ATTESTATION=test-corpus \
ESHU_E2E_CORPUS_REPOSITORY_COUNT=1 \
"$target" >"$tmp/stdout" 2>"$tmp/stderr"
collision_status=$?
set -e

if [[ "$collision_status" -eq 0 ]]; then
  echo "retained console proof helper must reject a pre-existing sidecar" >&2
  exit 1
fi
if rg -q '^rm -f ' "$docker_log"; then
  echo "retained console proof helper removed a sidecar it did not create" >&2
  exit 1
fi

: >"$docker_log"
set +e
PATH="$tmp/bin:$PATH" \
ESHU_TEST_DOCKER_LOG="$docker_log" \
ESHU_TEST_COLLISION=schema \
ESHU_E2E_RETAINED_PROOF_ID=existing_schema \
ESHU_E2E_WIZARD_NEW_PASSWORD=not-a-real-secret \
ESHU_E2E_CORPUS_IDENTITY=test-corpus \
ESHU_E2E_CORPUS_REPOSITORY_COUNT=1 \
"$target" >"$tmp/schema-stdout" 2>"$tmp/schema-stderr"
schema_collision_status=$?
set -e

if [[ "$schema_collision_status" -eq 0 ]]; then
  echo "retained console proof helper must reject a pre-existing proof schema" >&2
  exit 1
fi
if rg -q 'rm -f|DROP SCHEMA' "$docker_log"; then
  echo "retained console proof helper cleaned up schema/container state it did not create" >&2
  exit 1
fi

echo "retained console proof lifecycle contract: PASS"
