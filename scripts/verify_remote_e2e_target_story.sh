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
# shellcheck source=scripts/lib/remote_e2e_target_story_common.sh
source "${SCRIPT_DIR}/lib/remote_e2e_target_story_common.sh"
# shellcheck source=scripts/lib/remote_e2e_target_story_alignment.sh
source "${SCRIPT_DIR}/lib/remote_e2e_target_story_alignment.sh"
# shellcheck source=scripts/lib/remote_e2e_target_story_readbacks.sh
source "${SCRIPT_DIR}/lib/remote_e2e_target_story_readbacks.sh"
# shellcheck source=scripts/lib/remote_e2e_target_story_cicd.sh
source "${SCRIPT_DIR}/lib/remote_e2e_target_story_cicd.sh"
# shellcheck source=scripts/lib/remote_e2e_service_catalog.sh
source "${SCRIPT_DIR}/lib/remote_e2e_service_catalog.sh"
# shellcheck source=scripts/lib/remote_e2e_target_story_source_evidence.sh
source "${SCRIPT_DIR}/lib/remote_e2e_target_story_source_evidence.sh"

cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

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

	local proof_mode repo_selector expected_security_repo expected_source_repo expected_service_id expected_workload_id
	local expected_image_repo expected_image_digest expected_image_ref
	local expected_sbom_digest expected_cloud_resource_id expected_security_rows_file
	local expected_incident_id expected_incident_provider expected_incident_scope expected_incident_service
	local expected_work_item_scope expected_work_item_key expected_work_item_provider_id expected_work_item_url_fingerprint
	proof_mode="$(target_story_proof_mode)"
	repo_selector="$(manifest_string '.target_repository_id')"
	expected_security_repo="$(manifest_string '.expected_security_alert_repository')"
	expected_source_repo="$(manifest_string '.expected_source_repository_id')"
	expected_service_id="$(manifest_string '.expected_service_id')"
	expected_workload_id="$(manifest_string '.expected_workload_id')"
	expected_image_repo="$(manifest_string '.expected_oci_repository_id')"
	expected_image_digest="$(manifest_string '.expected_image_digest')"
	expected_image_ref="$(manifest_string '.expected_image_ref')"
	expected_sbom_digest="$(manifest_string '.expected_sbom_subject_digest')"
	expected_cloud_resource_id="$(manifest_string '.expected_cloud_resource_id')"
	expected_security_rows_file="$(manifest_string '.expected_security_alert_rows_file')"
	expected_incident_id="$(manifest_string '.expected_provider_incident_id')"
	expected_incident_provider="$(manifest_string '.expected_incident_provider')"
	if [[ -z "${expected_incident_provider}" ]]; then
		expected_incident_provider="pagerduty"
	fi
	expected_incident_scope="$(manifest_string '.expected_incident_scope_id')"
	expected_incident_service="$(manifest_string '.expected_incident_service_id')"
	if [[ -z "${expected_incident_service}" ]]; then
		expected_incident_service="${expected_service_id}"
	fi
	expected_work_item_scope="$(manifest_string '.expected_work_item_scope_id')"
	expected_work_item_key="$(manifest_string '.expected_work_item_key')"
	expected_work_item_provider_id="$(manifest_string '.expected_work_item_provider_id')"
	expected_work_item_url_fingerprint="$(manifest_string '.expected_work_item_url_fingerprint')"
	if [[ -z "${repo_selector}" ]]; then
		echo "target story manifest requires target_repository_id" >&2
		return 1
	fi
	if [[ -z "${expected_source_repo}" ]]; then
		expected_source_repo="${repo_selector}"
	fi

	local impact_min security_min image_min sbom_min catalog_min cicd_min cloud_min
	local documentation_min incident_min work_item_min
	impact_min="$(manifest_int '.minimums.impact_findings' 0)"
	security_min="$(manifest_int '.minimums.security_alert_reconciliations' 0)"
	image_min="$(manifest_int '.minimums.container_image_identities' 0)"
	sbom_min="$(manifest_int '.minimums.sbom_attachments' 0)"
	catalog_min="$(manifest_int '.minimums.service_catalog_correlations' 0)"
	cicd_min="$(manifest_int '.minimums.ci_cd_run_correlations' 0)"
	cloud_min="$(manifest_int '.minimums.cloud_resources' 0)"
	documentation_min="$(manifest_int '.minimums.documentation_findings' 0)"
	incident_min="$(manifest_int '.minimums.incident_contexts' 0)"
	work_item_min="$(manifest_int '.minimums.work_item_evidence' 0)"
	require_non_negative_integer minimums.impact_findings "${impact_min}"
	require_non_negative_integer minimums.security_alert_reconciliations "${security_min}"
	require_non_negative_integer minimums.container_image_identities "${image_min}"
	require_non_negative_integer minimums.sbom_attachments "${sbom_min}"
	require_non_negative_integer minimums.service_catalog_correlations "${catalog_min}"
	require_non_negative_integer minimums.ci_cd_run_correlations "${cicd_min}"
	require_non_negative_integer minimums.cloud_resources "${cloud_min}"
	require_non_negative_integer minimums.documentation_findings "${documentation_min}"
	require_non_negative_integer minimums.incident_contexts "${incident_min}"
	require_non_negative_integer minimums.work_item_evidence "${work_item_min}"
	local documentation_reason incident_reason work_item_reason
	documentation_reason="$(target_story_unsupported_reason documentation_findings "${documentation_min}")"
	incident_reason="$(target_story_unsupported_reason incident_contexts "${incident_min}")"
	work_item_reason="$(target_story_unsupported_reason work_item_evidence "${work_item_min}")"
	local service_story_min=0
	if ((image_min > 0 && sbom_min > 0)); then
		service_story_min=1
	fi
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
	if ((catalog_min > 0 || cicd_min > 0 || cloud_min > 0 || service_story_min > 0 || documentation_min > 0 || incident_min > 0 || work_item_min > 0)) && [[ -z "${MCP_URL}" ]]; then
		echo "ESHU_REMOTE_E2E_MCP_URL is required when target story MCP proof is required" >&2
		return 1
	fi

	echo "Checking remote E2E target story proof (proof_mode=${proof_mode})..."
	local repo_file="${TMP_DIR}/repo-story.json"
	check_repository_story "${repo_selector}" "${repo_file}"

	local repo_query
	repo_query="$(urlencode "${repo_selector}")"
	local impact_count=0 security_count=0 security_expected_rows_count=0 image_count=0 sbom_count=0 catalog_count=0 cicd_count=0 cloud_count=0
	local service_story_count=0 mcp_service_story_count=0
	local documentation_count=0 incident_count=0 work_item_count=0
	local mcp_catalog_count=0 mcp_cicd_count=0 mcp_cloud_count=0 mcp_documentation_count=0 mcp_incident_count=0 mcp_work_item_count=0
	local cicd_static_state=not_checked cicd_live_state=not_checked
	local mcp_cicd_static_state=not_checked mcp_cicd_live_state=not_checked
	local catalog_local_descriptor_state="not_checked"
	local catalog_external_confirmation_state="not_checked"
	local catalog_external_confirmation_reason=""
	local mcp_catalog_local_descriptor_state="not_checked"
	local mcp_catalog_external_confirmation_state="not_checked"
	local mcp_catalog_external_confirmation_reason=""
	if ((documentation_min > 0)); then
		local documentation_limit documentation_file="${TMP_DIR}/documentation-findings.json"
		documentation_limit="$(list_limit_for_minimum documentation_findings "${documentation_min}")"
		api_get "/documentation/findings?repo=${repo_query}&limit=${documentation_limit}" "${documentation_file}"
		documentation_count="$(documentation_finding_match_count "${documentation_file}" "${repo_selector}")"
		require_target_min_count documentation_findings "${documentation_count}" "${documentation_min}"
		local mcp_documentation_file="${TMP_DIR}/mcp-documentation-findings.json"
		local mcp_documentation_args
		mcp_documentation_args="$(jq -n \
			--arg repo "${repo_selector}" \
			--argjson limit "${documentation_limit}" \
			'{repo:$repo, limit:$limit}')"
		mcp_tool_envelope list_documentation_findings "${mcp_documentation_args}" "${mcp_documentation_file}"
		mcp_documentation_count="$(documentation_finding_match_count "${mcp_documentation_file}" "${repo_selector}")"
		require_target_min_count mcp_documentation_findings "${mcp_documentation_count}" "${documentation_min}"
	fi
	if ((incident_min > 0)); then
		require_incident_target_anchors "${expected_incident_id}" "${expected_incident_service}"
		local incident_limit incident_file="${TMP_DIR}/incident-context.json"
		incident_limit="$(list_limit_for_minimum incident_contexts "${incident_min}")"
		local incident_path="/incidents/$(urlencode "${expected_incident_id}")/context?provider=$(urlencode "${expected_incident_provider}")"
		if [[ -n "${expected_incident_scope}" ]]; then
			incident_path="${incident_path}&scope_id=$(urlencode "${expected_incident_scope}")"
		fi
		incident_path="${incident_path}&service_id=$(urlencode "${expected_incident_service}")&limit=${incident_limit}"
		api_get "${incident_path}" "${incident_file}"
		incident_count="$(incident_context_match_count "${incident_file}" "${expected_incident_provider}" "${expected_incident_id}" "${expected_incident_scope}" "${expected_incident_service}")"
		require_target_min_count incident_contexts "${incident_count}" "${incident_min}"
		local mcp_incident_file="${TMP_DIR}/mcp-incident-context.json"
		local mcp_incident_args
		mcp_incident_args="$(jq -n \
			--arg provider "${expected_incident_provider}" \
			--arg provider_incident_id "${expected_incident_id}" \
			--arg scope_id "${expected_incident_scope}" \
			--arg service_id "${expected_incident_service}" \
			--argjson limit "${incident_limit}" \
			'{provider:$provider, provider_incident_id:$provider_incident_id, scope_id:$scope_id, service_id:$service_id, limit:$limit}')"
		mcp_tool_envelope get_incident_context "${mcp_incident_args}" "${mcp_incident_file}"
		mcp_incident_count="$(incident_context_match_count "${mcp_incident_file}" "${expected_incident_provider}" "${expected_incident_id}" "${expected_incident_scope}" "${expected_incident_service}")"
		require_target_min_count mcp_incident_contexts "${mcp_incident_count}" "${incident_min}"
	fi
	if ((work_item_min > 0)); then
		require_work_item_target_anchor "${expected_work_item_key}" "${expected_work_item_provider_id}" "${expected_work_item_url_fingerprint}"
		local work_item_limit work_item_file="${TMP_DIR}/work-item-evidence.json"
		work_item_limit="$(list_limit_for_minimum work_item_evidence "${work_item_min}")"
		local work_item_path="/work-items/evidence?"
		if [[ -n "${expected_work_item_scope}" ]]; then
			work_item_path="${work_item_path}scope_id=$(urlencode "${expected_work_item_scope}")&"
		fi
		if [[ -n "${expected_work_item_key}" ]]; then
			work_item_path="${work_item_path}work_item_key=$(urlencode "${expected_work_item_key}")&"
		fi
		if [[ -n "${expected_work_item_provider_id}" ]]; then
			work_item_path="${work_item_path}provider_work_item_id=$(urlencode "${expected_work_item_provider_id}")&"
		fi
		if [[ -n "${expected_work_item_url_fingerprint}" ]]; then
			work_item_path="${work_item_path}url_fingerprint=$(urlencode "${expected_work_item_url_fingerprint}")&"
		fi
		work_item_path="${work_item_path}limit=${work_item_limit}"
		api_get "${work_item_path}" "${work_item_file}"
		work_item_count="$(work_item_evidence_match_count "${work_item_file}" "${expected_work_item_scope}" "${expected_work_item_key}" "${expected_work_item_provider_id}" "${expected_work_item_url_fingerprint}")"
		require_target_min_count work_item_evidence "${work_item_count}" "${work_item_min}"
		local mcp_work_item_file="${TMP_DIR}/mcp-work-item-evidence.json"
		local mcp_work_item_args
		mcp_work_item_args="$(jq -n \
			--arg scope_id "${expected_work_item_scope}" \
			--arg work_item_key "${expected_work_item_key}" \
			--arg provider_work_item_id "${expected_work_item_provider_id}" \
			--arg url_fingerprint "${expected_work_item_url_fingerprint}" \
			--argjson limit "${work_item_limit}" \
			'{scope_id:$scope_id, work_item_key:$work_item_key, provider_work_item_id:$provider_work_item_id, url_fingerprint:$url_fingerprint, limit:$limit}')"
		mcp_tool_envelope list_work_item_evidence "${mcp_work_item_args}" "${mcp_work_item_file}"
		mcp_work_item_count="$(work_item_evidence_match_count "${mcp_work_item_file}" "${expected_work_item_scope}" "${expected_work_item_key}" "${expected_work_item_provider_id}" "${expected_work_item_url_fingerprint}")"
		require_target_min_count mcp_work_item_evidence "${mcp_work_item_count}" "${work_item_min}"
	fi
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
		if [[ -n "${expected_source_repo}" ]]; then
			image_path="${image_path}&source_repository_id=$(urlencode "${expected_source_repo}")"
		fi
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
	if ((service_story_min > 0)); then
		local service_selector
		service_selector="$(target_story_service_selector "${expected_service_id}" "${expected_workload_id}")"
		if [[ -z "${service_selector}" ]]; then
			echo "target service_story_image_package requires expected_service_id or expected_workload_id" >&2
			return 1
		fi
		local service_story_file="${TMP_DIR}/service-story.json"
		api_get "$(target_story_service_api_path "${service_selector}" "${expected_service_id}" "${expected_workload_id}" "${repo_selector}")" "${service_story_file}"
		service_story_count="$(service_story_image_package_match_count "${service_story_file}" "${expected_image_digest}" "${expected_image_ref}" "${expected_sbom_digest}")"
		require_min_count service_story_image_package "${service_story_count}" "${service_story_min}"

		local mcp_service_story_file="${TMP_DIR}/mcp-service-story.json"
		local mcp_service_story_args
		mcp_service_story_args="$(jq -n --arg workload_id "${service_selector}" '{workload_id:$workload_id}')"
		mcp_tool_envelope get_service_story "${mcp_service_story_args}" "${mcp_service_story_file}"
		mcp_service_story_count="$(service_story_image_package_match_count "${mcp_service_story_file}" "${expected_image_digest}" "${expected_image_ref}" "${expected_sbom_digest}")"
		require_min_count mcp_service_story_image_package "${mcp_service_story_count}" "${service_story_min}"
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

	local documentation_reason_segment incident_reason_segment work_item_reason_segment
	documentation_reason_segment="$(target_story_reason_segment documentation_findings "${documentation_reason}")"
	incident_reason_segment="$(target_story_reason_segment incident_contexts "${incident_reason}")"
	work_item_reason_segment="$(target_story_reason_segment work_item_evidence "${work_item_reason}")"
	printf 'remote E2E target story proof counts: proof_mode=%s repository_story=1 impact_findings=%s security_alert_reconciliations=%s security_alert_expected_rows=%s container_image_identities=%s sbom_attachments=%s service_story_image_package=%s service_catalog_correlations=%s service_catalog_local_descriptors=%s service_catalog_external_confirmation=%s service_catalog_external_confirmation_reason=%s ci_cd_run_correlations=%s ci_cd_static_workflow_state=%s ci_cd_live_run_state=%s cloud_resources=%s mcp_service_story_image_package=%s mcp_service_catalog_correlations=%s mcp_service_catalog_local_descriptors=%s mcp_service_catalog_external_confirmation=%s mcp_service_catalog_external_confirmation_reason=%s mcp_ci_cd_run_correlations=%s mcp_ci_cd_static_workflow_state=%s mcp_ci_cd_live_run_state=%s mcp_cloud_resources=%s documentation_findings=%s incident_contexts=%s work_item_evidence=%s mcp_documentation_findings=%s mcp_incident_contexts=%s mcp_work_item_evidence=%s%s%s%s\n' \
		"${proof_mode}" \
		"${impact_count}" \
		"${security_count}" \
		"${security_expected_rows_count}" \
		"${image_count}" \
		"${sbom_count}" \
		"${service_story_count}" \
		"${catalog_count}" \
		"${catalog_local_descriptor_state}" \
		"${catalog_external_confirmation_state}" \
		"${catalog_external_confirmation_reason}" \
		"${cicd_count}" \
		"${cicd_static_state}" \
		"${cicd_live_state}" \
		"${cloud_count}" \
		"${mcp_service_story_count}" \
		"${mcp_catalog_count}" \
		"${mcp_catalog_local_descriptor_state}" \
		"${mcp_catalog_external_confirmation_state}" \
		"${mcp_catalog_external_confirmation_reason}" \
		"${mcp_cicd_count}" \
		"${mcp_cicd_static_state}" \
		"${mcp_cicd_live_state}" \
		"${mcp_cloud_count}" \
		"${documentation_count}" \
		"${incident_count}" \
		"${work_item_count}" \
		"${mcp_documentation_count}" \
		"${mcp_incident_count}" \
		"${mcp_work_item_count}" \
		"${documentation_reason_segment}" \
		"${incident_reason_segment}" \
		"${work_item_reason_segment}"
}

main "$@"
