#!/usr/bin/env bash

target_story_alignment_manifest_string() {
	local manifest_file="$1"
	local filter="$2"
	jq -r "${filter} // \"\"" "${manifest_file}"
}

target_story_alignment_manifest_int() {
	local manifest_file="$1"
	local filter="$2"
	jq -r "${filter} // 0" "${manifest_file}"
}

target_story_alignment_lower() {
	printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

target_story_alignment_strip_oci_digest() {
	local value="$1"
	if [[ "${value}" =~ @sha256:[0-9a-f]{64}$ ]]; then
		value="${value%@sha256:*}"
	fi
	printf '%s' "${value}"
}

target_story_alignment_strip_git_suffix() {
	local value="$1"
	value="${value%.git}"
	printf '%s' "${value}"
}

target_story_alignment_emit_token() {
	local token="$1"
	token="$(target_story_alignment_lower "${token}")"
	token="${token%%\?*}"
	token="${token%%#*}"
	token="$(target_story_alignment_strip_oci_digest "${token}")"
	token="$(target_story_alignment_strip_git_suffix "${token}")"
	if [[ "${#token}" -ge 3 ]]; then
		printf '%s\n' "${token}"
	fi
}

target_story_alignment_tokens() {
	local value
	local protocol_stripped path_tail colon_tail underscore_tail tagless_tail
	value="$(target_story_alignment_lower "$1")"
	value="${value%%\?*}"
	value="${value%%#*}"
	value="$(target_story_alignment_strip_oci_digest "${value}")"
	target_story_alignment_emit_token "${value}"

	protocol_stripped="${value#*://}"
	target_story_alignment_emit_token "${protocol_stripped}"

	path_tail="${protocol_stripped##*/}"
	target_story_alignment_emit_token "${path_tail}"
	colon_tail="${path_tail##*:}"
	target_story_alignment_emit_token "${colon_tail}"
	underscore_tail="${colon_tail##*_}"
	target_story_alignment_emit_token "${underscore_tail}"

	tagless_tail="${path_tail%%:*}"
	target_story_alignment_emit_token "${tagless_tail}"

	colon_tail="${tagless_tail##*:}"
	target_story_alignment_emit_token "${colon_tail}"
	underscore_tail="${colon_tail##*_}"
	target_story_alignment_emit_token "${underscore_tail}"
}

target_story_alignment_matches() {
	local left="$1"
	local right="$2"
	local left_token right_token
	while IFS= read -r left_token; do
		while IFS= read -r right_token; do
			if [[ "${left_token}" == "${right_token}" ]]; then
				return 0
			fi
		done < <(target_story_alignment_tokens "${right}")
	done < <(target_story_alignment_tokens "${left}")
	return 1
}

target_story_require_aligned_target() {
	local manifest_file="$1"
	local field="$2"
	local value="$3"
	local repo_selector
	repo_selector="$(target_story_alignment_manifest_string "${manifest_file}" '.target_repository_id')"
	if [[ -z "${value}" ]]; then
		return 0
	fi
	if target_story_alignment_matches "${repo_selector}" "${value}"; then
		return 0
	fi
	printf 'target story alignment mismatch: %s does not align with target_repository_id\n' "${field}" >&2
	return 1
}

target_story_validate_alignment() {
	local manifest_file="$1"
	local proof_mode="$2"
	if [[ "${proof_mode}" != "code_to_cloud" ]]; then
		return 0
	fi

	local security_min image_min catalog_min sbom_min
	local expected_security_repo expected_image_repo expected_image_ref
	local expected_service_id expected_workload_id expected_image_digest expected_sbom_digest
	security_min="$(target_story_alignment_manifest_int "${manifest_file}" '.minimums.security_alert_reconciliations')"
	image_min="$(target_story_alignment_manifest_int "${manifest_file}" '.minimums.container_image_identities')"
	catalog_min="$(target_story_alignment_manifest_int "${manifest_file}" '.minimums.service_catalog_correlations')"
	sbom_min="$(target_story_alignment_manifest_int "${manifest_file}" '.minimums.sbom_attachments')"
	expected_security_repo="$(target_story_alignment_manifest_string "${manifest_file}" '.expected_security_alert_repository')"
	expected_image_repo="$(target_story_alignment_manifest_string "${manifest_file}" '.expected_oci_repository_id')"
	expected_image_ref="$(target_story_alignment_manifest_string "${manifest_file}" '.expected_image_ref')"
	expected_service_id="$(target_story_alignment_manifest_string "${manifest_file}" '.expected_service_id')"
	expected_workload_id="$(target_story_alignment_manifest_string "${manifest_file}" '.expected_workload_id')"
	expected_image_digest="$(target_story_alignment_manifest_string "${manifest_file}" '.expected_image_digest')"
	expected_sbom_digest="$(target_story_alignment_manifest_string "${manifest_file}" '.expected_sbom_subject_digest')"

	if ((security_min > 0)); then
		target_story_require_aligned_target "${manifest_file}" expected_security_alert_repository "${expected_security_repo}" || return 1
	fi
	if ((image_min > 0)); then
		if [[ -n "${expected_image_repo}" ]]; then
			target_story_require_aligned_target "${manifest_file}" expected_oci_repository_id "${expected_image_repo}" || return 1
		elif [[ -n "${expected_image_ref}" ]]; then
			target_story_require_aligned_target "${manifest_file}" expected_image_ref "${expected_image_ref}" || return 1
		fi
	fi
	if ((catalog_min > 0)); then
		target_story_require_aligned_target "${manifest_file}" expected_service_id "${expected_service_id}" || return 1
		target_story_require_aligned_target "${manifest_file}" expected_workload_id "${expected_workload_id}" || return 1
	fi
	if ((sbom_min > 0)) && [[ -n "${expected_image_digest}" && -n "${expected_sbom_digest}" && "${expected_image_digest}" != "${expected_sbom_digest}" ]]; then
		printf 'target story alignment mismatch: expected_sbom_subject_digest does not match expected_image_digest\n' >&2
		return 1
	fi
}
