#!/usr/bin/env bash

target_story_verify_cicd_run_correlations() {
	local repo_query="$1"
	local repo_selector="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	local cicd_min="$5"
	if [[ -z "${expected_image_digest}" && -z "${expected_image_ref}" ]]; then
		echo "target ci_cd_run_correlations requires expected_image_digest or expected_image_ref" >&2
		return 1
	fi

	local cicd_limit
	cicd_limit="$(list_limit_for_minimum ci_cd_run_correlations "${cicd_min}")"
	local cicd_file="${TMP_DIR}/cicd-count.json"
	local cicd_list_file="${TMP_DIR}/cicd-list.json"
	local cicd_list_path
	if [[ -n "${expected_image_digest}" ]]; then
		api_get "/ci-cd/run-correlations/count?repository_id=${repo_query}&artifact_digest=$(urlencode "${expected_image_digest}")" "${cicd_file}"
		cicd_list_path="/ci-cd/run-correlations?repository_id=${repo_query}&artifact_digest=$(urlencode "${expected_image_digest}")&limit=${cicd_limit}"
		cicd_count="$(json_int "${cicd_file}" '.total_correlations')"
	else
		api_get "/ci-cd/run-correlations/count?repository_id=${repo_query}&image_ref=$(urlencode "${expected_image_ref}")" "${cicd_file}"
		cicd_list_path="/ci-cd/run-correlations?repository_id=${repo_query}&image_ref=$(urlencode "${expected_image_ref}")&limit=${cicd_limit}"
		cicd_count="$(json_int "${cicd_file}" '.total_correlations')"
	fi
	require_min_count ci_cd_run_correlations "${cicd_count}" "${cicd_min}"

	api_get "${cicd_list_path}" "${cicd_list_file}"
	cicd_static_state="$(jq -r '(.data // .).evidence_summary.static_workflow_artifacts.state // "missing"' "${cicd_list_file}")"
	cicd_live_state="$(jq -r '(.data // .).evidence_summary.live_run_correlations.state // "missing"' "${cicd_list_file}")"

	local mcp_cicd_file="${TMP_DIR}/mcp-cicd.json"
	local mcp_cicd_args
	if [[ -n "${expected_image_digest}" ]]; then
		mcp_cicd_args="$(jq -n \
			--arg repository_id "${repo_selector}" \
			--arg artifact_digest "${expected_image_digest}" \
			--argjson limit "${cicd_limit}" \
			'{repository_id:$repository_id, artifact_digest:$artifact_digest, limit:$limit}')"
	else
		mcp_cicd_args="$(jq -n \
			--arg repository_id "${repo_selector}" \
			--arg image_ref "${expected_image_ref}" \
			--argjson limit "${cicd_limit}" \
			'{repository_id:$repository_id, image_ref:$image_ref, limit:$limit}')"
	fi
	mcp_tool_envelope list_ci_cd_run_correlations "${mcp_cicd_args}" "${mcp_cicd_file}"
	mcp_cicd_count="$(json_int "${mcp_cicd_file}" '.count')"
	require_min_count mcp_ci_cd_run_correlations "${mcp_cicd_count}" "${cicd_min}"
	mcp_cicd_static_state="$(jq -r '(.data // .).evidence_summary.static_workflow_artifacts.state // "missing"' "${mcp_cicd_file}")"
	mcp_cicd_live_state="$(jq -r '(.data // .).evidence_summary.live_run_correlations.state // "missing"' "${mcp_cicd_file}")"
}
