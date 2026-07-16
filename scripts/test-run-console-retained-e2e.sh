#!/usr/bin/env bash
# Static lifecycle contract for the retained browser-session console proof.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="$repo_root/scripts/run-console-retained-e2e.sh"
create_schema_sql="$repo_root/scripts/lib/console-retained-create-proof-schema.sql"
verify_identity_sql="$repo_root/scripts/lib/console-retained-verify-public-identity.sql"

[[ -f "$target" ]] || {
  echo "missing retained console proof lifecycle helper: $target" >&2
  exit 1
}
bash -n "$target"

for sql_file in "$create_schema_sql" "$verify_identity_sql"; do
  [[ -f "$sql_file" ]] || {
    echo "missing retained console proof SQL fixture: $sql_file" >&2
    exit 1
  }
done

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
  rg -q --fixed-strings "$required" "$target" "$create_schema_sql" "$verify_identity_sql" || {
    echo "retained console proof helper missing lifecycle contract: $required" >&2
    exit 1
  }
done

runner_hash_contains_fixture() {
  local runner="$1"
  local sql_fixture="$2"
  sed -n '/^RUNNER_INPUT_HASH=/,/^node_version=/p' "$runner" |
    rg -q --fixed-strings "$sql_fixture"
}

for sql_fixture in \
  'scripts/lib/console-retained-create-proof-schema.sql' \
  'scripts/lib/console-retained-verify-public-identity.sql'; do
  runner_hash_contains_fixture "$target" "$sql_fixture" || {
    echo "retained console proof runner hash omits SQL fixture: $sql_fixture" >&2
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
mkdir -p "$hash_repo/apps/console" "$hash_repo/scripts/lib"
printf '%s\n' '{"name":"proof"}' >"$hash_repo/package.json"
printf '%s\n' '{"lockfileVersion":3}' >"$hash_repo/package-lock.json"
printf '%s\n' 'runner' >"$hash_repo/apps/console/runner.ts"
for input in \
  run-console-live-e2e.sh \
  run-console-retained-e2e.sh \
  console-live-e2e-runtime.mjs; do
  printf '%s\n' "$input" >"$hash_repo/scripts/$input"
done
printf '%s\n' 'create proof schema' >"$hash_repo/scripts/lib/console-retained-create-proof-schema.sql"
printf '%s\n' 'verify public identity' >"$hash_repo/scripts/lib/console-retained-verify-public-identity.sql"
git -C "$hash_repo" init -q
git -C "$hash_repo" add .

# Mutation-test the production block binding. A fixture path elsewhere in the
# runner must not hide its omission from RUNNER_INPUT_HASH.
mutated_runner="$hash_repo/run-console-retained-e2e.sh"
for sql_fixture in \
  'scripts/lib/console-retained-create-proof-schema.sql' \
  'scripts/lib/console-retained-verify-public-identity.sql'; do
  sed "\\|    ${sql_fixture}|d" "$target" >"$mutated_runner"
  if runner_hash_contains_fixture "$mutated_runner" "$sql_fixture"; then
    echo "runner hash fixture assertion accepted an omitted fixture: $sql_fixture" >&2
    exit 1
  fi
done

runner_hash() {
  (
    cd "$hash_repo"
    {
      printf '%s\0' package.json package-lock.json
      git ls-files -z -co --exclude-standard -- \
        apps/console \
        scripts/run-console-live-e2e.sh \
        scripts/run-console-retained-e2e.sh \
        scripts/console-live-e2e-runtime.mjs \
        scripts/lib/console-retained-create-proof-schema.sql \
        scripts/lib/console-retained-verify-public-identity.sql
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

for sql_fixture in \
  'console-retained-create-proof-schema.sql' \
  'console-retained-verify-public-identity.sql'; do
  fixture_path="$hash_repo/scripts/lib/$sql_fixture"
  original_fixture="$(<"$fixture_path")"
  before_sql_hash="$(runner_hash)"
  printf '%s\n' 'changed' >>"$fixture_path"
  after_sql_hash="$(runner_hash)"
  printf '%s\n' "$original_fixture" >"$fixture_path"
  if [[ "$before_sql_hash" == "$after_sql_hash" ]]; then
    echo "runner identity ignored a retained SQL fixture change: $sql_fixture" >&2
    exit 1
  fi
done

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
cat >"$tmp/bin/docker" <<'MOCK_DOCKER_HEADER'
#!/usr/bin/env bash
set -euo pipefail
mode="${ESHU_TEST_COLLISION:-container}"
printf '%s\n' "$*" >>"$ESHU_TEST_DOCKER_LOG"
if [[ "$*" == "container inspect"* ]]; then
  [[ "$mode" == "container" ]] && exit 0
  exit 1
fi
MOCK_DOCKER_HEADER
cat >>"$tmp/bin/docker" <<'MOCK_DOCKER_PSQL'
if [[ "$*" == *"exec -T postgres psql"* ]]; then
  sql="$(cat)"
  printf '%s\n' "$sql" >>"$ESHU_TEST_DOCKER_LOG"
  [[ "$mode" == "schema" ]] && exit 1
  if [[ "$mode" =~ ^(verify_failure|cleanup_verify_failure)$ &&
        "$sql" == *"current_digest <> baseline.row_digest"* ]]; then
    exit 73
  fi
  if [[ "$mode" == "drop_failure" && "$sql" == *"DROP SCHEMA IF EXISTS %I CASCADE"* ]]; then
    exit 74
  fi
  exit 0
fi
MOCK_DOCKER_PSQL
cat >>"$tmp/bin/docker" <<'MOCK_DOCKER_FAILURES'
if [[ "$mode" == "failed_keep" && "$*" == *"run -d --no-deps"* ]]; then
  exit 42
fi
if [[ "$mode" == "remove_failure" && "$*" == "rm -f "* ]]; then
  exit 75
fi
MOCK_DOCKER_FAILURES
cat >>"$tmp/bin/docker" <<'MOCK_DOCKER_VERSION'
if [[ "$mode" =~ ^(verify_failure|remove_failure|drop_failure)$ && "$*" == *"eshu-api --version"* ]]; then
  api_hash="$({
    printf '%s\0' Dockerfile
    git ls-files -z -co --exclude-standard -- go sdk/go
  } | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
  printf 'eshu-api proof-%s\n' "$api_hash"
  exit 0
fi
MOCK_DOCKER_VERSION
cat >>"$tmp/bin/docker" <<'MOCK_DOCKER_NORNIC'
if [[ "$mode" =~ ^(verify_failure|remove_failure|drop_failure)$ && "$*" == *"ps -q nornicdb"* ]]; then
  printf '%s\n' 'retained-nornicdb'
  exit 0
fi
exit 0
MOCK_DOCKER_NORNIC
chmod +x "$tmp/bin/docker"

cat >"$tmp/bin/curl" <<'MOCK_CURL'
#!/usr/bin/env bash
printf '%s' '200'
MOCK_CURL
chmod +x "$tmp/bin/curl"

cat >"$tmp/bin/npm" <<'MOCK_NPM'
#!/usr/bin/env bash
exit 0
MOCK_NPM
chmod +x "$tmp/bin/npm"

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

# When sidecar startup fails, a proof retained for evidence must still verify
# that the shared public identity tables did not change and preserve its owned
# schema instead of deleting the evidence.
: >"$docker_log"
set +e
PATH="$tmp/bin:$PATH" \
ESHU_TEST_DOCKER_LOG="$docker_log" \
ESHU_TEST_COLLISION=failed_keep \
ESHU_KEEP_RETAINED_PROOF=true \
ESHU_E2E_RETAINED_PROOF_ID=failed_kept_proof \
ESHU_E2E_WIZARD_NEW_PASSWORD=not-a-real-secret \
ESHU_E2E_CORPUS_IDENTITY=test-corpus \
ESHU_E2E_CORPUS_REPOSITORY_COUNT=1 \
"$target" >"$tmp/failed-keep-stdout" 2>"$tmp/failed-keep-stderr"
failed_keep_status=$?
set -e

if [[ "$failed_keep_status" -eq 0 ]]; then
  echo "retained console proof failed+keep model unexpectedly passed" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'current_digest <> baseline.row_digest' "$docker_log"; then
  echo "retained console proof failed+keep path skipped public identity verification" >&2
  exit 1
fi
if rg -q 'rm -f|DROP SCHEMA' "$docker_log"; then
  echo "retained console proof failed+keep path deleted retained evidence" >&2
  exit 1
fi

# A failed public-identity verification must not short-circuit cleanup of an
# owned sidecar or schema. The verification status remains the terminal result.
: >"$docker_log"
set +e
PATH="$tmp/bin:$PATH" \
ESHU_TEST_DOCKER_LOG="$docker_log" \
ESHU_TEST_COLLISION=verify_failure \
ESHU_E2E_RETAINED_PROOF_ID=failed_verification_cleanup \
ESHU_E2E_WIZARD_NEW_PASSWORD=not-a-real-secret \
ESHU_E2E_CORPUS_ATTESTATION=test-corpus \
ESHU_E2E_CORPUS_REPOSITORY_COUNT=1 \
"$target" >"$tmp/failed-verification-stdout" 2>"$tmp/failed-verification-stderr"
failed_verification_status=$?
set -e

if [[ "$failed_verification_status" -ne 73 ]]; then
  echo "retained console proof did not propagate public identity verification failure" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'current_digest <> baseline.row_digest' "$docker_log"; then
  echo "retained console proof cleanup skipped failed public identity verification" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'rm -f eshu-dashboard-session-failed-verification-cleanup' "$docker_log"; then
  echo "retained console proof verification failure leaked its owned sidecar" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'DROP SCHEMA IF EXISTS %I CASCADE' "$docker_log"; then
  echo "retained console proof verification failure leaked its owned schema" >&2
  exit 1
fi

# If the main proof fails before its explicit identity check, a verification
# failure inside the EXIT trap must not prevent either owned resource cleanup.
# The original proof failure remains the terminal status.
: >"$docker_log"
set +e
PATH="$tmp/bin:$PATH" \
ESHU_TEST_DOCKER_LOG="$docker_log" \
ESHU_TEST_COLLISION=cleanup_verify_failure \
ESHU_E2E_RETAINED_PROOF_ID=failed_during_cleanup_verification \
ESHU_E2E_WIZARD_NEW_PASSWORD=not-a-real-secret \
ESHU_E2E_CORPUS_ATTESTATION=test-corpus \
ESHU_E2E_CORPUS_REPOSITORY_COUNT=1 \
"$target" >"$tmp/cleanup-verification-stdout" 2>"$tmp/cleanup-verification-stderr"
cleanup_verification_status=$?
set -e

if [[ "$cleanup_verification_status" -ne 1 ]]; then
  echo "retained console proof did not preserve the original failure over cleanup verification" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'public identity verification failed during cleanup (status 73)' "$tmp/cleanup-verification-stderr"; then
  echo "retained console proof hid its cleanup-time identity verification failure" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'current_digest <> baseline.row_digest' "$docker_log"; then
  echo "retained console proof skipped cleanup-time public identity verification" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'rm -f eshu-dashboard-session-failed-during-cleanup-verification' "$docker_log"; then
  echo "retained console proof cleanup-time verification failure leaked its owned sidecar" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'DROP SCHEMA IF EXISTS %I CASCADE' "$docker_log"; then
  echo "retained console proof cleanup-time verification failure leaked its owned schema" >&2
  exit 1
fi

# Keeping a failed proof after its explicit verification must retain both the
# owned sidecar and schema for inspection while propagating the failure.
: >"$docker_log"
set +e
PATH="$tmp/bin:$PATH" \
ESHU_TEST_DOCKER_LOG="$docker_log" \
ESHU_TEST_COLLISION=verify_failure \
ESHU_KEEP_RETAINED_PROOF=true \
ESHU_E2E_RETAINED_PROOF_ID=failed_verification_kept \
ESHU_E2E_WIZARD_NEW_PASSWORD=not-a-real-secret \
ESHU_E2E_CORPUS_ATTESTATION=test-corpus \
ESHU_E2E_CORPUS_REPOSITORY_COUNT=1 \
"$target" >"$tmp/failed-verification-kept-stdout" 2>"$tmp/failed-verification-kept-stderr"
failed_verification_kept_status=$?
set -e

if [[ "$failed_verification_kept_status" -ne 73 ]]; then
  echo "retained console proof did not propagate a kept verification failure" >&2
  exit 1
fi
for retained_message in \
  'keeping proof sidecar eshu-dashboard-session-failed-verification-kept' \
  'keeping isolated auth schema dashboard_session_failed_verification_kept'; do
  if ! rg -q --fixed-strings "$retained_message" "$tmp/failed-verification-kept-stdout"; then
    echo "retained console proof did not report retained failed evidence: $retained_message" >&2
    exit 1
  fi
done
if rg -q 'rm -f|DROP SCHEMA' "$docker_log"; then
  echo "retained console proof deleted explicitly kept sidecar/schema evidence" >&2
  exit 1
fi

# Cleanup must continue from a failed sidecar removal to schema teardown, and
# the first cleanup failure must surface after an otherwise successful proof.
: >"$docker_log"
set +e
PATH="$tmp/bin:$PATH" \
ESHU_TEST_DOCKER_LOG="$docker_log" \
ESHU_TEST_COLLISION=remove_failure \
ESHU_E2E_RETAINED_PROOF_ID=failed_sidecar_removal \
ESHU_E2E_WIZARD_NEW_PASSWORD=not-a-real-secret \
ESHU_E2E_CORPUS_ATTESTATION=test-corpus \
ESHU_E2E_CORPUS_REPOSITORY_COUNT=1 \
"$target" >"$tmp/failed-remove-stdout" 2>"$tmp/failed-remove-stderr"
failed_remove_status=$?
set -e

if [[ "$failed_remove_status" -ne 75 ]]; then
  echo "retained console proof hid its sidecar cleanup failure" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'DROP SCHEMA IF EXISTS %I CASCADE' "$docker_log"; then
  echo "retained console proof sidecar removal failure skipped schema cleanup" >&2
  exit 1
fi
if rg -q --fixed-strings 'isolated retained-corpus proof PASS' "$tmp/failed-remove-stdout"; then
  echo "retained console proof reported PASS after sidecar cleanup failure" >&2
  exit 1
fi

# A schema teardown failure must be terminal and must suppress the PASS marker.
: >"$docker_log"
set +e
PATH="$tmp/bin:$PATH" \
ESHU_TEST_DOCKER_LOG="$docker_log" \
ESHU_TEST_COLLISION=drop_failure \
ESHU_E2E_RETAINED_PROOF_ID=failed_schema_drop \
ESHU_E2E_WIZARD_NEW_PASSWORD=not-a-real-secret \
ESHU_E2E_CORPUS_ATTESTATION=test-corpus \
ESHU_E2E_CORPUS_REPOSITORY_COUNT=1 \
"$target" >"$tmp/failed-drop-stdout" 2>"$tmp/failed-drop-stderr"
failed_drop_status=$?
set -e

if [[ "$failed_drop_status" -ne 74 ]]; then
  echo "retained console proof hid its schema cleanup failure" >&2
  exit 1
fi
if ! rg -q --fixed-strings 'rm -f eshu-dashboard-session-failed-schema-drop' "$docker_log"; then
  echo "retained console proof schema drop failure skipped sidecar cleanup" >&2
  exit 1
fi
if rg -q --fixed-strings 'isolated retained-corpus proof PASS' "$tmp/failed-drop-stdout"; then
  echo "retained console proof reported PASS after schema cleanup failure" >&2
  exit 1
fi

echo "retained console proof lifecycle contract: PASS"
