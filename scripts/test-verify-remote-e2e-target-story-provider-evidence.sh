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

if [[ -z "${curl_config}" ]]; then
  echo "curl call is missing config file" >&2
  exit 2
fi
if [[ "$*" != *"--max-time"* ]]; then
  echo "curl call is missing max-time" >&2
  exit 2
fi

if [[ "$*" == *"/mcp/message"* ]]; then
  if [[ -z "${payload_file}" || ! -f "${payload_file}" ]]; then
    echo "mcp call is missing JSON-RPC payload" >&2
    exit 2
  fi
  tool_name="$(jq -r '.params.name // ""' "${payload_file}")"
  case "${tool_name}" in
    list_documentation_findings)
      cat "${state_dir}/mcp-documentation-findings.json"
      ;;
    get_incident_context)
      cat "${state_dir}/mcp-incident-context.json"
      ;;
    list_work_item_evidence)
      cat "${state_dir}/mcp-work-item-evidence.json"
      ;;
    *)
      echo "unexpected mcp tool: ${tool_name}" >&2
      exit 2
      ;;
  esac
  exit 0
fi

if ! rg -q 'Accept: application/eshu.envelope\+json' "${curl_config}"; then
  echo "curl call is missing Eshu envelope Accept header" >&2
  exit 2
fi

case "$*" in
  *"/api/v0/repositories/repo%3A%2F%2Fexample%2Fapi/story"*)
    cat "${state_dir}/repo-story.json"
    ;;
  *"/api/v0/documentation/findings?repo=repo%3A%2F%2Fexample%2Fapi&limit=1"*)
    cat "${state_dir}/documentation-findings.json"
    ;;
  *"/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pagerduty-prod&service_id=service%3Aapi&limit=1"*)
    cat "${state_dir}/incident-context.json"
    ;;
  *"/api/v0/work-items/evidence?work_item_key=OPS-123&limit=1"*)
    cat "${state_dir}/work-item-evidence.json"
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
  "proof_mode": "partial",
  "proof_mode_reason": "provider evidence target-story proof only",
  "target_repository_id": "repo://example/api",
  "expected_service_id": "service:api",
  "expected_provider_incident_id": "PABC123",
  "expected_incident_provider": "pagerduty",
  "expected_incident_scope_id": "pagerduty-prod",
  "expected_work_item_key": "OPS-123",
  "minimums": {
    "documentation_findings": 1,
    "incident_context": 1,
    "work_item_evidence": 1
  }
}
JSON
}

mcp_envelope() {
  local text="$1"
  jq -n --arg text "${text}" \
    '{jsonrpc:"2.0", id:1, result:{content:[{type:"resource", resource:{uri:"eshu://tool-result/envelope", mimeType:"application/eshu.envelope+json", text:$text}}], isError:false}}'
}

reset_state() {
  rm -f "${state_dir}/curl-targets"
  write_manifest
  cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/documentation-findings.json" <<'JSON'
{"data":{"count":1,"findings":[{"finding_id":"finding:docs:1","repository_id":"repo://example/api","status":"conflict"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/incident-context.json" <<'JSON'
{"data":{"incident":{"provider":"pagerduty","provider_incident_id":"PABC123","evidence_fact_id":"fact-incident"},"evidence_path":[{"slot":"incident","truth_label":"exact_provider_fact","evidence":[{"fact_kind":"incident.record","fact_id":"fact-incident"}]}],"missing_evidence":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/work-item-evidence.json" <<'JSON'
{"data":{"count":1,"evidence":[{"fact_id":"fact-work-item","work_item_key":"OPS-123","evidence_state":"exact_provider_fact"}],"missing_evidence":false,"states":["exact_provider_fact"],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  mcp_envelope '{"data":{"count":1,"findings":[{"finding_id":"finding:docs:1","repository_id":"repo://example/api","status":"conflict"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}' >"${state_dir}/mcp-documentation-findings.json"
  mcp_envelope '{"data":{"incident":{"provider":"pagerduty","provider_incident_id":"PABC123","evidence_fact_id":"fact-incident"},"evidence_path":[{"slot":"incident","truth_label":"exact_provider_fact","evidence":[{"fact_kind":"incident.record","fact_id":"fact-incident"}]}],"missing_evidence":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}' >"${state_dir}/mcp-incident-context.json"
  mcp_envelope '{"data":{"count":1,"evidence":[{"fact_id":"fact-work-item","work_item_key":"OPS-123","evidence_state":"exact_provider_fact"}],"missing_evidence":false,"states":["exact_provider_fact"],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}' >"${state_dir}/mcp-work-item-evidence.json"
}

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
    ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
    ESHU_REMOTE_E2E_API_KEY="test-api-key" \
    "${verifier}" >/tmp/eshu-remote-e2e-target-story-provider.out 2>/tmp/eshu-remote-e2e-target-story-provider.err
}

expect_pass() {
  if ! run_verifier; then
    printf 'expected target-story provider-evidence verifier to pass\n' >&2
    sed -n '1,160p' /tmp/eshu-remote-e2e-target-story-provider.err >&2
    exit 1
  fi
}

expect_fail_with() {
  local pattern="$1"
  if run_verifier; then
    printf 'expected target-story provider-evidence verifier to fail with %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-provider.out >&2
    exit 1
  fi
  if ! rg -q "${pattern}" /tmp/eshu-remote-e2e-target-story-provider.err; then
    printf 'expected failure output to contain %s\n' "${pattern}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-provider.err >&2
    exit 1
  fi
}

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
expect_pass
for expected in \
  'documentation_findings=1' \
  'incident_context=1' \
  'work_item_evidence=1' \
  'mcp_documentation_findings=1' \
  'mcp_incident_context=1' \
  'mcp_work_item_evidence=1'; do
  if ! rg -q "${expected}" /tmp/eshu-remote-e2e-target-story-provider.out; then
    printf 'expected target-story proof output to contain %s\n' "${expected}" >&2
    sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-provider.out >&2
    exit 1
  fi
done
for route in \
  '/api/v0/documentation/findings?repo=repo%3A%2F%2Fexample%2Fapi&limit=1' \
  '/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pagerduty-prod&service_id=service%3Aapi&limit=1' \
  '/api/v0/work-items/evidence?work_item_key=OPS-123&limit=1' \
  '/mcp/message'; do
  if ! rg -F -q "${route}" "${state_dir}/curl-targets"; then
    printf 'expected target-story verifier to call %s\n' "${route}" >&2
    sed -n '1,200p' "${state_dir}/curl-targets" >&2
    exit 1
  fi
done
if rg -q 'repo://example/api|service:api|PABC123|OPS-123' /tmp/eshu-remote-e2e-target-story-provider.out; then
  printf 'target-story provider proof leaked private target values\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-provider.out >&2
  exit 1
fi

reset_state
unset ESHU_REMOTE_E2E_MCP_URL
if ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
  PATH="${fake_bin}:${PATH}" \
  ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
  ESHU_REMOTE_E2E_API_KEY="test-api-key" \
  "${verifier}" >/tmp/eshu-remote-e2e-target-story-provider.out 2>/tmp/eshu-remote-e2e-target-story-provider.err; then
  printf 'expected target-story provider proof to require MCP URL\n' >&2
  exit 1
fi
if ! rg -q 'ESHU_REMOTE_E2E_MCP_URL is required when target story MCP proof is required' /tmp/eshu-remote-e2e-target-story-provider.err; then
  printf 'expected missing MCP URL failure\n' >&2
  sed -n '1,200p' /tmp/eshu-remote-e2e-target-story-provider.err >&2
  exit 1
fi

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/documentation-findings.json" <<'JSON'
{"data":{"count":1,"findings":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target documentation_findings=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/documentation-findings.json" <<'JSON'
{"data":{"count":0,"findings":[],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target documentation_findings=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/incident-context.json" <<'JSON'
{"data":{"incident":{"provider":"pagerduty","provider_incident_id":"PABC123"},"evidence_path":[{"slot":"incident","truth_label":"missing_evidence"}],"missing_evidence":[{"slot":"incident","reason":"unsupported_target_evidence"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target incident_context=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
cat >"${state_dir}/work-item-evidence.json" <<'JSON'
{"data":{"count":0,"evidence":[],"missing_evidence":true,"states":["missing_evidence"],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
expect_fail_with 'target work_item_evidence=0 below required minimum 1'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq 'del(.expected_provider_incident_id)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target incident_context requires expected_provider_incident_id'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq 'del(.expected_work_item_key)' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
expect_fail_with 'target work_item_evidence requires expected_work_item_key, expected_work_item_external_url, or expected_work_item_provider_id'

reset_state
export ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json"
jq '
  .minimums.documentation_findings = 0 |
  .minimums.incident_context = 0 |
  .minimums.work_item_evidence = 0 |
  del(.expected_provider_incident_id) |
  del(.expected_incident_provider) |
  del(.expected_incident_scope_id) |
  del(.expected_work_item_key)
' "${state_dir}/target-story.json" >"${state_dir}/target-story-next.json"
mv "${state_dir}/target-story-next.json" "${state_dir}/target-story.json"
unset ESHU_REMOTE_E2E_MCP_URL
if ! ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
  PATH="${fake_bin}:${PATH}" \
  ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
  ESHU_REMOTE_E2E_API_KEY="test-api-key" \
  "${verifier}" >/tmp/eshu-remote-e2e-target-story-provider.out 2>/tmp/eshu-remote-e2e-target-story-provider.err; then
  printf 'expected disabled provider-evidence target proof to pass without MCP URL\n' >&2
  sed -n '1,160p' /tmp/eshu-remote-e2e-target-story-provider.err >&2
  exit 1
fi

printf 'verify-remote-e2e-target-story provider-evidence tests passed\n'
