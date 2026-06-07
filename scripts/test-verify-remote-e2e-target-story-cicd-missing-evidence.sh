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

write_manifest() {
  cat >"${state_dir}/target-story.json" <<'JSON'
{
  "proof_mode": "partial",
  "proof_mode_reason": "live CI provider evidence intentionally absent from this representative proof",
  "target_repository_id": "repo://example/api",
  "minimums": {
    "impact_findings": 0,
    "security_alert_reconciliations": 0,
    "container_image_identities": 0,
    "sbom_attachments": 0,
    "service_catalog_correlations": 0,
    "ci_cd_run_correlations": 0,
    "cloud_resources": 0
  },
  "expected_ci_cd_missing_evidence": [
    "ci_run_to_image_artifact_evidence_missing",
    "source_to_ci_run_evidence_missing"
  ]
}
JSON
}

write_repo_story() {
  cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
}

write_cicd_api_missing() {
  cat >"${state_dir}/cicd-list.json" <<'JSON'
{"data":{"count":0,"correlations":[],"limit":1,"truncated":false,"evidence_summary":{"static_workflow_artifacts":{"state":"present","count":1,"paths":[".github/workflows/deploy.yml"]},"live_run_correlations":{"state":"missing","count":0,"reason":"workflow_image_ref_static_only"},"run_artifact_evidence":{"state":"missing","count":0,"artifact_digest_count":0,"image_ref_count":0,"ambiguous_count":0,"reason":"workflow_image_ref_static_only"},"missing_evidence":["ci_run_to_image_artifact_evidence_missing","source_to_ci_run_evidence_missing"],"reason":"workflow_image_ref_static_only"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
}

write_cicd_mcp_missing() {
  cat >"${state_dir}/mcp-cicd.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 0 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":0,\"correlations\":[],\"limit\":1,\"truncated\":false,\"evidence_summary\":{\"static_workflow_artifacts\":{\"state\":\"present\",\"count\":1,\"paths\":[\".github/workflows/deploy.yml\"]},\"live_run_correlations\":{\"state\":\"missing\",\"count\":0,\"reason\":\"workflow_image_ref_static_only\"},\"run_artifact_evidence\":{\"state\":\"missing\",\"count\":0,\"artifact_digest_count\":0,\"image_ref_count\":0,\"ambiguous_count\":0,\"reason\":\"workflow_image_ref_static_only\"},\"missing_evidence\":[\"ci_run_to_image_artifact_evidence_missing\",\"source_to_ci_run_evidence_missing\"],\"reason\":\"workflow_image_ref_static_only\"}},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
}

reset_state() {
  rm -f "${state_dir}/curl-targets" "${state_dir}/mcp-tools"
  write_manifest
  export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
  write_repo_story
  write_cicd_api_missing
  write_cicd_mcp_missing
}

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
    ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
    ESHU_REMOTE_E2E_API_KEY="test-api-key" \
    "${verifier}" >/tmp/eshu-remote-e2e-target-story-cicd.out 2>/tmp/eshu-remote-e2e-target-story-cicd.err
}

expect_pass() {
  if ! run_verifier; then
    printf 'expected CI/CD missing-evidence verifier to pass\n' >&2
    sed -n '1,160p' /tmp/eshu-remote-e2e-target-story-cicd.err >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  if run_verifier; then
    printf 'expected CI/CD missing-evidence verifier to fail with %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-cicd.out >&2
    exit 1
  fi
  if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-target-story-cicd.err; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-cicd.err >&2
    exit 1
  fi
}

reset_state
expect_pass
if ! rg -q 'ci_cd_missing_evidence=ci_run_to_image_artifact_evidence_missing,source_to_ci_run_evidence_missing .*mcp_ci_cd_missing_evidence=ci_run_to_image_artifact_evidence_missing,source_to_ci_run_evidence_missing' /tmp/eshu-remote-e2e-target-story-cicd.out; then
  printf 'expected target-story proof to report API and MCP CI/CD missing evidence classes\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-cicd.out >&2
  exit 1
fi
rg -F -q '/api/v0/ci-cd/run-correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&limit=1' "${state_dir}/curl-targets"
rg -x -q 'list_ci_cd_run_correlations' "${state_dir}/mcp-tools"
if rg -q 'repo://example/api|test-api-key' /tmp/eshu-remote-e2e-target-story-cicd.out; then
  printf 'target-story CI/CD missing-evidence proof leaked private target values\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-cicd.out >&2
  exit 1
fi

reset_state
cat >"${state_dir}/cicd-list.json" <<'JSON'
{"data":{"count":0,"correlations":[],"limit":1,"truncated":false,"evidence_summary":{"missing_evidence":["ci_run_to_image_artifact_evidence_missing"]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target ci_cd_missing_evidence missing required class source_to_ci_run_evidence_missing'

reset_state
cat >"${state_dir}/mcp-cicd.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 0 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":0,\"correlations\":[],\"limit\":1,\"truncated\":false,\"evidence_summary\":{\"missing_evidence\":[\"source_to_ci_run_evidence_missing\"]}},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
expect_fail_with 'target mcp_ci_cd_missing_evidence missing required class ci_run_to_image_artifact_evidence_missing'

printf 'verify-remote-e2e-target-story CI/CD missing-evidence tests passed\n'
