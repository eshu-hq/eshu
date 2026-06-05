#!/usr/bin/env bash
set -euo pipefail

TARGET_STORY_FILE="${ESHU_REMOTE_E2E_TARGET_STORY_FILE:-}"
API_BASE_URL="${ESHU_REMOTE_E2E_API_BASE_URL:-}"
API_KEY="${ESHU_REMOTE_E2E_API_KEY:-}"
API_TIMEOUT_SECONDS="${ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS:-30}"
TMP_DIR="$(mktemp -d)"

cleanup() {
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

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

manifest_string() {
	local filter="$1"
	jq -r "${filter} // \"\"" "${TARGET_STORY_FILE}"
}

manifest_int() {
	local filter="$1"
	local default_value="$2"
	jq -r "${filter} // ${default_value}" "${TARGET_STORY_FILE}"
}

json_int() {
	local file="$1"
	local filter="$2"
	jq -r "(.data // .) | ${filter} // 0" "${file}"
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

provider_repository_match_count() {
	local file="$1"
	local expected="$2"
	jq -r --arg expected "${expected}" '
		(.data // .) as $body |
		($expected | ascii_downcase) as $expected_lower |
		def repository_anchor_matches($actual):
			$actual == $expected_lower
			or ($actual | endswith(":" + $expected_lower))
			or ($actual | endswith("/" + $expected_lower));
		def provider_repository_candidates($alert):
			[
				$alert.repository_id?,
				$alert.repository_slug?,
				$alert.repository_name?,
				$alert.repository_full_name?,
				$alert.provider_repository?,
				$alert.provider_repository_id?,
				$alert.provider_repository_name?,
				(($alert.provider_alert_id? // "") | capture("security-alert:[^:]+:(?<repository>[^:]+)")? | .repository)
			]
			| map(select(type == "string" and length > 0) | ascii_downcase);
		[
			$body.reconciliations[]?
			| provider_repository_candidates(.provider_alert // {}) as $anchors
			| select(any($anchors[]; repository_anchor_matches(.)))
		] | length
	' "${file}"
}

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

main() {
	if [[ -z "${TARGET_STORY_FILE}" ]]; then
		echo "remote E2E target story proof skipped: no target story configured"
		return 0
	fi
	if [[ ! -f "${TARGET_STORY_FILE}" ]]; then
		echo "target story file not found" >&2
		return 1
	fi
	require_tool curl
	require_tool jq
	require_positive_integer ESHU_REMOTE_E2E_API_TIMEOUT_SECONDS "${API_TIMEOUT_SECONDS}"
	if [[ -z "${API_BASE_URL}" ]]; then
		echo "ESHU_REMOTE_E2E_API_BASE_URL is required when target story proof is configured" >&2
		return 1
	fi

	local repo_selector expected_security_repo expected_service_id expected_workload_id
	local expected_image_repo expected_image_digest expected_image_ref
	local expected_sbom_digest
	repo_selector="$(manifest_string '.target_repository_id')"
	expected_security_repo="$(manifest_string '.expected_security_alert_repository')"
	expected_service_id="$(manifest_string '.expected_service_id')"
	expected_workload_id="$(manifest_string '.expected_workload_id')"
	expected_image_repo="$(manifest_string '.expected_oci_repository_id')"
	expected_image_digest="$(manifest_string '.expected_image_digest')"
	expected_image_ref="$(manifest_string '.expected_image_ref')"
	expected_sbom_digest="$(manifest_string '.expected_sbom_subject_digest')"
	if [[ -z "${repo_selector}" ]]; then
		echo "target story manifest requires target_repository_id" >&2
		return 1
	fi

	local impact_min security_min image_min sbom_min catalog_min cicd_min
	impact_min="$(manifest_int '.minimums.impact_findings' 0)"
	security_min="$(manifest_int '.minimums.security_alert_reconciliations' 0)"
	image_min="$(manifest_int '.minimums.container_image_identities' 0)"
	sbom_min="$(manifest_int '.minimums.sbom_attachments' 0)"
	catalog_min="$(manifest_int '.minimums.service_catalog_correlations' 0)"
	cicd_min="$(manifest_int '.minimums.ci_cd_run_correlations' 0)"
	require_non_negative_integer minimums.impact_findings "${impact_min}"
	require_non_negative_integer minimums.security_alert_reconciliations "${security_min}"
	require_non_negative_integer minimums.container_image_identities "${image_min}"
	require_non_negative_integer minimums.sbom_attachments "${sbom_min}"
	require_non_negative_integer minimums.service_catalog_correlations "${catalog_min}"
	require_non_negative_integer minimums.ci_cd_run_correlations "${cicd_min}"

	echo "Checking remote E2E target story proof..."
	local repo_file="${TMP_DIR}/repo-story.json"
	check_repository_story "${repo_selector}" "${repo_file}"

	local repo_query
	repo_query="$(urlencode "${repo_selector}")"
	local impact_count=0 security_count=0 image_count=0 sbom_count=0 catalog_count=0 cicd_count=0
	if ((impact_min > 0)); then
		local impact_file="${TMP_DIR}/impact-count.json"
		api_get "/supply-chain/impact/findings/count?repository_id=${repo_query}&profile=comprehensive" "${impact_file}"
		impact_count="$(json_int "${impact_file}" '.total_findings')"
		require_min_count impact_findings "${impact_count}" "${impact_min}"
	fi
	if ((security_min > 0)); then
		if [[ -z "${expected_security_repo}" ]]; then
			echo "target security_alert_reconciliations requires expected_security_alert_repository" >&2
			return 1
		fi
		local security_limit
		security_limit="$(list_limit_for_minimum security_alert_reconciliations "${security_min}")"
		local security_file="${TMP_DIR}/security-alert-count.json"
		api_get "/supply-chain/security-alerts/reconciliations?repository_id=${repo_query}&limit=${security_limit}" "${security_file}"
		security_count="$(provider_repository_match_count "${security_file}" "${expected_security_repo}")"
		require_min_count security_alert_reconciliations "${security_count}" "${security_min}"
	fi
	if ((image_min > 0)); then
		local image_anchor image_param image_file="${TMP_DIR}/container-image-count.json"
		if [[ -n "${expected_image_digest}" ]]; then
			image_anchor="${expected_image_digest}"
			image_param="digest"
		elif [[ -n "${expected_image_ref}" ]]; then
			image_anchor="${expected_image_ref}"
			image_param="image_ref"
		else
			image_anchor=""
			image_param=""
		fi
		if [[ -z "${image_anchor}" ]]; then
			echo "target container_image_identities requires expected_image_digest or expected_image_ref" >&2
			return 1
		fi
		local image_path="/supply-chain/container-images/identities/count?${image_param}=$(urlencode "${image_anchor}")"
		if [[ -n "${expected_image_repo}" ]]; then
			image_path="${image_path}&repository_id=$(urlencode "${expected_image_repo}")"
		fi
		api_get "${image_path}" "${image_file}"
		image_count="$(json_int "${image_file}" '.total_identities')"
		require_min_count container_image_identities "${image_count}" "${image_min}"
	fi
	if ((sbom_min > 0)); then
		if [[ -z "${expected_sbom_digest}" ]]; then
			expected_sbom_digest="${expected_image_digest}"
		fi
		if [[ -z "${expected_sbom_digest}" ]]; then
			echo "target sbom_attachments requires expected_sbom_subject_digest or expected_image_digest" >&2
			return 1
		fi
		local sbom_file="${TMP_DIR}/sbom-count.json"
		api_get "/supply-chain/sbom-attestations/attachments/count?subject_digest=$(urlencode "${expected_sbom_digest}")" "${sbom_file}"
		sbom_count="$(json_int "${sbom_file}" '.total_attachments')"
		require_min_count sbom_attachments "${sbom_count}" "${sbom_min}"
	fi
	if ((catalog_min > 0)); then
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
	fi
	if ((cicd_min > 0)); then
		if [[ -z "${expected_image_digest}" && -z "${expected_image_ref}" ]]; then
			echo "target ci_cd_run_correlations requires expected_image_digest or expected_image_ref" >&2
			return 1
		fi
		local cicd_file="${TMP_DIR}/cicd-count.json"
		if [[ -n "${expected_image_digest}" ]]; then
			api_get "/ci-cd/run-correlations/count?repository_id=${repo_query}&artifact_digest=$(urlencode "${expected_image_digest}")" "${cicd_file}"
			cicd_count="$(json_int "${cicd_file}" '.total_correlations')"
		else
			local cicd_limit
			cicd_limit="$(list_limit_for_minimum ci_cd_run_correlations "${cicd_min}")"
			api_get "/ci-cd/run-correlations?repository_id=${repo_query}&limit=${cicd_limit}" "${cicd_file}"
			cicd_count="$(cicd_image_ref_match_count "${cicd_file}" "${expected_image_ref}")"
		fi
		require_min_count ci_cd_run_correlations "${cicd_count}" "${cicd_min}"
	fi

	printf 'remote E2E target story proof counts: repository_story=1 impact_findings=%s security_alert_reconciliations=%s container_image_identities=%s sbom_attachments=%s service_catalog_correlations=%s ci_cd_run_correlations=%s\n' \
		"${impact_count}" \
		"${security_count}" \
		"${image_count}" \
		"${sbom_count}" \
		"${catalog_count}" \
		"${cicd_count}"
}

main "$@"
