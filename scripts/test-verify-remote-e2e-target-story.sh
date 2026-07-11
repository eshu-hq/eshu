#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_target_story.sh"
# shellcheck source=scripts/lib/remote_e2e_target_story_alignment.sh
source "${repo_root}/scripts/lib/remote_e2e_target_story_alignment.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

cp "${repo_root}/scripts/lib/remote_e2e_target_story_fake_curl.sh" "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

write_manifest() {
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-target-story.json" >"${state_dir}/target-story.json"
}

reset_state() {
  rm -f "${state_dir}/curl-targets" "${state_dir}/mcp-tools"
  write_manifest
  export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
  cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/impact-count.json" <<'JSON'
{"data":{"total_findings":5,"affected_findings":5},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-security-alert-count.json" >"${state_dir}/security-alert-count.json"
  cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/sbom-count.json" <<'JSON'
{"data":{"total_attachments":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":1,"correlations":[{"correlation_id":"corr-1","service_id":"service:api"}],"truncated":false,"evidence_summary":{"local_descriptors":{"state":"present","count":1,"providers":["backstage"],"source_uris":["file://repo/catalog-info.yaml"]},"external_catalog_confirmation":{"state":"present","count":1,"reason":"catalog_match"}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/service-story.json" <<'JSON'
{"data":{"code_to_runtime_trace":{"segments":[{"name":"image_package","status":"exact","basis":"container_image_identity_and_sbom_attachment","evidence":[{"image_ref":"registry.example.com/team/api:prod","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","sbom_attachment_id":"sbom-attachment-1","sbom_attachment_status":"attached_verified"}]}]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/cicd-count.json" <<'JSON'
{"data":{"total_correlations":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-cicd-list.json" >"${state_dir}/cicd-list.json"
  cat >"${state_dir}/cloud-resources.json" <<'JSON'
{"data":{"count":1,"results":[{"id":"cloud-resource:api","resource_id":"arn:aws:lambda:us-east-1:111122223333:function:example-api","arn":"arn:aws:lambda:us-east-1:111122223333:function:example-api","provider":"aws"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-mcp-service-catalog.json" >"${state_dir}/mcp-service-catalog.json"
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-mcp-cicd.json" >"${state_dir}/mcp-cicd.json"
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-mcp-service-story.json" >"${state_dir}/mcp-service-story.json"
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-mcp-cloud-resources.json" >"${state_dir}/mcp-cloud-resources.json"
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

expect_alignment_match() {
  local left="$1"
  local right="$2"
  if ! target_story_alignment_matches "${left}" "${right}"; then
    printf 'expected target-story tokens to align: %s vs %s\n' "${left}" "${right}" >&2
    exit 1
  fi
}

expect_alignment_mismatch() {
  local left="$1"
  local right="$2"
  if target_story_alignment_matches "${left}" "${right}"; then
    printf 'expected target-story tokens to mismatch: %s vs %s\n' "${left}" "${right}" >&2
    exit 1
  fi
}

expect_alignment_match 'repo://example/api' 'git@github.com:example/api.git'
expect_alignment_match 'repo://example/api' 'https://user@github.com/example/api.git'
expect_alignment_match 'registry.example.com/team/api' 'registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
expect_alignment_mismatch 'repo://example/api' 'git@github.com:example/other-api.git'

reset_state
expect_pass
rg -F -q '/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo%3A%2F%2Fexample%2Fapi&subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "${state_dir}/curl-targets"
if rg -F -q '/api/v0/supply-chain/sbom-attestations/attachments/count?subject_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "${state_dir}/curl-targets"; then
  printf 'target-story verifier must prove SBOM attachments from the repository selector\n' >&2
  sed -n '1,200p' "${state_dir}/curl-targets" >&2
  exit 1
fi
rg -F -q '/api/v0/ci-cd/run-correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&artifact_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=1' "${state_dir}/curl-targets"
rg -x -q 'list_ci_cd_run_correlations' "${state_dir}/mcp-tools"
if rg -q 'repo://example/api|oci-registry://registry.example/team/api|arn:aws' /tmp/eshu-remote-e2e-target-story.out; then
	printf 'target-story proof leaked private target values\n' >&2
	sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
	exit 1
fi
if ! rg -q 'service_catalog_local_descriptors=present service_catalog_external_confirmation=present' /tmp/eshu-remote-e2e-target-story.out; then
	printf 'expected target-story proof to report service-catalog evidence states\n' >&2
	sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
	exit 1
fi
if ! rg -F -q '/api/v0/ci-cd/run-correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&artifact_digest=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=1' "${state_dir}/curl-targets"; then
  printf 'expected target-story verifier to list CI/CD rows by artifact digest\n' >&2
  sed -n '1,200p' "${state_dir}/curl-targets" >&2
  exit 1
fi
if ! rg -q 'ci_cd_static_workflow_state=present' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected target-story proof to report CI/CD static workflow state\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi
if ! rg -q 'mcp_ci_cd_live_run_state=present' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected target-story proof to report MCP CI/CD live run state\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi
if ! rg -q 'service_catalog_external_confirmation_reason=catalog_match .*mcp_service_catalog_external_confirmation_reason=catalog_match' /tmp/eshu-remote-e2e-target-story.out; then
	printf 'expected target-story proof to report API and MCP service-catalog evidence reasons\n' >&2
	sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
	exit 1
fi
if rg -F -q '/api/v0/services/api/story?service_id=service%3Aapi' "${state_dir}/curl-targets"; then
  printf 'target-story verifier must not pass catalog service_id as service-story service_id\n' >&2
  sed -n '1,200p' "${state_dir}/curl-targets" >&2
  exit 1
fi
if ! rg -F -q '/api/v0/services/api/story?repo=repo%3A%2F%2Fexample%2Fapi' "${state_dir}/curl-targets"; then
  printf 'expected target-story verifier to call service story by service path and repo selector\n' >&2
  sed -n '1,200p' "${state_dir}/curl-targets" >&2
  exit 1
fi

reset_state
jq '.expected_oci_repository_id = "oci-registry://registry.example/team/other-api"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target story alignment mismatch: expected_oci_repository_id does not align with target_repository_id'

reset_state
jq '.expected_source_repository_id = "repo://example/other-api"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target story alignment mismatch: expected_source_repository_id does not align with target_repository_id'

reset_state
cat >"${state_dir}/cicd-list.json" <<'JSON'
{"data":{"count":1,"correlations":[{"correlation_id":"cicd-other","repository_id":"repo://example/other","artifact_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","image_ref":"registry.example.com/team/api:prod"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target ci_cd_run_correlations=0 below required minimum 1'

reset_state
# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-mcp-cicd-other.json" >"${state_dir}/mcp-cicd.json"
expect_fail_with 'target mcp_ci_cd_run_correlations=0 below required minimum 1'

reset_state
jq -n '{alerts:[{provider_alert_number:42, ecosystem:"npm", package_name:"left-pad", manifest_path:"package-lock.json", vulnerable_range:"<1.2.3", fixed_version:"1.2.3", reconciliation_status:"matched", requires_evidence:true}]}' >"${state_dir}/expected-security-alerts.json"
jq --arg expected_rows "${state_dir}/expected-security-alerts.json" '.expected_security_alert_rows_file = $expected_rows' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass
if ! rg -q 'security_alert_expected_rows=1' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected target-story proof to report expected security-alert rows\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi

reset_state
jq -n '{alerts:[{provider_alert_number:42, ecosystem:"npm", package_name:"axios", manifest_path:"package-lock.json", vulnerable_range:"<1.2.3", fixed_version:"1.2.3", reconciliation_status:"matched", impact_status:"affected_exact", requires_evidence:true}]}' >"${state_dir}/expected-security-alerts.json"
jq --arg expected_rows "${state_dir}/expected-security-alerts.json" '.expected_security_alert_rows_file = $expected_rows' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target security_alert_expected_rows missing_count=0 mismatch_count=1 evidence_gap_count=0'

reset_state
jq -n '{alerts:[{provider_alert_number:42, ecosystem:"npm", package_name:"left-pad", manifest_path:"package-lock.json", vulnerable_range:"<1.2.3", fixed_version:"1.2.3", reconciliation_status:"matched", requires_evidence:true}]}' >"${state_dir}/expected-security-alerts.json"
jq 'del(.data.reconciliations[0].evidence_fact_ids)' "${state_dir}/security-alert-count.json" >"${state_dir}/security-alert-count-next.json"
mv "${state_dir}/security-alert-count-next.json" "${state_dir}/security-alert-count.json"
jq --arg expected_rows "${state_dir}/expected-security-alerts.json" '.expected_security_alert_rows_file = $expected_rows' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target security_alert_expected_rows missing_count=0 mismatch_count=0 evidence_gap_count=1'

reset_state
jq -n '{alerts:[{provider_alert_number:42, ecosystem:"npm", package_name:"left-pad", manifest_path:"package-lock.json", vulnerable_range:"<1.2.3", fixed_version:"1.2.3", reconciliation_status:"matched", requires_evidence:true}]}' >"${state_dir}/expected-security-alerts.json"
jq 'del(.data.reconciliations[0].evidence_fact_ids) | .data.reconciliations[0].reason = "provider-only alert has no Eshu impact yet"' "${state_dir}/security-alert-count.json" >"${state_dir}/security-alert-count-next.json"
mv "${state_dir}/security-alert-count-next.json" "${state_dir}/security-alert-count.json"
jq --arg expected_rows "${state_dir}/expected-security-alerts.json" '.expected_security_alert_rows_file = $expected_rows' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass

reset_state
jq -n '{alerts:[{provider_alert_number:42, ecosystem:"npm", package_name:"left-pad", manifest_path:"package-lock.json", vulnerable_range:"<1.2.3", fixed_version:"1.2.3", installed_version:"1.2.0"}]}' >"${state_dir}/expected-security-alerts.json"
jq --arg expected_rows "${state_dir}/expected-security-alerts.json" '.expected_security_alert_rows_file = $expected_rows' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass

reset_state
jq -n '{alerts:[{provider_alert_number:42, ecosystem:"npm", package_name:"left-pad", manifest_path:"package-lock.json", vulnerable_range:"<1.2.3", fixed_version:"1.2.3", installed_version:120}]}' >"${state_dir}/expected-security-alerts.json"
jq --arg expected_rows "${state_dir}/expected-security-alerts.json" '.expected_security_alert_rows_file = $expected_rows' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target security_alert_expected_rows version fields must be strings'

reset_state
jq -n '{alerts:[{provider_alert_number:42, ecosystem:"npm", package_name:"left-pad", manifest_path:"package-lock.json", vulnerable_range:"<1.2.3", fixed_version:"1.2.3", observed_version:"1.2.0"}]}' >"${state_dir}/expected-security-alerts.json"
jq 'del(.data.reconciliations[0].eshu_package) | .data.reconciliations[0].observed_version = "1.2.0"' "${state_dir}/security-alert-count.json" >"${state_dir}/security-alert-count-next.json"
mv "${state_dir}/security-alert-count-next.json" "${state_dir}/security-alert-count.json"
jq --arg expected_rows "${state_dir}/expected-security-alerts.json" '.expected_security_alert_rows_file = $expected_rows' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target security_alert_expected_rows missing_count=0 mismatch_count=1 evidence_gap_count=0'

reset_state
jq -n '{alerts:[{provider_alert_number:42, ecosystem:"npm", package_name:"left-pad", manifest_path:"package-lock.json", vulnerable_range:"<1.2.3", fixed_version:"1.2.3", observed_version:"1.2.0"}]}' >"${state_dir}/expected-security-alerts.json"
jq '.data.reconciliations[0].eshu_package.observed_version = "1.1.0"' "${state_dir}/security-alert-count.json" >"${state_dir}/security-alert-count-next.json"
mv "${state_dir}/security-alert-count-next.json" "${state_dir}/security-alert-count.json"
jq --arg expected_rows "${state_dir}/expected-security-alerts.json" '.expected_security_alert_rows_file = $expected_rows' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target security_alert_expected_rows missing_count=0 mismatch_count=1 evidence_gap_count=0'

reset_state
jq 'del(.proof_mode)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass
if ! rg -q 'proof_mode=code_to_cloud' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected target-story proof mode default in output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi

reset_state
jq '
  del(.expected_image_digest) |
  del(.expected_sbom_subject_digest) |
  .expected_image_ref = "registry.example.com/team/api:prod"
' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass
if ! rg -F -q '/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo%3A%2F%2Fexample%2Fapi' "${state_dir}/curl-targets"; then
  printf 'expected target-story verifier to count SBOM attachments by repository selector without requiring a digest\n' >&2
  sed -n '1,200p' "${state_dir}/curl-targets" >&2
  exit 1
fi
if ! rg -F -q '/api/v0/ci-cd/run-correlations/count?repository_id=repo%3A%2F%2Fexample%2Fapi&image_ref=registry.example.com%2Fteam%2Fapi%3Aprod' "${state_dir}/curl-targets"; then
  printf 'expected target-story verifier to count CI/CD rows by image_ref\n' >&2
  sed -n '1,200p' "${state_dir}/curl-targets" >&2
  exit 1
fi
if ! rg -F -q '/api/v0/ci-cd/run-correlations?repository_id=repo%3A%2F%2Fexample%2Fapi&image_ref=registry.example.com%2Fteam%2Fapi%3Aprod&limit=1' "${state_dir}/curl-targets"; then
  printf 'expected target-story verifier to list CI/CD rows by image_ref\n' >&2
  sed -n '1,200p' "${state_dir}/curl-targets" >&2
  exit 1
fi

reset_state
jq '.expected_security_alert_repository = "repository:r_example_api"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass

reset_state
cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":0},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target container_image_identities=0 below required minimum 1'

reset_state
cat >"${state_dir}/service-story.json" <<'JSON'
{"data":{"code_to_runtime_trace":{"segments":[{"name":"image_package","status":"missing_evidence","basis":"container_image_identity_and_sbom_attachment","missing_evidence":["sbom_attachment_missing"],"evidence":[]}]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target service_story_image_package=0 below required minimum 1'

reset_state
# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-mcp-service-story-missing-evidence.json" >"${state_dir}/mcp-service-story.json"
expect_fail_with 'target mcp_service_story_image_package=0 below required minimum 1'

reset_state
jq '
  .minimums.container_image_identities = 0 |
  .minimums.sbom_attachments = 0
' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target proof_mode=code_to_cloud requires minimums.container_image_identities >= 1'

reset_state
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
jq '.proof_mode = "partial" | del(.proof_mode_reason)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target story proof_mode=partial requires proof_mode_reason'

reset_state
jq '.proof_mode = "artifactish"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target story proof_mode must be one of code_to_cloud, vulnerability_only, partial'

reset_state
cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":0,"correlations":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target service_catalog_correlations=0 below required minimum 1'

reset_state
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
jq 'del(.expected_image_digest)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target container_image_identities requires expected_image_digest or expected_image_ref'

reset_state
jq 'del(.expected_security_alert_repository)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target security_alert_reconciliations requires expected_security_alert_repository'

reset_state
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
cat >"${state_dir}/cloud-resources.json" <<'JSON'
{"data":{"count":1,"results":[{"id":"cloud-resource:other","resource_id":"arn:aws:lambda:us-east-1:111122223333:function:other-api","arn":"arn:aws:lambda:us-east-1:111122223333:function:other-api","provider":"aws"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target cloud_resources=0 below required minimum 1'

reset_state
# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-mcp-cloud-resources-other.json" >"${state_dir}/mcp-cloud-resources.json"
expect_fail_with 'target mcp_cloud_resources=0 below required minimum 1'

reset_state
cat >"${state_dir}/mcp-service-catalog.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 0 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":0,\"correlations\":[],\"truncated\":false},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
expect_fail_with 'target mcp_service_catalog_correlations=0 below required minimum 1'

reset_state
jq 'del(.expected_cloud_resource_id)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target cloud_resources requires expected_cloud_resource_id'

reset_state
rm -f "${state_dir}/target-story.json"
expect_fail_with 'target story file not found'

reset_state
unset ESHU_REMOTE_E2E_TARGET_STORY_FILE
expect_pass
if ! rg -q 'remote E2E target story proof skipped: no target story configured' /tmp/eshu-remote-e2e-target-story.out; then
  printf 'expected no-target skip message\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story.out >&2
  exit 1
fi
"${repo_root}/scripts/test-verify-remote-e2e-target-story-runtime-missing-evidence.sh"
"${repo_root}/scripts/test-verify-remote-e2e-target-story-artifact-anchors.sh"
"${repo_root}/scripts/test-verify-remote-e2e-target-story-source-evidence.sh"
"${repo_root}/scripts/test-verify-remote-e2e-target-story-canonical-ids.sh"
"${repo_root}/scripts/test-verify-remote-e2e-target-story-cicd-missing-evidence.sh"
printf 'verify-remote-e2e-target-story tests passed\n'
