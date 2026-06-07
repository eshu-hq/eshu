#!/usr/bin/env bash

target_story_cicd_match_count() {
	local file="$1"
	local repo_selector="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	jq -r \
		--arg repo_selector "${repo_selector}" \
		--arg expected_image_digest "${expected_image_digest}" \
		--arg expected_image_ref "${expected_image_ref}" '
		(.data // .) as $body |
		def selected_anchor($row):
			if $expected_image_digest != "" then
				(($row.artifact_digest // "") == $expected_image_digest)
			elif $expected_image_ref != "" then
				(($row.image_ref // "") == $expected_image_ref)
			else
				true
			end;
		[
			$body.correlations[]?
			| select((.repository_id // "") == $repo_selector and selected_anchor(.))
		] | length
	' "${file}"
}

target_story_cicd_mcp_args() {
	local expected_image_digest="$1"
	local expected_image_ref="$2"
	local repo_selector="$3"
	local limit="$4"
	jq -n \
		--arg artifact_digest "${expected_image_digest}" \
		--arg image_ref "${expected_image_ref}" \
		--arg repository_id "${repo_selector}" \
		--argjson limit "${limit}" '
		{repository_id:$repository_id, limit:$limit}
		+ if $artifact_digest != "" then
			{artifact_digest:$artifact_digest}
		elif $image_ref != "" then
			{image_ref:$image_ref}
		else
			{}
		end
	'
}

target_story_cicd_expected_missing_evidence_count() {
	jq -r '
		(.expected_ci_cd_missing_evidence // []) as $value |
		if ($value | type) == "string" then
			if $value == "" then 0 else 1 end
		elif ($value | type) == "array" then
			[$value[] | select(type == "string" and . != "")] | length
		else
			0
		end
	' "${TARGET_STORY_FILE}"
}

target_story_cicd_missing_evidence_csv() {
	local file="$1"
	jq -r '
		(.data // .).evidence_summary.missing_evidence // [] |
		map(select(type == "string" and . != "")) |
		unique |
		sort |
		join(",")
	' "${file}"
}

target_story_require_cicd_missing_evidence() {
	local file="$1"
	local label="$2"
	local missing
	missing="$(jq -r --slurpfile manifest "${TARGET_STORY_FILE}" '
		($manifest[0].expected_ci_cd_missing_evidence // []) as $expected |
		((.data // .).evidence_summary.missing_evidence // []) as $actual |
		(if ($expected | type) == "string" then
			[$expected]
		elif ($expected | type) == "array" then
			$expected
		else
			[]
		end) |
		map(select(type == "string" and . != "")) |
		unique |
		map(select(. as $item | ($actual | index($item)) | not)) |
		.[0] // ""
	' "${file}")"
	if [[ -n "${missing}" ]]; then
		printf 'target %s missing required class %s\n' "${label}" "${missing}" >&2
		return 1
	fi
}

target_story_cicd_api_path() {
	local repo_query="$1"
	local expected_image_digest="$2"
	local expected_image_ref="$3"
	local limit="$4"
	local path="/ci-cd/run-correlations?repository_id=${repo_query}"
	if [[ -n "${expected_image_digest}" ]]; then
		path="${path}&artifact_digest=$(urlencode "${expected_image_digest}")"
	elif [[ -n "${expected_image_ref}" ]]; then
		path="${path}&image_ref=$(urlencode "${expected_image_ref}")"
	fi
	printf '%s&limit=%s' "${path}" "${limit}"
}

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

	local anchor_param anchor_value
	if [[ -n "${expected_image_digest}" ]]; then
		anchor_param="artifact_digest"
		anchor_value="${expected_image_digest}"
	else
		anchor_param="image_ref"
		anchor_value="${expected_image_ref}"
	fi

	local cicd_limit encoded_anchor
	cicd_limit="$(list_limit_for_minimum ci_cd_run_correlations "${cicd_min}")"
	encoded_anchor="$(urlencode "${anchor_value}")"

	local cicd_file="${TMP_DIR}/cicd-count.json"
	api_get "/ci-cd/run-correlations/count?repository_id=${repo_query}&${anchor_param}=${encoded_anchor}" "${cicd_file}"
	cicd_count="$(json_int "${cicd_file}" '.total_correlations')"
	require_min_count ci_cd_run_correlations "${cicd_count}" "${cicd_min}"

	local cicd_list_file="${TMP_DIR}/cicd-list.json"
	api_get "/ci-cd/run-correlations?repository_id=${repo_query}&${anchor_param}=${encoded_anchor}&limit=${cicd_limit}" "${cicd_list_file}"
	cicd_count="$(target_story_cicd_match_count "${cicd_list_file}" "${repo_selector}" "${expected_image_digest}" "${expected_image_ref}")"
	require_min_count ci_cd_run_correlations "${cicd_count}" "${cicd_min}"
	cicd_static_state="$(jq -r '(.data // .).evidence_summary.static_workflow_artifacts.state // "missing"' "${cicd_list_file}")"
	cicd_live_state="$(jq -r '(.data // .).evidence_summary.live_run_correlations.state // "missing"' "${cicd_list_file}")"

	local mcp_cicd_file="${TMP_DIR}/mcp-cicd.json"
	local mcp_cicd_args
	mcp_cicd_args="$(target_story_cicd_mcp_args "${expected_image_digest}" "${expected_image_ref}" "${repo_selector}" "${cicd_limit}")"
	mcp_tool_envelope list_ci_cd_run_correlations "${mcp_cicd_args}" "${mcp_cicd_file}"
	mcp_cicd_count="$(target_story_cicd_match_count "${mcp_cicd_file}" "${repo_selector}" "${expected_image_digest}" "${expected_image_ref}")"
	require_min_count mcp_ci_cd_run_correlations "${mcp_cicd_count}" "${cicd_min}"
	mcp_cicd_static_state="$(jq -r '(.data // .).evidence_summary.static_workflow_artifacts.state // "missing"' "${mcp_cicd_file}")"
	mcp_cicd_live_state="$(jq -r '(.data // .).evidence_summary.live_run_correlations.state // "missing"' "${mcp_cicd_file}")"
}

target_story_verify_cicd_missing_evidence() {
	local repo_query="$1"
	local repo_selector="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	local cicd_limit=1
	local cicd_list_file="${TMP_DIR}/cicd-list.json"
	api_get "$(target_story_cicd_api_path "${repo_query}" "${expected_image_digest}" "${expected_image_ref}" "${cicd_limit}")" "${cicd_list_file}"
	cicd_count="$(target_story_cicd_match_count "${cicd_list_file}" "${repo_selector}" "${expected_image_digest}" "${expected_image_ref}")"
	cicd_static_state="$(jq -r '(.data // .).evidence_summary.static_workflow_artifacts.state // "missing"' "${cicd_list_file}")"
	cicd_live_state="$(jq -r '(.data // .).evidence_summary.live_run_correlations.state // "missing"' "${cicd_list_file}")"
	cicd_missing_evidence="$(target_story_cicd_missing_evidence_csv "${cicd_list_file}")"
	target_story_require_cicd_missing_evidence "${cicd_list_file}" ci_cd_missing_evidence

	local mcp_cicd_file="${TMP_DIR}/mcp-cicd.json"
	local mcp_cicd_args
	mcp_cicd_args="$(target_story_cicd_mcp_args "${expected_image_digest}" "${expected_image_ref}" "${repo_selector}" "${cicd_limit}")"
	mcp_tool_envelope list_ci_cd_run_correlations "${mcp_cicd_args}" "${mcp_cicd_file}"
	mcp_cicd_count="$(target_story_cicd_match_count "${mcp_cicd_file}" "${repo_selector}" "${expected_image_digest}" "${expected_image_ref}")"
	mcp_cicd_static_state="$(jq -r '(.data // .).evidence_summary.static_workflow_artifacts.state // "missing"' "${mcp_cicd_file}")"
	mcp_cicd_live_state="$(jq -r '(.data // .).evidence_summary.live_run_correlations.state // "missing"' "${mcp_cicd_file}")"
	mcp_cicd_missing_evidence="$(target_story_cicd_missing_evidence_csv "${mcp_cicd_file}")"
	target_story_require_cicd_missing_evidence "${mcp_cicd_file}" mcp_ci_cd_missing_evidence
}
