#!/usr/bin/env bash
# Browser-auth E2E gate (issue #4971 phase 2).
#
# Unlike scripts/run-console-live-e2e.sh (which assumes an already-running,
# operator-managed corpus stack), this gate's acceptance items only mean
# something against a stack with ZERO local identities. Reusing a long-lived
# stack would let a prior run's admin identity mask a real "dead-end login
# form" regression. So this script OWNS the docker-compose.e2e.yaml stack's
# full lifecycle: it always brings up a fresh stack and always tears it down
# (`down -v` removes the named Postgres/graph volumes too), using an isolated
# Compose project name so it never touches a developer's own manually-started
# `eshu-e2e` stack (docs/public/run-locally/docker-compose.md#sso-auth-e2e-stack).
#
# Usage: scripts/run-auth-e2e.sh
#   ESHU_KEEP_COMPOSE_STACK=true   Skip the `down -v` teardown for debugging.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# shellcheck source=scripts/lib/auth_e2e_cli.sh
source "$repo_root/scripts/lib/auth_e2e_cli.sh"

project="${ESHU_E2E_PROJECT_NAME:-eshu-e2e-auth}"
bind_addr="${ESHU_E2E_BIND_ADDR:-127.0.0.1}"
api_port="${ESHU_E2E_API_PORT:-28080}"
postgres_port="${ESHU_E2E_POSTGRES_PORT:-28432}"
postgres_password="${ESHU_E2E_POSTGRES_PASSWORD:-change-me}"
mock_oidc_port="${ESHU_E2E_MOCK_OIDC_PORT:-28090}"
mock_oidc_admin_port="${ESHU_E2E_MOCK_OIDC_ADMIN_PORT:-28091}"
keep_stack="${ESHU_KEEP_COMPOSE_STACK:-false}"

export ESHU_E2E_PROJECT_NAME="$project"
export ESHU_E2E_BIND_ADDR="$bind_addr"
export ESHU_E2E_API_PORT="$api_port"
export ESHU_E2E_POSTGRES_PORT="$postgres_port"
export ESHU_E2E_POSTGRES_PASSWORD="$postgres_password"
export ESHU_E2E_MOCK_OIDC_PORT="$mock_oidc_port"
export ESHU_E2E_MOCK_OIDC_ADMIN_PORT="$mock_oidc_admin_port"
# This suite uses `up --build`, so it intentionally inherits the repository's
# exact-source NornicDB default. Published-image compatibility comparisons must
# run without `--build` and set NORNICDB_IMAGE/NORNICDB_PULL_POLICY together.

for tool in docker node go; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "run-auth-e2e: missing required tool: $tool" >&2
    exit 1
  }
done

teardown() {
  local exit_code=$?
  trap - EXIT

  if ! auth_e2e_cli_cleanup; then
    echo "run-auth-e2e: temporary CLI cleanup failed" >&2
    exit_code=1
  fi
  if [[ "$keep_stack" == "true" ]]; then
    echo "run-auth-e2e: ESHU_KEEP_COMPOSE_STACK=true — leaving stack up: docker compose -p $project -f docker-compose.e2e.yaml down -v"
    exit "$exit_code"
  fi
  echo "run-auth-e2e: tearing down project $project"
  if ! docker compose -p "$project" -f docker-compose.e2e.yaml down -v --remove-orphans >/dev/null 2>&1; then
    echo "run-auth-e2e: Compose teardown failed for project $project" >&2
    exit_code=1
  fi
  exit "$exit_code"
}
trap teardown EXIT

echo "run-auth-e2e: typechecking the gate"
npx tsc -p apps/console/e2e/tsconfig.json

echo "run-auth-e2e: building the exact-source eshu CLI"
auth_e2e_cli_build "$repo_root"

echo "run-auth-e2e: bringing up a FRESH stack (project $project) — zero identities required for the acceptance items"
docker compose -p "$project" -f docker-compose.e2e.yaml up -d --build --wait

api_base="http://${bind_addr}:${api_port}"
echo "run-auth-e2e: probing readiness at $api_base"
for endpoint in /healthz /readyz; do
  code="$(curl -sS -m 5 -o /dev/null -w '%{http_code}' "${api_base}${endpoint}" || true)"
  if [[ "$code" != "200" ]]; then
    echo "run-auth-e2e: ${api_base}${endpoint} returned ${code}; stack not ready" >&2
    exit 1
  fi
done

echo "run-auth-e2e: running the browser gate"
ESHU_E2E_API_BASE="$api_base" \
  ESHU_E2E_POSTGRES_HOST="$bind_addr" \
  ESHU_E2E_POSTGRES_PORT="$postgres_port" \
  ESHU_E2E_POSTGRES_PASSWORD="$postgres_password" \
  ESHU_E2E_MOCK_OIDC_PORT="$mock_oidc_port" \
  ESHU_E2E_MOCK_OIDC_ADMIN_PORT="$mock_oidc_admin_port" \
  ESHU_E2E_ESHU_BINARY="$AUTH_E2E_CLI_BIN" \
  node scripts/console-auth-e2e-runtime.mjs
