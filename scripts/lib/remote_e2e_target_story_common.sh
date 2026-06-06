#!/usr/bin/env bash
# Shared helpers for public-safe target-story proof.

require_tool() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required tool: $1" >&2
		exit 1
	}
}

require_positive_integer() {
	local name="$1"
	local value="$2"
	if [[ ! "${value}" =~ ^[0-9]+$ ]] || ((value <= 0)); then
		echo "${name} must be a positive integer, got ${value}" >&2
		return 1
	fi
}

require_non_negative_integer() {
	local name="$1"
	local value="$2"
	if [[ ! "${value}" =~ ^[0-9]+$ ]]; then
		echo "${name} must be a non-negative integer, got ${value}" >&2
		return 1
	fi
}

urlencode() {
	jq -rn --arg value "$1" '$value|@uri'
}

api_get() {
	local path="$1"
	local output_file="$2"
	local -a curl_args=(-fsS)
	require_positive_integer ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${API_TIMEOUT_SECONDS}"
	local curl_config="${TMP_DIR}/curl.conf"
	printf 'header = "Accept: application/eshu.envelope+json"\n' >"${curl_config}"
	if [[ -n "${API_KEY}" ]]; then
		local escaped_api_key="${API_KEY//\\/\\\\}"
		escaped_api_key="${escaped_api_key//\"/\\\"}"
		printf 'header = "Authorization: Bearer %s"\n' "${escaped_api_key}" >>"${curl_config}"
	fi
	chmod 600 "${curl_config}"
	curl_args+=(-K "${curl_config}")
	curl_args+=(--max-time "${API_TIMEOUT_SECONDS}")
	curl_args+=("${API_BASE_URL}${path}")
	curl "${curl_args[@]}" >"${output_file}"
	if ! jq -e 'has("data") and has("truth") and (.error == null)' "${output_file}" >/dev/null; then
		echo "target story API response missing Eshu truth envelope" >&2
		return 1
	fi
}

api_post_json() {
	local path="$1"
	local body_file="$2"
	local output_file="$3"
	local -a curl_args=(-fsS)
	require_positive_integer ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${API_TIMEOUT_SECONDS}"
	local curl_config="${TMP_DIR}/curl-post.conf"
	printf 'header = "Accept: application/eshu.envelope+json"\n' >"${curl_config}"
	printf 'header = "Content-Type: application/json"\n' >>"${curl_config}"
	if [[ -n "${API_KEY}" ]]; then
		local escaped_api_key="${API_KEY//\\/\\\\}"
		escaped_api_key="${escaped_api_key//\"/\\\"}"
		printf 'header = "Authorization: Bearer %s"\n' "${escaped_api_key}" >>"${curl_config}"
	fi
	chmod 600 "${curl_config}"
	curl_args+=(-K "${curl_config}")
	curl_args+=(--max-time "${API_TIMEOUT_SECONDS}")
	curl_args+=(--data-binary "@${body_file}")
	curl_args+=("${API_BASE_URL}${path}")
	curl "${curl_args[@]}" >"${output_file}"
	if ! jq -e 'has("data") and has("truth") and (.error == null)' "${output_file}" >/dev/null; then
		echo "target story API response missing Eshu truth envelope" >&2
		return 1
	fi
}

mcp_tool_envelope() {
	local tool_name="$1"
	local args_json="$2"
	local output_file="$3"
	local response_file="${TMP_DIR}/mcp-${tool_name}.json"
	local payload_file="${TMP_DIR}/mcp-${tool_name}-payload.json"
	local curl_config="${TMP_DIR}/mcp-curl.conf"
	jq -n --arg name "${tool_name}" --argjson arguments "${args_json}" \
		'{jsonrpc:"2.0", id:1, method:"tools/call", params:{name:$name, arguments:$arguments}}' >"${payload_file}"
	printf 'header = "Content-Type: application/json"\n' >"${curl_config}"
	if [[ -n "${MCP_TOKEN}" ]]; then
		local escaped_token="${MCP_TOKEN//\\/\\\\}"
		escaped_token="${escaped_token//\"/\\\"}"
		printf 'header = "Authorization: Bearer %s"\n' "${escaped_token}" >>"${curl_config}"
	fi
	chmod 600 "${curl_config}"
	curl -fsS -K "${curl_config}" --max-time "${API_TIMEOUT_SECONDS}" --data-binary "@${payload_file}" "${MCP_URL}" >"${response_file}"
	if ! jq -e '(.error == null) and ((.result.isError // false) | not)' "${response_file}" >/dev/null; then
		echo "target story MCP tool ${tool_name} failed" >&2
		return 1
	fi
	local envelope_text
	envelope_text="$(jq -r 'first(.result.content[]? | select(.type == "resource" and .resource.uri == "eshu://tool-result/envelope") | .resource.text) // ""' "${response_file}")"
	if [[ -z "${envelope_text}" ]]; then
		echo "target story MCP tool ${tool_name} response missing Eshu envelope resource" >&2
		return 1
	fi
	printf '%s' "${envelope_text}" >"${output_file}"
	if ! jq -e 'has("data") and has("truth") and (.error == null)' "${output_file}" >/dev/null; then
		echo "target story MCP tool ${tool_name} envelope is invalid" >&2
		return 1
	fi
}

manifest_string() {
	local filter="$1"
	jq -r "${filter} // \"\"" "${TARGET_STORY_FILE}"
}

manifest_int() {
	local filter="$1"
	local default_value="$2"
	jq -r "${filter} // ${default_value}" "${TARGET_STORY_FILE}"
}

target_story_proof_mode() {
	local mode
	mode="$(manifest_string '.proof_mode')"
	if [[ -z "${mode}" ]]; then
		mode="code_to_cloud"
	fi
	case "${mode}" in
		code_to_cloud | vulnerability_only | partial)
			printf '%s' "${mode}"
			;;
		*)
			echo "target story proof_mode must be one of code_to_cloud, vulnerability_only, partial" >&2
			return 1
			;;
	esac
}

validate_target_story_proof_mode() {
	local mode="$1"
	local image_min="$2"
	local sbom_min="$3"
	local reason
	case "${mode}" in
		code_to_cloud)
			if ((image_min < 1)); then
				echo "target proof_mode=code_to_cloud requires minimums.container_image_identities >= 1" >&2
				return 1
			fi
			if ((sbom_min < 1)); then
				echo "target proof_mode=code_to_cloud requires minimums.sbom_attachments >= 1" >&2
				return 1
			fi
			;;
		vulnerability_only | partial)
			reason="$(manifest_string '.proof_mode_reason')"
			if [[ -z "${reason}" ]]; then
				echo "target story proof_mode=${mode} requires proof_mode_reason" >&2
				return 1
			fi
			;;
	esac
}

json_int() {
	local file="$1"
	local filter="$2"
	jq -r "(.data // .) | ${filter} // 0" "${file}"
}

json_string() {
	local file="$1"
	local filter="$2"
	jq -r "(.data // .) | ${filter} // \"\"" "${file}"
}

require_min_count() {
	local label="$1"
	local value="$2"
	local minimum="$3"
	if ((value < minimum)); then
		echo "target ${label}=${value} below required minimum ${minimum}" >&2
		return 1
	fi
}

check_repository_story() {
	local selector="$1"
	local output_file="$2"
	api_get "/repositories/$(urlencode "${selector}")/story" "${output_file}"
	if ! jq -e --arg selector "${selector}" '
		(.data // .) |
		[
			.repository.id?,
			.repository.name?,
			.repository.local_path?,
			.data.repository.id?,
			.data.repository.name?,
			.data.repository.local_path?
		] | map(select(. == $selector)) | length > 0
	' "${output_file}" >/dev/null; then
		echo "target repository_story=0 below required minimum 1" >&2
		return 1
	fi
}

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
