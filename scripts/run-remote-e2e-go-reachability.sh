#!/usr/bin/env bash
set -euo pipefail

# Bring-up + capture driver for the go-reachability remote Compose proof
# (eshu-hq/eshu#10031, part of eshu-hq/eshu#10002). It stands up the base
# remote-e2e stack plus the scanner go-reachability fragment (both resolved
# from the required ESHU_DEPLOY_CHECKOUT checkout), waits for the
# scanner_worker work items to reach terminal success, shapes the runtime
# truth into normalized proof artifacts, then runs
# scripts/verify-remote-e2e-go-reachability.sh against them.
#
# Runs in evidence mode only: the driver captures what the stack committed
# and fails closed on anything else. It never replays a cassette and never
# fabricates findings — an empty or partial stack fails the verifier, it is
# not backfilled here. The seed image is the v0.1.0 scanner-go-reachability
# release (multi-arch index digest
# sha256:8e4059a2acbd020a761862a8196e25567b7119bb97ef42a228432c59f5e39284,
# verified against the GHCR package versions API and a manifest inspect):
# ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE must be a digest-pinned ref
# (name@sha256:<64 hex>); anything else is refused before any container
# starts, so a floating tag can never slip into the proof surface.
#
# The artifacts are normalized on purpose: only the fields the proof asserts
# are emitted (component trust/enablement, workflow terminal state,
# per-CVE finding wire values, bounded fact-family counts). Raw fact
# payloads and source URIs are never dumped, so the verifier's redaction
# canary holds by construction.
#
# Usage (from repo root, after exporting the required seed image):
#   export ESHU_DEPLOY_CHECKOUT=/path/to/eshu-deploy
#   export ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE=ghcr.io/eshu-hq/scanner-go-reachability@sha256:<digest>
#   scripts/run-remote-e2e-go-reachability.sh [--artifacts <dir>] [--keep-up]
#
# Environment overrides (defaults target the documented proof stack):
#   GOREACHABILITY_PROOF_PROJECT  docker compose project name (default: go-reachability-proof)
#   ESHU_GOREACHABILITY_FRAGMENT  scanner fragment path        (default: ${ESHU_DEPLOY_CHECKOUT}/compose/fragments/docker-compose.scanner-go-reachability.yaml)
#   ESHU_POSTGRES_PORT            postgres host port           (default: 25434)
#   ESHU_HTTP_PORT                API host port                (default: 28086)
#   ESHU_MCP_PORT                 MCP host port                (default: 28087)
#   ESHU_WORKFLOW_COORDINATOR_HTTP_PORT    coordinator host port    (default: 28089)
#   ESHU_WORKFLOW_COORDINATOR_METRICS_PORT coordinator metrics port (default: 29471)

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
cd "${repo_root}"

# The deploy stack builds the eshu image from ${ESHU_REPO_ROOT:-.} (base
# stack convention). Default it at this checkout so every build context
# resolves to an eshu root with its Dockerfile. An explicit ESHU_REPO_ROOT
# wins for operators who build from a different checkout of the same commit.
export ESHU_REPO_ROOT="${ESHU_REPO_ROOT:-${repo_root}}"
project="${GOREACHABILITY_PROOF_PROJECT:-go-reachability-proof}"
worker="${project}-collector-go-reachability-1"
postgres="${project}-postgres-1"
api="${project}-eshu-1"
mcp="${project}-mcp-server-1"
component_id="eshu.scanner.go-reachability"
component_home="/components"
artifacts_dir=""
keep_up=false

die() {
	printf 'run-remote-e2e-go-reachability: %s\n' "$*" >&2
	exit 1
}

log() {
	printf 'run-remote-e2e-go-reachability: %s\n' "$*"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		--keep-up) keep_up=true; shift ;;
		-h|--help) sed -n '3,40p' "$0"; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

# Required checkout first: every path below resolves from it, so a missing
# checkout must fail here with its own message, not as a missing file later.
[[ -n "${ESHU_DEPLOY_CHECKOUT:-}" ]] || die "ESHU_DEPLOY_CHECKOUT must point at an eshu-hq/eshu-deploy checkout"
base_compose="${ESHU_DEPLOY_CHECKOUT}/compose/docker-compose.remote-e2e.yaml"
fragment="${ESHU_GOREACHABILITY_FRAGMENT:-${ESHU_DEPLOY_CHECKOUT}/compose/fragments/docker-compose.scanner-go-reachability.yaml}"

# Digest gate: the seed image must be a digest-pinned ref. Tags, digests
# with the wrong length or alphabet, and non-sha256 algorithms are refused
# before any container starts. This gate runs before the tool checks so the
# refusal is hermetic: it needs no docker, no network, no live backend.
seed_image="${ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE:-}"
[[ -n "${seed_image}" ]] || die "ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE is required (name@sha256:<64 hex>; no floating tags)"
[[ "${seed_image}" =~ ^[^@[:space:]]+@sha256:[0-9a-f]{64}$ ]] \
	|| die "ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE must be a digest-pinned ref (name@sha256:<64 hex>); refusing '${seed_image%%@*}@<'redacted'>'"
seed_digest="sha256:${seed_image##*@sha256:}"

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v jq >/dev/null 2>&1 || die "jq is required"
command -v rg >/dev/null 2>&1 || die "rg is required"
docker compose version >/dev/null 2>&1 || die "docker compose v2+ is required"

[[ -f "${base_compose}" ]] || die "base compose file not found: ${base_compose} (is ESHU_DEPLOY_CHECKOUT current?)"
[[ -f "${fragment}" ]] || die "scanner fragment not found: ${fragment} (needs eshu-hq/eshu-deploy#40)"

if [[ -z "${artifacts_dir}" ]]; then
	artifacts_dir="$(mktemp -d "${TMPDIR:-/tmp}/go-reachability-proof-artifacts.XXXXXX")"
fi
mkdir -p "${artifacts_dir}"

proof_deps_overlay="${repo_root}/scripts/docker-compose.go-reachability-proof-deps.yaml"

compose() {
	# Backend topology lives in the proof overlay (not the shared neo4j
	# overlay: that file redefines services with bare `context: .` and
	# profile-gates the coordinator, both of which break this proof).
	docker compose -p "${project}" -f "${base_compose}" -f "${fragment}" -f "${proof_deps_overlay}" "$@"
}

# Remap every published host port into a high range so the proof stack never
# collides with another Eshu stack an operator may already be running.
export ESHU_POSTGRES_PORT="${ESHU_POSTGRES_PORT:-25434}"
export ESHU_HTTP_PORT="${ESHU_HTTP_PORT:-28086}"
export ESHU_MCP_PORT="${ESHU_MCP_PORT:-28087}"
export ESHU_WORKFLOW_COORDINATOR_HTTP_PORT="${ESHU_WORKFLOW_COORDINATOR_HTTP_PORT:-28089}"
export ESHU_WORKFLOW_COORDINATOR_METRICS_PORT="${ESHU_WORKFLOW_COORDINATOR_METRICS_PORT:-29471}"
# The graph backend publishes the Neo4j-compatible ports on the host; remap
# them too so the proof stack never collides with another live Eshu stack
# (or with zombie bindings orphaned by killed runs).
export NEO4J_HTTP_PORT="${NEO4J_HTTP_PORT:-27574}"
export NEO4J_BOLT_PORT="${NEO4J_BOLT_PORT:-27787}"
export ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE="${seed_image}"
proof_services="postgres db-migrate bootstrap-index workflow-coordinator collector-go-reachability eshu mcp-server"

log "bringing up go-reachability proof stack (project ${project})"
compose config >/dev/null \
	|| die "compose config failed; export the missing variables it names and retry"
# shellcheck disable=SC2086
compose up -d --build ${proof_services}

cleanup() {
	if [[ "${keep_up}" == false ]]; then
		log "tearing down stack (pass --keep-up to inspect)"
		# Project-scoped teardown (no profile filter) so every service —
		# including the default-profile dependencies depends_on pulls in —
		# is removed.
		docker compose -p "${project}" down -v --remove-orphans >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

# Wait for the scanner_worker work items to reach terminal success. A failed
# or dead-letter item dumps the rows for triage and fails closed; the
# verifier re-asserts the terminal state on the captured artifacts. The 5s
# poll and 600s bound mirror the component-extension OCI driver; the bound
# is wider because a govulncheck symbol scan takes longer than a component
# install.
log "waiting for scanner_worker work items to complete"
deadline=$(( $(date +%s) + 600 ))
terminal=false
while [[ "$(date +%s)" -lt "${deadline}" ]]; do
	if ! docker inspect "${postgres}" >/dev/null 2>&1; then
		sleep 5; continue
	fi
	rows="$(docker exec "${postgres}" psql -U eshu -d eshu -tAF'|' -c \
		"SELECT status, count(*) FROM workflow_work_items WHERE collector_kind='scanner_worker' GROUP BY status;" 2>/dev/null || true)"
	if printf '%s' "${rows}" | rg -q '^(failed|dead_letter)\|'; then
		docker exec "${postgres}" psql -U eshu -d eshu -c \
			"SELECT work_item_id,status,last_error FROM workflow_work_items WHERE collector_kind='scanner_worker';" || true
		die "scanner_worker work item entered failed/dead_letter state"
	fi
	if printf '%s' "${rows}" | rg -q '^completed\|[1-9]'; then
		terminal=true; break
	fi
	sleep 5
done
[[ "${terminal}" == true ]] || { compose logs --tail=60 collector-go-reachability || true; die "timed out waiting for terminal success"; }
log "scanner_worker work items reached terminal success"

docker inspect "${worker}" >/dev/null 2>&1 || die "worker container not found: ${worker}"
docker inspect "${api}" >/dev/null 2>&1 || die "API container not found: ${api}"
docker inspect "${mcp}" >/dev/null 2>&1 || die "MCP container not found: ${mcp}"

capture_findings_http() {
	local cve_id="$1"
	local output="$2"
	docker exec --env GOREACHABILITY_CVE_PATH="/api/v0/supply-chain/impact/findings?limit=10&cve_id=${cve_id}&profile=comprehensive" "${api}" sh -c '
		token=""
		while IFS= read -r line; do
			case "$line" in
				ESHU_API_KEY=*) token="${line#ESHU_API_KEY=}" ;;
			esac
		done < /data/.eshu/.env
		[ -n "$token" ] || exit 2
		curl -fsS \
			-H "Authorization: Bearer ${token}" \
			-H "Accept: application/eshu.envelope+json" \
			"http://localhost:8080${GOREACHABILITY_CVE_PATH}"
	' >"${output}"
}

capture_findings_mcp() {
	local cve_id="$1"
	local output="$2"
	local response
	local payload
	response="$(mktemp "${TMPDIR:-/tmp}/go-reachability-mcp-response.XXXXXX")"
	payload="$(jq -nc --arg cve "${cve_id}" \
		'{jsonrpc:"2.0", id:1, method:"tools/call", params:{name:"list_supply_chain_impact_findings", arguments:{cve_id:$cve, limit:10, profile:"comprehensive"}}}')"
	docker exec --env GOREACHABILITY_PAYLOAD="${payload}" "${mcp}" sh -c '
		token=""
		while IFS= read -r line; do
			case "$line" in
				ESHU_API_KEY=*) token="${line#ESHU_API_KEY=}" ;;
			esac
		done < /data/.eshu/.env
		[ -n "$token" ] || exit 2
		curl -fsS -X POST \
			-H "Authorization: Bearer ${token}" \
			-H "Content-Type: application/json" \
			--data "${GOREACHABILITY_PAYLOAD}" \
			http://localhost:8080/mcp/message
	' >"${response}"
	jq -e '.jsonrpc == "2.0" and .error == null and (.result | type == "object") and (.result.isError == null or .result.isError == false)' "${response}" >/dev/null \
		|| die "MCP tool failed: list_supply_chain_impact_findings ${cve_id}"
	jq -e '
		(if (.result | has("structuredContent")) then .result.structuredContent
		 else first(.result.content[]? | select(.type == "resource" and .resource.uri == "eshu://tool-result/envelope") | .resource.text | fromjson)
		 end)
		| select(type == "object" and has("data") and has("truth") and .error == null and (.data.findings | type == "array"))
	' "${response}" >"${output}" || die "MCP tool response missing findings envelope: ${cve_id}"
	rm -f "${response}"
}

# 1. Inventory: component trust readback plus the digest-pinned seed image.
#    Trust flags mirror the allowlist policy the reconcile one-shot installs
#    under; the digest gate above already refused anything unpinned.
cli_json="$(docker exec "${worker}" eshu component list \
	--component-home "${component_home}" \
	--trust-mode allowlist \
	--allow-id "${component_id}" \
	--allow-publisher eshu-hq \
	--json)"

state_block="$(printf '%s' "${cli_json}" | rg -U -o '"states"\s*:\s*\[[^]]*\]' || true)"
installed=false; enabled=false
printf '%s' "${state_block}" | rg --quiet '"installed"' && installed=true
printf '%s' "${state_block}" | rg --quiet '"enabled"' && enabled=true
trusted=false
printf '%s' "${cli_json}" | rg --quiet '"verified"\s*:\s*true' && trusted=true
digest="$(printf '%s' "${cli_json}" | rg -o '"manifest_digest"\s*:\s*"[^"]*"' | rg -o 'sha256:[A-Fa-f0-9]+' || echo "")"

cat >"${artifacts_dir}/inventory.json" <<JSON
{
  "component_id": "${component_id}",
  "installed": ${installed},
  "enabled": ${enabled},
  "trusted": ${trusted},
  "manifest_digest": "${digest}",
  "seed_image": "${seed_image}",
  "seed_digest": "${seed_digest}"
}
JSON

# 2. Findings per CVE on both public surfaces: the deployed HTTP read and
#    the deployed MCP tool must independently prove all three recorded
#    verdicts (symbol_reachable, not_called, analysis_failed).
for cve_id in CVE-2026-9229 CVE-2026-9301 CVE-2026-9401; do
	lower="$(printf '%s' "${cve_id}" | tr '[:upper:]-' '[:lower:]_')"
	capture_findings_http "${cve_id}" "${artifacts_dir}/api-findings-${lower}.json"
	capture_findings_mcp "${cve_id}" "${artifacts_dir}/mcp-findings-${lower}.json"
done

# 3. Workflow items: terminal state per scanner_worker work item. Postgres
#    'completed' maps to the proof's terminal-success state; any
#    non-terminal or failed status is preserved verbatim so the verifier
#    can fail closed.
work_rows="$(docker exec "${postgres}" psql -U eshu -d eshu -tAF'|' -c \
	"SELECT work_item_id, collector_kind, status FROM workflow_work_items WHERE collector_kind='scanner_worker' ORDER BY work_item_id;")"
{
	printf '{\n  "items": [\n'
	first=true
	while IFS='|' read -r wid kind status; do
		[[ -n "${wid}" ]] || continue
		[[ "${first}" == true ]] && first=false || printf ',\n'
		printf '    {"work_item_id": "%s", "collector_kind": "%s", "state": "%s"}' "${wid}" "${kind}" "${status}"
	done <<<"${work_rows}"
	printf '\n  ]\n}\n'
} >"${artifacts_dir}/workflow-items.json"

# 4. Facts: committed vulnerability fact-family counts. Counts only, never
#    payloads, so no source material can leak into the proof surface.
fact_rows="$(docker exec "${postgres}" psql -U eshu -d eshu -tAF'|' -c \
	"SELECT fact_kind, count(*) FROM fact_records WHERE fact_kind IN ('vulnerability.go_module_evidence','vulnerability.go_call_reachability','scanner_worker.warning') GROUP BY fact_kind ORDER BY fact_kind;")"
{
	printf '{\n'
	first=true
	while IFS='|' read -r kind count; do
		[[ -n "${kind}" ]] || continue
		[[ "${first}" == true ]] && first=false || printf ',\n'
		printf '  "%s": %s' "${kind}" "${count}"
	done <<<"${fact_rows}"
	printf '\n}\n'
} >"${artifacts_dir}/facts.json"

# 5. Provenance: the immutable facts that make this run reproducible and
#    auditable — Eshu commit, digest-pinned seed image, backend, queue
#    terminal state, and the telemetry handle. Only low-cardinality
#    identifiers, version strings, and port-only metric handles are
#    recorded, so no host path, hostname, IP, or credential can leak into
#    the proof surface.
eshu_commit="$(git -C "${repo_root}" rev-parse HEAD 2>/dev/null || echo "unknown")"
core_version="$(docker exec "${worker}" eshu version 2>/dev/null | rg -o '[0-9A-Za-z._+-]+$' | head -1 || echo "unknown")"
backend="$(docker exec "${worker}" sh -c 'printf %s "${ESHU_GRAPH_BACKEND:-nornicdb}"' 2>/dev/null || echo "nornicdb")"
queue_state="$(docker exec "${postgres}" psql -U eshu -d eshu -tAc \
	"SELECT string_agg(DISTINCT status, ',' ORDER BY status) FROM workflow_work_items WHERE collector_kind='scanner_worker';" 2>/dev/null | tr -d '[:space:]')"
[[ -n "${queue_state}" ]] || queue_state="none"
cat >"${artifacts_dir}/provenance.json" <<JSON
{
  "eshu_commit": "${eshu_commit}",
  "component_id": "${component_id}",
  "seed_image": "${seed_image}",
  "seed_digest": "${seed_digest}",
  "component_digest": "${digest}",
  "core_version": "${core_version}",
  "backend": "${backend}",
  "queue_terminal_state": "${queue_state}",
  "metrics_handle": ":29471/metrics",
  "evidence_mode": true,
  "source_evidence_only": true
}
JSON

printf 'captured proof artifacts to %s\n' "${artifacts_dir}"
"${repo_root}/scripts/verify-remote-e2e-go-reachability.sh" --artifacts "${artifacts_dir}"
