#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
RUNTIME_LIB="${REPO_ROOT}/scripts/lib/compose_verification_runtime_common.sh"

COMPOSE_FILES="${ESHU_REMOTE_E2E_COMPOSE_FILES:-docker-compose.remote-e2e.yaml}"
COMPOSE_ENV_FILE="${ESHU_REMOTE_E2E_ENV_FILE:-}"
API_BASE_URL="${ESHU_REMOTE_E2E_API_BASE_URL:-}"
API_KEY="${ESHU_REMOTE_E2E_API_KEY:-}"
CORE_SERVICES="${ESHU_REMOTE_E2E_REQUIRED_SERVICES:-eshu mcp-server ingester resolution-engine workflow-coordinator}"
COLLECTOR_SERVICES="${ESHU_REMOTE_E2E_COLLECTOR_SERVICES:-collector-terraform-state collector-oci-registry collector-package-registry collector-aws-cloud}"
EXTRA_SERVICES="${ESHU_REMOTE_E2E_EXTRA_SERVICES:-}"
TMP_DIR="$(mktemp -d)"
INDEX_STATUS_FILE="${TMP_DIR}/index-status.json"
COMPOSE_CMD=()

# shellcheck source=scripts/lib/compose_verification_runtime_common.sh disable=SC1091
source "${RUNTIME_LIB}"

cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

configure_compose() {
	COMPOSE_CMD=(docker compose)
	if [[ -n "${COMPOSE_ENV_FILE}" ]]; then
		COMPOSE_CMD+=(--env-file "${COMPOSE_ENV_FILE}")
	fi

	local compose_file
	IFS=':' read -r -a compose_file_paths <<<"${COMPOSE_FILES}"
	for compose_file in "${compose_file_paths[@]}"; do
		[[ -n "${compose_file}" ]] || continue
		COMPOSE_CMD+=(-f "${compose_file}")
	done
}

inspect_service_state() {
	local service="$1"
	local container_id snapshot runtime_state health_state

	container_id="$("${COMPOSE_CMD[@]}" ps -q "${service}")"
	if [[ -z "${container_id}" ]]; then
		echo "remote E2E service ${service} has no container; start it before accepting the run" >&2
		return 1
	fi

	snapshot="$(docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "${container_id}" 2>/dev/null || true)"
	if [[ -z "${snapshot}" ]]; then
		echo "remote E2E service ${service} could not be inspected" >&2
		return 1
	fi

	read -r runtime_state health_state <<<"${snapshot}"
	if [[ "${runtime_state}" != "running" ]]; then
		echo "remote E2E service ${service} is ${runtime_state}; expected running" >&2
		return 1
	fi
	if [[ "${health_state}" != "none" && "${health_state}" != "healthy" ]]; then
		echo "remote E2E service ${service} health is ${health_state}; expected healthy" >&2
		return 1
	fi

	printf 'service %-28s state=%s health=%s\n' "${service}" "${runtime_state}" "${health_state}"
}

verify_service_group() {
	local label="$1"
	local services="$2"
	local service

	echo "Checking ${label} services..."
	for service in ${services}; do
		[[ -n "${service}" ]] || continue
		inspect_service_state "${service}"
	done
}

resolve_api_base_url() {
	if [[ -n "${API_BASE_URL}" ]]; then
		return 0
	fi

	local mapped port
	mapped="$("${COMPOSE_CMD[@]}" port eshu 8080 | tail -n 1)"
	if [[ -z "${mapped}" ]]; then
		echo "could not resolve published API port from compose service eshu" >&2
		return 1
	fi
	port="${mapped##*:}"
	API_BASE_URL="http://127.0.0.1:${port}/api/v0"
}

resolve_api_key() {
	if [[ -n "${API_KEY}" ]]; then
		return 0
	fi
	API_KEY="$(eshu_compose_read_api_key || true)"
}

api_get() {
	local path="$1"
	local output_file="$2"
	local -a curl_args=(-fsS)
	if [[ -n "${API_KEY}" ]]; then
		curl_args+=(-H "Authorization: Bearer ${API_KEY}")
	fi
	curl_args+=("${API_BASE_URL}${path}")
	curl "${curl_args[@]}" >"${output_file}"
}

verify_queue_completion() {
	echo "Checking checkpointed index completion..."
	api_get "/index-status" "${INDEX_STATUS_FILE}"
	if jq -e '
		(.status // "") == "healthy" and
		((.queue.outstanding // 0) == 0) and
		((.queue.in_flight // 0) == 0) and
		((.queue.pending // 0) == 0) and
		((.queue.retrying // 0) == 0) and
		((.queue.failed // 0) == 0) and
		((.queue.dead_letter // 0) == 0)
	' "${INDEX_STATUS_FILE}" >/dev/null; then
		echo "remote E2E queue completion verified"
		return 0
	fi

	echo "remote E2E queue completion not reached" >&2
	cat "${INDEX_STATUS_FILE}" >&2
	return 1
}

main() {
	eshu_require_tool curl
	eshu_require_tool docker
	eshu_require_tool jq

	configure_compose
	verify_service_group "core runtime" "${CORE_SERVICES}"
	verify_service_group "collector" "${COLLECTOR_SERVICES}"
	if [[ -n "${EXTRA_SERVICES}" ]]; then
		verify_service_group "extra" "${EXTRA_SERVICES}"
	fi
	resolve_api_base_url
	resolve_api_key
	verify_queue_completion
}

main "$@"
