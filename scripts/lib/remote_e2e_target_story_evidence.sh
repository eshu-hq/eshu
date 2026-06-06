#!/usr/bin/env bash

cicd_image_ref_match_count() {
	local file="$1"
	local expected="$2"
	jq -r --arg expected "${expected}" '
		(.data // .) as $body |
		[
			$body.correlations[]?
			| select((.image_ref // "") == $expected)
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

target_story_documentation_findings_count() {
	local file="$1"
	jq -r '
		(.data // .) as $body |
		[
			$body.findings[]?
			| select((.status // "") != "unsupported")
		] | length
	' "${file}"
}

target_story_incident_context_match_count() {
	local file="$1"
	local expected="$2"
	jq -r --arg expected "${expected}" '
		(.data // .) as $body |
		[
			$body.incident.provider_incident_id?,
			$body.incident.incident_id?,
			$body.incident.id?
		] as $ids |
		if ([ $ids[]? | select(. == $expected) ] | length) == 0 then
			0
		elif ([
			$body.evidence_path[]?
			| select((.slot // "") == "incident")
			| select((.truth_label // "") != "missing_evidence" and (.truth_label // "") != "missing")
		] | length) > 0 then
			1
		elif (($body.incident.evidence_fact_id // "") != "") then
			1
		else
			0
		end
	' "${file}"
}

target_story_work_item_evidence_count() {
	local file="$1"
	jq -r '
		(.data // .) as $body |
		if (($body.missing_evidence // false) == true) then
			0
		else
			[
				$body.evidence[]?
				| select((.evidence_state // "exact_provider_fact") == "exact_provider_fact")
			] | length
		end
	' "${file}"
}

target_story_append_query_param() {
	local path="$1"
	local name="$2"
	local value="$3"
	if [[ -z "${value}" ]]; then
		printf '%s' "${path}"
		return 0
	fi
	if [[ "${path}" == *"?"* ]]; then
		printf '%s&%s=%s' "${path}" "${name}" "$(urlencode "${value}")"
	else
		printf '%s?%s=%s' "${path}" "${name}" "$(urlencode "${value}")"
	fi
}

target_story_check_provider_evidence() {
	local repo_selector="$1"
	local expected_service_id="$2"
	local expected_documentation_scope_id="$3"
	local expected_documentation_source_id="$4"
	local expected_incident_id="$5"
	local expected_incident_provider="$6"
	local expected_incident_scope_id="$7"
	local expected_work_item_key="$8"
	local expected_work_item_external_url="$9"
	local expected_work_item_provider_id="${10}"
	local documentation_min="${11}"
	local incident_min="${12}"
	local work_item_min="${13}"

	local documentation_count=0 incident_count=0 work_item_count=0
	local mcp_documentation_count=0 mcp_incident_count=0 mcp_work_item_count=0
	if ((documentation_min > 0)); then
		local documentation_limit documentation_path documentation_file
		documentation_limit="$(list_limit_for_minimum documentation_findings "${documentation_min}")"
		documentation_path="/documentation/findings?repo=$(urlencode "${repo_selector}")&limit=${documentation_limit}"
		documentation_path="$(target_story_append_query_param "${documentation_path}" scope_id "${expected_documentation_scope_id}")"
		documentation_path="$(target_story_append_query_param "${documentation_path}" source_id "${expected_documentation_source_id}")"
		documentation_file="${TMP_DIR}/documentation-findings.json"
		api_get "${documentation_path}" "${documentation_file}"
		documentation_count="$(target_story_documentation_findings_count "${documentation_file}")"
		require_min_count documentation_findings "${documentation_count}" "${documentation_min}"

		local mcp_documentation_file mcp_documentation_args
		mcp_documentation_file="${TMP_DIR}/mcp-documentation-findings.json"
		mcp_documentation_args="$(jq -n \
			--arg repo "${repo_selector}" \
			--arg scope_id "${expected_documentation_scope_id}" \
			--arg source_id "${expected_documentation_source_id}" \
			--argjson limit "${documentation_limit}" \
			'{repo:$repo, scope_id:$scope_id, source_id:$source_id, limit:$limit} | with_entries(select(.value != ""))')"
		mcp_tool_envelope list_documentation_findings "${mcp_documentation_args}" "${mcp_documentation_file}"
		mcp_documentation_count="$(target_story_documentation_findings_count "${mcp_documentation_file}")"
		require_min_count mcp_documentation_findings "${mcp_documentation_count}" "${documentation_min}"
	fi

	if ((incident_min > 0)); then
		if [[ -z "${expected_incident_id}" ]]; then
			echo "target incident_context requires expected_provider_incident_id" >&2
			return 1
		fi
		local incident_limit incident_path incident_file
		incident_limit="$(list_limit_for_minimum incident_context "${incident_min}")"
		incident_path="/incidents/$(urlencode "${expected_incident_id}")/context"
		incident_path="$(target_story_append_query_param "${incident_path}" provider "${expected_incident_provider}")"
		incident_path="$(target_story_append_query_param "${incident_path}" scope_id "${expected_incident_scope_id}")"
		incident_path="$(target_story_append_query_param "${incident_path}" service_id "${expected_service_id}")"
		incident_path="$(target_story_append_query_param "${incident_path}" limit "${incident_limit}")"
		incident_file="${TMP_DIR}/incident-context.json"
		api_get "${incident_path}" "${incident_file}"
		incident_count="$(target_story_incident_context_match_count "${incident_file}" "${expected_incident_id}")"
		require_min_count incident_context "${incident_count}" "${incident_min}"

		local mcp_incident_file mcp_incident_args
		mcp_incident_file="${TMP_DIR}/mcp-incident-context.json"
		mcp_incident_args="$(jq -n \
			--arg provider_incident_id "${expected_incident_id}" \
			--arg provider "${expected_incident_provider}" \
			--arg scope_id "${expected_incident_scope_id}" \
			--arg service_id "${expected_service_id}" \
			--argjson limit "${incident_limit}" \
			'{provider_incident_id:$provider_incident_id, provider:$provider, scope_id:$scope_id, service_id:$service_id, limit:$limit} | with_entries(select(.value != ""))')"
		mcp_tool_envelope get_incident_context "${mcp_incident_args}" "${mcp_incident_file}"
		mcp_incident_count="$(target_story_incident_context_match_count "${mcp_incident_file}" "${expected_incident_id}")"
		require_min_count mcp_incident_context "${mcp_incident_count}" "${incident_min}"
	fi

	if ((work_item_min > 0)); then
		if [[ -z "${expected_work_item_key}" && -z "${expected_work_item_external_url}" && -z "${expected_work_item_provider_id}" ]]; then
			echo "target work_item_evidence requires expected_work_item_key, expected_work_item_external_url, or expected_work_item_provider_id" >&2
			return 1
		fi
		local work_item_limit work_item_path work_item_file
		work_item_limit="$(list_limit_for_minimum work_item_evidence "${work_item_min}")"
		work_item_path="/work-items/evidence"
		work_item_path="$(target_story_append_query_param "${work_item_path}" work_item_key "${expected_work_item_key}")"
		work_item_path="$(target_story_append_query_param "${work_item_path}" external_url "${expected_work_item_external_url}")"
		work_item_path="$(target_story_append_query_param "${work_item_path}" provider_work_item_id "${expected_work_item_provider_id}")"
		work_item_path="$(target_story_append_query_param "${work_item_path}" limit "${work_item_limit}")"
		work_item_file="${TMP_DIR}/work-item-evidence.json"
		api_get "${work_item_path}" "${work_item_file}"
		work_item_count="$(target_story_work_item_evidence_count "${work_item_file}")"
		require_min_count work_item_evidence "${work_item_count}" "${work_item_min}"

		local mcp_work_item_file mcp_work_item_args
		mcp_work_item_file="${TMP_DIR}/mcp-work-item-evidence.json"
		mcp_work_item_args="$(jq -n \
			--arg work_item_key "${expected_work_item_key}" \
			--arg external_url "${expected_work_item_external_url}" \
			--arg provider_work_item_id "${expected_work_item_provider_id}" \
			--argjson limit "${work_item_limit}" \
			'{work_item_key:$work_item_key, external_url:$external_url, provider_work_item_id:$provider_work_item_id, limit:$limit} | with_entries(select(.value != ""))')"
		mcp_tool_envelope list_work_item_evidence "${mcp_work_item_args}" "${mcp_work_item_file}"
		mcp_work_item_count="$(target_story_work_item_evidence_count "${mcp_work_item_file}")"
		require_min_count mcp_work_item_evidence "${mcp_work_item_count}" "${work_item_min}"
	fi

	printf '%s %s %s %s %s %s\n' \
		"${documentation_count}" "${incident_count}" "${work_item_count}" \
		"${mcp_documentation_count}" "${mcp_incident_count}" "${mcp_work_item_count}"
}
