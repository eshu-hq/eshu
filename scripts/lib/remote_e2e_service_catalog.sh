#!/usr/bin/env bash

target_story_check_service_catalog_correlations() {
	local repo_query="$1"
	local repo_selector="$2"
	local catalog_min="$3"
	local expected_service_id="$4"
	local expected_workload_id="$5"

	local catalog_limit
	catalog_limit="$(list_limit_for_minimum service_catalog_correlations "${catalog_min}")"
	local catalog_file="${TMP_DIR}/service-catalog.json"
	local catalog_path="/service-catalog/correlations?repository_id=${repo_query}&limit=${catalog_limit}"
	if [[ -n "${expected_service_id}" ]]; then
		catalog_path="${catalog_path}&service_id=$(urlencode "${expected_service_id}")"
	fi
	if [[ -n "${expected_workload_id}" ]]; then
		catalog_path="${catalog_path}&workload_id=$(urlencode "${expected_workload_id}")"
	fi
	api_get "${catalog_path}" "${catalog_file}"
	catalog_count="$(json_int "${catalog_file}" '.count')"
	require_min_count service_catalog_correlations "${catalog_count}" "${catalog_min}"
	catalog_local_descriptor_state="$(json_string "${catalog_file}" '.evidence_summary.local_descriptors.state')"
	catalog_external_confirmation_state="$(json_string "${catalog_file}" '.evidence_summary.external_catalog_confirmation.state')"
	catalog_external_confirmation_reason="$(json_string "${catalog_file}" '.evidence_summary.external_catalog_confirmation.reason')"
	if [[ -z "${catalog_local_descriptor_state}" || -z "${catalog_external_confirmation_state}" ]]; then
		echo "target service_catalog_correlations missing evidence_summary local/external states" >&2
		return 1
	fi

	local mcp_catalog_file="${TMP_DIR}/mcp-service-catalog.json"
	local mcp_catalog_args
	mcp_catalog_args="$(jq -n \
		--arg repository_id "${repo_selector}" \
		--arg service_id "${expected_service_id}" \
		--arg workload_id "${expected_workload_id}" \
		--argjson limit "${catalog_limit}" \
		'{repository_id:$repository_id, service_id:$service_id, workload_id:$workload_id, limit:$limit}')"
	mcp_tool_envelope list_service_catalog_correlations "${mcp_catalog_args}" "${mcp_catalog_file}"
	mcp_catalog_count="$(json_int "${mcp_catalog_file}" '.count')"
	require_min_count mcp_service_catalog_correlations "${mcp_catalog_count}" "${catalog_min}"
	mcp_catalog_local_descriptor_state="$(json_string "${mcp_catalog_file}" '.evidence_summary.local_descriptors.state')"
	mcp_catalog_external_confirmation_state="$(json_string "${mcp_catalog_file}" '.evidence_summary.external_catalog_confirmation.state')"
	mcp_catalog_external_confirmation_reason="$(json_string "${mcp_catalog_file}" '.evidence_summary.external_catalog_confirmation.reason')"
	if [[ "${mcp_catalog_local_descriptor_state}" != "${catalog_local_descriptor_state}" ||
		"${mcp_catalog_external_confirmation_state}" != "${catalog_external_confirmation_state}" ||
		"${mcp_catalog_external_confirmation_reason}" != "${catalog_external_confirmation_reason}" ]]; then
		echo "target MCP service_catalog evidence_summary disagrees with API" >&2
		return 1
	fi
}
