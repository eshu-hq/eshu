# MCP Tool Contract Matrix

This matrix is the prompt-readiness contract for the current `ReadOnlyTools`
set. "Prompt-ready" means the tool has a bounded scope contract, deterministic
result order where the handler lists rows, a limit or explicit singleton key,
MCP envelope negotiation through `Accept: application/eshu.envelope+json`, and
a clear drilldown path when more data is available.

## Prompt-Family Audit

Use this table to pick the named tool before falling back to diagnostics-only
Cypher.

| Prompt family from docs | Primary current MCP path | Current status | Remaining tracked work |
| --- | --- | --- | --- |
| Cross-repo service story, onboarding, runbooks | `get_service_story`, `investigate_service` | Story-first path returns bounded service dossiers. | None |
| Symbol discovery and implementation lookup | `find_symbol`, `find_code`, `execute_language_query` | Definition lookup is bounded, paged, source-handle backed, and does not require raw Cypher. | None |
| Broad code-topic and implementation investigation | `investigate_code_topic` | Content-index investigation returns ranked files, symbols, searched terms, coverage, truncation, and follow-up handles. | None |
| Callers, callees, imports, call chains | `get_code_relationship_story`, `find_function_call_chain`, `investigate_import_dependencies`, `inspect_call_graph_metrics` | Relationship reads resolve ambiguity first, anchor graph traversal by entity or repository scope, and expose paging/truncation. | None |
| Dead code and code quality | `find_dead_code`, `find_most_complex_functions`, `inspect_code_quality` | Complexity, long-function, high-argument, refactoring, and dead-code prompts use first-class bounded tools. | None |
| Security hardcoded secrets | `investigate_hardcoded_secrets` | Redacted content-index investigation returns severity, confidence, suppression notes, source handles, paging, and truncation. | None |
| Deployment, GitOps, and resource tracing | `trace_deployment_chain`, `trace_resource_to_code`, `investigate_deployment_config`, story tools | Deployment and resource prompts use story or bounded trace routes. | None |
| Environment comparison | `compare_environments` | Workload/environment comparison returns a bounded story packet with evidence, limitations, coverage, and next calls. | None |
| Runtime and indexing status prompts | `get_index_status`, `list_ingesters`, `get_ingester_status` | Runtime prompts use shipped status tools instead of stale job-status names. | None |
| Documentation/confluence prompts | story routes, `list_documentation_facts`, plus `build_evidence_citation_packet` | Exact source, docs, manifest, collected documentation facts, and deployment proof use bounded fact reads or citation packets from returned handles. | None |
| Structural code inventory | `inspect_code_inventory` | Content-index inventory covers functions, classes, file-local entities, decorators, methods, and file-level counts. | None |
| Incident response prompts | `get_incident_context` | Incident context returns PagerDuty source evidence, declared/applied/live routing slots, fallback change candidates, explicit missing Jira/PR/build/commit slots, deployable/runtime/image slots only when explicit service-catalog, image-identity, and Kubernetes reducer evidence exists, build/commit slots only when reducer-owned CI/CD run correlations match the selected image, PR slots from provider merged-PR evidence tied to the selected commit, and work-item slots from Jira remote links or issue-key evidence. | Root-cause attribution, service-health inference, blast-radius inference, and Jira-only PR verification remain out of scope. |

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
| `count_repositories_by_language` | explicit language family | count-only via `limit=0` | yes | prompt-ready; avoids per-repo coverage fan-out |
| `list_repositories_by_language` | explicit language family | `limit` and `offset` | yes | prompt-ready; returns repository handles and `truncated` |
| `get_repository_language_inventory` | explicit whole-index language inventory | `limit` and `offset` | yes | prompt-ready; aggregate language buckets only |
| `get_repository_stats` | repository selector optional; empty selector returns inventory | singleton or inventory | partial | usable, but prefer `list_indexed_repositories` for inventory |
| `execute_language_query` | language and entity type filters, optional repository selector | `limit` | yes | prompt-ready for bounded language scans |
| `find_function_call_chain` | start and end names required | `max_depth` | yes | prompt-ready when both endpoints are known |
| `get_ecosystem_overview` | explicit whole-index ecosystem overview | singleton summary | yes | prompt-ready |
| `trace_deployment_chain` | service name required | singleton trace | yes | prompt-ready after service is resolved |
| `investigate_deployment_config` | service name or workload id plus optional environment | per-section `limit` | yes | prompt-ready for image tags, runtime settings, resource limits, values layers, rendered targets, and read-first file handles |
| `find_blast_radius` | target id required | `limit` | yes | prompt-ready after target is resolved; returns `truncated` |
| `find_infra_resources` | query plus optional category | `limit` | yes | prompt-ready for bounded infra search |
| `count_infra_resources` | optional category/kind/resource_type/provider/environment/resource_service/resource_category scope | scope-only, no `limit` | yes | prompt-ready for bounded infra rollups by provider, environment, and label |
| `get_infra_resource_inventory` | optional scope filters plus `group_by` (provider / environment / resource_category / resource_service / label) | `limit` (default 100, max 500), bounded `offset` (max 10000) | yes | prompt-ready for bounded infra inventory grouped by one dimension |
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
| `count_package_registry_packages` | optional ecosystem, registry, namespace, package_manager, or visibility scope | singleton totals envelope | yes | prompt-ready; cheap-summary count + per-ecosystem rollup over the graph (:Package) corpus using the cookbook Area-5 grouped-count hot path; replaces page-and-iterate caller workflow for ecosystem totals questions |
| `get_package_registry_package_inventory` | optional ecosystem, registry, namespace, package_manager, or visibility scope; `group_by` selects dimension | `limit` default 100, max 500, deterministic ordering, `next_offset` on truncation | yes | prompt-ready; paginated grouped count of (:Package) nodes along one dimension (ecosystem, registry, namespace, package_manager, visibility) anchored on indexed graph properties |
| `list_ci_cd_run_correlations` | scope id, repository id, commit sha, provider run id plus provider when run-only, artifact digest, or environment required | `limit` default 50, optional cursor | yes | prompt-ready; reports exact, derived, ambiguous, unresolved, and rejected outcomes without promoting CI success to deployment truth |
| `count_ci_cd_run_correlations` | optional scope id, repository id, commit sha, provider, artifact digest, environment, or outcome scope | singleton totals envelope | yes | prompt-ready; cheap-summary count and per-outcome / per-environment / per-provider rollup over reducer-owned run correlation facts; replaces page-and-iterate caller workflow for ecosystem totals questions |
| `get_ci_cd_run_correlation_inventory` | optional scope id, repository id, commit sha, provider, artifact digest, environment, or outcome scope; `group_by` selects dimension | `limit` default 100, max 500, deterministic ordering, `next_offset` on truncation | yes | prompt-ready; paginated grouped count of reducer-owned run correlations along one dimension (outcome, environment, repository_id, provider) |
| `list_service_catalog_correlations` | scope id, entity ref, repository id, service id, workload id, or owner ref required | `limit` default 50, optional cursor | yes | prompt-ready; reports catalog ownership and drift outcomes without promoting catalog declarations over graph truth |
| `list_kubernetes_correlations` | scope id, cluster id, workload object id, namespace, image ref, or source digest required | `limit` default 50, optional cursor | yes | prompt-ready; reports live Kubernetes workload ownership and drift outcomes (exact, derived, ambiguous, unresolved, stale, rejected) without promoting a tag coincidence or label selector to exact ownership |
| `list_observability_coverage_correlations` | scope id, provider, coverage signal, observability object ref, target uid, or target service ref required | `limit` default 50, optional cursor | yes | prompt-ready; reports which monitored cloud resources and services have alarm/dashboard/log/trace coverage versus gaps, as structural correlation only, never a health assertion from telemetry values |
| `list_container_image_identities` | digest, image ref, repository id, or outcome required | `limit` default 50, optional cursor | yes | prompt-ready; reports digest-first reducer identity rows with source layers and evidence fact ids without promoting weak, ambiguous, unresolved, or stale tags |
| `count_container_image_identities` | optional digest, image ref, repository id, or outcome scope | singleton totals envelope | yes | prompt-ready; cheap-summary count and per-outcome / per-identity-strength rollup over reducer-owned container image identity facts; replaces page-and-iterate caller workflow for ecosystem totals questions |
| `get_container_image_identity_inventory` | optional digest, image ref, repository id, or outcome scope; `group_by` selects dimension | `limit` default 100, max 500, deterministic ordering, `next_offset` on truncation | yes | prompt-ready; paginated grouped count of reducer-owned identities along one dimension (outcome, identity_strength, repository_id) |
| `list_advisory_evidence` | CVE, advisory id, or package id required; optional source narrows anchored pages | `limit` default 50, optional cursor | yes | prompt-ready; groups source-only GHSA/CVE/OSV/NVD/EPSS/KEV/CWE/range/fixed-version/withdrawn evidence without implying impact |
| `get_vulnerability_scanner_read_contract` | optional route name; unknown routes rejected before any read-model access | singleton contract | yes | prompt-ready; machine-readable scanner filter support, route bounds, backing read models, missing-evidence semantics, and provider-only separation |
| `list_supply_chain_impact_findings` | CVE, advisory id, package id, repository id or selector, subject digest, impact status, ecosystem, service, workload, environment, severity, priority bucket, or `min_priority_score > 0` required; optional `profile=precise` (default) or `comprehensive` | `limit` default 50, optional cursor; `sort` can use finding id or priority score | yes | prompt-ready; keeps CVSS, EPSS, KEV, priority contributions, source snapshot freshness, reachability, fixed version, missing evidence, and per-row `detection_profile` separate |
| `count_supply_chain_impact_findings` | optional CVE, advisory id, package id, repository id or selector, subject digest, impact status, ecosystem, service, workload, environment, severity, priority, or suppression scope | singleton totals envelope | yes | prompt-ready; cheap-summary count and by-priority/severity rollup over reducer-owned impact facts; replaces page-and-iterate caller workflow for ecosystem totals questions |
| `get_supply_chain_impact_inventory` | optional CVE, advisory id, package id, repository id or selector, subject digest, impact status, ecosystem, service, workload, environment, severity, priority, or suppression scope; `group_by` selects dimension | `limit` default 100, max 500, deterministic ordering, `next_offset` on truncation | yes | prompt-ready; paginated grouped count of reducer-owned impact findings along one dimension (impact_status, priority_bucket, severity, repository_id, ecosystem) |
| `explain_supply_chain_impact` | finding id, or advisory/CVE plus package, repository, or image digest required | singleton explanation | yes | prompt-ready; hydrates only the finding's bounded evidence fact ids and does not perform whole-graph explain or invent reachability/deployment truth |
| `list_security_alert_reconciliations` | repository id or selector, provider, package id, CVE, or GHSA required; provider state and reconciliation status filter anchored pages only | `limit` default 50, optional cursor | yes | prompt-ready; keeps provider alert state separate from Eshu impact state |
| `count_security_alert_reconciliations` | optional repository id or selector, provider, package id, CVE, GHSA, provider state, or reconciliation status scope | singleton totals envelope | yes | prompt-ready; cheap-summary count and per-reconciliation-status / per-provider / per-provider-state rollup over reducer-owned reconciliation facts; replaces page-and-iterate caller workflow for ecosystem totals questions |
| `get_security_alert_reconciliation_inventory` | optional repository id or selector, provider, package id, CVE, GHSA, provider state, or reconciliation status scope; `group_by` selects dimension | `limit` default 100, max 500, deterministic ordering, `next_offset` on truncation | yes | prompt-ready; paginated grouped count of reducer-owned reconciliations along one dimension (reconciliation_status, provider, provider_state, repository_id, package_id) |
| `list_sbom_attestation_attachments` | subject digest, document id, or document digest required | `limit` default 50, optional cursor | yes | prompt-ready; preserves attached verified, unverified, parse-only, subject mismatch, ambiguous subject, unknown subject, and unparseable states without turning components into vulnerability impact |
| `count_sbom_attestation_attachments` | optional subject digest, document id, document digest, attachment_status, or artifact_kind scope | singleton totals envelope | yes | prompt-ready; cheap-summary count and per-attachment-status / per-artifact-kind rollup over reducer-owned attachment facts; replaces page-and-iterate caller workflow for ecosystem totals questions |
| `get_sbom_attestation_attachment_inventory` | optional subject digest, document id, document digest, attachment_status, or artifact_kind scope; `group_by` selects dimension | `limit` default 100, max 500, deterministic ordering, `next_offset` on truncation | yes | prompt-ready; paginated grouped count of reducer-owned attachments along one dimension (attachment_status, artifact_kind, subject_digest) |
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
| `get_incident_context` | provider incident id required; optional provider, scope id, service id, and time window narrow the read | `limit` default 25, max 100 | yes | prompt-ready for on-call incident packets; separates exact provider evidence, intended/applied/live routing evidence, fallback service/time change candidates, derived edges, ambiguous candidates, drifted or permission-hidden routing, and explicit missing evidence |
| `get_file_content` | repository selector and relative path required | singleton file | yes | prompt-ready for exact source read |
| `get_file_lines` | repository selector, relative path, and line range required | explicit line range | yes | prompt-ready for citations |
| `get_entity_content` | canonical entity id required | singleton entity source | yes | prompt-ready after entity resolution |
| `search_file_content` | pattern plus optional repository selectors | `limit` and `offset` | yes | prompt-ready; unsupported filters are not advertised |
| `search_entity_content` | pattern plus optional repository selectors | `limit` and `offset` | yes | prompt-ready; unsupported filters are not advertised |
| `list_documentation_findings` | optional finding, source, document, status, truth, freshness, and updated-since filters | `limit` and `cursor` | yes | prompt-ready for durable documentation truth finding review; reads the same fact-backed route as HTTP |
| `count_documentation_findings` | optional scope_id, finding_type, source_id, document_id, status, truth_level, or freshness_state scope | singleton totals envelope | yes | prompt-ready; cheap-summary count and per-status / per-truth-level / per-freshness-state rollup over durable documentation finding facts; inherits the same per-document read permissions as the list endpoint; replaces page-and-iterate caller workflow for ecosystem totals questions |
| `get_documentation_finding_inventory` | optional scope_id, finding_type, source_id, document_id, status, truth_level, or freshness_state scope; `group_by` selects dimension | `limit` default 100, max 500, deterministic ordering, `next_offset` on truncation | yes | prompt-ready; paginated grouped count of documentation findings along one dimension (status, truth_level, freshness_state, finding_type, source_id); inherits the same per-document read permissions |
| `list_documentation_facts` | scope, source, document, or section anchor required except `fact_kind=source`; optional fact kind and text query | `limit` and `cursor` | yes | prompt-ready for collected Confluence and source-neutral documentation facts before findings exist |
| `get_documentation_evidence_packet` | finding id required | singleton evidence packet | yes | prompt-ready for documentation updater evidence snapshots |
| `check_documentation_evidence_packet_freshness` | packet id required, saved packet version optional | singleton freshness result | yes | prompt-ready for stale-packet checks before updater publication |
| `list_ingesters` | explicit runtime inventory | `limit` and `offset` | yes | prompt-ready for runtime diagnostics |
| `get_ingester_status` | ingester id required | singleton status | yes | prompt-ready for runtime diagnostics |
| `get_index_status` | optional repository selector | singleton status | yes | prompt-ready for runtime diagnostics |

## Verification

No-Regression Evidence: focused query, MCP, and telemetry tests cover:

- Terraform import-plan candidates, including safe S3 and Lambda import blocks,
  request-derived S3 account and region hints, sensitive-resource refusal,
  ARN-only `resource_id` handling, OpenAPI response fields, MCP tool
  definitions, route mapping, envelope negotiation, bounded paging, and span
  names.
- Dead-code investigation routes, including HTTP route shape, OpenAPI entries,
  MCP tool definitions, route mapping, envelope negotiation, bounded paging,
  and TypeScript ambiguity policy.
- Legacy impact and environment-comparison no-cache bounds, including returned
  limit and truncation behavior.
- Environment comparison story packets, deployment configuration influence, and
  resource-first investigation route contracts.
- Incident context route contracts, explicit missing evidence slots, explicit
  PagerDuty service URL to runtime/image evidence, image-to-build/commit
  evidence, commit-to-PR evidence, Jira work-item enrichment, ambiguous
  provider-scope errors, OpenAPI fields, and MCP route mapping.
- Broader MCP dispatch and query contracts through `go test ./internal/mcp ./internal/query -count=1`.

Observability Evidence: `telemetry.SpanQueryIaCTerraformImportPlan` (`query.iac_terraform_import_plan`) wraps the import-plan candidate HTTP/MCP route with stable `http.route` and `eshu.capability` attributes. `telemetry.SpanQueryDeadCodeInvestigation` (`query.dead_code_investigation`) wraps the dead-code investigation route with the same route and capability attributes. `telemetry.SpanQueryIncidentContext` (`query.incident_context`) wraps the incident-context route with stable route and capability attributes. Existing MCP `dispatch tool` debug logs, HTTP response envelopes, query handler errors, graph `neo4j.query` spans, Postgres `postgres.query` spans, service query timing logs, and bounded `coverage.limit`/`coverage.truncated` response fields diagnose whether a prompt call was scoped, complete, or needs a follow-up page.
