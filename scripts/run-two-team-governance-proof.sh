#!/usr/bin/env bash
set -euo pipefail

# Live driver for the two-team hosted governance cross-scope denial proof
# (#1910). It stands up a real Eshu Compose stack (base + the two-team overlay
# docs/public/run-locally/docker-compose.governance-two-team.yaml), seeds two
# operator-managed scoped tokens through ESHU_SCOPED_TOKENS_FILE, and asserts
# through the LIVE API and MCP surfaces that:
#
#   - team-A's token reads only team-A's repository and CANNOT see team-B's,
#   - team-B's token reads only team-B's repository and CANNOT see team-A's,
#   - the single-repository selector for an out-of-grant repo fails closed (404),
#   - an admin (all-scopes) token sees every repository,
#   - unauthenticated reads are rejected (401), and
#   - the API and MCP readbacks agree (parity).
#
# It shapes those results into normalized proof artifacts (counts and HTTP
# states only, never response bodies or token material) and runs
# scripts/verify-two-team-governance-proof.sh against them.
#
# Usage (from repo root):
#   scripts/run-two-team-governance-proof.sh [--artifacts <dir>] [--keep-up]
#
# Environment overrides:
#   GOV_PROOF_PROJECT     compose project name      (default: gov-two-team)
#   GOV_PROOF_API_PORT    host port for the API     (default: 28080)
#   GOV_PROOF_MCP_PORT    host port for the MCP srv  (default: 28081)
#   GOV_PROOF_PG_PORT     host port for postgres     (default: 25932)
#   GOV_PROOF_NEO4J_HTTP  host port for nornicdb http(default: 27574)
#   GOV_PROOF_NEO4J_BOLT  host port for nornicdb bolt(default: 27787)

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"

project="${GOV_PROOF_PROJECT:-gov-two-team}"
api_port="${GOV_PROOF_API_PORT:-28080}"
mcp_port="${GOV_PROOF_MCP_PORT:-28081}"
pg_port="${GOV_PROOF_PG_PORT:-25932}"
neo4j_http="${GOV_PROOF_NEO4J_HTTP:-27574}"
neo4j_bolt="${GOV_PROOF_NEO4J_BOLT:-27787}"
artifacts_dir=""
keep_up=false

admin_token="gov-proof-admin-$(date +%s)"
team_a_token="gov-proof-team-a-$(date +%s)"
team_b_token="gov-proof-team-b-$(date +%s)"

die() {
	printf 'run-two-team-governance-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		--keep-up) keep_up=true; shift ;;
		-h|--help) sed -n '3,33p' "$0"; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v rg >/dev/null 2>&1 || die "rg is required"
command -v curl >/dev/null 2>&1 || die "curl is required"
sha256_cmd=""
if command -v shasum >/dev/null 2>&1; then
	sha256_cmd="shasum -a 256"
elif command -v sha256sum >/dev/null 2>&1; then
	sha256_cmd="sha256sum"
else
	die "shasum or sha256sum is required"
fi

if [[ -z "${artifacts_dir}" ]]; then
	artifacts_dir="$(mktemp -d "${TMPDIR:-/tmp}/gov-two-team-artifacts.XXXXXX")"
fi
mkdir -p "${artifacts_dir}"

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/gov-two-team-work.XXXXXX")"
tokens_file="${work_dir}/scoped-tokens.json"

compose() {
	docker compose \
		-p "${project}" \
		-f "${repo_root}/docker-compose.yaml" \
		-f "${repo_root}/docs/public/run-locally/docker-compose.governance-two-team.yaml" \
		"$@"
}

cleanup() {
	if [[ "${keep_up}" != true ]]; then
		compose down -v --remove-orphans >/dev/null 2>&1 || true
	fi
	rm -rf "${work_dir}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

sha256_hex() {
	printf '%s' "$1" | ${sha256_cmd} | rg -o '^[0-9a-f]{64}' | head -1
}

# Shared compose environment. Ports are remapped to avoid colliding with other
# local stacks; the admin token is the shared bearer that resolves to all-scopes.
export ESHU_HTTP_PORT="${api_port}"
export ESHU_MCP_PORT="${mcp_port}"
export ESHU_POSTGRES_PORT="${pg_port}"
export NEO4J_HTTP_PORT="${neo4j_http}"
export NEO4J_BOLT_PORT="${neo4j_bolt}"
export ESHU_API_KEY="${admin_token}"
export ESHU_AUTO_GENERATE_API_KEY="false"
# Remap every auxiliary host port the base compose exposes so the proof stack
# never collides with another local stack's metrics/aux listeners.
export ESHU_API_METRICS_PORT="${GOV_PROOF_API_METRICS_PORT:-29464}"
export ESHU_MCP_METRICS_PORT="${GOV_PROOF_MCP_METRICS_PORT:-29468}"
export ESHU_INGESTER_METRICS_PORT="${GOV_PROOF_INGESTER_METRICS_PORT:-29465}"
export ESHU_RESOLUTION_ENGINE_METRICS_PORT="${GOV_PROOF_RE_METRICS_PORT:-29466}"
export ESHU_BOOTSTRAP_METRICS_PORT="${GOV_PROOF_BOOTSTRAP_METRICS_PORT:-29467}"
export ESHU_WORKFLOW_COORDINATOR_METRICS_PORT="${GOV_PROOF_WC_METRICS_PORT:-29469}"
export ESHU_WORKFLOW_COORDINATOR_HTTP_PORT="${GOV_PROOF_WC_HTTP_PORT:-28082}"
export ESHU_WEBHOOK_LISTENER_HTTP_PORT="${GOV_PROOF_WEBHOOK_HTTP_PORT:-28083}"
export ESHU_COMPONENT_EXTENSION_COLLECTOR_HTTP_PORT="${GOV_PROOF_CE_HTTP_PORT:-28084}"
export ESHU_COMPONENT_EXTENSION_COLLECTOR_METRICS_PORT="${GOV_PROOF_CE_METRICS_PORT:-29470}"
# Phase 1: bring the stack up with NO scoped-token registry so the admin token
# can enumerate every ingested repository.
export ESHU_SCOPED_TOKENS_FILE=""
export GOV_PROOF_TOKENS_HOST_PATH="/dev/null"

api_base="http://localhost:${api_port}"
mcp_base="http://localhost:${mcp_port}"

printf '==> building and starting two-team governance stack (project=%s)\n' "${project}"
compose up -d --build

wait_for() {
	local url="$1" want="$2" tries=60
	while [[ "${tries}" -gt 0 ]]; do
		code="$(curl -s -o /dev/null -w '%{http_code}' "${url}" || echo 000)"
		[[ "${code}" == "${want}" ]] && return 0
		tries=$((tries - 1))
		sleep 5
	done
	return 1
}

printf '==> waiting for API health\n'
wait_for "${api_base}/health" "200" || die "API never became healthy on ${api_base}"
printf '==> waiting for MCP health\n'
wait_for "${mcp_base}/healthz" "200" || die "MCP never became healthy on ${mcp_base}"

# api_repo_ids prints the repository ids the API returns for a given bearer token,
# one per line. Repository ids carry a "repository:" prefix, so matching that
# token avoids the JSON-RPC envelope's own numeric/string id fields. counts and
# states are derived from this downstream; the raw body is never persisted.
api_repo_ids() {
	local token="$1"
	curl -s -H "Authorization: Bearer ${token}" "${api_base}/api/v0/repositories" \
		| rg -o 'repository:[A-Za-z0-9_]+' | sort -u
}

# mcp_repo_ids prints the repository ids the MCP list_indexed_repositories tool
# returns for a given bearer token, one per line. It drives the genuine MCP tool
# dispatch path (POST /mcp/message, JSON-RPC tools/call), which applies the same
# scoped-token resolver in a separate process, so this is an independent surface,
# not an API proxy. The result envelope escapes the repository ids, so the
# "repository:" token match still finds them.
mcp_repo_ids() {
	local token="$1"
	curl -s -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
		-X POST "${mcp_base}/mcp/message" \
		--data '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_indexed_repositories","arguments":{}}}' \
		| rg -o 'repository:[A-Za-z0-9_]+' | sort -u
}

http_status() {
	curl -s -o /dev/null -w '%{http_code}' "$@"
}

# Phase 1 readback: admin enumerates every repository, pick two distinct ids.
printf '==> reading back repositories with admin (all-scopes) token\n'
admin_ids="$(api_repo_ids "${admin_token}" | sort -u)"
admin_count="$(printf '%s\n' "${admin_ids}" | rg -c '^.+$' || echo 0)"
[[ "${admin_count}" -ge 2 ]] || die "admin enumerated ${admin_count} repositories; need at least 2 (check bootstrap ingestion)"

repo_a="$(printf '%s\n' "${admin_ids}" | sed -n '1p')"
repo_b="$(printf '%s\n' "${admin_ids}" | sed -n '2p')"
[[ -n "${repo_a}" && -n "${repo_b}" && "${repo_a}" != "${repo_b}" ]] || die "could not pick two distinct repositories"
printf '    team-A repo: %s\n    team-B repo: %s\n' "${repo_a}" "${repo_b}"

cat >"${artifacts_dir}/admin.json" <<JSON
{
  "tenant": "admin",
  "repository_count": ${admin_count},
  "team_a_repo": "${repo_a}",
  "team_b_repo": "${repo_b}"
}
JSON

# Phase 2: write the scoped-token registry granting team-A repo_a and team-B
# repo_b, then remount it and recreate the API and MCP services so the registry
# takes effect. The registry stores only token hashes, never the raw tokens.
admin_hash="$(sha256_hex "${admin_token}")"
team_a_hash="$(sha256_hex "${team_a_token}")"
team_b_hash="$(sha256_hex "${team_b_token}")"

cat >"${tokens_file}" <<JSON
{
  "version": 1,
  "tokens": [
    {"token_sha256": "${admin_hash}", "tenant_id": "tenant-admin", "workspace_id": "ws-admin", "all_scopes": true},
    {"token_sha256": "${team_a_hash}", "tenant_id": "tenant-a", "workspace_id": "ws-a", "allowed_repository_ids": ["${repo_a}"]},
    {"token_sha256": "${team_b_hash}", "tenant_id": "tenant-b", "workspace_id": "ws-b", "allowed_repository_ids": ["${repo_b}"]}
  ]
}
JSON

printf '==> remounting scoped-token registry and recreating API + MCP\n'
export ESHU_SCOPED_TOKENS_FILE="/run/secrets/scoped-tokens.json"
export GOV_PROOF_TOKENS_HOST_PATH="${tokens_file}"
compose up -d --no-deps --force-recreate eshu mcp-server

printf '==> waiting for API + MCP to come back with the registry mounted\n'
wait_for "${api_base}/health" "200" || die "API did not return after registry mount"
wait_for "${mcp_base}/healthz" "200" || die "MCP did not return after registry mount"

# Helper: does the id list contain a given id?
contains_id() {
	rg -q -F -x "$1"
}

# capture_team writes a normalized team artifact (counts + presence booleans +
# selector status, per surface). own/other repo presence is derived from the
# live id lists; the selector status is the HTTP code for the OTHER team's repo.
capture_team() {
	local file="$1" token="$2" own="$3" other="$4"

	local api_ids mcp_ids
	api_ids="$(api_repo_ids "${token}" | sort -u)"
	mcp_ids="$(mcp_repo_ids "${token}")"

	local api_count mcp_count
	api_count="$(printf '%s\n' "${api_ids}" | rg -c '^.+$' || echo 0)"
	mcp_count="$(printf '%s\n' "${mcp_ids}" | rg -c '^.+$' || echo 0)"

	# Presence checks must not abort under set -e: an absent (correctly denied)
	# id makes rg exit non-zero, which is the success path for the OTHER repo.
	local api_own=false api_other=false mcp_own=false mcp_other=false
	printf '%s\n' "${api_ids}" | contains_id "${own}"   && api_own=true   || true
	printf '%s\n' "${api_ids}" | contains_id "${other}" && api_other=true || true
	printf '%s\n' "${mcp_ids}" | contains_id "${own}"   && mcp_own=true   || true
	printf '%s\n' "${mcp_ids}" | contains_id "${other}" && mcp_other=true || true

	# Single-repository context selector for the OTHER team's repo. This route is
	# not in the scoped-read allowlist, so it fails closed with 403
	# permission_denied for any scoped token (defense in depth: scoped tokens
	# cannot reach the richer single-repository surface at all). The captured
	# status is whatever the live server returns; the verifier asserts 403.
	local api_sel mcp_sel
	api_sel="$(http_status -H "Authorization: Bearer ${token}" "${api_base}/api/v0/repositories/${other}/context")"
	mcp_sel="$(http_status -H "Authorization: Bearer ${token}" "${mcp_base}/api/v0/repositories/${other}/context")"

	cat >"${file}" <<JSON
{
  "own_repo": "${own}",
  "other_repo": "${other}",
  "api_repository_count": ${api_count},
  "api_own_repo_present": "${api_own}",
  "api_other_repo_present": "${api_other}",
  "api_other_repo_selector_status": ${api_sel},
  "mcp_repository_count": ${mcp_count},
  "mcp_own_repo_present": "${mcp_own}",
  "mcp_other_repo_present": "${mcp_other}",
  "mcp_other_repo_selector_status": ${mcp_sel}
}
JSON
}

printf '==> capturing team-A scoped reads (allowed=%s, denied=%s)\n' "${repo_a}" "${repo_b}"
capture_team "${artifacts_dir}/team-a.json" "${team_a_token}" "${repo_a}" "${repo_b}"
printf '==> capturing team-B scoped reads (allowed=%s, denied=%s)\n' "${repo_b}" "${repo_a}"
capture_team "${artifacts_dir}/team-b.json" "${team_b_token}" "${repo_b}" "${repo_a}"

# Unauthenticated reads must be rejected on both surfaces. The MCP server's
# bearer-auth middleware wraps the mounted /api/* read routes (the JSON-RPC
# /mcp/message transport is the protocol channel, not a bearer-gated read), so
# the unauth rejection is asserted on the auth-wrapped MCP repository route.
printf '==> capturing unauthenticated rejection states\n'
unauth_api="$(http_status "${api_base}/api/v0/repositories")"
unauth_mcp="$(http_status "${mcp_base}/api/v0/repositories")"
cat >"${artifacts_dir}/unauth.json" <<JSON
{
  "api_status": ${unauth_api},
  "mcp_status": ${unauth_mcp}
}
JSON

# Provenance: reproducibility/audit metadata. Only low-cardinality identifiers
# and a port-only metrics handle are recorded; no token, host path, or IP leaks.
eshu_commit="$(git -C "${repo_root}" rev-parse --short HEAD 2>/dev/null || echo unknown)"
backend="$(compose exec -T eshu printenv ESHU_GRAPH_BACKEND 2>/dev/null | tr -d '[:space:]' || echo nornicdb)"
[[ -n "${backend}" ]] || backend="nornicdb"
cat >"${artifacts_dir}/provenance.json" <<JSON
{
  "eshu_commit": "${eshu_commit}",
  "backend": "${backend}",
  "registry_token_count": 3,
  "metrics_handle": ":9464/metrics",
  "counts_and_states_only": true
}
JSON

printf 'captured proof artifacts to %s\n' "${artifacts_dir}"
"${repo_root}/scripts/verify-two-team-governance-proof.sh" --artifacts "${artifacts_dir}"
