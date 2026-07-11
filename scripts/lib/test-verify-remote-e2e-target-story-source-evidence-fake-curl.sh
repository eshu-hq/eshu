#!/usr/bin/env bash
set -euo pipefail

state_dir="${ESHU_REMOTE_E2E_TEST_STATE:?set ESHU_REMOTE_E2E_TEST_STATE}"
printf '%s\n' "$*" >>"${state_dir}/curl-targets"
if [[ "$*" == *"test-api-key"* ]]; then
  echo "curl arguments leaked API key" >&2
  exit 2
fi

payload_file=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  if [[ "${args[$i]}" == "--data-binary" || "${args[$i]}" == "--data" || "${args[$i]}" == "-d" ]]; then
    payload_file="${args[$((i + 1))]:-}"
    payload_file="${payload_file#@}"
  fi
done

if [[ "$*" == *"/mcp/message"* ]]; then
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
  *"/api/v0/work-items/evidence?scope_id=jira-prod&work_item_key=OPS-123&limit=1"*)
    cat "${state_dir}/work-item-evidence.json"
    ;;
  *)
    echo "unexpected curl target: $*" >&2
    exit 2
    ;;
esac
