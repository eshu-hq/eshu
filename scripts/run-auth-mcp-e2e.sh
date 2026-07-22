#!/usr/bin/env bash
# MCP-identity E2E gate (F-9, issue #5170). A SIBLING of scripts/run-auth-e2e.sh
# (issue #4971's browser-auth gate), not an extension of it: this suite proves
# the epic #5161 identity story specifically against the MCP HTTP transport
# (GET /sse, POST /mcp/message) -- personal tokens, GitHub-mapped grants, and
# scripted RFC 9728 OAuth -- across three sequential org-shape phases (token
# -> +GitHub -> +OIDC) on ONE freshly booted, zero-identity docker-compose.e2e.yaml
# stack. See /private/tmp/.../f9-design.md for the full design; §1 explains why
# this is a sibling script/runner/project rather than growing runAuthE2E.ts.
#
# Own isolated Compose project (eshu-e2e-auth-mcp) and 29xxx port block, both
# disjoint from #4971's eshu-e2e-auth / 28xxx block, so both suites can run
# concurrently on one machine (repo-wide convention: unique compose project
# names, see AGENTS.md/CLAUDE.md memory on concurrent local stacks).
#
# Usage: scripts/run-auth-mcp-e2e.sh [--module <name>]
#   ESHU_KEEP_COMPOSE_STACK=true   Skip the `down -v` teardown for debugging
#                                  (also required by the sensitivity-mutation
#                                  script, which reuses the still-up stack).
#   --module <name>                Forwarded to the runner to run only one
#                                  shape/module (see runAuthMcpE2E.ts). Used by
#                                  scripts/verify-auth-mcp-e2e-sensitivity.sh to
#                                  re-run just the negative/challenge module
#                                  against a mutated service without paying for
#                                  the full suite's wall time.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# shellcheck source=scripts/lib/auth_e2e_cli.sh
source "$repo_root/scripts/lib/auth_e2e_cli.sh"

runner_module=""
if [[ "${1:-}" == "--module" ]]; then
  runner_module="${2:?--module requires a value}"
fi

project="${ESHU_E2E_PROJECT_NAME:-eshu-e2e-auth-mcp}"
bind_addr="${ESHU_E2E_BIND_ADDR:-127.0.0.1}"
api_port="${ESHU_E2E_API_PORT:-29080}"
postgres_port="${ESHU_E2E_POSTGRES_PORT:-29432}"
postgres_password="${ESHU_E2E_POSTGRES_PASSWORD:-change-me}"
mock_oidc_port="${ESHU_E2E_MOCK_OIDC_PORT:-29090}"
mock_oidc_admin_port="${ESHU_E2E_MOCK_OIDC_ADMIN_PORT:-29091}"
mcp_port="${ESHU_E2E_MCP_PORT:-29081}"
mock_github_port="${ESHU_E2E_MOCK_GITHUB_PORT:-29092}"
# docker-compose.e2e.yaml's nornicdb service shares its ESHU_E2E_NORNICDB_*
# env var NAMES with #4971's own eshu-e2e-auth stack (both suites `extends`
# the same base file, and neither namespaces these two vars per-suite) --
# their DEFAULTS (27474/27687) collide with a concurrently running #4971
# stack. This suite has its own zero-corpus graph backend (mcp-server never
# reads content from it for the tools this suite calls), so give it 29xxx
# block values distinct from both #4971's defaults and this suite's own
# api/postgres/mcp/mock ports.
nornicdb_http_port="${ESHU_E2E_NORNICDB_HTTP_PORT:-29474}"
nornicdb_bolt_port="${ESHU_E2E_NORNICDB_BOLT_PORT:-29687}"
keep_stack="${ESHU_KEEP_COMPOSE_STACK:-false}"

export ESHU_E2E_PROJECT_NAME="$project"
export ESHU_E2E_BIND_ADDR="$bind_addr"
export ESHU_E2E_API_PORT="$api_port"
export ESHU_E2E_POSTGRES_PORT="$postgres_port"
export ESHU_E2E_POSTGRES_PASSWORD="$postgres_password"
export ESHU_E2E_MOCK_OIDC_PORT="$mock_oidc_port"
export ESHU_E2E_MOCK_OIDC_ADMIN_PORT="$mock_oidc_admin_port"
export ESHU_E2E_MCP_PORT="$mcp_port"
export ESHU_E2E_MOCK_GITHUB_PORT="$mock_github_port"
export ESHU_E2E_NORNICDB_HTTP_PORT="$nornicdb_http_port"
export ESHU_E2E_NORNICDB_BOLT_PORT="$nornicdb_bolt_port"
# This suite's dev server runs on devServerPort 5195 (runAuthMcpE2E.ts),
# distinct from #4971's fixed 5185 -- the shared oidc-static-config.json
# fixture's pc_e2e_admin_static provider hardcodes redirect_url to an
# ABSOLUTE http://127.0.0.1:5185/... URL, so this suite needs its OWN
# fixture variant with the matching port (docker-compose.e2e.yaml's
# ESHU_E2E_OIDC_STATIC_CONFIG_PATH comment has the full story).
export ESHU_E2E_OIDC_STATIC_CONFIG_PATH="./apps/console/e2e/fixtures/oidc-static-config-mcp-e2e.json"
# This suite uses `up --build`, so it intentionally inherits the repository's
# exact-source NornicDB default -- see run-auth-e2e.sh's identical comment.

for tool in docker node go; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "run-auth-mcp-e2e: missing required tool: $tool" >&2
    exit 1
  }
done

teardown() {
  local exit_code=$?
  trap - EXIT

  if ! auth_e2e_cli_cleanup; then
    echo "run-auth-mcp-e2e: temporary CLI cleanup failed" >&2
    exit_code=1
  fi
  if [[ "$keep_stack" == "true" ]]; then
    echo "run-auth-mcp-e2e: ESHU_KEEP_COMPOSE_STACK=true — leaving stack up: docker compose -p $project -f docker-compose.e2e.yaml down -v"
    exit "$exit_code"
  fi
  echo "run-auth-mcp-e2e: tearing down project $project"
  if ! docker compose -p "$project" -f docker-compose.e2e.yaml down -v --remove-orphans >/dev/null 2>&1; then
    echo "run-auth-mcp-e2e: Compose teardown failed for project $project" >&2
    exit_code=1
  fi
  exit "$exit_code"
}
trap teardown EXIT

echo "run-auth-mcp-e2e: typechecking the gate"
npx tsc -p apps/console/e2e/tsconfig.json

echo "run-auth-mcp-e2e: building the exact-source eshu CLI"
auth_e2e_cli_build "$repo_root"

echo "run-auth-mcp-e2e: bringing up a FRESH stack (project $project) — zero identities/providers required at boot"
docker compose -p "$project" -f docker-compose.e2e.yaml up -d --build --wait \
  postgres nornicdb db-migrate workspace-setup eshu mcp-server mock-oidc-idp mock-oidc-idp-admin mock-github

api_base="http://${bind_addr}:${api_port}"
mcp_base="http://${bind_addr}:${mcp_port}"
echo "run-auth-mcp-e2e: probing readiness at $api_base and $mcp_base"
for endpoint in "${api_base}/healthz" "${api_base}/readyz" "${mcp_base}/health"; do
  code="$(curl -sS -m 5 -o /dev/null -w '%{http_code}' "${endpoint}" || true)"
  if [[ "$code" != "200" ]]; then
    echo "run-auth-mcp-e2e: ${endpoint} returned ${code}; stack not ready" >&2
    exit 1
  fi
done

echo "run-auth-mcp-e2e: running the MCP-identity browser+scripted gate"
ESHU_E2E_API_BASE="$api_base" \
  ESHU_E2E_MCP_BASE="$mcp_base" \
  ESHU_E2E_POSTGRES_HOST="$bind_addr" \
  ESHU_E2E_POSTGRES_PORT="$postgres_port" \
  ESHU_E2E_POSTGRES_PASSWORD="$postgres_password" \
  ESHU_E2E_MOCK_OIDC_PORT="$mock_oidc_port" \
  ESHU_E2E_MOCK_OIDC_ADMIN_PORT="$mock_oidc_admin_port" \
  ESHU_E2E_MOCK_GITHUB_PORT="$mock_github_port" \
  ESHU_E2E_NORNICDB_HTTP_PORT="$nornicdb_http_port" \
  ESHU_E2E_MCP_MODULE="$runner_module" \
  ESHU_E2E_ESHU_BINARY="$AUTH_E2E_CLI_BIN" \
  node scripts/auth-mcp-e2e-runtime.mjs
