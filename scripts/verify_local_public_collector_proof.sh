#!/usr/bin/env bash

# verify_local_public_collector_proof.sh
#
# No-credential, public-endpoint collector proof gate (issue #3347).
#
# Brings up the default Docker Compose stack plus the workflow-coordinator with a
# small, bounded claim instance that collects against PUBLIC, unauthenticated
# endpoints only:
#
#   - vulnerability intelligence: CISA KEV, FIRST EPSS, OSV (NVD stays key-gated
#     and is intentionally excluded)
#   - package registry: public npm (registry.npmjs.org)
#
# It then claim-drives the two collector workers, waits for the reducer to drain
# to zero with no dead letters, and asserts aggregate fact counts plus API and
# MCP readback truth labels.
#
# Public-safe contract:
#   - No operator credentials are required or read.
#   - Output is aggregate-only: counts, states, and terminal queue depth.
#   - No targets, tokens, account IDs, registry hosts, repository names, raw
#     provider locators, or machine-specific paths are printed.
#   - Network access is bounded: small page/version limits and short timeouts.
#
# Live mode hits public network endpoints and requires Docker Compose. Use
# `--check` (alias `--dry-run`) for a no-network, no-Docker preflight that only
# validates required tooling, and `--help` for usage.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
RUNTIME_LIB="${REPO_ROOT}/scripts/lib/compose_verification_runtime_common.sh"

KEEP_STACK="${ESHU_KEEP_COMPOSE_STACK:-false}"
MODE="live"

# Bounded knobs. Kept small so the proof stays public-safe and fast. EPSS needs
# at least one CVE id to query; this is a well-known public CVE used only as a
# bounded EPSS/KEV probe, not operator data.
PROOF_EPSS_CVE_ID="${ESHU_PUBLIC_PROOF_EPSS_CVE_ID:-CVE-2021-44228}"
PROOF_NPM_PACKAGE="${ESHU_PUBLIC_PROOF_NPM_PACKAGE:-lodash}"
PROOF_NPM_VERSION_LIMIT="${ESHU_PUBLIC_PROOF_NPM_VERSION_LIMIT:-5}"
DRAIN_ATTEMPTS="${ESHU_PUBLIC_PROOF_DRAIN_ATTEMPTS:-180}"
DRAIN_SLEEP_SECONDS="${ESHU_PUBLIC_PROOF_DRAIN_SLEEP_SECONDS:-2}"
API_TIMEOUT_SECONDS="${ESHU_PUBLIC_PROOF_API_TIMEOUT_SECONDS:-30}"

# Default host ports. Each is rebased to a free port at runtime so this proof
# can run alongside other local stacks. The graph backend host ports are mapped
# through the NEO4J_* compose variables (the NORNICDB_* names are in-container).
POSTGRES_PORT_BASE="${ESHU_POSTGRES_PORT:-25432}"
NEO4J_HTTP_PORT_BASE="${NEO4J_HTTP_PORT:-27474}"
NEO4J_BOLT_PORT_BASE="${NEO4J_BOLT_PORT:-27687}"
ESHU_HTTP_PORT_BASE="${ESHU_HTTP_PORT:-28080}"
ESHU_MCP_PORT_BASE="${ESHU_MCP_PORT:-28081}"
ESHU_WORKFLOW_COORDINATOR_HTTP_PORT_BASE="${ESHU_WORKFLOW_COORDINATOR_HTTP_PORT:-28082}"
ESHU_API_METRICS_PORT_BASE="${ESHU_API_METRICS_PORT:-29464}"
ESHU_MCP_METRICS_PORT_BASE="${ESHU_MCP_METRICS_PORT:-29468}"
ESHU_RESOLUTION_ENGINE_METRICS_PORT_BASE="${ESHU_RESOLUTION_ENGINE_METRICS_PORT:-29466}"
ESHU_WORKFLOW_COORDINATOR_METRICS_PORT_BASE="${ESHU_WORKFLOW_COORDINATOR_METRICS_PORT:-29469}"

TMP_DIR=""
COMPOSE_CMD=()
COMPOSE_DISPLAY=""
API_BASE_URL=""
MCP_BASE_URL=""
API_KEY=""

usage() {
	cat <<'USAGE'
Usage: verify_local_public_collector_proof.sh [--check|--dry-run] [--keep-stack] [--help]

No-credential, public-endpoint collector proof gate (issue #3347).

Proves the vulnerability-intelligence (CISA KEV, FIRST EPSS, OSV) and
package-registry (public npm) lanes live on the default local Compose stack,
claim-driven through the workflow-coordinator, with no operator credentials.

Options:
  --check, --dry-run   Validate required tooling and exit. No Docker, no network.
  --keep-stack         Leave the Compose stack running after the proof.
  --help               Show this help and exit.

Environment overrides (all bounded, public-safe):
  ESHU_PUBLIC_PROOF_EPSS_CVE_ID        Public CVE id used as the EPSS/KEV probe.
  ESHU_PUBLIC_PROOF_NPM_PACKAGE        Public npm package name to collect.
  ESHU_PUBLIC_PROOF_NPM_VERSION_LIMIT  Max npm versions to fetch.
  ESHU_PUBLIC_PROOF_DRAIN_ATTEMPTS     Reducer drain poll attempts.
  ESHU_PUBLIC_PROOF_DRAIN_SLEEP_SECONDS Seconds between drain polls.
  ESHU_KEEP_COMPOSE_STACK=true         Same as --keep-stack.

Output is aggregate-only and public-safe: no targets, tokens, or paths.
USAGE
}

for arg in "$@"; do
	case "${arg}" in
		--check | --dry-run)
			MODE="check"
			;;
		--keep-stack)
			KEEP_STACK="true"
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			echo "Unknown argument: ${arg}" >&2
			usage >&2
			exit 2
			;;
	esac
done

require_tool() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required tool: $1" >&2
		exit 1
	}
}

resolve_compose_cmd() {
	if docker compose version >/dev/null 2>&1; then
		COMPOSE_CMD=(docker compose)
		COMPOSE_DISPLAY="docker compose"
	elif command -v docker-compose >/dev/null 2>&1; then
		COMPOSE_CMD=(docker-compose)
		COMPOSE_DISPLAY="docker-compose"
	else
		echo "Missing required compose command: docker compose or docker-compose" >&2
		exit 1
	fi
	COMPOSE_CMD+=(--profile workflow-coordinator)
	COMPOSE_DISPLAY+=" --profile workflow-coordinator"
}

configure_runtime_addresses() {
	# eshu_assign_reserved_port (from the shared runtime lib) tracks every port
	# it hands out within this run, so two services never receive the same host
	# port even when other local stacks already occupy the default range.
	eshu_reset_reserved_ports
	eshu_assign_reserved_port ESHU_POSTGRES_PORT "$POSTGRES_PORT_BASE"
	eshu_assign_reserved_port NEO4J_HTTP_PORT "$NEO4J_HTTP_PORT_BASE"
	eshu_assign_reserved_port NEO4J_BOLT_PORT "$NEO4J_BOLT_PORT_BASE"
	eshu_assign_reserved_port ESHU_HTTP_PORT "$ESHU_HTTP_PORT_BASE"
	eshu_assign_reserved_port ESHU_MCP_PORT "$ESHU_MCP_PORT_BASE"
	eshu_assign_reserved_port ESHU_WORKFLOW_COORDINATOR_HTTP_PORT "$ESHU_WORKFLOW_COORDINATOR_HTTP_PORT_BASE"
	eshu_assign_reserved_port ESHU_API_METRICS_PORT "$ESHU_API_METRICS_PORT_BASE"
	eshu_assign_reserved_port ESHU_MCP_METRICS_PORT "$ESHU_MCP_METRICS_PORT_BASE"
	eshu_assign_reserved_port ESHU_RESOLUTION_ENGINE_METRICS_PORT "$ESHU_RESOLUTION_ENGINE_METRICS_PORT_BASE"
	eshu_assign_reserved_port ESHU_WORKFLOW_COORDINATOR_METRICS_PORT "$ESHU_WORKFLOW_COORDINATOR_METRICS_PORT_BASE"

	API_BASE_URL="http://127.0.0.1:${ESHU_HTTP_PORT}/api/v0"
	MCP_BASE_URL="http://127.0.0.1:${ESHU_MCP_PORT}"
}

# build_public_claim_instances emits the coordinator claim instance JSON. Only
# public, unauthenticated sources are configured. NVD is intentionally omitted
# because it is key-gated.
build_public_claim_instances() {
	jq -n \
		--arg epss_cve "${PROOF_EPSS_CVE_ID}" \
		--arg npm_pkg "${PROOF_NPM_PACKAGE}" \
		--argjson npm_version_limit "${PROOF_NPM_VERSION_LIMIT}" \
		'[
			{
				instance_id: "public-proof-vulnerability-intelligence",
				collector_kind: "vulnerability_intelligence",
				mode: "continuous",
				enabled: true,
				claims_enabled: true,
				configuration: {
					targets: [
						{ source: "cisa_kev", scope_id: "vuln-intel://cisa/kev" },
						{ source: "first_epss", scope_id: "vuln-intel://first/epss", cve_ids: [$epss_cve] },
						{
							source: "osv",
							scope_id: "vuln-intel://osv/npm/public-proof",
							ecosystem: "npm",
							queries: [
								{ package: { name: $npm_pkg, ecosystem: "npm" } }
							]
						}
					]
				}
			},
			{
				instance_id: "public-proof-package-registry",
				collector_kind: "package_registry",
				mode: "continuous",
				enabled: true,
				claims_enabled: true,
				configuration: {
					targets: [
						{
							provider: "npm",
							ecosystem: "npm",
							registry: "https://registry.npmjs.org",
							scope_id: "package-registry://npm/npm/public-proof",
							packages: [$npm_pkg],
							package_limit: 1,
							version_limit: $npm_version_limit,
							visibility: "public",
							metadata_url: ("https://registry.npmjs.org/" + $npm_pkg),
							document_format: "native"
						}
					]
				}
			}
		]'
}

read_api_key() {
	# The single-quoted script is intentionally evaluated inside the container,
	# not by the host shell.
	# shellcheck disable=SC2016
	API_KEY="$("${COMPOSE_CMD[@]}" exec -T eshu sh -lc '
		token="${ESHU_API_KEY:-}"
		if [ -n "$token" ]; then
			printf %s "$token"
			exit 0
		fi
		home="${ESHU_HOME:-/data/.eshu}"
		if [ -f "$home/.env" ]; then
			sed -n "s/^ESHU_API_KEY=//p" "$home/.env" | tail -n 1 | tr -d "\n"
		fi
	')"
	if [[ -z "${API_KEY}" ]]; then
		echo "Could not resolve the locally auto-generated API key" >&2
		return 1
	fi
}

# api_get performs a bounded authenticated GET against the API.
api_get() {
	local path="$1" output_file="$2"
	local curl_config="${TMP_DIR}/curl-auth.conf"
	local escaped_api_key="${API_KEY//\\/\\\\}"
	escaped_api_key="${escaped_api_key//\"/\\\"}"
	printf 'header = "Authorization: Bearer %s"\n' "${escaped_api_key}" >"${curl_config}"
	chmod 600 "${curl_config}"
	curl -fsS -K "${curl_config}" --max-time "${API_TIMEOUT_SECONDS}" \
		"${API_BASE_URL}${path}" >"${output_file}"
}

# mcp_tool_envelope calls one MCP JSON-RPC tool and extracts the Eshu truth
# envelope, validating that it carries data and a truth label.
mcp_tool_envelope() {
	local tool_name="$1" args_json="$2" output_file="$3"
	local response_file="${TMP_DIR}/mcp-${tool_name}.json"
	local payload_file="${TMP_DIR}/mcp-${tool_name}-payload.json"
	local curl_config="${TMP_DIR}/mcp-curl.conf"
	jq -n --arg name "${tool_name}" --argjson arguments "${args_json}" \
		'{jsonrpc:"2.0", id:1, method:"tools/call", params:{name:$name, arguments:$arguments}}' >"${payload_file}"
	printf 'header = "Content-Type: application/json"\n' >"${curl_config}"
	local escaped_api_key="${API_KEY//\\/\\\\}"
	escaped_api_key="${escaped_api_key//\"/\\\"}"
	printf 'header = "Authorization: Bearer %s"\n' "${escaped_api_key}" >>"${curl_config}"
	chmod 600 "${curl_config}"
	curl -fsS -K "${curl_config}" --max-time "${API_TIMEOUT_SECONDS}" \
		--data-binary "@${payload_file}" "${MCP_BASE_URL}/mcp/message" >"${response_file}"
	if ! jq -e '(.error == null) and ((.result.isError // false) | not)' "${response_file}" >/dev/null; then
		echo "MCP tool ${tool_name} returned an error" >&2
		return 1
	fi
	local envelope_text
	envelope_text="$(jq -r 'first(.result.content[]? | select(.type == "resource" and .resource.uri == "eshu://tool-result/envelope") | .resource.text) // ""' "${response_file}")"
	if [[ -z "${envelope_text}" ]]; then
		echo "MCP tool ${tool_name} response missing Eshu envelope resource" >&2
		return 1
	fi
	printf '%s' "${envelope_text}" >"${output_file}"
	if ! jq -e 'has("data") and has("truth") and (.error == null)' "${output_file}" >/dev/null; then
		echo "MCP tool ${tool_name} envelope is invalid" >&2
		return 1
	fi
}

wait_for_http() {
	local url="$1" attempts="$2" sleep_seconds="$3"
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		curl -fsS "$url" >/dev/null 2>&1 && return 0
		sleep "$sleep_seconds"
	done
	echo "Timed out waiting for $url" >&2
	return 1
}

# wait_for_reducer_drain polls /index-status until the queue has drained to zero
# with no retrying, failed, or dead-letter work.
wait_for_reducer_drain() {
	local status_file="${TMP_DIR}/index-status.json"
	for ((attempt = 1; attempt <= DRAIN_ATTEMPTS; attempt++)); do
		if api_get "/index-status" "${status_file}" 2>/dev/null &&
			jq -e '
				((.queue.outstanding // 0) == 0) and
				((.queue.in_flight // 0) == 0) and
				((.queue.pending // 0) == 0) and
				((.queue.retrying // 0) == 0) and
				((.queue.failed // 0) == 0) and
				((.queue.dead_letter // 0) == 0)
			' "${status_file}" >/dev/null; then
			return 0
		fi
		sleep "${DRAIN_SLEEP_SECONDS}"
	done
	echo "Timed out waiting for reducer drain to zero" >&2
	if [[ -s "${status_file}" ]]; then
		jq -r '
			"last queue state: outstanding=\(.queue.outstanding // 0) in_flight=\(.queue.in_flight // 0) pending=\(.queue.pending // 0) retrying=\(.queue.retrying // 0) failed=\(.queue.failed // 0) dead_letter=\(.queue.dead_letter // 0)"
		' "${status_file}" >&2 || true
	fi
	return 1
}

# wait_for_positive_count polls an API count endpoint until the named integer
# field is positive, then prints the aggregate value.
wait_for_positive_count() {
	local path="$1" jq_filter="$2" attempts="$3"
	local count_file="${TMP_DIR}/count.json" value=""
	for ((attempt = 1; attempt <= attempts; attempt++)); do
		if api_get "${path}" "${count_file}" 2>/dev/null; then
			value="$(jq -r "${jq_filter} // 0" "${count_file}")"
			if [[ "${value}" =~ ^[1-9][0-9]*$ ]]; then
				echo "${value}"
				return 0
			fi
		fi
		sleep "${DRAIN_SLEEP_SECONDS}"
	done
	echo "Timed out waiting for a positive count from ${path}" >&2
	return 1
}

cleanup() {
	local exit_code=$?
	if [[ "${MODE}" != "check" && -n "${COMPOSE_DISPLAY}" ]]; then
		if [[ "${exit_code}" -ne 0 ]]; then
			echo
			echo "public-collector proof failed."
			echo "Inspect aggregate logs with:"
			echo "  ${COMPOSE_DISPLAY} logs --tail=200 workflow-coordinator resolution-engine eshu"
		fi
		if [[ "${KEEP_STACK}" != "true" ]]; then
			"${COMPOSE_CMD[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
		fi
	fi
	[[ -n "${TMP_DIR}" ]] && rm -rf "${TMP_DIR}"
	exit "${exit_code}"
}

main() {
	require_tool jq

	if [[ "${MODE}" == "check" ]]; then
		require_tool curl
		require_tool docker
		require_tool nc
		resolve_compose_cmd
		# Validate the claim instance JSON shape without contacting the network.
		build_public_claim_instances >/dev/null
		echo "public-collector proof preflight passed (check mode)."
		echo "Lanes: vulnerability-intelligence (CISA KEV, FIRST EPSS, OSV) + package-registry (public npm)."
		echo "No operator credentials required. NVD is excluded (key-gated)."
		echo "Run without --check to execute the live proof (requires Docker Compose + public network)."
		return 0
	fi

	require_tool curl
	require_tool docker
	require_tool nc

	# shellcheck source=scripts/lib/compose_verification_runtime_common.sh disable=SC1091
	source "${RUNTIME_LIB}"

	TMP_DIR="$(mktemp -d)"
	trap cleanup EXIT

	resolve_compose_cmd
	cd "${REPO_ROOT}"
	configure_runtime_addresses

	export ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE="active"
	export ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED="true"
	ESHU_COLLECTOR_INSTANCES_JSON="$(build_public_claim_instances)"
	export ESHU_COLLECTOR_INSTANCES_JSON

	echo "Starting default Compose stack + workflow-coordinator (public-safe, no credentials)..."
	"${COMPOSE_CMD[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
	"${COMPOSE_CMD[@]}" up -d --build \
		nornicdb postgres db-migrate eshu mcp-server resolution-engine workflow-coordinator

	echo "Waiting for API, MCP, and coordinator readiness..."
	wait_for_http "http://127.0.0.1:${ESHU_HTTP_PORT}/healthz" 120 2
	wait_for_http "http://127.0.0.1:${ESHU_HTTP_PORT}/readyz" 120 2
	wait_for_http "http://127.0.0.1:${ESHU_MCP_PORT}/healthz" 60 2
	wait_for_http "http://127.0.0.1:${ESHU_WORKFLOW_COORDINATOR_HTTP_PORT}/healthz" 120 2

	read_api_key

	echo "Claim-driving public collector workers..."
	# The collector workers are not part of the default stack; run them as
	# claim-driven workers in the coordinator service's environment, bound to the
	# coordinator instance ids above. The image has no ENTRYPOINT, so passing the
	# binary as the run command fully replaces the service CMD.
	"${COMPOSE_CMD[@]}" run -d --no-deps \
		-e ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID=public-proof-vulnerability-intelligence \
		-e ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID=public-proof-vulnerability-worker \
		-e ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL=2s \
		workflow-coordinator /usr/local/bin/eshu-collector-vulnerability-intelligence >/dev/null
	"${COMPOSE_CMD[@]}" run -d --no-deps \
		-e ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID=public-proof-package-registry \
		-e ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID=public-proof-package-worker \
		-e ESHU_PACKAGE_REGISTRY_POLL_INTERVAL=2s \
		workflow-coordinator /usr/local/bin/eshu-collector-package-registry >/dev/null

	echo "Waiting for facts to commit and the reducer to drain to zero..."
	# A positive advisory count and package count prove fact commit for both
	# public lanes; drain-to-zero proves the reducer cleared the claimed work
	# with no dead letters. The advisory catalog surface is anchorless and reads
	# directly from active vulnerability source facts, so a positive count there
	# is fact-commit truth for the KEV/EPSS/OSV lane.
	local advisory_count package_count
	advisory_count="$(wait_for_positive_count "/supply-chain/advisories?limit=1" '.count' 180)"
	package_count="$(wait_for_positive_count "/package-registry/packages/count?ecosystem=npm" '.total_packages' 180)"
	wait_for_reducer_drain

	echo "Asserting API readback truth labels..."
	local advisories_file detail_file
	advisories_file="${TMP_DIR}/advisories.json"
	api_get "/supply-chain/advisories?limit=1" "${advisories_file}"
	if ! jq -e '(.advisories | type == "array") and ((.count // 0) >= 1)' "${advisories_file}" >/dev/null; then
		echo "API advisory catalog readback returned no rows" >&2
		exit 1
	fi
	local advisory_key kev_label
	advisory_key="$(jq -r '.advisories[0].advisory_key // .advisories[0].canonical_id // ""' "${advisories_file}")"
	kev_label="$(jq -r '.advisories[0].kev // false' "${advisories_file}")"
	if [[ -z "${advisory_key}" ]]; then
		echo "API advisory catalog readback missing an advisory key" >&2
		exit 1
	fi
	detail_file="${TMP_DIR}/advisory-detail.json"
	api_get "/supply-chain/vulnerabilities/${advisory_key}" "${detail_file}"
	if ! jq -e 'has("sources") and (.sources | type == "array") and (.sources | length >= 1)' "${detail_file}" >/dev/null; then
		echo "API advisory detail readback missing source evidence" >&2
		exit 1
	fi

	echo "Asserting MCP readback truth labels..."
	local mcp_pkg_file mcp_truth mcp_count
	mcp_pkg_file="${TMP_DIR}/mcp-packages.json"
	mcp_tool_envelope "list_package_registry_packages" '{"ecosystem":"npm","limit":1}' "${mcp_pkg_file}"
	mcp_truth="$(jq -r '.truth.level // ""' "${mcp_pkg_file}")"
	mcp_count="$(jq -r '.data.count // (.data.packages | length) // 0' "${mcp_pkg_file}")"
	if [[ -z "${mcp_truth}" ]]; then
		echo "MCP package readback missing a truth label" >&2
		exit 1
	fi

	echo
	echo "public-collector proof passed."
	echo "Lanes proven live with no operator credentials:"
	echo "  vulnerability-intelligence (CISA KEV, FIRST EPSS, OSV)"
	echo "  package-registry (public npm)"
	echo "Aggregate evidence (public-safe):"
	echo "  supply-chain advisory evidence count: ${advisory_count}"
	echo "  npm package count: ${package_count}"
	echo "  API advisory catalog rows: >=1 (kev_label=${kev_label})"
	echo "  API advisory detail source evidence: present"
	echo "  MCP list_package_registry_packages: truth=${mcp_truth} count=${mcp_count}"
	echo "  reducer queue: drained to zero, 0 dead letters"
}

main "$@"
