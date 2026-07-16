#!/usr/bin/env bash
# Owns a rerunnable browser-session proof identity and API sidecar while reusing
# an already-running retained Postgres/NornicDB corpus. It never stops or removes
# the retained Compose project or its volumes.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
create_proof_schema_sql="$repo_root/scripts/lib/console-retained-create-proof-schema.sql"
verify_public_identity_sql="$repo_root/scripts/lib/console-retained-verify-public-identity.sql"

compose_project="${ESHU_E2E_RETAINED_PROJECT:-eshu}"
compose_file="${ESHU_E2E_RETAINED_COMPOSE_FILE:-docker-compose.yaml}"
compose_env_file="${ESHU_E2E_COMPOSE_ENV_FILE:-}"
proof_id="${ESHU_E2E_RETAINED_PROOF_ID:-$(date -u +%Y%m%d%H%M%S)_$$}"
api_port="${ESHU_E2E_RETAINED_API_PORT:-18086}"
keep_proof="${ESHU_KEEP_RETAINED_PROOF:-false}"
schema_created=false
container_created=false
public_identity_verification_attempted=false
proof_completed=false

[[ "$proof_id" =~ ^[a-z0-9_]+$ ]] || {
  echo "run-console-retained-e2e: proof id must match [a-z0-9_]+" >&2
  exit 1
}
[[ "$api_port" =~ ^[0-9]+$ ]] || {
  echo "run-console-retained-e2e: API port must be numeric" >&2
  exit 1
}

if [[ -n "$compose_env_file" ]]; then
  [[ -f "$compose_env_file" ]] || {
    echo "run-console-retained-e2e: compose env file does not exist" >&2
    exit 1
  }
fi

postgres_port="${ESHU_POSTGRES_PORT:-15432}"
postgres_password="${ESHU_POSTGRES_PASSWORD:-change-me}"
auth_secret_enc_key="${ESHU_AUTH_SECRET_ENC_KEY:-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=}"
wizard_password="${ESHU_E2E_WIZARD_NEW_PASSWORD:-}"
corpus_attestation="${ESHU_E2E_CORPUS_ATTESTATION:-${ESHU_E2E_CORPUS_IDENTITY:-}}"
corpus_repository_count="${ESHU_E2E_CORPUS_REPOSITORY_COUNT:-}"
[[ -n "$wizard_password" ]] || {
  echo "run-console-retained-e2e: ESHU_E2E_WIZARD_NEW_PASSWORD is required" >&2
  exit 1
}
[[ -n "$corpus_attestation" ]] || {
  echo "run-console-retained-e2e: ESHU_E2E_CORPUS_ATTESTATION is required (ESHU_E2E_CORPUS_IDENTITY is accepted as a deprecated fallback)" >&2
  exit 1
}
[[ "$corpus_repository_count" =~ ^[0-9]+$ ]] || {
  echo "run-console-retained-e2e: ESHU_E2E_CORPUS_REPOSITORY_COUNT must be a non-negative integer" >&2
  exit 1
}

schema="dashboard_session_${proof_id}"
container_name="eshu-dashboard-session-${proof_id//_/-}"
container_dsn="postgresql://eshu:${postgres_password}@postgres:5432/eshu?options=-csearch_path%3D${schema}%2Cpublic"
host_dsn="postgresql://eshu:${postgres_password}@127.0.0.1:${postgres_port}/eshu?sslmode=disable&options=-csearch_path%3D${schema}%2Cpublic"
api_base="http://127.0.0.1:${api_port}"

compose=(docker compose -p "$compose_project" -f "$compose_file")
if [[ -n "$compose_env_file" ]]; then
  compose=(docker compose -p "$compose_project" --env-file "$compose_env_file" -f "$compose_file")
fi

for tool in docker curl node npm rg shasum; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "run-console-retained-e2e: missing required tool: $tool" >&2
    exit 1
  }
done
for sql_file in "$create_proof_schema_sql" "$verify_public_identity_sql"; do
  [[ -f "$sql_file" ]] || {
    echo "run-console-retained-e2e: missing required SQL fixture: $sql_file" >&2
    exit 1
  }
done

API_INPUT_HASH="$({
  printf '%s\0' Dockerfile
  git ls-files -z -co --exclude-standard -- go sdk/go
} | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
RUNNER_INPUT_HASH="$({
  printf '%s\0' package.json package-lock.json
  git ls-files -z -co --exclude-standard -- \
    apps/console \
    scripts/run-console-live-e2e.sh \
    scripts/run-console-retained-e2e.sh \
    scripts/console-live-e2e-runtime.mjs \
    scripts/lib/console-retained-create-proof-schema.sql \
    scripts/lib/console-retained-verify-public-identity.sql
} | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
node_version="$(node --version)"
playwright_version="$(node -p 'require("playwright/package.json").version')"

postgres_psql() {
  "${compose[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U eshu -d eshu "$@"
}

create_proof_schema() {
  printf '%s\n' "run-console-retained-e2e: creating isolated auth schema $schema"
  postgres_psql -v proof_schema="$schema" <"$create_proof_schema_sql"
  schema_created=true
}

verify_public_identity_unchanged() {
  public_identity_verification_attempted=true
  postgres_psql -v proof_schema="$schema" <"$verify_public_identity_sql" || return "$?"
}

drop_proof_schema() {
  postgres_psql -v proof_schema="$schema" <<'SQL'
SELECT format('DROP SCHEMA IF EXISTS %I CASCADE', :'proof_schema') \gexec
SQL
}

cleanup() {
  local original_status="${1:-$?}"
  local cleanup_status=0
  local operation_status=0
  local final_status=0

  trap - EXIT
  set +e

  if [[ "$schema_created" == "true" && "$public_identity_verification_attempted" != "true" ]]; then
    verify_public_identity_unchanged
    operation_status=$?
    if ((operation_status != 0)); then
      printf '%s\n' "run-console-retained-e2e: public identity verification failed during cleanup (status $operation_status)" >&2
      cleanup_status=$operation_status
    fi
  fi
  if [[ "$keep_proof" == "true" ]]; then
    if [[ "$container_created" == "true" ]]; then
      printf '%s\n' "run-console-retained-e2e: keeping proof sidecar $container_name at $api_base"
    fi
    if [[ "$schema_created" == "true" ]]; then
      printf '%s\n' "run-console-retained-e2e: keeping isolated auth schema $schema"
    fi
  else
    if [[ "$container_created" == "true" ]]; then
      docker rm -f "$container_name" >/dev/null 2>&1
      operation_status=$?
      if ((operation_status != 0)); then
        printf '%s\n' "run-console-retained-e2e: failed to remove proof sidecar $container_name (status $operation_status)" >&2
        if ((cleanup_status == 0)); then
          cleanup_status=$operation_status
        fi
      fi
    fi
    if [[ "$schema_created" == "true" ]]; then
      drop_proof_schema
      operation_status=$?
      if ((operation_status != 0)); then
        printf '%s\n' "run-console-retained-e2e: failed to drop isolated auth schema $schema (status $operation_status)" >&2
        if ((cleanup_status == 0)); then
          cleanup_status=$operation_status
        fi
      fi
    fi
  fi

  final_status=$original_status
  if ((final_status == 0)); then
    final_status=$cleanup_status
  fi
  if ((final_status == 0)) && [[ "$proof_completed" == "true" ]]; then
    printf '%s\n' "run-console-retained-e2e: isolated retained-corpus proof PASS"
  fi
  exit "$final_status"
}
trap 'cleanup "$?"' EXIT

"${compose[@]}" ps --status running postgres nornicdb >/dev/null
if docker container inspect "$container_name" >/dev/null 2>&1; then
  echo "run-console-retained-e2e: proof sidecar already exists: $container_name" >&2
  exit 1
fi
printf '%s\n' "run-console-retained-e2e: building exact API input $API_INPUT_HASH"
"${compose[@]}" build --build-arg "ESHU_VERSION=proof-${API_INPUT_HASH}" eshu
create_proof_schema

printf '%s\n' "run-console-retained-e2e: starting isolated API sidecar $container_name"
"${compose[@]}" run -d --no-deps \
  --name "$container_name" \
  -p "127.0.0.1:${api_port}:8080" \
  -e "ESHU_POSTGRES_DSN=${container_dsn}" \
  -e "ESHU_CONTENT_STORE_DSN=${container_dsn}" \
  -e "ESHU_AUTH_SECRET_ENC_KEY=${auth_secret_enc_key}" \
  eshu >/dev/null
container_created=true

api_version="$(docker exec "$container_name" eshu-api --version)"
printf '%s\n' "$api_version" | rg -q --fixed-strings "proof-${API_INPUT_HASH}" || {
  echo "run-console-retained-e2e: sidecar version does not match current API inputs" >&2
  exit 1
}
api_image_digest="$(docker inspect "$container_name" --format '{{.Image}}')"
nornic_container_id="$("${compose[@]}" ps -q nornicdb)"
[[ -n "$nornic_container_id" ]] || {
  echo "run-console-retained-e2e: retained NornicDB container is unavailable" >&2
  exit 1
}
nornic_image_digest="$(docker inspect "$nornic_container_id" --format '{{.Image}}')"
nornic_version="$(docker inspect "$nornic_container_id" --format '{{.Config.Image}}')"

for endpoint in /healthz /readyz; do
  ready=false
  for _ in $(seq 1 60); do
    if [[ "$(curl -sS -m 3 -o /dev/null -w '%{http_code}' "${api_base}${endpoint}" || true)" == "200" ]]; then
      ready=true
      break
    fi
    sleep 1
  done
  [[ "$ready" == "true" ]] || {
    echo "run-console-retained-e2e: ${endpoint} did not become ready" >&2
    exit 1
  }
done

ESHU_CONSOLE_E2E_ENV_FILE=/dev/null \
ESHU_E2E_AUTH_MODE=browser_session \
ESHU_E2E_API_BASE="$api_base" \
ESHU_E2E_POSTGRES_DSN="$host_dsn" \
ESHU_AUTH_SECRET_ENC_KEY="$auth_secret_enc_key" \
ESHU_E2E_WIZARD_NEW_PASSWORD="$wizard_password" \
ESHU_E2E_PROOF_ID="$proof_id" \
ESHU_E2E_SOURCE_HASH="$API_INPUT_HASH" \
ESHU_E2E_RUNNER_HASH="$RUNNER_INPUT_HASH" \
ESHU_E2E_API_IMAGE_DIGEST="$api_image_digest" \
ESHU_E2E_API_VERSION="$api_version" \
ESHU_E2E_NORNIC_IMAGE_DIGEST="$nornic_image_digest" \
ESHU_E2E_NORNIC_VERSION="$nornic_version" \
ESHU_E2E_NODE_VERSION="$node_version" \
ESHU_E2E_PLAYWRIGHT_VERSION="$playwright_version" \
ESHU_E2E_CORPUS_ATTESTATION="$corpus_attestation" \
ESHU_E2E_CORPUS_REPOSITORY_COUNT="$corpus_repository_count" \
npm run console:e2e

verify_public_identity_unchanged
proof_completed=true
