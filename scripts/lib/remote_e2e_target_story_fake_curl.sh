#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_E2E_TEST_STATE:?set ESHU_REMOTE_E2E_TEST_STATE}"
printf '%s\n' "$*" >>"${state_dir}/curl-targets"
if [[ "$*" == *"test-api-key"* ]]; then
	echo "curl arguments leaked API key" >&2
	exit 2
fi
curl_config=""
payload_file=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
	if [[ "${args[$i]}" == "-K" ]]; then
		curl_config="${args[$((i + 1))]:-}"
	fi
	if [[ "${args[$i]}" == "--data-binary" || "${args[$i]}" == "--data" || "${args[$i]}" == "-d" ]]; then
		payload_file="${args[$((i + 1))]:-}"
		payload_file="${payload_file#@}"
	fi
done
is_mcp=0
if [[ "$*" == *"/mcp/message"* ]]; then
	is_mcp=1
fi
if [[ -z "${curl_config}" ]]; then
	echo "curl call is missing config file" >&2
	exit 2
fi
if ((is_mcp == 0)) && ! rg -q 'Accept: application/eshu.envelope\+json' "${curl_config}"; then
	echo "curl call is missing Eshu envelope Accept header" >&2
	exit 2
fi
if [[ "$*" != *"--max-time"* ]]; then
	echo "curl call is missing max-time" >&2
	exit 2
fi
if ((is_mcp == 1)); then
	if [[ -z "${payload_file}" || ! -f "${payload_file}" ]]; then
		echo "mcp call is missing JSON-RPC payload" >&2
		exit 2
	fi
	tool_name="$(jq -r '.params.name // ""' "${payload_file}")"
	printf '%s\n' "${tool_name}" >>"${state_dir}/mcp-tools"
	case "${tool_name}" in
		list_service_catalog_correlations)
			cat "${state_dir}/mcp-service-catalog.json"
			;;
		list_ci_cd_run_correlations)
			expected_digest="$(jq -r '.expected_image_digest // ""' "${state_dir}/target-story.json")"
			expected_ref="$(jq -r '.expected_image_ref // ""' "${state_dir}/target-story.json")"
			if ! jq -e --arg digest "${expected_digest}" --arg ref "${expected_ref}" --arg repo "repo://example/api" '
				.params.arguments.repository_id == $repo
				and
				(if $digest != "" then
					.params.arguments.artifact_digest == $digest
				elif $ref != "" then
					.params.arguments.image_ref == $ref
				else
					((.params.arguments.artifact_digest // "") == "" and (.params.arguments.image_ref // "") == "")
				end)
			' "${payload_file}" >/dev/null; then
				echo "list_ci_cd_run_correlations used the wrong target anchor" >&2
				exit 2
			fi
			cat "${state_dir}/mcp-cicd.json"
			;;
		count_container_image_identities)
			cat "${state_dir}/mcp-image-count.json"
			;;
		count_sbom_attestation_attachments)
			cat "${state_dir}/mcp-sbom-count.json"
			;;
		get_service_story)
			cat "${state_dir}/mcp-service-story.json"
			;;
		find_infra_resources)
			cat "${state_dir}/mcp-cloud-resources.json"
			;;
		*)
			echo "unexpected mcp tool: ${tool_name}" >&2
			exit 2
			;;
	esac
	exit 0
fi
case "$*" in
	*"/api/v0/repositories/repo%3A%2F%2Fexample%2Fapi/story"*)
		cat "${state_dir}/repo-story.json"
		;;
	*"/api/v0/repositories/repository%3Ar_8f14e45f/story"*)
		cat "${state_dir}/repo-story.json"
		;;
	*"/api/v0/services/api/story"*)
		cat "${state_dir}/service-story.json"
		;;
	*"/api/v0/services/"*"/story?repo=repository%3Ar_8f14e45f"*)
		cat "${state_dir}/service-story.json"
		;;
	*"/api/v0/supply-chain/impact/findings/count?repository_id=repo%3A%2F%2Fexample%2Fapi&profile=comprehensive"*)
		cat "${state_dir}/impact-count.json"
		;;
	*"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1"*)
		cat "${state_dir}/security-alert-count.json"
		;;
	*"/api/v0/supply-chain/container-images/identities/count?digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&source_repository_id=repo%3A%2F%2Fexample%2Fapi&repository_id=oci-registry%3A%2F%2Fregistry.example%2Fteam%2Fapi"*)
		cat "${state_dir}/image-count.json"
		;;
	*"/api/v0/supply-chain/container-images/identities/count?image_ref=registry.example.com%2Fteam%2Fapi%3Aprod&source_repository_id=repo%3A%2F%2Fexample%2Fapi&repository_id=oci-registry%3A%2F%2Fregistry.example%2Fteam%2Fapi"*)
		cat "${state_dir}/image-count.json"
		;;
	*"/api/v0/supply-chain/container-images/identities/count?digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&source_repository_id=repo%3A%2F%2Fexample%2Fapi&repository_id=oci-registry%3A%2F%2Fregistry.example%2Fteam%2Fother-api"*)
		cat "${state_dir}/image-count.json"
		;;
	*"/api/v0/supply-chain/container-images/identities/count?digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
		cat "${state_dir}/image-count.json"
		;;
	*"/api/v0/supply-chain/container-images/identities/count?source_repository_id=repo%3A%2F%2Fexample%2Fapi"*)
		cat "${state_dir}/image-count.json"
		;;
	*"/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo%3A%2F%2Fexample%2Fapi&subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
		cat "${state_dir}/sbom-count.json"
		;;
	*"/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repository%3Ar_8f14e45f&subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
		cat "${state_dir}/sbom-count.json"
		;;
	*"/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo%3A%2F%2Fexample%2Fapi"*)
		cat "${state_dir}/sbom-count.json"
		;;
	*"/api/v0/supply-chain/sbom-attestations/attachments/count?subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
		cat "${state_dir}/sbom-count.json"
		;;
	*"/api/v0/service-catalog/correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1&service_id=service%3Aapi"*)
		cat "${state_dir}/service-catalog.json"
		;;
	*"/api/v0/service-catalog/correlations?repository_id=repository%3Ar_8f14e45f&limit=1&workload_id="*)
		cat "${state_dir}/service-catalog.json"
		;;
	*"/api/v0/ci-cd/run-correlations/count?repository_id=repo%3A%2F%2Fexample%2Fapi&artifact_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
		cat "${state_dir}/cicd-count.json"
		;;
	*"/api/v0/ci-cd/run-correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&artifact_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=1"*)
		cat "${state_dir}/cicd-list.json"
		;;
	*"/api/v0/infra/resources/search"*)
		cat "${state_dir}/cloud-resources.json"
		;;
	*"/api/v0/ci-cd/run-correlations/count?repository_id=repo%3A%2F%2Fexample%2Fapi&image_ref=registry.example.com%2Fteam%2Fapi%3Aprod"*)
		cat "${state_dir}/cicd-count.json"
		;;
	*"/api/v0/ci-cd/run-correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&image_ref=registry.example.com%2Fteam%2Fapi%3Aprod&limit=1"*)
		cat "${state_dir}/cicd-list.json"
		;;
	*"/api/v0/ci-cd/run-correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1"*)
		cat "${state_dir}/cicd-list.json"
		;;
	*)
		echo "unexpected curl target: $*" >&2
		exit 2
		;;
esac
