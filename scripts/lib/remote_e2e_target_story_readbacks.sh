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
	if [[ -n "${expected_workload_id}" ]]; then
		path="${path}${sep}service_id=$(urlencode "${expected_workload_id}")"
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
