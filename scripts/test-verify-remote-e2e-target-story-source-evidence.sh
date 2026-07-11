#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_target_story.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
mkdir -p "${fake_bin}" "${state_dir}"

# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
# the entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cp "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-source-evidence-fake-curl.sh" "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

write_manifest() {
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-source-evidence-target-story.json" >"${state_dir}/target-story.json"
}

reset_state() {
  rm -f "${state_dir}/curl-targets"
  write_manifest
  cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/documentation-findings.json" <<'JSON'
{"data":{"findings":[{"finding_id":"doc-finding-1","repo":"repo://example/api","status":"supported"}],"next_cursor":""},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/incident-context.json" <<'JSON'
{"data":{"query":{"provider":"pagerduty","provider_incident_id":"PABC123","scope_id":"pagerduty-prod","service_id":"service:api","limit":1},"incident":{"provider":"pagerduty","provider_incident_id":"PABC123","scope_id":"pagerduty-prod","service":{"id":"service:api"}},"timeline":[],"related_changes":[],"evidence_path":[],"missing_evidence":[],"ambiguous_evidence":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/work-item-evidence.json" <<'JSON'
{"data":{"evidence":[{"fact_id":"fact-work-1","fact_kind":"work_item.record","scope_id":"jira-prod","work_item_key":"OPS-123","evidence_state":"exact_provider_fact"}],"count":1,"limit":1,"truncated":false,"missing_evidence":false,"states":["exact_provider_fact"]},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/mcp-documentation-findings.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"findings\":[{\"finding_id\":\"doc-finding-1\",\"repo\":\"repo://example/api\",\"status\":\"supported\"}],\"next_cursor\":\"\"},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-source-evidence-mcp-incident-context.json" >"${state_dir}/mcp-incident-context.json"
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-remote-e2e-target-story-source-evidence-mcp-work-item-evidence.json" >"${state_dir}/mcp-work-item-evidence.json"
}

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
    ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
    ESHU_REMOTE_E2E_API_KEY="test-api-key" \
    ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json" \
    "${verifier}" >/tmp/eshu-remote-e2e-target-story-source.out 2>/tmp/eshu-remote-e2e-target-story-source.err
}

expect_pass() {
  if ! run_verifier; then
    printf 'expected target-story source evidence verifier to pass\n' >&2
    sed -n '1,160p' /tmp/eshu-remote-e2e-target-story-source.err >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  if run_verifier; then
    printf 'expected target-story source evidence verifier to fail with %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-source.out >&2
    exit 1
  fi
  if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-target-story-source.err; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-source.err >&2
    exit 1
  fi
}

reset_state
expect_pass
if ! rg -q 'documentation_findings=1 incident_contexts=1 work_item_evidence=1 mcp_documentation_findings=1 mcp_incident_contexts=1 mcp_work_item_evidence=1' /tmp/eshu-remote-e2e-target-story-source.out; then
  printf 'expected source evidence target counts in proof output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-source.out >&2
  exit 1
fi
if rg -q 'repo://example/api|service:api|OPS-123|PABC123|pagerduty-prod' /tmp/eshu-remote-e2e-target-story-source.out; then
  printf 'target-story source evidence proof leaked private target values\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-source.out >&2
  exit 1
fi

reset_state
cat >"${state_dir}/documentation-findings.json" <<'JSON'
{"data":{"findings":[{"finding_id":"doc-finding-other","repo":"repo://example/other"}],"next_cursor":""},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target documentation_findings missing_evidence reason=target_not_linked'

reset_state
cat >"${state_dir}/incident-context.json" <<'JSON'
{"data":{"query":{"provider":"pagerduty","provider_incident_id":"PABC123","scope_id":"pagerduty-prod","service_id":"service:api","limit":1},"incident":{"provider":"pagerduty","provider_incident_id":"PABC123","scope_id":"pagerduty-prod","service":{"id":"service:other"}},"timeline":[],"related_changes":[],"evidence_path":[],"missing_evidence":[],"ambiguous_evidence":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target incident_contexts missing_evidence reason=target_not_linked'

reset_state
cat >"${state_dir}/work-item-evidence.json" <<'JSON'
{"data":{"evidence":[{"fact_id":"fact-work-other","fact_kind":"work_item.record","scope_id":"jira-prod","work_item_key":"OPS-999","evidence_state":"exact_provider_fact"}],"count":1,"limit":1,"truncated":false,"missing_evidence":false,"states":["exact_provider_fact"]},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target work_item_evidence missing_evidence reason=target_not_linked'

reset_state
jq '.minimums.documentation_findings = 0 | .minimums.incident_contexts = 0 | .minimums.work_item_evidence = 0 | del(.expected_provider_incident_id, .expected_work_item_key)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass
if rg -q '/documentation/findings|/incidents/PABC123/context|/work-items/evidence' "${state_dir}/curl-targets"; then
  printf 'disabled source evidence minimums should not call target readbacks\n' >&2
  sed -n '1,200p' "${state_dir}/curl-targets" >&2
  exit 1
fi

reset_state
jq '.minimums.documentation_findings = 0 | .unsupported_target_evidence.documentation_findings = "target_link_not_modeled"' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass
if ! rg -q 'documentation_findings_reason=target_link_not_modeled' /tmp/eshu-remote-e2e-target-story-source.out; then
  printf 'expected unsupported target evidence reason in output\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-source.out >&2
  exit 1
fi

reset_state
jq '
  .minimums.documentation_findings = 0 |
  .minimums.incident_contexts = 0 |
  .minimums.work_item_evidence = 0 |
  .unsupported_target_evidence.documentation_findings = "target_link_not_modeled" |
  .unsupported_target_evidence.incident_contexts = "capability_not_supported" |
  .unsupported_target_evidence.work_item_evidence = "source_not_configured"
' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_pass
for expected in \
  'documentation_findings_reason=target_link_not_modeled' \
  'incident_contexts_reason=capability_not_supported' \
  'work_item_evidence_reason=source_not_configured'; do
  if ! rg -q "${expected}" /tmp/eshu-remote-e2e-target-story-source.out; then
    printf 'expected unsupported target evidence reason %s in output\n' "${expected}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-source.out >&2
    exit 1
  fi
done

printf 'verify-remote-e2e-target-story source evidence tests passed\n'
