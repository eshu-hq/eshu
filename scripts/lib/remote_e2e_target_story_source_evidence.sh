#!/usr/bin/env bash
# Target-scoped documentation, incident, and work-item proof helpers.

target_story_reason_class() {
	case "$1" in
		collector_disabled | source_not_configured | capability_not_supported | target_link_not_modeled)
			return 0
			;;
		*)
			return 1
			;;
	esac
}

target_story_unsupported_reason() {
	local label="$1"
	local minimum="$2"
	local reason
	reason="$(manifest_string ".unsupported_target_evidence.${label}")"
	if [[ -z "${reason}" ]]; then
		return 0
	fi
	if ((minimum > 0)); then
		echo "target ${label} unsupported_target_evidence requires minimums.${label} = 0" >&2
		return 1
	fi
	if ! target_story_reason_class "${reason}"; then
		echo "target ${label} unsupported_target_evidence reason must be a sanitized reason class" >&2
		return 1
	fi
	printf '%s' "${reason}"
}

target_story_reason_segment() {
	local label="$1"
	local reason="$2"
	if [[ -n "${reason}" ]]; then
		printf ' %s_reason=%s' "${label}" "${reason}"
	fi
}

require_target_min_count() {
	local label="$1"
	local value="$2"
	local minimum="$3"
	if ((value < minimum)); then
		echo "target ${label} missing_evidence reason=target_not_linked" >&2
		return 1
	fi
}

documentation_finding_match_count() {
	local file="$1"
	local expected_repo="$2"
	jq -r --arg expected "${expected_repo}" '
		(.data // .) as $body |
		[
			$body.findings[]?
			| [
				.repo?,
				.repo_id?,
				.repository?,
				.repository_id?,
				.target_repository_id?,
				.source_repository_id?
			]
			| map(select(type == "string" and . == $expected))
			| select(length > 0)
		] | length
	' "${file}"
}

incident_context_match_count() {
	local file="$1"
	local expected_provider="$2"
	local expected_incident_id="$3"
	local expected_scope="$4"
	local expected_service="$5"
	jq -r \
		--arg provider "${expected_provider}" \
		--arg incident "${expected_incident_id}" \
		--arg scope "${expected_scope}" \
		--arg service "${expected_service}" '
		(.data // .) as $body |
		def matches($actual; $expected): ($expected == "" or $actual == $expected);
		def edge_service_matches:
			any($body.evidence_path[]?; .slot == "service" and ((.value.service_id // "") == $service));
		if
			matches(($body.incident.provider // $body.query.provider // ""); $provider) and
			(($body.incident.provider_incident_id // $body.query.provider_incident_id // "") == $incident) and
			matches(($body.incident.scope_id // $body.query.scope_id // ""); $scope) and
			(
				$service == "" or
				(($body.incident.service.id // "") == $service) or
				edge_service_matches
			)
		then 1 else 0 end
	' "${file}"
}

work_item_evidence_match_count() {
	local file="$1"
	local expected_scope="$2"
	local expected_key="$3"
	local expected_provider_id="$4"
	local expected_url_fingerprint="$5"
	jq -r \
		--arg scope "${expected_scope}" \
		--arg key "${expected_key}" \
		--arg provider_id "${expected_provider_id}" \
		--arg fingerprint "${expected_url_fingerprint}" '
		(.data // .) as $body |
		($key | ascii_upcase) as $expected_key |
		def matches($actual; $expected): ($expected == "" or $actual == $expected);
		[
			$body.evidence[]?
			| select(matches((.scope_id // ""); $scope))
			| select($expected_key == "" or ((.work_item_key // "") | ascii_upcase) == $expected_key)
			| select(matches((.provider_work_item_id // ""); $provider_id))
			| select(matches((.url_fingerprint // ""); $fingerprint))
		] | length
	' "${file}"
}

require_incident_target_anchors() {
	local incident_id="$1"
	local service_id="$2"
	if [[ -z "${incident_id}" || -z "${service_id}" ]]; then
		echo "target incident_contexts requires expected_provider_incident_id and expected_incident_service_id or expected_service_id" >&2
		return 1
	fi
}

require_work_item_target_anchor() {
	local key="$1"
	local provider_id="$2"
	local fingerprint="$3"
	if [[ -z "${key}" && -z "${provider_id}" && -z "${fingerprint}" ]]; then
		echo "target work_item_evidence requires expected_work_item_key, expected_work_item_provider_id, or expected_work_item_url_fingerprint" >&2
		return 1
	fi
}
