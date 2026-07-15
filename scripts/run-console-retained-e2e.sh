#!/usr/bin/env bash
# Owns a rerunnable browser-session proof identity and API sidecar while reusing
# an already-running retained Postgres/NornicDB corpus. It never stops or removes
# the retained Compose project or its volumes.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

compose_project="${ESHU_E2E_RETAINED_PROJECT:-eshu}"
compose_file="${ESHU_E2E_RETAINED_COMPOSE_FILE:-docker-compose.yaml}"
compose_env_file="${ESHU_E2E_COMPOSE_ENV_FILE:-}"
proof_id="${ESHU_E2E_RETAINED_PROOF_ID:-$(date -u +%Y%m%d%H%M%S)_$$}"
api_port="${ESHU_E2E_RETAINED_API_PORT:-18086}"
keep_proof="${ESHU_KEEP_RETAINED_PROOF:-false}"
schema_created=false
container_created=false

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
corpus_identity="${ESHU_E2E_CORPUS_IDENTITY:-}"
corpus_repository_count="${ESHU_E2E_CORPUS_REPOSITORY_COUNT:-}"
[[ -n "$wizard_password" ]] || {
  echo "run-console-retained-e2e: ESHU_E2E_WIZARD_NEW_PASSWORD is required" >&2
  exit 1
}
[[ -n "$corpus_identity" ]] || {
  echo "run-console-retained-e2e: ESHU_E2E_CORPUS_IDENTITY is required" >&2
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

API_INPUT_HASH="$({
  printf '%s\0' Dockerfile
  git ls-files -z -co --exclude-standard -- go sdk/go
} | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
RUNNER_INPUT_HASH="$({
  git ls-files -z -co --exclude-standard -- \
    apps/console \
    scripts/run-console-live-e2e.sh \
    scripts/run-console-retained-e2e.sh \
    scripts/console-live-e2e-runtime.mjs
} | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"

postgres_psql() {
  "${compose[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U eshu -d eshu "$@"
}

create_proof_schema() {
  printf '%s\n' "run-console-retained-e2e: creating isolated auth schema $schema"
  postgres_psql -v proof_schema="$schema" <<'SQL'
BEGIN;
SELECT format('CREATE SCHEMA %I', :'proof_schema') \gexec
SELECT set_config('eshu.proof_schema', :'proof_schema', false);
CREATE TEMP TABLE proof_auth_tables(table_name text PRIMARY KEY);
INSERT INTO proof_auth_tables(table_name)
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public'
  AND (
    table_name LIKE 'identity_%'
    OR table_name IN (
      'browser_sessions',
      'governance_audit_events',
      'tenant_repository_grants',
      'tenant_scope_grants',
      'tenants',
      'workspaces'
    )
  )
ORDER BY table_name;

DO $proof$
DECLARE
  target_schema text := current_setting('eshu.proof_schema');
  target_table text;
BEGIN
  EXECUTE format(
    'CREATE TABLE %I._public_identity_counts (table_name text PRIMARY KEY, row_count bigint NOT NULL)',
    target_schema
  );
  FOR target_table IN SELECT table_name FROM proof_auth_tables ORDER BY table_name LOOP
    EXECUTE format(
      'CREATE TABLE %I.%I (LIKE public.%I INCLUDING ALL)',
      target_schema,
      target_table,
      target_table
    );
    EXECUTE format(
      'INSERT INTO %I._public_identity_counts SELECT %L, count(*) FROM public.%I',
      target_schema,
      target_table,
      target_table
    );
  END LOOP;
END
$proof$;

DO $proof$
BEGIN
  IF to_regclass(format('%I.fact_records', current_setting('eshu.proof_schema'))) IS NOT NULL THEN
    RAISE EXCEPTION 'proof schema must not shadow retained fact_records';
  END IF;
END
$proof$;
COMMIT;
SQL
  schema_created=true
}

verify_public_identity_unchanged() {
  postgres_psql -v proof_schema="$schema" <<'SQL'
SELECT set_config('eshu.proof_schema', :'proof_schema', false);
DO $proof$
DECLARE
  target_schema text := current_setting('eshu.proof_schema');
  baseline record;
  current_count bigint;
BEGIN
  FOR baseline IN EXECUTE format(
    'SELECT table_name, row_count FROM %I._public_identity_counts ORDER BY table_name',
    target_schema
  ) LOOP
    EXECUTE format('SELECT count(*) FROM public.%I', baseline.table_name) INTO current_count;
    IF current_count <> baseline.row_count THEN
      RAISE EXCEPTION 'retained public identity table % changed during isolated proof', baseline.table_name;
    END IF;
  END LOOP;
END
$proof$;
SQL
}

drop_proof_schema() {
  postgres_psql -v proof_schema="$schema" <<'SQL'
SELECT format('DROP SCHEMA IF EXISTS %I CASCADE', :'proof_schema') \gexec
SQL
}

cleanup() {
  if [[ "$keep_proof" == "true" ]]; then
    if [[ "$container_created" == "true" ]]; then
      printf '%s\n' "run-console-retained-e2e: keeping proof sidecar $container_name at $api_base"
    fi
    if [[ "$schema_created" == "true" ]]; then
      printf '%s\n' "run-console-retained-e2e: keeping isolated auth schema $schema"
    fi
    return
  fi
  if [[ "$container_created" == "true" ]]; then
    docker rm -f "$container_name" >/dev/null 2>&1 || true
  fi
  if [[ "$schema_created" == "true" ]]; then
    verify_public_identity_unchanged
    drop_proof_schema
  fi
}
trap cleanup EXIT

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
ESHU_E2E_CORPUS_IDENTITY="$corpus_identity" \
ESHU_E2E_CORPUS_REPOSITORY_COUNT="$corpus_repository_count" \
npm run console:e2e

verify_public_identity_unchanged
printf '%s\n' "run-console-retained-e2e: isolated retained-corpus proof PASS"
