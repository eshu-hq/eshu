# MCP Reference

The MCP server exposes read-only tools over stdio and HTTP/SSE. Tool dispatch
routes into the same `internal/query` HTTP handlers used by the API, so MCP and
HTTP share authorization passthrough, truth envelopes, pagination, and backend
behavior.

The code source of truth is `go/internal/mcp`: `ReadOnlyTools` registers tool
definitions, `resolveRoute` maps tool names to HTTP routes, and dispatch tests
prove every registered tool has a route.

For per-tool bounds, required fields, envelopes, and prompt-readiness notes, use
[MCP Tool Contract Matrix](mcp-tool-contract-matrix.md). This page is the public
tool index, not a second copy of every schema.

## Call Rules

- Prefer canonical IDs at the tool boundary when the tool supports them.
- Use repository selectors only as compatibility aliases; portable file answers
  should use `repo_id + relative_path`.
- Use story, investigation, or search tools first, then hydrate exact evidence
  with content or citation tools.
- Treat `execute_cypher_query` as diagnostics-only. Prefer named tools for user
  prompts.
- Inspect the embedded `application/eshu.envelope+json` resource when a tool
  returns one; human text is only a summary.

## Tool Families

`go/internal/mcp/README.md` records the current registry count. The public tool
families are:

| Family | Tools |
| --- | --- |
| Code search and analysis | `find_code`, `find_symbol`, `inspect_code_inventory`, `investigate_import_dependencies`, `inspect_call_graph_metrics`, `investigate_code_topic`, `investigate_hardcoded_secrets`, `get_code_relationship_story`, `analyze_code_relationships`, `find_dead_code`, `investigate_dead_code`, `find_cross_repo_dead_code`, `calculate_cyclomatic_complexity`, `find_most_complex_functions`, `inspect_code_quality`, `execute_language_query`, `find_function_call_chain` |
| IaC and cloud management | `find_dead_iac`, `find_unmanaged_resources`, `get_iac_management_status`, `explain_iac_management_status`, `propose_terraform_import_plan`, `compose_replatforming_plan`, `list_aws_runtime_drift_findings`, `list_cloud_runtime_drift_findings`, `get_replatforming_rollups`, `find_unmanaged_resource_owners` |
| Infrastructure and impact | `get_ecosystem_overview`, `trace_deployment_chain`, `investigate_deployment_config`, `find_blast_radius`, `find_infra_resources`, `list_cloud_resource_inventory`, `investigate_resource`, `analyze_infra_relationships`, `trace_resource_to_code`, `explain_dependency_path`, `find_change_surface`, `investigate_change_surface`, `analyze_pre_change_impact`, `plan_developer_change`, `compare_environments` |
| Repository and relationship drilldowns | `get_repo_summary`, `get_repo_context`, `get_repo_story`, `get_repository_coverage`, `count_repositories_by_language`, `list_repositories_by_language`, `get_repository_language_inventory`, `get_relationship_evidence`, `search_registry_bundles` |
| Context and stories | `resolve_entity`, `get_entity_context`, `get_workload_context`, `get_workload_story`, `get_service_context`, `get_service_story`, `investigate_service`, `get_incident_context`, `list_work_item_evidence` |
| Content and citations | `get_file_content`, `get_file_lines`, `get_entity_content`, `build_evidence_citation_packet`, `search_file_content`, `search_entity_content` |
| Visualization packets | `derive_visualization_packet` |
| Package registry | `list_package_registry_packages`, `list_package_registry_versions`, `list_package_registry_dependencies`, `list_package_registry_correlations` |
| CI/CD and supply chain | `list_ci_cd_run_correlations`, `list_advisory_evidence`, `list_supply_chain_impact_findings`, `explain_supply_chain_impact`, `get_vulnerability_scanner_read_contract`, `list_security_alert_reconciliations`, `list_sbom_attestation_attachments` |
| Secrets and IAM posture | `list_secrets_iam_secret_access_paths`, `list_secrets_iam_identity_trust_chains`, `list_secrets_iam_posture_gaps`, `list_secrets_iam_privilege_posture_observations`, `count_secrets_iam_posture` |
| Documentation truth | `list_documentation_findings`, `list_documentation_facts`, `get_documentation_evidence_packet`, `check_documentation_evidence_packet_freshness` |
| Guided investigation workflows | `list_investigation_workflows`, `resolve_investigation_workflow` |
| Semantic evidence and retrieval | `list_semantic_documentation_observations`, `list_semantic_code_hints`, `search_semantic_context` |
| Runtime status | `list_collectors`, `list_ingesters`, `get_ingester_status`, `get_index_status`, `get_semantic_capability_status`, `get_answer_narration_status`, `get_capability_catalog`, `list_component_extensions`, `get_component_extension_diagnostics` |
| Diagnostics | `execute_cypher_query`, `visualize_graph_query` |

## Content And Repository Identity

Repository-bearing results may include `repo_access` metadata. Remote runtimes
should treat repository identity as remote-first: use `repo_id`, `repo_slug`,
and repo-relative paths before assuming a server-local `local_path` is useful
to the caller.

Content tools use these portable handles:

- file lookup: `repo_id + relative_path`
- entity lookup: `entity_id`
- evidence hydration: file or entity handles returned by story, investigation,
  search, or drilldown tools

Repository context and relationship-evidence tools preserve HTTP correlation
confidence metadata in `structuredContent`. Relationship rows may include
`confidence_basis` (`evidence_constant`, `evidence_aggregate`, or
`assertion_override`) with `resolution_source`, `evidence_type`, and
`evidence_kinds`; code relationship tools still use `resolution_method`. The
relationship-story tools preserve the HTTP row-level `provenance` block in
`structuredContent`, including confidence state, method/source family, reason,
truth state, and derived/heuristic/unsupported flags for every returned
relationship row.
Relationship tools reserve snake_case `min_confidence` for the HTTP/MCP
confidence-floor contract. Tool schemas must advertise support before callers
depend on it. Omitted means no floor; values are numbers from `0` through `1`
and filter returned rows only, without changing canonical graph truth or
evidence drilldowns.

Deployed MCP/API runtimes use the PostgreSQL content store for content reads and
search. Local helper flows may report workspace or graph-cache fallbacks when
those are the answering backend.

## MCP Result Shape

MCP responses include a short text summary for humans and `structuredContent`
for clients that need evidence. When the underlying HTTP route returns the
canonical Eshu envelope, `structuredContent` contains that envelope and MCP also
returns a resource content block:

```json
{
  "structuredContent": {
    "data": {},
    "truth": {},
    "error": null
  },
  "content": [
    {
      "type": "text",
      "text": "Eshu query completed."
    },
    {
      "type": "resource",
      "resource": {
        "uri": "eshu://tool-result/envelope",
        "mimeType": "application/eshu.envelope+json",
        "text": "{\"data\":{},\"truth\":{},\"error\":null}"
      }
    }
  ]
}
```

When an HTTP route still returns plain JSON instead of the canonical envelope,
MCP preserves the plain JSON payload in `structuredContent` and in an
`application/json` resource:

```json
{
  "structuredContent": {
    "count": 1,
    "results": []
  },
  "content": [
    {
      "type": "text",
      "text": "Returned 1 result(s)."
    },
    {
      "type": "resource",
      "resource": {
        "uri": "eshu://tool-result/payload",
        "mimeType": "application/json",
        "text": "{\"count\":1,\"results\":[]}"
      }
    }
  ]
}
```

For prompt automation, read `structuredContent` first. Use the resource block
when the client wants the exact serialized payload. The text block is only a
summary and should not be treated as the evidence-bearing response.

### Text summaries are a convenience layer, not the canonical contract

For story, investigation, citation, and status/readiness tools, the text block
is a tool-aware, deterministic, bounded summary (for example: service identity
plus truth level, freshness, API-surface size, dependency and consumer counts,
and the top limitation for `get_service_story`; resolved-vs-requested coverage
for `build_evidence_citation_packet`; missing and ambiguous evidence counts for
`get_incident_context`; node and edge counts for `derive_visualization_packet`;
health state plus the leading reason for the status tools). These summaries
surface truth, freshness, and partial/error details so a rich or degraded result
never collapses into generic success text.

The text summary is still only a convenience for human readers. It is derived
from the same envelope, is length-capped, and is never the canonical contract.
The `structuredContent` and the embedded resource block remain byte-identical to
the canonical envelope the handler produced; only the text string changes.
Clients MUST read `structuredContent` (or the resource block) for evidence and
MUST NOT parse the text summary.

Citation handles use the same
[Evidence Citation Handle Contract](evidence-citation-handles.md) across HTTP
and MCP. The current runtime hydrates file and entity handles; expanded handle
kinds must preserve API/MCP parity before they are advertised as hydrated.

## Related Docs

- [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md)
- [MCP Guide](../guides/mcp-guide.md)
- [HTTP API Reference](http-api.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [Vulnerability Scanner Read Contract](vulnerability-scanner-read-contract.md)
- [Hardcoded Secrets Investigation](hardcoded-secrets-investigation.md)
- [Supply-Chain Traceability](../supply-chain-traceability.md)
