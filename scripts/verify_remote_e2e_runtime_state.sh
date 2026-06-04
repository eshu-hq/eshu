#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
RUNTIME_LIB="${REPO_ROOT}/scripts/lib/compose_verification_runtime_common.sh"

COMPOSE_FILES="${ESHU_REMOTE_E2E_COMPOSE_FILES:-docker-compose.remote-e2e.yaml}"
COMPOSE_ENV_FILE="${ESHU_REMOTE_E2E_ENV_FILE:-}"
API_BASE_URL="${ESHU_REMOTE_E2E_API_BASE_URL:-}"
API_KEY="${ESHU_REMOTE_E2E_API_KEY:-}"
API_TIMEOUT_SECONDS="${ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS:-30}"
CORE_SERVICES="${ESHU_REMOTE_E2E_REQUIRED_SERVICES:-eshu mcp-server ingester projector resolution-engine workflow-coordinator}"
COLLECTOR_SERVICES="${ESHU_REMOTE_E2E_COLLECTOR_SERVICES:-collector-terraform-state collector-oci-registry collector-package-registry collector-sbom-attestation collector-security-alerts collector-vulnerability-intelligence collector-aws-cloud scanner-worker}"
EXTRA_SERVICES="${ESHU_REMOTE_E2E_EXTRA_SERVICES:-}"
CORPUS_MODE="${ESHU_REMOTE_E2E_CORPUS_MODE:-smoke}"
ADVISORY_EVIDENCE_CVE_ID="${ESHU_REMOTE_E2E_ADVISORY_EVIDENCE_CVE_ID:-${ESHU_VULNERABILITY_E2E_CVE_ID:-CVE-2021-44228}}"
PACKAGE_REGISTRY_GAP_PACKAGE_ID="${ESHU_REMOTE_E2E_PACKAGE_REGISTRY_GAP_PACKAGE_ID:-}"
DERIVED_TARGET_LIMIT="${ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT:-100}"
REPRESENTATIVE_MAX_QUEUE_OUTSTANDING="${ESHU_REMOTE_E2E_REPRESENTATIVE_MAX_QUEUE_OUTSTANDING:-}"
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

	container_id="$("${COMPOSE_CMD[@]}" ps -a -q "${service}" 2>/dev/null || true)"
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
	require_positive_integer ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${API_TIMEOUT_SECONDS}"
	if [[ -n "${API_KEY}" ]]; then
		local curl_config="${TMP_DIR}/curl-auth.conf"
		local escaped_api_key="${API_KEY//\\/\\\\}"
		escaped_api_key="${escaped_api_key//\"/\\\"}"
		printf 'header = "Authorization: Bearer %s"\n' "${escaped_api_key}" >"${curl_config}"
		chmod 600 "${curl_config}"
		curl_args+=(-K "${curl_config}")
	fi
	curl_args+=(--max-time "${API_TIMEOUT_SECONDS}")
	curl_args+=("${API_BASE_URL}${path}")
	curl "${curl_args[@]}" >"${output_file}"
}

require_non_negative_integer() {
	local name="$1"
	local value="$2"
	if [[ -z "${value}" ]]; then
		return 0
	fi
	if [[ ! "${value}" =~ ^[0-9]+$ ]]; then
		echo "${name} must be a non-negative integer, got ${value}" >&2
		return 1
	fi
}

require_positive_integer() {
	local name="$1"
	local value="$2"
	if [[ ! "${value}" =~ ^[0-9]+$ ]] || (( value <= 0 )); then
		echo "${name} must be a positive integer, got ${value}" >&2
		return 1
	fi
}

representative_default_min() {
	local value="$1"
	if [[ -n "${value}" ]]; then
		printf '%s' "${value}"
	elif [[ "${CORPUS_MODE}" == "representative" ]]; then
		printf '1'
	else
		printf '0'
	fi
}

is_representative_mode() {
	[[ "${CORPUS_MODE}" == "representative" ]]
}

representative_max_queue_outstanding() {
	if [[ -n "${REPRESENTATIVE_MAX_QUEUE_OUTSTANDING}" ]]; then
		require_non_negative_integer ESHU_REMOTE_E2E_REPRESENTATIVE_MAX_QUEUE_OUTSTANDING "${REPRESENTATIVE_MAX_QUEUE_OUTSTANDING}"
		printf '%s' "${REPRESENTATIVE_MAX_QUEUE_OUTSTANDING}"
		return 0
	fi
	local effective_limit
	effective_limit="$(effective_derived_target_limit)"
	printf '%s' "$((effective_limit * 10))"
}

effective_derived_target_limit() {
	require_non_negative_integer ESHU_REMOTE_E2E_DERIVED_TARGET_LIMIT "${DERIVED_TARGET_LIMIT}"
	if (( DERIVED_TARGET_LIMIT > 0 )); then
		printf '%s' "${DERIVED_TARGET_LIMIT}"
		return 0
	fi
	printf '100'
}

json_int() {
	local file="$1"
	local filter="$2"
	jq -r "${filter} // 0" "${file}"
}

require_min_count() {
	local label="$1"
	local value="$2"
	local minimum="$3"
	if (( value < minimum )); then
		echo "remote E2E aggregate proof count ${label}=${value} below required minimum ${minimum}" >&2
		return 1
	fi
}

should_probe_aggregate_count() {
	local minimum="$1"
	if [[ "${CORPUS_MODE}" != "representative" ]]; then
		return 0
	fi
	(( minimum > 0 ))
}

report_skipped_aggregate_count() {
	local label="$1"
	local minimum="$2"
	printf 'remote E2E aggregate proof count %s skipped: minimum=%s\n' "${label}" "${minimum}"
}

verify_queue_completion() {
	echo "Checking checkpointed index completion..."
	if ! api_get "/index-status" "${INDEX_STATUS_FILE}"; then
		echo "remote E2E queue completion check could not read ${API_BASE_URL}/index-status" >&2
		echo "verify the API is reachable and ESHU_REMOTE_E2E_API_KEY is valid when set" >&2
		return 1
	fi
	if jq -e '
		(.status // "") == "healthy" and
		((.queue.outstanding // 0) == 0) and
		((.queue.in_flight // 0) == 0) and
		((.queue.pending // 0) == 0) and
		((.queue.retrying // 0) == 0) and
		((.queue.failed // 0) == 0) and
		((.queue.dead_letter // 0) == 0)
	' "${INDEX_STATUS_FILE}" >/dev/null; then
		jq -r '
			"remote E2E terminal queue state: outstanding=\(.queue.outstanding // 0) in_flight=\(.queue.in_flight // 0) pending=\(.queue.pending // 0) retrying=\(.queue.retrying // 0) failed=\(.queue.failed // 0) dead_letter=\(.queue.dead_letter // 0)"
		' "${INDEX_STATUS_FILE}"
		echo "remote E2E queue completion verified"
		return 0
	fi

	echo "remote E2E queue completion not reached" >&2
	cat "${INDEX_STATUS_FILE}" >&2
	return 1
}

verify_representative_runtime_safety() {
	echo "Checking representative runtime safety..."
	if ! api_get "/index-status" "${INDEX_STATUS_FILE}"; then
		echo "remote E2E representative runtime check could not read ${API_BASE_URL}/index-status" >&2
		echo "verify the API is reachable and ESHU_REMOTE_E2E_API_KEY is valid when set" >&2
		return 1
	fi
	local max_outstanding outstanding
	max_outstanding="$(representative_max_queue_outstanding)"
	outstanding="$(json_int "${INDEX_STATUS_FILE}" '.queue.outstanding')"
	if (( outstanding > max_outstanding )); then
		printf 'remote E2E representative derived fanout exceeded: outstanding=%s max_queue_outstanding=%s derived_target_limit=%s\n' \
			"${outstanding}" "${max_outstanding}" "$(effective_derived_target_limit)" >&2
		cat "${INDEX_STATUS_FILE}" >&2
		return 1
	fi
	if jq -e '
		def count_value($section; $name):
			if ((.coordinator[$section] // null) | type) == "array" then
				([.coordinator[$section][]? | select(.name == $name) | (.count // 0)] | add // 0)
			elif ((.coordinator[$section] // null) | type) == "object" then
				(.coordinator[$section][$name] // 0)
			else
				0
			end;
		((.status // "") == "healthy" or (.status // "") == "progressing") and
		(.queue | type == "object") and
		((.queue.retrying // 0) == 0) and
		((.queue.failed // 0) == 0) and
		((.queue.dead_letter // 0) == 0) and
		(.coordinator | type == "object") and
		(count_value("run_status_counts"; "failed") == 0) and
		(count_value("completeness_counts"; "blocked") == 0)
	' "${INDEX_STATUS_FILE}" >/dev/null; then
		jq -r '
			def count_value($section; $name):
				if ((.coordinator[$section] // null) | type) == "array" then
					([.coordinator[$section][]? | select(.name == $name) | (.count // 0)] | add // 0)
				elif ((.coordinator[$section] // null) | type) == "object" then
					(.coordinator[$section][$name] // 0)
				else
					0
				end;
			"remote E2E representative scoped terminal state: status=\(.status // "unknown") outstanding=\(.queue.outstanding // 0) in_flight=\(.queue.in_flight // 0) pending=\(.queue.pending // 0) retrying=\(.queue.retrying // 0) failed=\(.queue.failed // 0) dead_letter=\(.queue.dead_letter // 0) reducer_converging=\(count_value("run_status_counts"; "reducer_converging")) pending_completeness=\(count_value("completeness_counts"; "pending")) blocked_completeness=\(count_value("completeness_counts"; "blocked"))"
		' "${INDEX_STATUS_FILE}"
		echo "remote E2E representative runtime safety verified"
		return 0
	fi

	echo "remote E2E representative runtime safety not reached" >&2
	cat "${INDEX_STATUS_FILE}" >&2
	return 1
}

verify_aggregate_counts() {
	echo "Checking aggregate proof counts..."

	local package_min advisory_min impact_min security_alert_min sbom_min image_min
	package_min="$(representative_default_min "${ESHU_REMOTE_E2E_MIN_PACKAGE_COUNT:-}")"
	advisory_min="$(representative_default_min "${ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT:-}")"
	impact_min="$(representative_default_min "${ESHU_REMOTE_E2E_MIN_IMPACT_FINDING_COUNT:-}")"
	security_alert_min="$(representative_default_min "${ESHU_REMOTE_E2E_MIN_SECURITY_ALERT_RECONCILIATION_COUNT:-}")"
	sbom_min="$(representative_default_min "${ESHU_REMOTE_E2E_MIN_SBOM_ATTACHMENT_COUNT:-}")"
	image_min="$(representative_default_min "${ESHU_REMOTE_E2E_MIN_CONTAINER_IMAGE_IDENTITY_COUNT:-}")"

	require_non_negative_integer ESHU_REMOTE_E2E_MIN_PACKAGE_COUNT "${package_min}"
	require_non_negative_integer ESHU_REMOTE_E2E_MIN_ADVISORY_EVIDENCE_COUNT "${advisory_min}"
	require_non_negative_integer ESHU_REMOTE_E2E_MIN_IMPACT_FINDING_COUNT "${impact_min}"
	require_non_negative_integer ESHU_REMOTE_E2E_MIN_SECURITY_ALERT_RECONCILIATION_COUNT "${security_alert_min}"
	require_non_negative_integer ESHU_REMOTE_E2E_MIN_SBOM_ATTACHMENT_COUNT "${sbom_min}"
	require_non_negative_integer ESHU_REMOTE_E2E_MIN_CONTAINER_IMAGE_IDENTITY_COUNT "${image_min}"

	local package_file="${TMP_DIR}/package-count.json"
	local advisory_file="${TMP_DIR}/advisory-evidence.json"
	local impact_file="${TMP_DIR}/impact-count.json"
	local security_alert_file="${TMP_DIR}/security-alert-count.json"
	local sbom_file="${TMP_DIR}/sbom-count.json"
	local image_file="${TMP_DIR}/container-image-count.json"

	local package_count advisory_count impact_count affected_count security_alert_count sbom_count image_count
	package_count=0
	advisory_count=0
	impact_count=0
	affected_count=0
	security_alert_count=0
	sbom_count=0
	image_count=0

	if should_probe_aggregate_count "${package_min}"; then
		api_get "/package-registry/packages/count" "${package_file}"
		package_count="$(json_int "${package_file}" '.total_packages')"
	else
		report_skipped_aggregate_count package_registry_packages "${package_min}"
	fi
	if should_probe_aggregate_count "${advisory_min}"; then
		api_get "/supply-chain/advisories/evidence?cve_id=${ADVISORY_EVIDENCE_CVE_ID}&limit=1" "${advisory_file}"
		advisory_count="$(json_int "${advisory_file}" '.count')"
	else
		report_skipped_aggregate_count advisory_evidence "${advisory_min}"
	fi
	if should_probe_aggregate_count "${impact_min}"; then
		api_get "/supply-chain/impact/findings/count" "${impact_file}"
		impact_count="$(json_int "${impact_file}" '.total_findings')"
		affected_count="$(json_int "${impact_file}" '.affected_findings')"
	else
		report_skipped_aggregate_count impact_findings "${impact_min}"
	fi
	if should_probe_aggregate_count "${security_alert_min}"; then
		api_get "/supply-chain/security-alerts/reconciliations/count" "${security_alert_file}"
		security_alert_count="$(json_int "${security_alert_file}" '.total_reconciliations')"
	else
		report_skipped_aggregate_count security_alert_reconciliations "${security_alert_min}"
	fi
	if should_probe_aggregate_count "${sbom_min}"; then
		api_get "/supply-chain/sbom-attestations/attachments/count" "${sbom_file}"
		sbom_count="$(json_int "${sbom_file}" '.total_attachments')"
	else
		report_skipped_aggregate_count sbom_attachments "${sbom_min}"
	fi
	if should_probe_aggregate_count "${image_min}"; then
		api_get "/supply-chain/container-images/identities/count" "${image_file}"
		image_count="$(json_int "${image_file}" '.total_identities')"
	else
		report_skipped_aggregate_count container_image_identities "${image_min}"
	fi

	require_min_count package_registry_packages "${package_count}" "${package_min}"
	require_min_count advisory_evidence "${advisory_count}" "${advisory_min}"
	require_min_count impact_findings "${impact_count}" "${impact_min}"
	require_min_count security_alert_reconciliations "${security_alert_count}" "${security_alert_min}"
	require_min_count sbom_attachments "${sbom_count}" "${sbom_min}"
	require_min_count container_image_identities "${image_count}" "${image_min}"

	printf 'remote E2E aggregate proof counts: package_registry_packages=%s advisory_evidence=%s impact_findings=%s affected_findings=%s security_alert_reconciliations=%s sbom_attachments=%s container_image_identities=%s\n' \
		"${package_count}" \
		"${advisory_count}" \
		"${impact_count}" \
		"${affected_count}" \
		"${security_alert_count}" \
		"${sbom_count}" \
		"${image_count}"
}

verify_package_registry_metadata_gap() {
	if [[ -z "${PACKAGE_REGISTRY_GAP_PACKAGE_ID}" ]]; then
		return 0
	fi

	echo "Checking package-registry expected coverage gap..."

	local gap_file="${TMP_DIR}/package-registry-metadata-gap.json"
	api_get "/supply-chain/impact/findings?package_id=${PACKAGE_REGISTRY_GAP_PACKAGE_ID}&limit=1" "${gap_file}"

	local gap_count
	gap_count="$(jq -r '
		[
			.readiness.unsupported_targets[]?
			| select(.target_kind == "package_registry_metadata" and .reason == "metadata_too_large")
			| (.count // 1)
		] | add // 0
	' "${gap_file}")"
	require_min_count package_registry_metadata_too_large_gaps "${gap_count}" 1
	printf 'remote E2E package-registry expected gap proof: package_registry_metadata_too_large_gaps=%s\n' \
		"${gap_count}"
}

verify_target_story() {
	ESHU_REMOTE_E2E_API_BASE_URL="${API_BASE_URL}" \
		ESHU_REMOTE_E2E_API_KEY="${API_KEY}" \
		ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS="${API_TIMEOUT_SECONDS}" \
		"${SCRIPT_DIR}/verify_remote_e2e_target_story.sh"
}

verify_workflow_completion() {
	echo "Checking workflow coordinator completion..."
	if jq -e '
		def count_value($section; $name):
			if ((.coordinator[$section] // null) | type) == "array" then
				([.coordinator[$section][]? | select(.name == $name) | (.count // 0)] | add // 0)
			elif ((.coordinator[$section] // null) | type) == "object" then
				(.coordinator[$section][$name] // 0)
			else
				0
			end;
		(.coordinator | type == "object") and
		(count_value("run_status_counts"; "failed") == 0) and
		(count_value("run_status_counts"; "reducer_converging") == 0) and
		(count_value("completeness_counts"; "blocked") == 0) and
		(count_value("completeness_counts"; "pending") == 0)
	' "${INDEX_STATUS_FILE}" >/dev/null; then
		echo "remote E2E workflow completion verified"
		return 0
	fi

	echo "remote E2E workflow completion not reached" >&2
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
	if is_representative_mode; then
		verify_representative_runtime_safety
		verify_aggregate_counts
		verify_target_story
		verify_package_registry_metadata_gap
		verify_representative_runtime_safety
	else
		verify_queue_completion
		verify_workflow_completion
		verify_aggregate_counts
		verify_target_story
		verify_package_registry_metadata_gap
	fi
}

main "$@"
