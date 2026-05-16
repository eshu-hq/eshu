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
| `inspect_code_inventory` | repository, language, entity-kind, file-path, symbol, decorator, method, and class filters optional by prompt shape | `limit` and `offset` | yes | prompt-ready for structural inventory without raw Cypher; returns source handles and truncation |
| `investigate_import_dependencies` | repository, language, source-file, target-file, source-module, and target-module filters optional by prompt shape; at least one scope anchor required | non-negative `limit` and `offset` | yes | prompt-ready for imports by file, module importers, package imports, Python file import cycles, and cross-module calls without raw Cypher; returns one canonical row key by query type |
| `inspect_call_graph_metrics` | repository required; language optional | positive `limit` and non-negative `offset` | yes | prompt-ready for recursive function and high-degree hub-function prompts without raw Cypher; returns canonical `functions` rows with hub call counts, source handles, recursion evidence, and truncation |
| `investigate_code_topic` | topic plus optional repository selectors | `limit` | yes | prompt-ready for topic summaries with handles |
| `get_code_relationship_story` | canonical `entity_id` or name plus optional repo | singleton story | yes | prompt-ready after target is resolved |
| `analyze_code_relationships` | exact target plus relationship query type; callers/callees/importers route through relationship story | `limit`, `offset`, and `max_depth` for transitive aliases | yes | compatibility prompt alias for relationship-story, call-chain, class hierarchy, and override routes without raw Cypher |
| `find_dead_code` | repository selector optional; authoritative profile required | `limit` | yes | prompt-ready for bounded candidate scans |
| `investigate_dead_code` | repository selector optional; authoritative profile required; reports coverage and language maturity | `limit` and `offset` | yes | prompt-ready investigation packet; JavaScript/TypeScript candidates stay ambiguous until corpus precision is proven |
| `investigate_hardcoded_secrets` | repository selector and language optional; finding kind filters optional | `limit` and `offset` | yes | prompt-ready redacted security investigation packet; returns suppression notes, source handles, and truncation coverage without raw secret values |
| `find_dead_iac` | repository selector required or explicit broader scan | `limit` | yes | prompt-ready for bounded IaC candidate scans |
| `find_unmanaged_resources` | AWS scope/account required; authoritative profile required | `limit` and `offset` | yes | prompt-ready for bounded IaC management scans with `safety_summary`, redacted sensitive evidence values, and refused import-plan actions |
| `get_iac_management_status` | AWS scope/account plus exact ARN | singleton | yes | story-first status for one AWS resource; read-only with `safety_gate` and no Terraform/cloud mutation |
| `explain_iac_management_status` | AWS scope/account plus exact ARN | singleton | yes | grouped reducer evidence for one AWS resource; raw tags remain provenance-only, sensitive values are redacted, and `safety_gate` names review/refusal state |
| `propose_terraform_import_plan` | AWS scope/account required; exact ARN optional | `limit` and `offset` | yes | prompt-ready read-only Terraform import-plan candidates; returns import blocks only for safety-approved supported cloud-only findings and refusal reasons for the rest |
| `list_aws_runtime_drift_findings` | AWS scope/account required; exact ARN optional | `limit` and `offset` | yes | prompt-ready drift read surface with exact/derived/ambiguous/stale/unknown outcomes, rejected promotion status, evidence, and no raw Cypher |
| `calculate_cyclomatic_complexity` | entity id, function name, or repository selector | singleton or `limit` | yes | prompt-ready; list calls return `truncated` |
| `find_most_complex_functions` | optional repository selector | `limit` | yes | prompt-ready; deterministic ordering and `truncated` |
| `inspect_code_quality` | optional repository selector plus optional language/function scope | `limit` and `offset` | yes | prompt-ready for complexity, long-function, high-argument, and refactoring-candidate prompts with source handles and `truncated` |
| `execute_cypher_query` | explicit read-only Cypher supplied by caller | `limit` plus timeout | yes | diagnostics-only; use named tools first |
| `visualize_graph_query` | explicit Cypher supplied by caller | visualization URL | no | diagnostics-only browser helper |
| `search_registry_bundles` | optional query string over repository bundle catalog | `limit` | yes | prompt-ready for bounded catalog lookup |
| `list_indexed_repositories` | explicit whole-index inventory | `limit` and `offset` | yes | prompt-ready; returns `truncated` for paging |
| `get_repository_stats` | repository selector optional; empty selector returns inventory | singleton or inventory | partial | usable, but prefer `list_indexed_repositories` for inventory |
| `execute_language_query` | language and entity type filters, optional repository selector | `limit` | yes | prompt-ready for bounded language scans |
| `find_function_call_chain` | start and end names required | `max_depth` | yes | prompt-ready when both endpoints are known |
| `get_ecosystem_overview` | explicit whole-index ecosystem overview | singleton summary | yes | prompt-ready |
| `trace_deployment_chain` | service name required | singleton trace | yes | prompt-ready after service is resolved |
| `investigate_deployment_config` | service name or workload id plus optional environment | per-section `limit` | yes | prompt-ready for image tags, runtime settings, resource limits, values layers, rendered targets, and read-first file handles |
| `find_blast_radius` | target id required | `limit` | yes | prompt-ready after target is resolved; returns `truncated` |
| `find_infra_resources` | query plus optional category | `limit` | yes | prompt-ready for bounded infra search |
| `investigate_resource` | query or canonical resource id plus optional resource family/environment | per-section `limit` and `max_depth` | yes | prompt-ready for resource-first prompts; resolves ambiguity before traversal and returns workloads, repository provenance paths, source handles, limitations, and next calls |
| `analyze_infra_relationships` | target plus relationship type | bounded graph read | yes | prompt-ready after target is resolved |
| `get_repo_summary` | repository selector required | singleton summary | yes | prompt-ready |
| `get_repo_context` | repository selector required | singleton context | yes | prompt-ready |
| `get_relationship_evidence` | resolved relationship id required | singleton evidence packet | yes | prompt-ready |
| `build_evidence_citation_packet` | explicit file or entity handles required; input array capped at 500 | `limit` up to 50 | yes | prompt-ready for source, docs, manifest, and deployment citations without graph traversal |
| `list_package_registry_packages` | package id, ecosystem, or name filter | `limit` | yes | prompt-ready |
| `list_package_registry_versions` | package id required | `limit` | yes | prompt-ready |
| `list_package_registry_dependencies` | package id or version id required | `limit` default 50, optional cursor | yes | prompt-ready; returns `next_cursor` when truncated |
| `list_package_registry_correlations` | package id or repository id required | `limit` default 50, optional cursor | yes | prompt-ready; separates ownership candidates, publication evidence, and consumption truth |
| `list_ci_cd_run_correlations` | scope id, repository id, commit sha, provider run id plus provider when run-only, artifact digest, or environment required | `limit` default 50, optional cursor | yes | prompt-ready; reports exact, derived, ambiguous, unresolved, and rejected outcomes without promoting CI success to deployment truth |
| `list_supply_chain_impact_findings` | CVE, package id, repository id, subject digest, or impact status required | `limit` default 50, optional cursor | yes | prompt-ready; keeps CVSS, EPSS, KEV, reachability, fixed version, and missing evidence separate |
| `list_sbom_attestation_attachments` | subject digest, document id, or document digest required | `limit` default 50, optional cursor | yes | prompt-ready; preserves attached verified, unverified, parse-only, subject mismatch, ambiguous subject, unknown subject, and unparseable states without turning components into vulnerability impact |
| `get_repo_story` | repository selector required | singleton story | yes | prompt-ready |
| `get_repository_coverage` | repository selector required | singleton coverage | yes | prompt-ready |
| `trace_resource_to_code` | resource id or selector required | `max_depth` and `limit` | yes | prompt-ready; returns `truncated` |
| `explain_dependency_path` | source and target required | bounded path search | yes | prompt-ready |
| `find_change_surface` | entity scope required | `limit` | yes | legacy entity-scoped path; prefer `investigate_change_surface` for code-topic and changed-path prompts |
| `investigate_change_surface` | changed path, topic, or entity scope required | bounded investigation | yes | prompt-ready |
| `compare_environments` | workload plus two environments | per-environment `limit` | yes | prompt-ready; returns story, summary, shared/dedicated resources, evidence, limitations, next calls, and side-specific truncation coverage |
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

No-Regression Evidence: Issue #125 covers Terraform import-plan candidates with `go test ./internal/query -run 'TestHandleTerraformImportPlanCandidates|TestOpenAPITerraformImportPlanCandidates|TestServeOpenAPI' -count=1`, `go test ./internal/mcp -run 'TestResolveRouteMapsTerraformImportPlanCandidates|TestReadOnlyTools|TestCodebaseTools|TestEveryRegisteredToolHasDispatchRoute|TestMCPToolContractMatrixCoversReadOnlyTools' -count=1`, and `go test ./internal/telemetry -run TestSpanNames -count=1`; those tests exercise safe S3 and Lambda import blocks, request-derived S3 account/region hints, sensitive-resource refusal, ARN-only `resource_id` handling, OpenAPI response fields, MCP tool definition, route mapping, envelope negotiation, bounded paging, and span-name contract. `go test ./internal/query -run 'TestHandleDeadCodeInvestigation|TestOpenAPI|TestOpenAPIDeadCode' -count=1` and `go test ./internal/mcp -run 'TestResolveRouteMapsInvestigateDeadCode|TestReadOnlyTools|TestCodebaseTools|TestEveryRegisteredToolHasDispatchRoute|TestMCPToolContractMatrixCoversReadOnlyTools' -count=1` exercise the new dead-code investigation HTTP route, OpenAPI entry, MCP tool definition, route mapping, envelope negotiation, bounded paging, and TypeScript ambiguity policy. `go test ./internal/mcp ./internal/query -count=1` exercises the broader MCP dispatch contracts, query envelope negotiation, bounded list behavior, and content-search schema truth for the changed surfaces. Issue #301 additionally covers legacy impact and environment-comparison no-cache bounds with `go test ./internal/query -run 'TestFindBlastRadiusUsesRequestedLimitAndReportsTruncation|TestTraceResourceToCodeUsesRequestedLimitAndReportsTruncation|TestFindChangeSurfaceUsesRequestedLimitAndReportsTruncation|TestCompareEnvironmentsBoundsResourceReadsAndReportsTruncation' -count=1` and `go test ./internal/mcp -run 'TestNoCachePromptToolsAdvertiseBounds|TestNoCachePromptRoutesPassBounds|TestResolveRouteMapsAnalyzeCodeRelationships' -count=1`. Issue #296 covers the environment comparison story packet with `go test ./internal/query -run 'TestCompareEnvironmentsReturnsStoryGradePacket|TestCompareEnvironmentsStoryReportsMissingEvidenceLimitations' -count=1`. Issue #294 covers deployment configuration influence with `go test ./internal/query -run TestBuildDeploymentConfigInfluenceResponseReturnsPromptReadyFiles -count=1` and `go test ./internal/mcp -run 'Test(DeploymentConfigInfluenceToolContract|ResolveRouteMapsDeploymentConfigInfluenceToBoundedBody|ReadOnlyTools|EcosystemTools|EveryRegisteredToolHasDispatchRoute)' -count=1`. Issue #293 covers resource-first investigation with `go test ./internal/query -run 'TestInvestigateResource|TestTraceResourceToCodeUsesRequestedLimitAndReportsTruncation' -count=1` and `go test ./internal/mcp -run 'TestResourceInvestigation|TestResolveRouteMapsResourceInvestigation|TestReadOnlyTools|TestEcosystemTools|TestEveryRegisteredToolHasDispatchRoute' -count=1`.

Observability Evidence: `telemetry.SpanQueryIaCTerraformImportPlan` (`query.iac_terraform_import_plan`) wraps the import-plan candidate HTTP/MCP route with stable `http.route` and `eshu.capability` attributes. `telemetry.SpanQueryDeadCodeInvestigation` (`query.dead_code_investigation`) wraps the dead-code investigation route with the same route and capability attributes. Existing MCP `dispatch tool` debug logs, HTTP response envelopes, query handler errors, graph `neo4j.query` spans, Postgres `postgres.query` spans, service query timing logs, and bounded `coverage.limit`/`coverage.truncated` response fields diagnose whether a prompt call was scoped, complete, or needs a follow-up page.
