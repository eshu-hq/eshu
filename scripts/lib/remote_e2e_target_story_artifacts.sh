#!/usr/bin/env bash

target_story_container_image_list_api_path() {
	local source_repository_id="$1"
	local expected_image_repo="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	local limit="$5"
	local path="/supply-chain/container-images/identities"
	local sep="?"
	if [[ -n "${expected_image_digest}" ]]; then
		path="${path}${sep}digest=$(urlencode "${expected_image_digest}")"
		sep="&"
	elif [[ -n "${expected_image_ref}" ]]; then
		path="${path}${sep}image_ref=$(urlencode "${expected_image_ref}")"
		sep="&"
	fi
	if [[ -n "${source_repository_id}" ]]; then
		path="${path}${sep}source_repository_id=$(urlencode "${source_repository_id}")"
		sep="&"
	fi
	if [[ -n "${expected_image_repo}" ]]; then
		path="${path}${sep}repository_id=$(urlencode "${expected_image_repo}")"
		sep="&"
	fi
	printf '%s%s%s' "${path}" "${sep}" "limit=${limit}"
}

target_story_container_image_list_mcp_args() {
	local source_repository_id="$1"
	local expected_image_repo="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	local limit="$5"
	jq -n \
		--arg source_repository_id "${source_repository_id}" \
		--arg repository_id "${expected_image_repo}" \
		--arg digest "${expected_image_digest}" \
		--arg image_ref "${expected_image_ref}" \
		--argjson limit "${limit}" '
		{limit:$limit}
		+ if $source_repository_id != "" then {source_repository_id:$source_repository_id} else {} end
		+ if $repository_id != "" then {repository_id:$repository_id} else {} end
		+ if $digest != "" then {digest:$digest}
		  elif $image_ref != "" then {image_ref:$image_ref}
		  else {} end
	'
}

target_story_container_image_match_count() {
	local file="$1"
	local source_repository_id="$2"
	local expected_image_repo="$3"
	local expected_image_digest="$4"
	local expected_image_ref="$5"
	local expected_source_revision="$6"
	jq -r \
		--arg source_repository_id "${source_repository_id}" \
		--arg repository_id "${expected_image_repo}" \
		--arg digest "${expected_image_digest}" \
		--arg image_ref "${expected_image_ref}" \
		--arg source_revision "${expected_source_revision}" '
		(.data // .) as $body |
		[
			$body.identities[]?
			| select($digest == "" or ((.digest // "") == $digest))
			| select($image_ref == "" or ((.image_ref // "") == $image_ref))
			| select($repository_id == "" or ((.repository_id // "") == $repository_id))
			| select($source_repository_id == "" or ((.source_repository_ids // []) | index($source_repository_id)))
			| select($source_revision == "" or ((.source_revision // "") == $source_revision))
		] | length
	' "${file}"
}

target_story_verify_container_image_positive_evidence() {
	local source_repository_id="$1"
	local expected_image_repo="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	local expected_source_revision="$5"
	local image_min="$6"
	local image_anchor=""
	if [[ -n "${expected_image_digest}" ]]; then
		image_anchor="${expected_image_digest}"
	elif [[ -n "${expected_image_ref}" ]]; then
		image_anchor="${expected_image_ref}"
	fi
	if [[ -z "${image_anchor}" ]]; then
		echo "target container_image_identities requires expected_image_digest or expected_image_ref" >&2
		return 1
	fi

	local image_file="${TMP_DIR}/container-image-count.json"
	api_get "$(target_story_container_image_count_api_path "${source_repository_id}" "${expected_image_repo}" "${expected_image_digest}" "${expected_image_ref}")" "${image_file}"
	require_min_count container_image_identities "$(json_int "${image_file}" '.total_identities')" "${image_min}"

	local image_limit image_list_file
	image_limit="$(list_limit_for_minimum container_image_identities "${image_min}")"
	image_list_file="${TMP_DIR}/container-image-list.json"
	api_get "$(target_story_container_image_list_api_path "${source_repository_id}" "${expected_image_repo}" "${expected_image_digest}" "${expected_image_ref}" "${image_limit}")" "${image_list_file}"
	image_count="$(target_story_container_image_match_count "${image_list_file}" "${source_repository_id}" "${expected_image_repo}" "${expected_image_digest}" "${expected_image_ref}" "${expected_source_revision}")"
	require_min_count container_image_identities "${image_count}" "${image_min}"

	local mcp_image_file="${TMP_DIR}/mcp-container-image-list.json"
	local mcp_image_args
	mcp_image_args="$(target_story_container_image_list_mcp_args "${source_repository_id}" "${expected_image_repo}" "${expected_image_digest}" "${expected_image_ref}" "${image_limit}")"
	mcp_tool_envelope list_container_image_identities "${mcp_image_args}" "${mcp_image_file}"
	mcp_image_count="$(target_story_container_image_match_count "${mcp_image_file}" "${source_repository_id}" "${expected_image_repo}" "${expected_image_digest}" "${expected_image_ref}" "${expected_source_revision}")"
	require_min_count mcp_container_image_identities "${mcp_image_count}" "${image_min}"
}

target_story_sbom_list_api_path() {
	local repo_query="$1"
	local expected_sbom_digest="$2"
	local limit="$3"
	local path="/supply-chain/sbom-attestations/attachments?repository_id=${repo_query}"
	if [[ -n "${expected_sbom_digest}" ]]; then
		path="${path}&subject_digest=$(urlencode "${expected_sbom_digest}")"
	fi
	printf '%s&limit=%s' "${path}" "${limit}"
}

target_story_sbom_list_mcp_args() {
	local repo_selector="$1"
	local expected_sbom_digest="$2"
	local limit="$3"
	jq -n \
		--arg repository_id "${repo_selector}" \
		--arg subject_digest "${expected_sbom_digest}" \
		--argjson limit "${limit}" '
		{repository_id:$repository_id, limit:$limit}
		+ if $subject_digest != "" then {subject_digest:$subject_digest} else {} end
	'
}

target_story_sbom_match_count() {
	local file="$1"
	local repo_selector="$2"
	local expected_sbom_digest="$3"
	jq -r \
		--arg repository_id "${repo_selector}" \
		--arg subject_digest "${expected_sbom_digest}" '
		(.data // .) as $body |
		[
			$body.attachments[]?
			| select($subject_digest == "" or ((.subject_digest // "") == $subject_digest))
			| select((.repository_ids // []) | index($repository_id))
		] | length
	' "${file}"
}

target_story_verify_sbom_positive_evidence() {
	local repo_query="$1"
	local repo_selector="$2"
	local expected_sbom_digest="$3"
	local sbom_min="$4"
	local sbom_file="${TMP_DIR}/sbom-count.json"
	api_get "$(target_story_sbom_count_api_path "${repo_query}" "${expected_sbom_digest}")" "${sbom_file}"
	require_min_count sbom_attachments "$(json_int "${sbom_file}" '.total_attachments')" "${sbom_min}"

	local sbom_limit sbom_list_file
	sbom_limit="$(list_limit_for_minimum sbom_attachments "${sbom_min}")"
	sbom_list_file="${TMP_DIR}/sbom-list.json"
	api_get "$(target_story_sbom_list_api_path "${repo_query}" "${expected_sbom_digest}" "${sbom_limit}")" "${sbom_list_file}"
	sbom_count="$(target_story_sbom_match_count "${sbom_list_file}" "${repo_selector}" "${expected_sbom_digest}")"
	require_min_count sbom_attachments "${sbom_count}" "${sbom_min}"

	local mcp_sbom_file="${TMP_DIR}/mcp-sbom-list.json"
	local mcp_sbom_args
	mcp_sbom_args="$(target_story_sbom_list_mcp_args "${repo_selector}" "${expected_sbom_digest}" "${sbom_limit}")"
	mcp_tool_envelope list_sbom_attestation_attachments "${mcp_sbom_args}" "${mcp_sbom_file}"
	mcp_sbom_count="$(target_story_sbom_match_count "${mcp_sbom_file}" "${repo_selector}" "${expected_sbom_digest}")"
	require_min_count mcp_sbom_attachments "${mcp_sbom_count}" "${sbom_min}"
}
