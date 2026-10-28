# MCP tool groups and routes

## Tool groups

`ReadOnlyTools` assembles the registered tools from the tool definition files.
`ReadOnlyTools()` (and the `Verify ReadOnlyTools count` gate) is the authoritative
count; the per-group table below lists the major static groups for orientation
and is not an exhaustive enumeration (reachability is assembled dynamically and
is not broken out here).

| Group | Count | Source file |
|---|---|---|
| `codebaseTools` | 37 | `tools_codebase.go` (splices `code/intel/tools.go`, `code/dead/tools.go`, `code/divergence/tools.go`, `code/quality/tools.go`, `iac/management/tools.go`, `replatforming/tools.go`), `relationships/code_tools.go`, `tools_import_dependencies.go`, `tools_security.go`, `tools_iac.go` |
| `codeFlowTools` | 4 | `types.go` (wraps `code/flow/tools.go`) |
| `repositoryLanguageTools` | 3 | `tools_repository_language.go` |
| `ecosystemTools` | 23 | `ecosystem/tools.go`, `ecosystem/*_tools.go` |
| `infraResourceAggregateTools` | 2 | `types.go` (wraps `infra/inventory/tools.go`) |
| `cloudInventoryTools` | 1 | `cloud/inventory_tools.go` |
| `cloudRuntimeDriftTools` | 1 | `cloud/runtime_drift_tools.go` |
| `packageRegistryTools` | 2 | `tools_package_registry.go` |
| `admissionDecisionTools` | 1 | `types.go` (wraps `admission/decisions/tools.go`) |
| `packageRegistryAggregateTools` | 2 | `tools_package_registry_aggregates.go` |
| `cicdTools` | 1 | `tools_cicd.go` |
| `cicdRunCorrelationAggregateTools` | 2 | `tools_cicd_aggregates.go` |
| `serviceCatalogTools` | 1 | `service/catalog_tools.go` |
| `codeownersTools` | 1 | `types.go` (wraps `code/owners/tools.go`) |
| `kubernetesTools` | 1 | `types.go` (wraps `kubernetes/tools.go`) |
| `secretsIAMTools` | 5 | `types.go` (wraps `access/posture/tools.go`) |
| `observabilityCoverageTools` | 1 | `types.go` (wraps `observability/coverage/tools.go`) |
| `supplyChainTools` | 8 | `tools_supply_chain.go` (splices `container/image/tools.go`) |
| `supplyChainImpactAggregateTools` | 2 | `tools_supply_chain_aggregates.go` |
| `securityAlertReconciliationAggregateTools` | 2 | `types.go` (wraps `alerts/aggregates.go`) |
| `containerImageAggregateTools` | 2 | `types.go` (wraps `container/image/tools.go`) |
| `sbomAttestationAttachmentAggregateTools` | 2 | `tools_sbom_attachment_aggregates.go` |
| `incidentContextTools` | 1 | `tools_incident_context.go` |
| `workItemTools` | 1 | `tools_work_item.go` |
| `visualizationTools` | 1 | `visualization/tools.go` |
| `freshnessTools` | 4 | `freshness/tools.go` |
| `contextTools` | 7 | `tools_context.go`, `service/context_tools.go` |
| `serviceIntelligenceTools` | 1 | `service/intelligence_tools.go` |
| `contentTools` | 6 | `tools_content.go` |
| `documentationTools` | 4 | `documentation/tools.go` |
| `queryPlaybookTools` | 2 | `playbooks/tools.go` |
| `investigationWorkflowTools` | 2 | `investigation/workflow_tools.go` |
| `investigationPacketTools` | 3 | `investigation/packet_tools.go` |
| `semanticEvidenceTools` | 2 | `semantic/evidence_tools.go` |
| `semanticSearchTools` | 1 | `semantic/search_tools.go` |
| `documentationFindingAggregateTools` | 2 | `documentation/finding_aggregate_tools.go` |
| `componentExtensionTools` | 2 | `tools_component_extensions.go` |
| `collectorExtractionReadinessTools` | 2 | `tools_collector_extraction_readiness.go` |
| `runtimeTools` | 8 | `tools_runtime.go` |
| `askTools` | 1 | `ask/tools.go` |
| `relationshipEdgesTool` | 1 | `relationships/tools.go` |
| `repositoryFilesTool` | 1 | `tools_repository_files.go` |
| `phpConfigurationTool` | 1 | `tools_content.go` (appended after the existing registry) |

Representative tool-to-route mappings from `resolveRoute` (`dispatch.go:207`):

| Tool | HTTP method | Path |
|---|---|---|
| `find_code` | POST | `/api/v0/code/search` — case-sensitive entity-name lookup; global substring calls require at least three Unicode characters, while `exact=true` permits shorter complete names |
| `find_symbol` | POST | `/api/v0/code/symbols/search` |
| `inspect_code_inventory` | POST | `/api/v0/code/structure/inventory` |
| `investigate_import_dependencies` | POST | `/api/v0/code/imports/investigate` |
| `inspect_call_graph_metrics` | POST | `/api/v0/code/call-graph/metrics` |
| `investigate_code_topic` | POST | `/api/v0/code/topics/investigate` |
| `investigate_hardcoded_secrets` | POST | `/api/v0/code/security/secrets/investigate` |
| `investigate_dead_code` | POST | `/api/v0/code/dead-code/investigate` |
| `get_code_relationship_story` | POST | `/api/v0/code/relationships/story` |
| `analyze_code_relationships` | POST | `/api/v0/code/relationships/story` for callers/callees/importers/class hierarchy/overrides, including explicit `find_cross_repo_*` variants; `/api/v0/code/call-chain` for call chains with optional exact entity selectors and `cross_repo` endpoint repository selectors; `/api/v0/code/relationships` for unresolved compatibility fallbacks |
| `find_dead_iac` | POST | `/api/v0/iac/dead` |
| `find_unmanaged_resources` | POST | `/api/v0/iac/unmanaged-resources` |
| `get_iac_management_status` | POST | `/api/v0/iac/management-status` |
| `explain_iac_management_status` | POST | `/api/v0/iac/management-status/explain` |
| `propose_terraform_import_plan` | POST | `/api/v0/iac/terraform-import-plan/candidates` |
| `list_aws_runtime_drift_findings` | POST | `/api/v0/aws/runtime-drift/findings` |
| `get_replatforming_rollups` | POST | `/api/v0/replatforming/rollups` |
| `find_unmanaged_resource_owners` | POST | `/api/v0/replatforming/ownership-packets` |
| `calculate_cyclomatic_complexity` | POST | `/api/v0/code/complexity` |
| `get_relationship_evidence` | GET | `/api/v0/evidence/relationships/{resolved_id}` |
| `list_admission_decisions` | GET | `/api/v0/evidence/admission-decisions` |
| `export_deployable_unit_packet` | GET | `/api/v0/investigations/deployable-unit/packet` |
| `build_evidence_citation_packet` | POST | `/api/v0/evidence/citations` |
| `list_package_registry_packages` | GET | `/api/v0/package-registry/packages` |
| `list_package_registry_versions` | GET | `/api/v0/package-registry/versions` |
| `list_package_registry_dependencies` | GET | `/api/v0/package-registry/dependencies` |
| `list_package_registry_correlations` | GET | `/api/v0/package-registry/correlations` |
| `list_ci_cd_run_correlations` | GET | `/api/v0/ci-cd/run-correlations` |
| `list_service_catalog_correlations` | GET | `/api/v0/service-catalog/correlations` |
| `list_codeowners_ownership` | GET | `/api/v0/codeowners/ownership` |
| `list_kubernetes_correlations` | GET | `/api/v0/kubernetes/correlations` |
| `list_secrets_iam_identity_trust_chains` | GET | `/api/v0/secrets-iam/identity-trust-chains` |
| `list_secrets_iam_privilege_posture_observations` | GET | `/api/v0/secrets-iam/privilege-posture-observations` |
| `list_secrets_iam_secret_access_paths` | GET | `/api/v0/secrets-iam/secret-access-paths` |
| `list_secrets_iam_posture_gaps` | GET | `/api/v0/secrets-iam/posture-gaps` |
| `count_secrets_iam_posture` | GET | `/api/v0/secrets-iam/posture-summary` |
| `get_incident_context` | GET | `/api/v0/incidents/{incident_id}/context` |
| `list_work_item_evidence` | GET | `/api/v0/work-items/evidence` |
| `derive_visualization_packet` | POST | `/api/v0/visualizations/derive` |
| `get_generation_lifecycle` | GET | `/api/v0/freshness/generations` |
| `get_changed_since` | GET | `/api/v0/freshness/changed-since` (repository-scope delta diffing a prior generation against the current active generation by `stable_fact_key`; per-category added/updated/unchanged/retired/superseded counts plus bounded sample handles; unknown scope returns not-found, no current active generation returns an explicit unavailable diff, retention-pruned baselines return `unavailable_reason=retention_expired` with `get_generation_lifecycle` as the bounded next check, and a delta generation at either end returns `unavailable_reason=baseline_not_comparable`) |
| `get_repository_freshness` | GET | `/api/v0/repositories/{repo_id}/freshness` (repository commit receipt and build-completeness verdict; routed by `dispatch_repositories.go`, not the freshness router) |
| `get_service_changed_since` | GET | `/api/v0/freshness/services/changed-since` (service-scope delta diffing a prior service materialization generation against the current active generation by generation-independent `service_evidence_key`; reports the ownership, deployment, runtime, dependencies, docs, incidents, and vulnerabilities families; per-family added/updated/unchanged/retired/superseded counts plus bounded sample handles; unknown service returns service-not-found, no current active generation returns an explicit unavailable diff; optional `scope_id` picks one ingestion scope's lineage, and more than one readable lineage with an active generation (or, when none is active, more than one attributed lineage) without it returns a 409 `ambiguous` error listing the readable scope ids, #6475) |
| `get_vulnerability_scanner_read_contract` | GET | `/api/v0/supply-chain/vulnerability-scanner/contract` |
| `list_supply_chain_impact_findings` | GET | `/api/v0/supply-chain/impact/findings` (accepts repository ids or human repository selectors plus scanner filters such as `advisory_id`, `image_ref`, `ecosystem`, `service_id`, `workload_id`, `environment`, `severity`, `profile`, `include_suppressed`, and `suppression_state`; precise rows require supported exact-version evidence such as npm, Maven, Cargo, Pub `pubspec.lock`, NuGet, or Swift `Package.resolved`, and each row carries a `suppression` block with state, source, justification, author, timestamps, reason, and VEX provenance) |
| `list_sbom_attestation_attachments` | GET | `/api/v0/supply-chain/sbom-attestations/attachments` (use `subject_digest`/`digest`, `document_id`, `document_digest`, repository `repository_id` or selector, `workload_id`, or `service_id` for SBOM proof; scoped repository/workload/service reads return explicit missing image or image-to-SBOM evidence instead of silently flattening empty pages) |
| `list_advisory_evidence` | GET | `/api/v0/supply-chain/advisories/evidence` |
| `explain_supply_chain_impact` | GET | `/api/v0/supply-chain/impact/explain` |
| `export_supply_chain_impact_packet` | GET | `/api/v0/investigations/supply-chain/impact/packet` |
| `export_cloud_runtime_drift_packet` | GET | `/api/v0/investigations/drift/packet` |
| `list_security_alert_reconciliations` | GET | `/api/v0/supply-chain/security-alerts/reconciliations` (accepts repository ids or human repository selectors; rows include Eshu-owned `eshu_package.observed_version` when installed-version evidence exists) |
| `count_repositories_by_language` | GET | `/api/v0/repositories/by-language?limit=0` |
| `list_repositories_by_language` | GET | `/api/v0/repositories/by-language` |
| `get_repository_language_inventory` | GET | `/api/v0/repositories/language-inventory` |
| `get_repository_stats` | GET | `/api/v0/repositories/{repo_id}/stats` |
| `get_repo_summary` | GET | `/api/v0/repositories/{repo_id}/stats` — lightweight identity and coverage summary (file count, languages, entity count, entity types, indexing coverage state); use before `get_repo_context` for a quick overview. Requires exactly one of `repo_id` (preferred) or the legacy `repo_name` alias; neither is forced in the schema (so a `repo_name`-only call validates), and dispatch returns a clear error when both are absent |
| `get_repo_context` | GET | `/api/v0/repositories/{repo_id}/context` — full enriched context including entry points, infrastructure, relationships, API surface, and deployment evidence |
| `get_repo_story` | GET | `/api/v0/repositories/{repo_id}/story` |
| `investigate_contract_impact` | POST | `/api/v0/impact/contracts` |
| `investigate_change_surface` | POST | `/api/v0/impact/change-surface/investigate` |
| `analyze_pre_change_impact` | POST | `/api/v0/impact/pre-change` |
| `plan_developer_change` | POST | `/api/v0/impact/developer-change-plan` |
| `investigate_resource` | POST | `/api/v0/impact/resource-investigation` |
| `resolve_entity` | POST | `/api/v0/entities/resolve` — exact case-sensitive lookup; global calls require one supported `type`, except canonical `content-entity:` handles and workload resolution |
| `get_service_story` | GET | `/api/v0/services/{service_selector}/story` (canonical `workload:*` inputs also pass `service_id`; `service_name` plus `repo`/`repository_id`/`repo_id` forwards repository-scoped story selection to HTTP) |
| `investigate_service` | GET | `/api/v0/investigations/services/{service_name}` |
| `search_php_configuration` | POST | `/api/v0/content/php-config/search` — guarded directive or server-mode declarations |
| `get_file_content` | POST | `/api/v0/content/files/read` |
| `list_documentation_findings` | GET | `/api/v0/documentation/findings` with scope, repo, target, and service filters |
| `list_documentation_facts` | GET | `/api/v0/documentation/facts` with scope, repo, target, service, source, document, section, and search filters; reads the active generation unless `generation_id` names another |
| `list_semantic_documentation_observations` | GET | `/api/v0/semantic/documentation-observations` with scope, source, provider, prompt, freshness, policy, and admission filters |
| `list_semantic_code_hints` | GET | `/api/v0/semantic/code-hints` with repo, path, entity, provider, prompt, freshness, policy, and corroboration filters |
| `search_semantic_context` | POST | `/api/v0/search/semantic` with repository id, query, mode, limit, timeout, optional service/workload/environment anchors, and source-kind filters |
| `get_documentation_evidence_packet` | GET | `/api/v0/documentation/findings/{finding_id}/evidence-packet` |
| `check_documentation_evidence_packet_freshness` | GET | `/api/v0/documentation/evidence-packets/{packet_id}/freshness` |
| `list_query_playbooks` | GET | `/api/v0/query-playbooks` |
| `resolve_query_playbook` | POST | `/api/v0/query-playbooks/resolve` |
| `list_investigation_workflows` | GET | `/api/v0/investigation-workflows` |
| `resolve_investigation_workflow` | POST | `/api/v0/investigation-workflows/resolve` |
| `list_component_extensions` | GET | `/api/v0/component-extensions` with defaulted `limit` |
| `get_component_extension_diagnostics` | GET | `/api/v0/component-extensions/{component_id}/diagnostics` |
| `list_collector_extraction_readiness` | GET | `/api/v0/collector-extraction-readiness` with defaulted `limit` |
| `get_collector_extraction_readiness` | GET | `/api/v0/collector-extraction-readiness/{family}` |
| `list_collectors` | GET | `/api/v0/status/collectors` |
| `list_ingesters` | GET | `/api/v0/status/ingesters` |
| `get_hosted_readiness` | GET | `/api/v0/status/hosted-readiness` with fail-closed queue, collector, projection, and API/MCP query-readback checks |
| `get_collector_readiness` | GET | `/api/v0/status/collector-readiness` with per-collector-family promotion state, reducer readback, evidence counts, last proof time, blockers, and recommended next action; redacted |
| `get_hosted_governance_status` | GET | `/api/v0/status/governance` with redacted governance mode, policy state, readiness booleans, aggregate counts, and low-cardinality reasons |
| `get_semantic_capability_status` | GET | `/api/v0/status/semantic-extraction` with redacted provider profile, semantic queue, budget, and audit status when configured |
| `get_answer_narration_status` | GET | `/api/v0/status/answer-narration` with disabled-by-default narration posture, deterministic fallback state, retention posture, and validator reason codes |
| `trace_deployment_chain` | POST | `/api/v0/impact/trace-deployment-chain` |
| `investigate_deployment_config` | POST | `/api/v0/impact/deployment-config-influence` |

`get_repo_context`, `get_relationship_evidence`, `get_code_relationship_story`,
and the direct relationship fallback for `analyze_code_relationships` forward
HTTP relationship confidence metadata unchanged. Correlation relationship rows
carry `confidence_basis` (`evidence_constant`, `evidence_aggregate`, or
`assertion_override`) alongside `confidence`, `resolution_source`,
`evidence_type`, and `evidence_kinds`; code relationship tools keep using
`resolution_method`.
Relationship-story rows preserve the HTTP `provenance` block in
`structuredContent` with confidence state, method/source family, reason, truth
state, and derived/heuristic/unsupported flags.
`get_code_relationship_story` and the relationship-story routes behind
`analyze_code_relationships` accept `min_confidence` as the transport field for
the HTTP/MCP confidence-floor contract. Omitted means no floor; accepted values
are numbers from `0` through `1`; filtering applies only to returned rows and
never changes canonical graph truth or relationship evidence drilldowns.
No-Regression Evidence: `go test ./internal/mcp -run
'TestResolveRouteMaps(RelationshipStoryMinConfidence|AnalyzeCallersMinConfidence)|TestRelationshipToolsAdvertiseMinConfidence|TestRelationshipStoryToolsAdvertiseProvenanceOutput'
-count=1` covers MCP schema advertisement and dispatch forwarding.

`investigate_import_dependencies` passes paging and scope arguments directly to
the HTTP handler. The handler rejects negative bounds, accepts `target_file`
only for cycle and cross-module queries, and returns exactly one canonical row
key for each `query_type`: `dependencies`, `modules`, `cycles`, or
`cross_module_calls`. Internal candidate scans stop at 25,000 rows and return
HTTP 422 with an instruction to narrow scope rather than raising the timeout.

`inspect_call_graph_metrics` keeps MCP as transport for recursive and
hub-function prompts. Dispatch forwards `repo_id`, `language`, `metric_type`,
`limit`, and `offset` to the HTTP handler; the query layer owns graph bounds,
truth metadata, source handles, and the canonical `functions` row key.

`build_evidence_citation_packet` keeps MCP as transport only: dispatch forwards
the caller's bounded handle array to the HTTP evidence route. The advertised
schema caps input at 500 handles and the query handler hydrates at most 50
citations per packet. Route selection for this tool, together with `get_file_content`, `get_file_lines`, `search_file_content`, and `search_entity_content`, lives in the `content` child package. `derive_visualization_packet` follows the same
transport-only pattern for visualization packets: MCP forwards the caller's
already-authorized source response and optional source truth to
`POST /api/v0/visualizations/derive`, preserving the returned canonical
envelope as structured content and as the envelope resource.

Package-registry tools keep MCP as transport too. Ownership candidates,
package-version publication evidence, and manifest-backed consumption all come
from the query handler; the `package/registry` child owns the bounded request
selection and `dispatch_package_registry.go` adapts it into a transport route.

Repository-language tools keep MCP as transport only. The HTTP query layer owns
the content-index aggregate, language-family aliases, paging, truncation, and
truth metadata so MCP clients do not have to fan out through every repository
coverage response to answer inventory questions.

`get_repository_stats` also stays transport-only. The HTTP query layer resolves
human repository selectors to a canonical repository id, performs only a bounded
repository identity lookup in the graph, and reads file/entity/language/type
counts from content-store coverage when that read model is available. The HTTP
route owns the 2s stats read budget; coverage timeouts return explicit
`coverage.partial_results`, `coverage.truncated`, `coverage.timeout`, and
`coverage.missing_evidence` metadata instead of MCP inventing totals or retrying
a whole-graph traversal.

`analyze_pre_change_impact` stays transport-only. MCP forwards base/head
provenance, changed paths, structured file changes, optional target/topic
scope, and bounds to `/api/v0/impact/pre-change`. The HTTP query layer owns
path validation, change-surface evidence resolution, missing-evidence reasons,
truth labels, truncation, and AnswerPacket metadata.

`plan_developer_change` follows the same transport-only boundary. MCP forwards
the caller-derived changed files, optional base/head provenance, optional
developer intent, target/topic scope, and bounds to
`/api/v0/impact/developer-change-plan`. The HTTP query layer owns the read-only
`developer_change_plan.v1` action list, patch guidance, missing-evidence
blocking, recommended tests, bounded next calls, truth labels, truncation, and
AnswerPacket metadata.

`investigate_contract_impact` is transport-only. MCP forwards family, provider
repository, consumer repository, route, topic, gRPC service name, HTTP method,
and bounds to `/api/v0/impact/contracts`. The HTTP query layer owns the
deterministic evidence boundary, capability gating, graph reads, unsupported
topic/gRPC family states, truncation, and truth envelope.

No-Regression Evidence: pre-change MCP schema and dispatch parity are covered
by `go test ./internal/mcp -run 'TestPreChangeImpact|TestDeveloperChangePlan|TestContractImpact|TestResolveRouteMaps(PreChange|DeveloperChangePlan|ContractImpact)|TestReadOnlyTools' -count=1`.

No-Observability-Change: pre-change, developer-change-plan, and contract-impact
MCP dispatch add no runtime span, metric, datastore access, graph traversal, or
retry loop. They preserve the existing MCP `dispatch tool` debug log and
canonical HTTP envelope.

Supply-chain tools keep the same transport-only contract. The impact explain
tool forwards one `finding_id` or advisory/CVE plus package, repository, or
image-digest scope to the HTTP route; MCP does not hydrate evidence or infer
reachability itself. The SBOM/attestation tool forwards repository IDs or human
repository selectors to the HTTP route, which resolves them before its bounded
attachment read. Its schema accepts only the reducer-owned attachment statuses,
including `ambiguous_subject`, so multi-subject attestations stay visible
without becoming canonical image attachments.
`list_advisory_evidence` forwards repository, service, and workload scope to
HTTP so the query layer can derive advisory anchors from reducer-owned impact
findings before reading source-only advisory facts. MCP must not synthesize
advisory evidence from provider alert fields or fetch impact findings itself.
Provider security alert reconciliation stays transport-only as well. MCP maps
repository ids or human repository selectors, provider, package, CVE, or GHSA
anchors plus optional state/status filters to the HTTP read model and preserves
the response shape that keeps provider alert state separate from Eshu-owned
package and impact state. List rows carry Eshu dependency evidence under
`eshu_package`; MCP must not copy provider payload fields into
`observed_version`. Count and list responses include the same provider-source
coverage summary as HTTP: a truncated open-alert read is `target_incomplete`, and
an empty answer the collector has not earned carries `coverage.reasons` (#8803).

IaC management tools also keep MCP as transport only. The HTTP query layer adds
`safety_gate`, `safety_summary`, import-plan candidate shaping, and
sensitive-value redaction before the envelope reaches MCP, so tool callers see
the same review-required and refused Terraform import-plan actions as HTTP
callers.

Documentation tools keep the same transport-only contract. Target-scoped
finding and fact reads forward `repo`, `target_kind`, `target_id`, and
`service_id` to HTTP so MCP callers see the same `coverage`, `related_facts`,
and `missing_evidence` metadata as API clients when raw target documentation
facts exist without admissible findings. Source-only external documentation
facts are also preserved from HTTP as aggregate `coverage.source_only_count`,
`coverage.source_only_fact_kinds`, and `target_link_not_modeled` missing
evidence; MCP does not infer target documentation from Confluence names or
generic mentions. Documentation fact list calls also preserve the HTTP
bounded-list metadata: `count`, `limit`, `truncated`, `missing_evidence`,
`states`, and `next_cursor` when a scoped fact page has more rows.

Semantic evidence tools are transport-only as well. They forward filters to the
HTTP semantic evidence routes and preserve the canonical envelope so callers
can read `truth.basis`, `truth.freshness`, provider profile, prompt version,
redaction version, policy state, admission state, and corroboration state
without raw prompt payloads, credentials, or provider responses. Code hints are
listed only by `list_semantic_code_hints`; deterministic code and relationship
tools do not mix them into graph-truth answers.

Component extension tools stay transport-only too. They forward inventory and
diagnostic requests to the HTTP component-extension routes, which read the
runtime's configured component registry and trust policy. Inventory forwards a
bounded `limit` and returns `count`, `total_count`, `limit`, and `truncated`.
The response carries component ID, version, manifest digest, lifecycle states,
activation `config_handle` values, and policy diagnostics; MCP never exposes
local manifest paths, activation config paths, or treats community index
membership as trust.
