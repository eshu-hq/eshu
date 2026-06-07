#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_target_story.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

cp "${repo_root}/scripts/lib/remote_e2e_target_story_fake_curl.sh" "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

cat >"${state_dir}/target-story.json" <<'JSON'
{
  "proof_mode": "code_to_cloud",
  "target_repository_id": "repo://example/api",
  "expected_source_repository_id": "repo://example/api",
  "expected_service_id": "service:api",
  "expected_oci_repository_id": "oci-registry://registry.example/team/api",
  "expected_image_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "expected_source_revision": "0123456789abcdef0123456789abcdef01234567",
  "minimums": {
    "impact_findings": 0,
    "security_alert_reconciliations": 0,
    "container_image_identities": 1,
    "sbom_attachments": 1,
    "service_catalog_correlations": 0,
    "ci_cd_run_correlations": 0,
    "cloud_resources": 0
  }
}
JSON

cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

cat >"${state_dir}/image-list.json" <<'JSON'
{"data":{"identities":[{"identity_id":"identity-1","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","image_ref":"registry.example.com/team/api:prod","repository_id":"oci-registry://registry.example/team/api","source_repository_ids":["repo://example/api"],"source_revision":"0123456789abcdef0123456789abcdef01234567","outcome":"exact_digest"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

cat >"${state_dir}/sbom-count.json" <<'JSON'
{"data":{"total_attachments":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

cat >"${state_dir}/sbom-list.json" <<'JSON'
{"data":{"attachments":[{"attachment_id":"sbom-attachment-1","subject_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","repository_ids":["repo://example/api"],"attachment_status":"attached_verified"}],"count":1,"limit":1,"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

cat >"${state_dir}/service-story.json" <<'JSON'
{"data":{"code_to_runtime_trace":{"segments":[{"name":"image_package","status":"exact","basis":"container_image_identity_and_sbom_attachment","evidence":[{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","sbom_attachment_id":"sbom-attachment-1","sbom_attachment_status":"attached_verified"}]}]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON

cat >"${state_dir}/mcp-service-story.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned service story."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"code_to_runtime_trace\":{\"segments\":[{\"name\":\"image_package\",\"status\":\"exact\",\"basis\":\"container_image_identity_and_sbom_attachment\",\"evidence\":[{\"digest\":\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"sbom_attachment_id\":\"sbom-attachment-1\",\"sbom_attachment_status\":\"attached_verified\"}]}]}},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON

cat >"${state_dir}/mcp-image-list.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"identities\":[{\"identity_id\":\"identity-1\",\"digest\":\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"image_ref\":\"registry.example.com/team/api:prod\",\"repository_id\":\"oci-registry://registry.example/team/api\",\"source_repository_ids\":[\"repo://example/api\"],\"source_revision\":\"0123456789abcdef0123456789abcdef01234567\",\"outcome\":\"exact_digest\"}],\"count\":1,\"limit\":1,\"truncated\":false},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON

cat >"${state_dir}/mcp-sbom-list.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"attachments\":[{\"attachment_id\":\"sbom-attachment-1\",\"subject_digest\":\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"repository_ids\":[\"repo://example/api\"],\"attachment_status\":\"attached_verified\"}],\"count\":1,\"limit\":1,\"truncated\":false},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON

ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
  PATH="${fake_bin}:${PATH}" \
  ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json" \
  ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
  ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
  ESHU_REMOTE_E2E_API_KEY="test-api-key" \
  "${verifier}" >/tmp/eshu-remote-e2e-target-story-artifacts.out 2>/tmp/eshu-remote-e2e-target-story-artifacts.err

rg -F -q '/api/v0/supply-chain/container-images/identities?digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&source_repository_id=repo%3A%2F%2Fexample%2Fapi&repository_id=oci-registry%3A%2F%2Fregistry.example%2Fteam%2Fapi&limit=1' "${state_dir}/curl-targets"
rg -F -q '/api/v0/supply-chain/sbom-attestations/attachments?repository_id=repo%3A%2F%2Fexample%2Fapi&subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=1' "${state_dir}/curl-targets"
rg -x -q 'list_container_image_identities' "${state_dir}/mcp-tools"
rg -x -q 'list_sbom_attestation_attachments' "${state_dir}/mcp-tools"

if rg -q 'repo://example/api|oci-registry://registry.example/team/api|0123456789abcdef|test-api-key' /tmp/eshu-remote-e2e-target-story-artifacts.out; then
  printf 'artifact anchor proof leaked private target values\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-artifacts.out >&2
  exit 1
fi

jq '.data.identities[0].source_revision = "ffffffffffffffffffffffffffffffffffffffff"' "${state_dir}/image-list.json" >"${state_dir}/image-list-next.json"
mv "${state_dir}/image-list-next.json" "${state_dir}/image-list.json"
if ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
  PATH="${fake_bin}:${PATH}" \
  ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json" \
  ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
  ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
  ESHU_REMOTE_E2E_API_KEY="test-api-key" \
  "${verifier}" >/tmp/eshu-remote-e2e-target-story-artifacts.out 2>/tmp/eshu-remote-e2e-target-story-artifacts.err; then
  printf 'expected artifact anchor proof to reject mismatched source revision\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-artifacts.out >&2
  exit 1
fi
if ! rg -q 'target container_image_identities=0 below required minimum 1' /tmp/eshu-remote-e2e-target-story-artifacts.err; then
  printf 'expected source revision mismatch failure\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-artifacts.err >&2
  exit 1
fi

printf 'verify-remote-e2e-target-story artifact anchor tests passed\n'
