# MCP Tool Contract Matrix

This matrix is the prompt-readiness contract for the current `ReadOnlyTools`
set. "Prompt-ready" means the tool has a bounded scope contract, deterministic
result order where the handler lists rows, a limit or explicit singleton key,
MCP envelope negotiation through `Accept: application/eshu.envelope+json`, and
a clear drilldown path when more data is available.

| Tool | Scope contract | Bound | Envelope | Prompt readiness |
| --- | --- | --- | --- | --- |
| `find_code` | repository selector optional; content fallback when graph is absent | `limit` | yes | prompt-ready for bounded symbol/code search |
| `find_symbol` | repository selector optional; symbol name required | `limit` | yes | prompt-ready for exact or fuzzy symbol lookup |
| `investigate_code_topic` | topic plus optional repository selectors | `limit` | yes | prompt-ready for topic summaries with handles |
| `get_code_relationship_story` | canonical `entity_id` or name plus optional repo | singleton story | yes | prompt-ready after target is resolved |
| `analyze_code_relationships` | `entity_id` or exact name plus direction | `limit` | yes | prompt-ready after target is resolved |
| `find_dead_code` | repository selector optional; authoritative profile required | `limit` | yes | prompt-ready for bounded candidate scans |
| `find_dead_iac` | repository selector required or explicit broader scan | `limit` | yes | prompt-ready for bounded IaC candidate scans |
| `find_unmanaged_resources` | repository selector optional; authoritative profile required | `limit` | yes | prompt-ready for bounded IaC management scans |
| `calculate_cyclomatic_complexity` | entity id, function name, or repository selector | singleton or `limit` | yes | prompt-ready; list calls return `truncated` |
| `find_most_complex_functions` | optional repository selector | `limit` | yes | prompt-ready; deterministic ordering and `truncated` |
| `execute_cypher_query` | explicit read-only Cypher supplied by caller | `limit` plus timeout | yes | diagnostics-only; use named tools first |
| `visualize_graph_query` | explicit Cypher supplied by caller | visualization URL | no | diagnostics-only browser helper |
| `search_registry_bundles` | optional query string over repository bundle catalog | `limit` | yes | prompt-ready for bounded catalog lookup |
| `list_indexed_repositories` | explicit whole-index inventory | `limit` and `offset` | yes | prompt-ready; returns `truncated` for paging |
| `get_repository_stats` | repository selector optional; empty selector returns inventory | singleton or inventory | partial | usable, but prefer `list_indexed_repositories` for inventory |
| `execute_language_query` | language and entity type filters, optional repository selector | `limit` | yes | prompt-ready for bounded language scans |
| `find_function_call_chain` | start and end names required | `max_depth` | yes | prompt-ready when both endpoints are known |
| `get_ecosystem_overview` | explicit whole-index ecosystem overview | singleton summary | yes | prompt-ready |
| `trace_deployment_chain` | service name required | singleton trace | yes | prompt-ready after service is resolved |
| `find_blast_radius` | target id required | bounded graph traversal | yes | prompt-ready after target is resolved |
| `find_infra_resources` | query plus optional category | `limit` | yes | prompt-ready for bounded infra search |
| `analyze_infra_relationships` | target plus relationship type | bounded graph read | yes | prompt-ready after target is resolved |
| `get_repo_summary` | repository selector required | singleton summary | yes | prompt-ready |
| `get_repo_context` | repository selector required | singleton context | yes | prompt-ready |
| `get_relationship_evidence` | resolved relationship id required | singleton evidence packet | yes | prompt-ready |
| `list_package_registry_packages` | package id, ecosystem, or name filter | `limit` | yes | prompt-ready |
| `list_package_registry_versions` | package id required | `limit` | yes | prompt-ready |
| `get_repo_story` | repository selector required | singleton story | yes | prompt-ready |
| `get_repository_coverage` | repository selector required | singleton coverage | yes | prompt-ready |
| `trace_resource_to_code` | resource id or selector required | bounded graph traversal | yes | prompt-ready |
| `explain_dependency_path` | source and target required | bounded path search | yes | prompt-ready |
| `find_change_surface` | changed path or entity scope required | bounded graph/content read | yes | prompt-ready |
| `investigate_change_surface` | changed path, topic, or entity scope required | bounded investigation | yes | prompt-ready |
| `compare_environments` | workload or service plus two environments | bounded comparison | yes | prompt-ready |
| `resolve_entity` | name/query plus optional repository selector and type | `limit` | yes | prompt-ready for disambiguation before drilldowns |
| `get_entity_context` | canonical entity id required | singleton context | partial | usable after `resolve_entity`; envelope hardening remains follow-up |
| `get_workload_context` | canonical workload id required | singleton context | partial | usable after workload resolution; envelope hardening remains follow-up |
| `get_workload_story` | canonical workload id required | singleton story | partial | usable after workload resolution; envelope hardening remains follow-up |
| `get_service_context` | service/workload selector required | singleton context | yes | prompt-ready |
| `get_service_story` | service/workload selector required | singleton story | yes | prompt-ready |
| `investigate_service` | service name plus optional environment/question | bounded investigation | yes | prompt-ready |
| `get_file_content` | repository selector and relative path required | singleton file | yes | prompt-ready for exact source read |
| `get_file_lines` | repository selector, relative path, and line range required | explicit line range | yes | prompt-ready for citations |
| `get_entity_content` | canonical entity id required | singleton entity source | yes | prompt-ready after entity resolution |
| `search_file_content` | pattern plus optional repository selectors | `limit` and `offset` | yes | prompt-ready; unsupported filters are not advertised |
| `search_entity_content` | pattern plus optional repository selectors | `limit` and `offset` | yes | prompt-ready; unsupported filters are not advertised |
| `list_ingesters` | explicit runtime inventory | `limit` and `offset` | yes | prompt-ready for runtime diagnostics |
| `get_ingester_status` | ingester id required | singleton status | yes | prompt-ready for runtime diagnostics |
| `get_index_status` | optional repository selector | singleton status | yes | prompt-ready for runtime diagnostics |

No-Regression Evidence: `go test ./internal/mcp ./internal/query -count=1` exercises the MCP dispatch contracts, query envelope negotiation, bounded list behavior, and content-search schema truth for the changed surfaces.

Observability Evidence: this PR changes read contracts only; existing MCP `dispatch tool` debug logs, HTTP response envelopes, query handler errors, and bounded `limit`/`truncated` response fields diagnose whether a prompt call was scoped, complete, or needs a follow-up page.
