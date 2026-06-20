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
		list_container_image_identities)
			if [[ -f "${state_dir}/mcp-image-list.json" ]]; then
				cat "${state_dir}/mcp-image-list.json"
			else
				source_repo="$(jq -r '.expected_source_repository_id // .target_repository_id // "repo://example/api"' "${state_dir}/target-story.json")"
				image_repo="$(jq -r '.expected_oci_repository_id // "oci-registry://registry.example/team/api"' "${state_dir}/target-story.json")"
				source_revision="$(jq -r '.expected_source_revision // ""' "${state_dir}/target-story.json")"
				jq -n --arg source_repo "${source_repo}" --arg image_repo "${image_repo}" --arg source_revision "${source_revision}" \
					'{jsonrpc:"2.0", id:1, result:{content:[{type:"text", text:"Returned 1 result(s)."},{type:"resource", resource:{uri:"eshu://tool-result/envelope", mimeType:"application/eshu.envelope+json", text:({data:{identities:[{identity_id:"identity-1", digest:"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", image_ref:"registry.example.com/team/api:prod", repository_id:$image_repo, source_repository_ids:[$source_repo], source_revision:$source_revision, outcome:"exact_digest"}], count:1, limit:1, truncated:false}, truth:{level:"exact", freshness:{state:"fresh"}}, error:null} | tostring)}}], isError:false}}'
			fi
			;;
		count_sbom_attestation_attachments)
			cat "${state_dir}/mcp-sbom-count.json"
			;;
		list_sbom_attestation_attachments)
			if [[ -f "${state_dir}/mcp-sbom-list.json" ]]; then
				cat "${state_dir}/mcp-sbom-list.json"
			else
				repo_selector="$(jq -r '.target_repository_id // "repo://example/api"' "${state_dir}/target-story.json")"
				jq -n --arg repo_selector "${repo_selector}" \
					'{jsonrpc:"2.0", id:1, result:{content:[{type:"text", text:"Returned 1 result(s)."},{type:"resource", resource:{uri:"eshu://tool-result/envelope", mimeType:"application/eshu.envelope+json", text:({data:{attachments:[{attachment_id:"sbom-attachment-1", subject_digest:"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", repository_ids:[$repo_selector], attachment_status:"attached_verified"}], count:1, limit:1, truncated:false}, truth:{level:"exact", freshness:{state:"fresh"}}, error:null} | tostring)}}], isError:false}}'
			fi
			;;
		get_service_story)
			cat "${state_dir}/mcp-service-story.json"
			;;
		find_infra_resources)
			cat "${state_dir}/mcp-cloud-resources.json"
			;;
		explain_supply_chain_impact)
			cat "${state_dir}/mcp-impact-explain.json"
			;;
		*)
			echo "unexpected mcp tool: ${tool_name}" >&2
			exit 2
			;;
	esac
	exit 0
fi
case "$*" in
	*"/api/v0/index-status"*)
		cat "${state_dir}/index-status.json"
		;;
	*"/api/v0/supply-chain/impact/explain?cve_id=CVE-2026-0001&package_id=pkg%3Anpm%2Fleft-pad&repository_id=repo%3A%2F%2Fexample%2Fapi"*)
		cat "${state_dir}/impact-explain.json"
		;;
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
	*"/api/v0/supply-chain/container-images/identities?digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&source_repository_id=repo%3A%2F%2Fexample%2Fapi&repository_id=oci-registry%3A%2F%2Fregistry.example%2Fteam%2Fapi&limit=1"*)
		if [[ -f "${state_dir}/image-list.json" ]]; then
			cat "${state_dir}/image-list.json"
		else
			cat <<'JSON'
{"data":{"identities":[{"identity_id":"identity-1","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","image_ref":"registry.example.com/team/api:prod","repository_id":"oci-registry://registry.example/team/api","source_repository_ids":["repo://example/api"],"outcome":"exact_digest"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
		fi
		;;
	*"/api/v0/supply-chain/container-images/identities?image_ref=registry.example.com%2Fteam%2Fapi%3Aprod&source_repository_id=repo%3A%2F%2Fexample%2Fapi&repository_id=oci-registry%3A%2F%2Fregistry.example%2Fteam%2Fapi&limit=1"*)
		if [[ -f "${state_dir}/image-list.json" ]]; then
			cat "${state_dir}/image-list.json"
		else
			cat <<'JSON'
{"data":{"identities":[{"identity_id":"identity-1","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","image_ref":"registry.example.com/team/api:prod","repository_id":"oci-registry://registry.example/team/api","source_repository_ids":["repo://example/api"],"outcome":"exact_digest"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
		fi
		;;
	*"/api/v0/supply-chain/container-images/identities?digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&source_repository_id=repository%3Ar_8f14e45f&limit=1"*)
		cat <<'JSON'
{"data":{"identities":[{"identity_id":"identity-1","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","image_ref":"registry.example.com/team/api:prod","source_repository_ids":["repository:r_8f14e45f"],"outcome":"exact_digest"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
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
	*"/api/v0/supply-chain/sbom-attestations/attachments?repository_id=repo%3A%2F%2Fexample%2Fapi&subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=1"*)
		if [[ -f "${state_dir}/sbom-list.json" ]]; then
			cat "${state_dir}/sbom-list.json"
		else
			cat <<'JSON'
{"data":{"attachments":[{"attachment_id":"sbom-attachment-1","subject_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","repository_ids":["repo://example/api"],"attachment_status":"attached_verified"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
		fi
		;;
	*"/api/v0/supply-chain/sbom-attestations/attachments?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1"*)
		if [[ -f "${state_dir}/sbom-list.json" ]]; then
			cat "${state_dir}/sbom-list.json"
		else
			cat <<'JSON'
{"data":{"attachments":[{"attachment_id":"sbom-attachment-1","subject_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","repository_ids":["repo://example/api"],"attachment_status":"attached_verified"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
		fi
		;;
	*"/api/v0/supply-chain/sbom-attestations/attachments?repository_id=repository%3Ar_8f14e45f&subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=1"*)
		cat <<'JSON'
{"data":{"attachments":[{"attachment_id":"sbom-attachment-1","subject_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","repository_ids":["repository:r_8f14e45f"],"attachment_status":"attached_verified"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
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
