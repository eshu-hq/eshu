#!/usr/bin/env bash
set -euo pipefail
TARGET_STORY_FILE="${ESHU_REMOTE_E2E_TARGET_STORY_FILE:-}"
API_BASE_URL="${ESHU_REMOTE_E2E_API_BASE_URL:-}"
API_KEY="${ESHU_REMOTE_E2E_API_KEY:-}"
MCP_URL="${ESHU_REMOTE_E2E_MCP_URL:-${ESHU_MCP_URL:-}}"
MCP_TOKEN="${ESHU_REMOTE_E2E_MCP_TOKEN:-${ESHU_MCP_TOKEN:-${API_KEY}}}"
API_TIMEOUT_SECONDS="${ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS:-30}"
TMP_DIR="$(mktemp -d)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=scripts/lib/remote_e2e_security_alerts.sh
source "${SCRIPT_DIR}/lib/remote_e2e_security_alerts.sh"
# shellcheck source=scripts/lib/remote_e2e_target_story_alignment.sh
source "${SCRIPT_DIR}/lib/remote_e2e_target_story_alignment.sh"
# shellcheck source=scripts/lib/remote_e2e_target_story_cicd.sh
source "${SCRIPT_DIR}/lib/remote_e2e_target_story_cicd.sh"
# shellcheck source=scripts/lib/remote_e2e_service_catalog.sh
source "${SCRIPT_DIR}/lib/remote_e2e_service_catalog.sh"
cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

require_tool() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required tool: $1" >&2
		exit 1
	}
}

require_positive_integer() {
	local name="$1"
	local value="$2"
	if [[ ! "${value}" =~ ^[0-9]+$ ]] || ((value <= 0)); then
		echo "${name} must be a positive integer, got ${value}" >&2
		return 1
	fi
}

require_non_negative_integer() {
	local name="$1"
	local value="$2"
	if [[ ! "${value}" =~ ^[0-9]+$ ]]; then
		echo "${name} must be a non-negative integer, got ${value}" >&2
		return 1
	fi
}

urlencode() {
	jq -rn --arg value "$1" '$value|@uri'
}

api_get() {
	local path="$1"
	local output_file="$2"
	local -a curl_args=(-fsS)
	require_positive_integer ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${API_TIMEOUT_SECONDS}"
	local curl_config="${TMP_DIR}/curl.conf"
	printf 'header = "Accept: application/eshu.envelope+json"\n' >"${curl_config}"
	if [[ -n "${API_KEY}" ]]; then
		local escaped_api_key="${API_KEY//\\/\\\\}"
		escaped_api_key="${escaped_api_key//\"/\\\"}"
		printf 'header = "Authorization: Bearer %s"\n' "${escaped_api_key}" >>"${curl_config}"
	fi
	chmod 600 "${curl_config}"
	curl_args+=(-K "${curl_config}")
	curl_args+=(--max-time "${API_TIMEOUT_SECONDS}")
	curl_args+=("${API_BASE_URL}${path}")
	curl "${curl_args[@]}" >"${output_file}"
	if ! jq -e 'has("data") and has("truth") and (.error == null)' "${output_file}" >/dev/null; then
		echo "target story API response missing Eshu truth envelope" >&2
		return 1
	fi
}

api_post_json() {
	local path="$1"
	local body_file="$2"
	local output_file="$3"
	local -a curl_args=(-fsS)
	require_positive_integer ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${API_TIMEOUT_SECONDS}"
	local curl_config="${TMP_DIR}/curl-post.conf"
	printf 'header = "Accept: application/eshu.envelope+json"\n' >"${curl_config}"
	printf 'header = "Content-Type: application/json"\n' >>"${curl_config}"
	if [[ -n "${API_KEY}" ]]; then
		local escaped_api_key="${API_KEY//\\/\\\\}"
		escaped_api_key="${escaped_api_key//\"/\\\"}"
		printf 'header = "Authorization: Bearer %s"\n' "${escaped_api_key}" >>"${curl_config}"
	fi
	chmod 600 "${curl_config}"
	curl_args+=(-K "${curl_config}")
	curl_args+=(--max-time "${API_TIMEOUT_SECONDS}")
	curl_args+=(--data-binary "@${body_file}")
	curl_args+=("${API_BASE_URL}${path}")
	curl "${curl_args[@]}" >"${output_file}"
	if ! jq -e 'has("data") and has("truth") and (.error == null)' "${output_file}" >/dev/null; then
		echo "target story API response missing Eshu truth envelope" >&2
		return 1
	fi
}

mcp_tool_envelope() {
	local tool_name="$1"
	local args_json="$2"
	local output_file="$3"
	local response_file="${TMP_DIR}/mcp-${tool_name}.json"
	local payload_file="${TMP_DIR}/mcp-${tool_name}-payload.json"
	local curl_config="${TMP_DIR}/mcp-curl.conf"
	jq -n --arg name "${tool_name}" --argjson arguments "${args_json}" \
		'{jsonrpc:"2.0", id:1, method:"tools/call", params:{name:$name, arguments:$arguments}}' >"${payload_file}"
	printf 'header = "Content-Type: application/json"\n' >"${curl_config}"
	if [[ -n "${MCP_TOKEN}" ]]; then
		local escaped_token="${MCP_TOKEN//\\/\\\\}"
		escaped_token="${escaped_token//\"/\\\"}"
		printf 'header = "Authorization: Bearer %s"\n' "${escaped_token}" >>"${curl_config}"
	fi
	chmod 600 "${curl_config}"
	curl -fsS -K "${curl_config}" --max-time "${API_TIMEOUT_SECONDS}" --data-binary "@${payload_file}" "${MCP_URL}" >"${response_file}"
	if ! jq -e '(.error == null) and ((.result.isError // false) | not)' "${response_file}" >/dev/null; then
		echo "target story MCP tool ${tool_name} failed" >&2
		return 1
	fi
	local envelope_text
	envelope_text="$(jq -r 'first(.result.content[]? | select(.type == "resource" and .resource.uri == "eshu://tool-result/envelope") | .resource.text) // ""' "${response_file}")"
	if [[ -z "${envelope_text}" ]]; then
		echo "target story MCP tool ${tool_name} response missing Eshu envelope resource" >&2
		return 1
	fi
	printf '%s' "${envelope_text}" >"${output_file}"
	if ! jq -e 'has("data") and has("truth") and (.error == null)' "${output_file}" >/dev/null; then
		echo "target story MCP tool ${tool_name} envelope is invalid" >&2
		return 1
	fi
}

manifest_string() {
	local filter="$1"
	jq -r "${filter} // \"\"" "${TARGET_STORY_FILE}"
}

manifest_int() {
	local filter="$1"
	local default_value="$2"
	jq -r "${filter} // ${default_value}" "${TARGET_STORY_FILE}"
}

target_story_proof_mode() {
	local mode
	mode="$(manifest_string '.proof_mode')"
	if [[ -z "${mode}" ]]; then
		mode="code_to_cloud"
	fi
	case "${mode}" in
		code_to_cloud | vulnerability_only | partial)
			printf '%s' "${mode}"
			;;
		*)
			echo "target story proof_mode must be one of code_to_cloud, vulnerability_only, partial" >&2
			return 1
			;;
	esac
}

validate_target_story_proof_mode() {
	local mode="$1"
	local image_min="$2"
	local sbom_min="$3"
	local reason
	case "${mode}" in
		code_to_cloud)
			if ((image_min < 1)); then
				echo "target proof_mode=code_to_cloud requires minimums.container_image_identities >= 1" >&2
				return 1
			fi
			if ((sbom_min < 1)); then
				echo "target proof_mode=code_to_cloud requires minimums.sbom_attachments >= 1" >&2
				return 1
			fi
			;;
		vulnerability_only | partial)
			reason="$(manifest_string '.proof_mode_reason')"
			if [[ -z "${reason}" ]]; then
				echo "target story proof_mode=${mode} requires proof_mode_reason" >&2
				return 1
			fi
			;;
	esac
}

json_int() {
	local file="$1"
	local filter="$2"
	jq -r "(.data // .) | ${filter} // 0" "${file}"
}

json_string() {
	local file="$1"
	local filter="$2"
	jq -r "(.data // .) | ${filter} // \"\"" "${file}"
}

require_min_count() {
	local label="$1"
	local value="$2"
	local minimum="$3"
	if ((value < minimum)); then
		echo "target ${label}=${value} below required minimum ${minimum}" >&2
		return 1
	fi
}

check_repository_story() {
	local selector="$1"
	local output_file="$2"
	api_get "/repositories/$(urlencode "${selector}")/story" "${output_file}"
	if ! jq -e --arg selector "${selector}" '
		(.data // .) |
		[
			.repository.id?,
			.repository.name?,
			.repository.local_path?,
			.data.repository.id?,
			.data.repository.name?,
			.data.repository.local_path?
		] | map(select(. == $selector)) | length > 0
	' "${output_file}" >/dev/null; then
		echo "target repository_story=0 below required minimum 1" >&2
		return 1
	fi
}

list_limit_for_minimum() {
	local label="$1"
	local value="$2"
	if ((value > 200)); then
		echo "target ${label} minimum cannot exceed bounded list limit 200" >&2
		return 1
	fi
	if ((value < 1)); then
		printf '1'
		return 0
	fi
	printf '%s' "${value}"
}

provider_repository_match_count() {
	local file="$1"
	local expected="$2"
	jq -r --arg expected "${expected}" '
		(.data // .) as $body |
		($expected | ascii_downcase) as $expected_lower |
		def repository_anchor_matches($actual):
			$actual == $expected_lower
			or ($actual | endswith(":" + $expected_lower))
			or ($actual | endswith("/" + $expected_lower));
		def provider_repository_candidates($alert):
			[
				$alert.repository_id?,
				$alert.repository_slug?,
				$alert.repository_name?,
				$alert.repository_full_name?,
				$alert.provider_repository?,
				$alert.provider_repository_id?,
				$alert.provider_repository_name?,
				(($alert.provider_alert_id? // "") | capture("security-alert:[^:]+:(?<repository>[^:]+)")? | .repository)
			]
			| map(select(type == "string" and length > 0) | ascii_downcase);
		[
			$body.reconciliations[]?
			| provider_repository_candidates(.provider_alert // {}) as $anchors
			| select(any($anchors[]; repository_anchor_matches(.)))
		] | length
	' "${file}"
}

cloud_resource_match_count() {
	local file="$1"
	local expected="$2"
	jq -r --arg expected "${expected}" '
		(.data // .) as $body |
		[
			$body.results[]?
			| select((.resource_id // "") == $expected or (.arn // "") == $expected or (.id // "") == $expected)
		] | length
	' "${file}"
}

main() {
	if [[ -z "${TARGET_STORY_FILE}" ]]; then
		echo "remote E2E target story proof skipped: no target story configured"
		return 0
	fi
	if [[ ! -f "${TARGET_STORY_FILE}" ]]; then
		echo "target story file not found" >&2
		return 1
	fi
	require_tool curl
	require_tool jq
	require_positive_integer ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${API_TIMEOUT_SECONDS}"
	if [[ -z "${API_BASE_URL}" ]]; then
		echo "ESHU_REMOTE_E2E_API_BASE_URL is required when target story proof is configured" >&2
		return 1
	fi

	local proof_mode repo_selector expected_security_repo expected_service_id expected_workload_id
	local expected_image_repo expected_image_digest expected_image_ref
	local expected_sbom_digest expected_cloud_resource_id expected_security_rows_file
	proof_mode="$(target_story_proof_mode)"
	repo_selector="$(manifest_string '.target_repository_id')"
	expected_security_repo="$(manifest_string '.expected_security_alert_repository')"
	expected_service_id="$(manifest_string '.expected_service_id')"
	expected_workload_id="$(manifest_string '.expected_workload_id')"
	expected_image_repo="$(manifest_string '.expected_oci_repository_id')"
	expected_image_digest="$(manifest_string '.expected_image_digest')"
	expected_image_ref="$(manifest_string '.expected_image_ref')"
	expected_sbom_digest="$(manifest_string '.expected_sbom_subject_digest')"
	expected_cloud_resource_id="$(manifest_string '.expected_cloud_resource_id')"
	expected_security_rows_file="$(manifest_string '.expected_security_alert_rows_file')"
	if [[ -z "${repo_selector}" ]]; then
		echo "target story manifest requires target_repository_id" >&2
		return 1
	fi

	local impact_min security_min image_min sbom_min catalog_min cicd_min cloud_min
	impact_min="$(manifest_int '.minimums.impact_findings' 0)"
	security_min="$(manifest_int '.minimums.security_alert_reconciliations' 0)"
	image_min="$(manifest_int '.minimums.container_image_identities' 0)"
	sbom_min="$(manifest_int '.minimums.sbom_attachments' 0)"
	catalog_min="$(manifest_int '.minimums.service_catalog_correlations' 0)"
	cicd_min="$(manifest_int '.minimums.ci_cd_run_correlations' 0)"
	cloud_min="$(manifest_int '.minimums.cloud_resources' 0)"
	require_non_negative_integer minimums.impact_findings "${impact_min}"
	require_non_negative_integer minimums.security_alert_reconciliations "${security_min}"
	require_non_negative_integer minimums.container_image_identities "${image_min}"
	require_non_negative_integer minimums.sbom_attachments "${sbom_min}"
	require_non_negative_integer minimums.service_catalog_correlations "${catalog_min}"
	require_non_negative_integer minimums.ci_cd_run_correlations "${cicd_min}"
	require_non_negative_integer minimums.cloud_resources "${cloud_min}"
	local expected_security_rows_count=0
	if [[ -n "${expected_security_rows_file}" ]]; then
		if [[ ! -f "${expected_security_rows_file}" ]]; then
			echo "target security_alert_expected_rows file not found" >&2
			return 1
		fi
		target_story_validate_expected_security_alert_rows "${expected_security_rows_file}"
		expected_security_rows_count="$(target_story_expected_security_alert_rows_count "${expected_security_rows_file}")"
		if ((expected_security_rows_count > security_min)); then
			security_min="${expected_security_rows_count}"
		fi
	fi
	validate_target_story_proof_mode "${proof_mode}" "${image_min}" "${sbom_min}"
	target_story_validate_alignment "${TARGET_STORY_FILE}" "${proof_mode}"
	if ((catalog_min > 0 || cicd_min > 0 || cloud_min > 0)) && [[ -z "${MCP_URL}" ]]; then
		echo "ESHU_REMOTE_E2E_MCP_URL is required when target story MCP proof is required" >&2
		return 1
	fi

	echo "Checking remote E2E target story proof (proof_mode=${proof_mode})..."
	local repo_file="${TMP_DIR}/repo-story.json"
	check_repository_story "${repo_selector}" "${repo_file}"

	local repo_query
	repo_query="$(urlencode "${repo_selector}")"
	local impact_count=0 security_count=0 security_expected_rows_count=0 image_count=0 sbom_count=0 catalog_count=0 cicd_count=0 cloud_count=0
	local mcp_catalog_count=0 mcp_cicd_count=0 mcp_cloud_count=0
	local cicd_static_state=not_checked cicd_live_state=not_checked
	local mcp_cicd_static_state=not_checked mcp_cicd_live_state=not_checked
	local catalog_local_descriptor_state="not_checked"
	local catalog_external_confirmation_state="not_checked"
	local catalog_external_confirmation_reason=""
	local mcp_catalog_local_descriptor_state="not_checked"
	local mcp_catalog_external_confirmation_state="not_checked"
	local mcp_catalog_external_confirmation_reason=""
	if ((impact_min > 0)); then
		local impact_file="${TMP_DIR}/impact-count.json"
		api_get "/supply-chain/impact/findings/count?repository_id=${repo_query}&profile=comprehensive" "${impact_file}"
		impact_count="$(json_int "${impact_file}" '.total_findings')"
		require_min_count impact_findings "${impact_count}" "${impact_min}"
	fi
	if ((security_min > 0)); then
		if [[ -z "${expected_security_repo}" ]]; then
			echo "target security_alert_reconciliations requires expected_security_alert_repository" >&2
			return 1
		fi
		local security_limit
		security_limit="$(list_limit_for_minimum security_alert_reconciliations "${security_min}")"
		local security_file="${TMP_DIR}/security-alert-count.json"
		api_get "/supply-chain/security-alerts/reconciliations?repository_id=${repo_query}&limit=${security_limit}" "${security_file}"
		security_count="$(target_story_provider_repository_match_count "${security_file}" "${expected_security_repo}")"
		require_min_count security_alert_reconciliations "${security_count}" "${security_min}"
		if [[ -n "${expected_security_rows_file}" ]]; then
			local security_missing_count security_mismatch_count security_evidence_gap_count
			read -r security_expected_rows_count security_missing_count security_mismatch_count security_evidence_gap_count < <(target_story_compare_security_alert_expected_rows "${expected_security_rows_file}" "${security_file}")
			if ((security_missing_count > 0 || security_mismatch_count > 0 || security_evidence_gap_count > 0)); then
				printf 'target security_alert_expected_rows missing_count=%s mismatch_count=%s evidence_gap_count=%s\n' \
					"${security_missing_count}" "${security_mismatch_count}" "${security_evidence_gap_count}" >&2
				return 1
			fi
		fi
	fi
	if ((image_min > 0)); then
		local image_anchor image_param image_file="${TMP_DIR}/container-image-count.json"
		if [[ -n "${expected_image_digest}" ]]; then
			image_anchor="${expected_image_digest}"
			image_param="digest"
		elif [[ -n "${expected_image_ref}" ]]; then
			image_anchor="${expected_image_ref}"
			image_param="image_ref"
		else
			image_anchor=""
			image_param=""
		fi
		if [[ -z "${image_anchor}" ]]; then
			echo "target container_image_identities requires expected_image_digest or expected_image_ref" >&2
			return 1
		fi
		local image_path
		image_path="/supply-chain/container-images/identities/count?${image_param}=$(urlencode "${image_anchor}")"
		if [[ -n "${expected_image_repo}" ]]; then
			image_path="${image_path}&repository_id=$(urlencode "${expected_image_repo}")"
		fi
		api_get "${image_path}" "${image_file}"
		image_count="$(json_int "${image_file}" '.total_identities')"
		require_min_count container_image_identities "${image_count}" "${image_min}"
	fi
	if ((sbom_min > 0)); then
		if [[ -z "${expected_sbom_digest}" ]]; then
			expected_sbom_digest="${expected_image_digest}"
		fi
		if [[ -z "${expected_sbom_digest}" ]]; then
			echo "target sbom_attachments requires expected_sbom_subject_digest or expected_image_digest" >&2
			return 1
		fi
		local sbom_file="${TMP_DIR}/sbom-count.json"
		api_get "/supply-chain/sbom-attestations/attachments/count?subject_digest=$(urlencode "${expected_sbom_digest}")" "${sbom_file}"
		sbom_count="$(json_int "${sbom_file}" '.total_attachments')"
		require_min_count sbom_attachments "${sbom_count}" "${sbom_min}"
	fi
	if ((catalog_min > 0)); then
		target_story_check_service_catalog_correlations \
			"${repo_query}" \
			"${repo_selector}" \
			"${catalog_min}" \
			"${expected_service_id}" \
			"${expected_workload_id}"
	fi
	if ((cicd_min > 0)); then
		target_story_verify_cicd_run_correlations \
			"${repo_query}" \
			"${repo_selector}" \
			"${expected_image_digest}" \
			"${expected_image_ref}" \
			"${cicd_min}"
	fi
	if ((cloud_min > 0)); then
		if [[ -z "${expected_cloud_resource_id}" ]]; then
			echo "target cloud_resources requires expected_cloud_resource_id" >&2
			return 1
		fi
		local cloud_body="${TMP_DIR}/cloud-resource-search-body.json"
		local cloud_file="${TMP_DIR}/cloud-resources.json"
		jq -n --arg query "${expected_cloud_resource_id}" '{query:$query, category:"cloud", limit:200}' >"${cloud_body}"
		api_post_json "/infra/resources/search" "${cloud_body}" "${cloud_file}"
		cloud_count="$(cloud_resource_match_count "${cloud_file}" "${expected_cloud_resource_id}")"
		require_min_count cloud_resources "${cloud_count}" "${cloud_min}"
		local mcp_cloud_file="${TMP_DIR}/mcp-cloud-resources.json"
		local mcp_cloud_args
		mcp_cloud_args="$(jq -n --arg query "${expected_cloud_resource_id}" '{query:$query, category:"cloud", limit:200}')"
		mcp_tool_envelope find_infra_resources "${mcp_cloud_args}" "${mcp_cloud_file}"
		mcp_cloud_count="$(cloud_resource_match_count "${mcp_cloud_file}" "${expected_cloud_resource_id}")"
		require_min_count mcp_cloud_resources "${mcp_cloud_count}" "${cloud_min}"
	fi
	printf 'remote E2E target story proof counts: proof_mode=%s repository_story=1 impact_findings=%s security_alert_reconciliations=%s security_alert_expected_rows=%s container_image_identities=%s sbom_attachments=%s service_catalog_correlations=%s service_catalog_local_descriptors=%s service_catalog_external_confirmation=%s service_catalog_external_confirmation_reason=%s ci_cd_run_correlations=%s ci_cd_static_workflow_state=%s ci_cd_live_run_state=%s cloud_resources=%s mcp_service_catalog_correlations=%s mcp_service_catalog_local_descriptors=%s mcp_service_catalog_external_confirmation=%s mcp_service_catalog_external_confirmation_reason=%s mcp_ci_cd_run_correlations=%s mcp_ci_cd_static_workflow_state=%s mcp_ci_cd_live_run_state=%s mcp_cloud_resources=%s\n' \
		"${proof_mode}" \
		"${impact_count}" \
		"${security_count}" \
		"${security_expected_rows_count}" \
		"${image_count}" \
		"${sbom_count}" \
		"${catalog_count}" \
		"${catalog_local_descriptor_state}" \
		"${catalog_external_confirmation_state}" \
		"${catalog_external_confirmation_reason}" \
		"${cicd_count}" \
		"${cicd_static_state}" \
		"${cicd_live_state}" \
		"${cloud_count}" \
		"${mcp_catalog_count}" \
		"${mcp_catalog_local_descriptor_state}" \
		"${mcp_catalog_external_confirmation_state}" \
		"${mcp_catalog_external_confirmation_reason}" \
		"${mcp_cicd_count}" \
		"${mcp_cicd_static_state}" \
		"${mcp_cicd_live_state}" \
		"${mcp_cloud_count}"
}

main "$@"
