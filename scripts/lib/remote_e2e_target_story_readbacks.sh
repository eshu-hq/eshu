#!/usr/bin/env bash

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

target_story_service_selector() {
	local expected_service_id="$1"
	local expected_workload_id="$2"
	if [[ -n "${expected_workload_id}" ]]; then
		printf '%s' "${expected_workload_id}"
		return 0
	fi
	printf '%s' "${expected_service_id}"
}

target_story_service_path_name() {
	local selector="$1"
	if [[ "${selector}" == *:* ]]; then
		printf '%s' "${selector#*:}"
		return 0
	fi
	printf '%s' "${selector}"
}

target_story_service_api_path() {
	local selector="$1"
	local expected_service_id="$2"
	local expected_workload_id="$3"
	local repo_selector="$4"
	local service_name path sep
	service_name="$(target_story_service_path_name "${selector}")"
	path="/services/$(urlencode "${service_name}")/story"
	sep="?"
	if [[ -n "${expected_workload_id}" && -n "${expected_service_id}" ]]; then
		path="${path}${sep}service_id=$(urlencode "${expected_service_id}")"
		sep="&"
	fi
	if [[ -n "${repo_selector}" ]]; then
		path="${path}${sep}repo=$(urlencode "${repo_selector}")"
	fi
	printf '%s' "${path}"
}

service_story_image_package_match_count() {
	local file="$1"
	local expected_digest="$2"
	local expected_image_ref="$3"
	local expected_sbom_digest="$4"
	jq -r \
		--arg digest "${expected_digest}" \
		--arg image_ref "${expected_image_ref}" \
		--arg sbom_digest "${expected_sbom_digest}" '
		(.data // .) as $body |
		def image_ref_matches($row):
			$image_ref == ""
			or (($row.image_ref // "") == $image_ref)
			or (($row.deployment_image_ref // "") == $image_ref);
		def digest_matches($row):
			$digest == "" or (($row.digest // "") == $digest);
		def sbom_matches($row):
			$sbom_digest == ""
			or (($row.sbom_subject_digest // "") == $sbom_digest)
			or (($row.digest // "") == $sbom_digest);
		[
			$body.code_to_runtime_trace.segments[]?
			| select((.name // "") == "image_package")
			| select((.status // "") == "exact")
			| .evidence[]?
			| select(image_ref_matches(.) and digest_matches(.) and sbom_matches(.))
			| select(((.sbom_attachment_status // "") | startswith("attached_")))
		] | length
	' "${file}"
}

target_story_image_package_expected_missing_evidence_count() {
	jq -r '
		(.expected_image_package_missing_evidence // []) as $value |
		if ($value | type) == "string" then
			if $value == "" then 0 else 1 end
		elif ($value | type) == "array" then
			[$value[] | select(type == "string" and . != "")] | length
		else
			0
		end
	' "${TARGET_STORY_FILE}"
}

target_story_container_image_expected_missing_evidence_count() {
	jq -r '
		(.expected_container_image_missing_evidence // []) as $value |
		if ($value | type) == "string" then
			if $value == "" then 0 else 1 end
		elif ($value | type) == "array" then
			[$value[] | select(type == "string" and . != "")] | length
		else
			0
		end
	' "${TARGET_STORY_FILE}"
}

target_story_sbom_expected_missing_evidence_count() {
	jq -r '
		(.expected_sbom_missing_evidence // []) as $value |
		if ($value | type) == "string" then
			if $value == "" then 0 else 1 end
		elif ($value | type) == "array" then
			[$value[] | select(type == "string" and . != "")] | length
		else
			0
		end
	' "${TARGET_STORY_FILE}"
}

service_story_image_package_missing_evidence_csv() {
	local file="$1"
	jq -r '
		(.data // .) as $body |
		[
			($body.code_to_runtime_trace.segments[]?
				| select((.name // "") == "image_package")
				| .missing_evidence[]?),
			($body.code_to_runtime_trace.segments[]?
				| select((.name // "") == "image_package")
				| .missing_evidence_details[]?.reason?)
		] |
		map(select(type == "string" and test("^[a-z0-9_:-]+$"))) |
		unique |
		sort |
		join(",")
	' "${file}"
}

service_story_image_package_collector_scope_csv() {
	local file="$1"
	jq -r '
		(.data // .) as $body |
		[
			$body.code_to_runtime_trace.segments[]?
			| select((.name // "") == "image_package")
			| .missing_evidence_details[]?.collector_scope?
		] |
		map(select(type == "string" and test("^[a-z0-9_:-]+$"))) |
		unique |
		sort |
		if length == 0 then "none" else join(",") end
	' "${file}"
}

target_story_missing_evidence_csv() {
	local file="$1"
	local filter="$2"
	jq -r "${filter} |
		map(select(type == \"string\" and test(\"^[a-z0-9_:-]+$\"))) |
		unique |
		sort |
		join(\",\")
	" "${file}"
}

target_story_require_expected_missing_evidence() {
	local file="$1"
	local manifest_key="$2"
	local actual_filter="$3"
	local label="$4"
	local missing
	missing="$(jq -r --arg key "${manifest_key}" --slurpfile actual "${file}" '
		(.[$key] // []) as $expected |
		($actual[0] | '"${actual_filter}"') as $actual_values |
		(if ($expected | type) == "string" then
			[$expected]
		elif ($expected | type) == "array" then
			$expected
		else
			[]
		end) |
		map(select(type == "string" and . != "")) |
		unique |
		map(select(. as $item | ($actual_values | index($item)) | not)) |
		.[0] // ""
	' "${TARGET_STORY_FILE}")"
	if [[ -n "${missing}" ]]; then
		printf 'target %s missing required class %s\n' "${label}" "${missing}" >&2
		return 1
	fi
}

target_story_require_image_package_missing_evidence() {
	local file="$1"
	local label="$2"
	local missing
	missing="$(jq -r --slurpfile manifest "${TARGET_STORY_FILE}" '
		($manifest[0].expected_image_package_missing_evidence // []) as $expected |
		(.data // .) as $body |
		[
			($body.code_to_runtime_trace.segments[]?
				| select((.name // "") == "image_package")
				| .missing_evidence[]?),
			($body.code_to_runtime_trace.segments[]?
				| select((.name // "") == "image_package")
				| .missing_evidence_details[]?.reason?)
		] as $actual |
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

target_story_container_image_count_api_path() {
	local source_repository_id="$1"
	local expected_image_repo="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	local path="/supply-chain/container-images/identities/count"
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
	fi
	printf '%s' "${path}"
}

target_story_container_image_count_mcp_args() {
	local source_repository_id="$1"
	local expected_image_repo="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	jq -n \
		--arg source_repository_id "${source_repository_id}" \
		--arg repository_id "${expected_image_repo}" \
		--arg digest "${expected_image_digest}" \
		--arg image_ref "${expected_image_ref}" '
		{}
		+ if $source_repository_id != "" then {source_repository_id:$source_repository_id} else {} end
		+ if $repository_id != "" then {repository_id:$repository_id} else {} end
		+ if $digest != "" then {digest:$digest}
		  elif $image_ref != "" then {image_ref:$image_ref}
		  else {} end
	'
}

target_story_verify_container_image_missing_evidence() {
	local source_repository_id="$1"
	local expected_image_repo="$2"
	local expected_image_digest="$3"
	local expected_image_ref="$4"
	local image_file="${TMP_DIR}/container-image-missing-count.json"
	api_get "$(target_story_container_image_count_api_path "${source_repository_id}" "${expected_image_repo}" "${expected_image_digest}" "${expected_image_ref}")" "${image_file}"
	image_count="$(json_int "${image_file}" '.total_identities')"
	image_missing_evidence="$(target_story_missing_evidence_csv "${image_file}" '(.data // .).source_bridge.missing_evidence // []')"
	target_story_require_expected_missing_evidence "${image_file}" expected_container_image_missing_evidence '(.data // .).source_bridge.missing_evidence // []' container_image_missing_evidence

	local mcp_image_file="${TMP_DIR}/mcp-container-image-missing-count.json"
	local mcp_image_args
	mcp_image_args="$(target_story_container_image_count_mcp_args "${source_repository_id}" "${expected_image_repo}" "${expected_image_digest}" "${expected_image_ref}")"
	mcp_tool_envelope count_container_image_identities "${mcp_image_args}" "${mcp_image_file}"
	mcp_image_count="$(json_int "${mcp_image_file}" '.total_identities')"
	mcp_image_missing_evidence="$(target_story_missing_evidence_csv "${mcp_image_file}" '(.data // .).source_bridge.missing_evidence // []')"
	target_story_require_expected_missing_evidence "${mcp_image_file}" expected_container_image_missing_evidence '(.data // .).source_bridge.missing_evidence // []' mcp_container_image_missing_evidence
}

target_story_sbom_count_api_path() {
	local repo_query="$1"
	local expected_sbom_digest="$2"
	local path="/supply-chain/sbom-attestations/attachments/count?repository_id=${repo_query}"
	if [[ -n "${expected_sbom_digest}" ]]; then
		path="${path}&subject_digest=$(urlencode "${expected_sbom_digest}")"
	fi
	printf '%s' "${path}"
}

target_story_sbom_count_mcp_args() {
	local repo_selector="$1"
	local expected_sbom_digest="$2"
	jq -n \
		--arg repository_id "${repo_selector}" \
		--arg subject_digest "${expected_sbom_digest}" '
		{repository_id:$repository_id}
		+ if $subject_digest != "" then {subject_digest:$subject_digest} else {} end
	'
}

target_story_verify_sbom_missing_evidence() {
	local repo_query="$1"
	local repo_selector="$2"
	local expected_sbom_digest="$3"
	local sbom_file="${TMP_DIR}/sbom-missing-count.json"
	api_get "$(target_story_sbom_count_api_path "${repo_query}" "${expected_sbom_digest}")" "${sbom_file}"
	sbom_count="$(json_int "${sbom_file}" '.total_attachments')"
	sbom_missing_evidence="$(target_story_missing_evidence_csv "${sbom_file}" '(.data // .).missing_evidence // []')"
	target_story_require_expected_missing_evidence "${sbom_file}" expected_sbom_missing_evidence '(.data // .).missing_evidence // []' sbom_missing_evidence

	local mcp_sbom_file="${TMP_DIR}/mcp-sbom-missing-count.json"
	local mcp_sbom_args
	mcp_sbom_args="$(target_story_sbom_count_mcp_args "${repo_selector}" "${expected_sbom_digest}")"
	mcp_tool_envelope count_sbom_attestation_attachments "${mcp_sbom_args}" "${mcp_sbom_file}"
	mcp_sbom_count="$(json_int "${mcp_sbom_file}" '.total_attachments')"
	mcp_sbom_missing_evidence="$(target_story_missing_evidence_csv "${mcp_sbom_file}" '(.data // .).missing_evidence // []')"
	target_story_require_expected_missing_evidence "${mcp_sbom_file}" expected_sbom_missing_evidence '(.data // .).missing_evidence // []' mcp_sbom_missing_evidence
}

target_story_verify_image_package_missing_evidence() {
	local repo_selector="$1"
	local expected_service_id="$2"
	local expected_workload_id="$3"
	local service_selector
	service_selector="$(target_story_service_selector "${expected_service_id}" "${expected_workload_id}")"
	if [[ -z "${service_selector}" ]]; then
		echo "target image_package_missing_evidence requires expected_service_id or expected_workload_id" >&2
		return 1
	fi

	local service_story_file="${TMP_DIR}/service-story-image-package-missing.json"
	api_get "$(target_story_service_api_path "${service_selector}" "${expected_service_id}" "${expected_workload_id}" "${repo_selector}")" "${service_story_file}"
	image_package_missing_evidence="$(service_story_image_package_missing_evidence_csv "${service_story_file}")"
	image_package_collector_scope="$(service_story_image_package_collector_scope_csv "${service_story_file}")"
	target_story_require_image_package_missing_evidence "${service_story_file}" image_package_missing_evidence

	local mcp_service_story_file="${TMP_DIR}/mcp-service-story-image-package-missing.json"
	local mcp_service_story_args
	mcp_service_story_args="$(jq -n --arg workload_id "${service_selector}" '{workload_id:$workload_id}')"
	mcp_tool_envelope get_service_story "${mcp_service_story_args}" "${mcp_service_story_file}"
	mcp_image_package_missing_evidence="$(service_story_image_package_missing_evidence_csv "${mcp_service_story_file}")"
	mcp_image_package_collector_scope="$(service_story_image_package_collector_scope_csv "${mcp_service_story_file}")"
	target_story_require_image_package_missing_evidence "${mcp_service_story_file}" mcp_image_package_missing_evidence
}
