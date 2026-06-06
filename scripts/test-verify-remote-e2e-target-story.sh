#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_target_story.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

cat >"${fake_bin}/curl" <<'SH'
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
  case "${tool_name}" in
    list_service_catalog_correlations)
      cat "${state_dir}/mcp-service-catalog.json"
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
  *"/api/v0/supply-chain/impact/findings/count?repository_id=repo%3A%2F%2Fexample%2Fapi&profile=comprehensive"*)
    cat "${state_dir}/impact-count.json"
    ;;
  *"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1"*)
    cat "${state_dir}/security-alert-count.json"
    ;;
  *"/api/v0/supply-chain/container-images/identities/count?digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&repository_id=oci-registry%3A%2F%2Fregistry.example%2Fteam%2Fapi"*)
    cat "${state_dir}/image-count.json"
    ;;
  *"/api/v0/supply-chain/sbom-attestations/attachments/count?subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
    cat "${state_dir}/sbom-count.json"
    ;;
  *"/api/v0/service-catalog/correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1&service_id=service%3Aapi"*)
    cat "${state_dir}/service-catalog.json"
    ;;
  *"/api/v0/ci-cd/run-correlations/count?repository_id=repo%3A%2F%2Fexample%2Fapi&artifact_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"*)
    cat "${state_dir}/cicd-count.json"
    ;;
  *"/api/v0/infra/resources/search"*)
    cat "${state_dir}/cloud-resources.json"
    ;;
  *)
    echo "unexpected curl target: $*" >&2
    exit 2
    ;;
esac
SH
chmod +x "${fake_bin}/curl"

write_manifest() {
  cat >"${state_dir}/target-story.json" <<'JSON'
{
  "proof_mode": "code_to_cloud",
  "target_repository_id": "repo://example/api",
  "expected_security_alert_repository": "example/api",
  "expected_service_id": "service:api",
  "expected_oci_repository_id": "oci-registry://registry.example/team/api",
  "expected_image_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "expected_sbom_subject_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "expected_cloud_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:example-api",
  "minimums": {
    "impact_findings": 1,
    "security_alert_reconciliations": 1,
    "container_image_identities": 1,
    "sbom_attachments": 1,
    "service_catalog_correlations": 1,
    "ci_cd_run_correlations": 1,
    "cloud_resources": 1
  }
}
JSON
}

reset_state() {
  rm -f "${state_dir}/curl-targets"
  write_manifest
  cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/impact-count.json" <<'JSON'
{"data":{"total_findings":5,"affected_findings":5},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/security-alert-count.json" <<'JSON'
{
  "data": {
    "count": 1,
    "reconciliations": [
      {
        "reconciliation_id": "rec-1",
        "provider_alert": {
          "provider_alert_id": "github_dependabot:security-alert:github:example/api:42",
          "repository_id": "repository:r_example_api"
        }
      }
    ]
  },
  "truth": {"level": "exact", "freshness": {"state": "fresh"}},
  "error": null
}
JSON
  cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/sbom-count.json" <<'JSON'
{"data":{"total_attachments":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":1,"correlations":[{"correlation_id":"corr-1","service_id":"service:api"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/cicd-count.json" <<'JSON'
{"data":{"total_correlations":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/cloud-resources.json" <<'JSON'
{"data":{"count":1,"results":[{"id":"cloud-resource:api","resource_id":"arn:aws:lambda:us-east-1:111122223333:function:example-api","arn":"arn:aws:lambda:us-east-1:111122223333:function:example-api","provider":"aws"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/mcp-service-catalog.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":1,\"correlations\":[{\"correlation_id\":\"corr-1\",\"service_id\":\"service:api\"}],\"truncated\":false},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
  cat >"${state_dir}/mcp-cloud-resources.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":1,\"results\":[{\"id\":\"cloud-resource:api\",\"resource_id\":\"arn:aws:lambda:us-east-1:111122223333:function:example-api\",\"arn\":\"arn:aws:lambda:us-east-1:111122223333:function:example-api\",\"provider\":\"aws\"}],\"truncated\":false},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
}

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
    ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
    ESHU_REMOTE_E2E_API_KEY="test-api-key" \
    "${verifier}" >/tmp/eshu-remote-e2e-target-story.out 2>/tmp/eshu-remote-e2e-target-story.err
}

expect_pass() {
  if ! run_verifier; then
    printf 'expected target-story verifier to pass\n' >&2
    sed -n '1,160p' /tmp/eshu-remote-e2e-target-story.err >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  if run_verifier; then
    printf 'expected target-story verifier to fail with %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
    exit 1
  fi
  if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-target-story.err; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.err >&2
    exit 1
  fi
}

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
expect_pass
if rg -q 'repo://example/api|oci-registry://registry.example/team/api|arn:aws' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'target-story proof leaked private target values\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi

reset_state
jq 'del(.proof_mode)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
expect_pass
if ! rg -q 'proof_mode=code_to_cloud' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected target-story proof mode default in output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi

reset_state
jq '.expected_security_alert_repository = "repository:r_example_api"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
expect_pass

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":0},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target container_image_identities=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq '
  .minimums.container_image_identities = 0 |
  .minimums.sbom_attachments = 0
' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target proof_mode=code_to_cloud requires minimums.container_image_identities >= 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq '
  .proof_mode = "vulnerability_only" |
  .proof_mode_reason = "artifact registry intentionally outside this proof" |
  del(.expected_oci_repository_id) |
  del(.expected_image_digest) |
  del(.expected_sbom_subject_digest) |
  del(.expected_cloud_resource_id) |
  .minimums.container_image_identities = 0 |
  .minimums.sbom_attachments = 0 |
  .minimums.service_catalog_correlations = 0 |
  .minimums.ci_cd_run_correlations = 0 |
  .minimums.cloud_resources = 0
' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass
if ! rg -q 'proof_mode=vulnerability_only' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected target-story proof mode in output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq '.proof_mode = "partial" | del(.proof_mode_reason)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target story proof_mode=partial requires proof_mode_reason'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq '.proof_mode = "artifactish"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target story proof_mode must be one of code_to_cloud, vulnerability_only, partial'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":0,"correlations":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target service_catalog_correlations=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/security-alert-count.json" <<'JSON'
{
  "data": {
    "count": 1,
    "reconciliations": [
      {
        "reconciliation_id": "rec-1",
        "provider_alert": {
          "repository_id": "security-alert:github:example/other"
        }
      }
    ]
  },
  "truth": {"level": "exact", "freshness": {"state": "fresh"}},
  "error": null
}
JSON
expect_fail_with 'target security_alert_reconciliations=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq 'del(.expected_image_digest)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target container_image_identities requires expected_image_digest or expected_image_ref'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq 'del(.expected_security_alert_repository)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target security_alert_reconciliations requires expected_security_alert_repository'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
unset ESHU_REMOTE_E2E_MCP_URL
if ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
  PATH="${fake_bin}:${PATH}" \
  ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
  ESHU_REMOTE_E2E_API_KEY="test-api-key" \
  "${verifier}" >/tmp/eshu-remote-e2e-target-story.out 2>/tmp/eshu-remote-e2e-target-story.err; then
  printf 'expected target-story verifier to require MCP URL when MCP-backed target proof is configured\n' >&2
  exit 1
fi
if ! rg -q 'ESHU_REMOTE_E2E_MCP_URL is required when target story MCP proof is required' /tmp/eshu-remote-e2e-target-story.err; then
  printf 'expected missing MCP URL failure\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.err >&2
  exit 1
fi

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/cloud-resources.json" <<'JSON'
{"data":{"count":1,"results":[{"id":"cloud-resource:other","resource_id":"arn:aws:lambda:us-east-1:111122223333:function:other-api","arn":"arn:aws:lambda:us-east-1:111122223333:function:other-api","provider":"aws"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target cloud_resources=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/mcp-cloud-resources.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":1,\"results\":[{\"id\":\"cloud-resource:other\",\"resource_id\":\"arn:aws:lambda:us-east-1:111122223333:function:other-api\",\"arn\":\"arn:aws:lambda:us-east-1:111122223333:function:other-api\",\"provider\":\"aws\"}],\"truncated\":false},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
expect_fail_with 'target mcp_cloud_resources=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/mcp-service-catalog.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 0 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":0,\"correlations\":[],\"truncated\":false},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
expect_fail_with 'target mcp_service_catalog_correlations=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq 'del(.expected_cloud_resource_id)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target cloud_resources requires expected_cloud_resource_id'

reset_state
rm -f "${state_dir}/target-story.json"
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
expect_fail_with 'target story file not found'

reset_state
unset ESHU_REMOTE_E2E_TARGET_STORY_FILE
expect_pass
if ! rg -q 'remote E2E target story proof skipped: no target story configured' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected no-target skip message\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi

printf 'verify-remote-e2e-target-story tests passed\n'
