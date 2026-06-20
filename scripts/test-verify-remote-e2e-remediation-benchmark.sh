#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-remote-e2e-remediation-benchmark.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

fake_bin="${tmp_root}/bin"
state_dir="${tmp_root}/state"
artifacts_dir="${tmp_root}/artifacts"
mkdir -p "${fake_bin}" "${state_dir}" "${artifacts_dir}"

cp "${repo_root}/scripts/lib/remote_e2e_target_story_fake_curl.sh" "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

write_manifest() {
  cat >"${state_dir}/target-story.json" <<'JSON'
{
  "proof_mode": "code_to_cloud",
  "target_repository_id": "repo://example/api",
  "expected_security_alert_repository": "example/api",
  "expected_source_repository_id": "repo://example/api",
  "expected_service_id": "service:api",
  "expected_oci_repository_id": "oci-registry://registry.example/team/api",
  "expected_image_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "expected_sbom_subject_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "expected_cloud_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:example-api",
  "remediation_benchmark": {
    "cve_id": "CVE-2026-0001",
    "package_id": "pkg:npm/left-pad"
  },
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
  rm -f "${state_dir}/curl-targets" "${state_dir}/mcp-tools"
  write_manifest
  cat >"${state_dir}/repo-story.json" <<'JSON'
{"data":{"repository":{"id":"repo://example/api","name":"api"}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/impact-count.json" <<'JSON'
{"data":{"total_findings":5,"affected_findings":5},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/security-alert-count.json" <<'JSON'
{"data":{"count":1,"reconciliations":[{"provider_alert":{"provider_repository":"example/api"},"reconciliation_status":"matched"}]},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/image-count.json" <<'JSON'
{"data":{"total_identities":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/sbom-count.json" <<'JSON'
{"data":{"total_attachments":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/service-catalog.json" <<'JSON'
{"data":{"count":1,"correlations":[{"correlation_id":"corr-1","service_id":"service:api"}],"truncated":false,"evidence_summary":{"local_descriptors":{"state":"present","count":1},"external_catalog_confirmation":{"state":"present","count":1,"reason":"catalog_match"}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/service-story.json" <<'JSON'
{"data":{"code_to_runtime_trace":{"segments":[{"name":"image_package","status":"exact","basis":"container_image_identity_and_sbom_attachment","evidence":[{"image_ref":"registry.example.com/team/api:prod","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","sbom_attachment_id":"sbom-attachment-1","sbom_attachment_status":"attached_verified"}]}]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/cicd-count.json" <<'JSON'
{"data":{"total_correlations":1},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/cicd-list.json" <<'JSON'
{"data":{"count":1,"correlations":[{"correlation_id":"cicd-1","repository_id":"repo://example/api","artifact_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","image_ref":"registry.example.com/team/api:prod","provider":"github_actions","run_id":"run-1","outcome":"exact","provenance_only":false,"canonical_writes":1}],"limit":1,"truncated":false,"evidence_summary":{"static_workflow_artifacts":{"state":"present","count":1},"live_run_correlations":{"state":"present","count":1}}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/cloud-resources.json" <<'JSON'
{"data":{"count":1,"results":[{"id":"cloud-resource:api","resource_id":"arn:aws:lambda:us-east-1:111122223333:function:example-api","arn":"arn:aws:lambda:us-east-1:111122223333:function:example-api","provider":"aws"}],"truncated":false},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/mcp-image-count.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"total_identities\":1},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
  cat >"${state_dir}/mcp-sbom-count.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"total_attachments\":1},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
  cat >"${state_dir}/mcp-service-catalog.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":1,\"correlations\":[{\"correlation_id\":\"corr-1\",\"service_id\":\"service:api\"}],\"truncated\":false,\"evidence_summary\":{\"local_descriptors\":{\"state\":\"present\",\"count\":1},\"external_catalog_confirmation\":{\"state\":\"present\",\"count\":1,\"reason\":\"catalog_match\"}}},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
  cat >"${state_dir}/mcp-service-story.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned service story."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"code_to_runtime_trace\":{\"segments\":[{\"name\":\"image_package\",\"status\":\"exact\",\"basis\":\"container_image_identity_and_sbom_attachment\",\"evidence\":[{\"image_ref\":\"registry.example.com/team/api:prod\",\"digest\":\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"sbom_attachment_id\":\"sbom-attachment-1\",\"sbom_attachment_status\":\"attached_verified\"}]}]}},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
  cat >"${state_dir}/mcp-cicd.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":1,\"correlations\":[{\"correlation_id\":\"cicd-1\",\"repository_id\":\"repo://example/api\",\"artifact_digest\":\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"image_ref\":\"registry.example.com/team/api:prod\",\"provider\":\"github_actions\",\"run_id\":\"run-1\",\"outcome\":\"exact\",\"provenance_only\":false,\"canonical_writes\":1}],\"limit\":1,\"truncated\":false,\"evidence_summary\":{\"static_workflow_artifacts\":{\"state\":\"present\",\"count\":1},\"live_run_correlations\":{\"state\":\"present\",\"count\":1}}},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
  cat >"${state_dir}/mcp-cloud-resources.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned 1 result(s)."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"count\":1,\"results\":[{\"id\":\"cloud-resource:api\",\"resource_id\":\"arn:aws:lambda:us-east-1:111122223333:function:example-api\",\"arn\":\"arn:aws:lambda:us-east-1:111122223333:function:example-api\",\"provider\":\"aws\"}],\"truncated\":false},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
  cat >"${state_dir}/index-status.json" <<'JSON'
{"status":"healthy","queue":{"outstanding":0,"pending":0,"in_flight":0,"retrying":0,"failed":0,"dead_letter":0},"coordinator":{"run_status_counts":[{"name":"complete","count":4}],"completeness_counts":[]},"fact_counts":{"fact_records":42,"supply_chain":7},"graph_writes":{"total":9,"relationship_edges":4}}
JSON
  cat >"${state_dir}/impact-explain.json" <<'JSON'
{"data":{"finding":{"finding_id":"finding-1","impact_status":"affected_exact"},"package":{"ecosystem":"npm"},"readiness":{"state":"ready","missing_evidence":[]},"remediation_packet":{"owner":{"state":"known"},"actions":[{"kind":"upgrade","state":"available"}]}},"truth":{"level":"exact","freshness":{"state":"fresh"}},"error":null}
JSON
  cat >"${state_dir}/mcp-impact-explain.json" <<'JSON'
{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Returned remediation packet."},{"type":"resource","resource":{"uri":"eshu://tool-result/envelope","mimeType":"application/eshu.envelope+json","text":"{\"data\":{\"finding\":{\"finding_id\":\"finding-1\",\"impact_status\":\"affected_exact\"},\"package\":{\"ecosystem\":\"npm\"},\"readiness\":{\"state\":\"ready\",\"missing_evidence\":[]},\"remediation_packet\":{\"owner\":{\"state\":\"known\"},\"actions\":[{\"kind\":\"upgrade\",\"state\":\"available\"}]}},\"truth\":{\"level\":\"exact\",\"freshness\":{\"state\":\"fresh\"}},\"error\":null}"}}],"isError":false}}
JSON
}

run_verifier() {
  ESHU_REMOTE_E2E_TEST_STATE="${state_dir}" \
    PATH="${fake_bin}:${PATH}" \
    ESHU_REMOTE_E2E_TARGET_STORY_FILE="${state_dir}/target-story.json" \
    ESHU_REMOTE_E2E_API_BASE_URL="http://127.0.0.1:18080/api/v0" \
    ESHU_REMOTE_E2E_MCP_URL="http://127.0.0.1:18081/mcp/message" \
    ESHU_REMOTE_E2E_API_KEY="test-api-key" \
    "${verifier}" --artifacts "${artifacts_dir}" \
      >/tmp/eshu-remediation-benchmark.out \
      2>/tmp/eshu-remediation-benchmark.err
}

reset_state
if ! run_verifier; then
  printf 'expected remediation benchmark verifier to pass\n' >&2
  sed -n '1,200p' /tmp/eshu-remediation-benchmark.err >&2
  exit 1
fi

summary="${artifacts_dir}/summary.json"
markdown="${artifacts_dir}/summary.md"
transcript="${artifacts_dir}/command-transcript.txt"

for artifact in "${summary}" "${markdown}" "${transcript}"; do
  [[ -s "${artifact}" ]] || {
    printf 'expected artifact to be written: %s\n' "${artifact}" >&2
    exit 1
  }
done

jq -e '
  .schema_version == "eshu.remediation_benchmark.v1" and
  .issue_refs == [3174,3178,3129,3061] and
  (.provenance.commit_sha | test("^[0-9a-f]{40}$")) and
  .provenance.image_digest == "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" and
  (.timing.wall_time_seconds >= 0) and
  .target.cve_id == "CVE-2026-0001" and
  .target.package_anchor_recorded == true and
  .target.repository_anchor_recorded == true and
  .queue.queue_terminal_ok == true and
  .fact_counts.state == "reported" and
  .fact_counts.values.fact_records == 42 and
  .graph_writes.state == "reported" and
  .graph_writes.values.total == 9 and
  .parity.api_mcp_parity == "pass" and
  .missing_evidence.state == "ready" and
  .missing_evidence.count == 0 and
  .redaction.private_locator_scan == "passed"
' "${summary}" >/dev/null

rg -F -q 'verify_remote_e2e_target_story.sh' "${transcript}"
rg -F -q 'GET /supply-chain/impact/explain' "${transcript}"
rg -F -q 'MCP explain_supply_chain_impact' "${transcript}"
rg -F -q 'remote remediation benchmark verified' /tmp/eshu-remediation-benchmark.out
rg -F -q '/api/v0/supply-chain/impact/explain?cve_id=CVE-2026-0001&package_id=pkg%3Anpm%2Fleft-pad&repository_id=repo%3A%2F%2Fexample%2Fapi' "${state_dir}/curl-targets"
rg -x -q 'explain_supply_chain_impact' "${state_dir}/mcp-tools"

if rg -q 'repo://example/api|pkg:npm/left-pad|registry.example|arn:aws|test-api-key|http://127.0.0.1|/Users/' \
  "${summary}" "${markdown}" "${transcript}" /tmp/eshu-remediation-benchmark.out; then
  printf 'remediation benchmark artifacts leaked private target values\n' >&2
  sed -n '1,200p' "${summary}" >&2
  exit 1
fi

reset_state
jq 'del(.queue.pending)' "${state_dir}/index-status.json" >"${state_dir}/index-status-new.json"
mv "${state_dir}/index-status-new.json" "${state_dir}/index-status.json"
if run_verifier; then
  printf 'expected remediation benchmark verifier to fail when queue counters are missing\n' >&2
  exit 1
fi
rg -F -q 'remote remediation benchmark queue fields must be present and numeric' /tmp/eshu-remediation-benchmark.err

reset_state
unsafe_counters="${state_dir}/unsafe-counters.json"
cat >"${unsafe_counters}" <<'JSON'
{
  "fact_counts": {
    "repo://example/api": 42
  },
  "graph_writes": {
    "total": 9
  }
}
JSON
if ESHU_REMOTE_E2E_BENCHMARK_COUNTERS_FILE="${unsafe_counters}" run_verifier; then
  printf 'expected remediation benchmark verifier to fail when counter keys contain private-shaped values\n' >&2
  exit 1
fi
rg -F -q 'remote remediation benchmark counter keys look private' /tmp/eshu-remediation-benchmark.err

printf 'remote remediation benchmark verifier test passed\n'
