# #5167: scoped-token full-catalog MCP sweep, live run

Acceptance item: "Every MCP tool succeeds with a personal token whose role
covers it, on a live stack (full-catalog sweep, scripted)", plus the other half,
that a ledger route refuses a scoped token and says why.

Command: `bash scripts/run-auth-mcp-e2e.sh --module catalog-sweep` (see
[Scoped-Token MCP Catalog Sweep](../../public/run-locally/mcp-catalog-sweep.md)),
run with the compose project name overridden to `eshu-e2e-auth-mcp-5167n4j`.

- **Stack commit:** `7664283f3`, clean worktree. That SHA and `4aa1e23e0`
  below are local pre-rebase commits that were never pushed, so they do not
  resolve on the remote: the sweep itself is in #7204 (`348861541`) and the
  Neo4j stack change is in this PR. The run is on
  **Neo4j**. `docker-compose.e2e.yaml` now runs its graph on Neo4j instead of
  NornicDB.
- **Images:**
  - graph: `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
    (Neo4j 2026.08.1, the digest `docker-compose.neo4j.yml` pins);
  - Postgres: `postgres@sha256:6c538e7206ea40ff740ef27883529390a690b6ead6ba96b44c67a9f7c638e8fd`
    (`postgres:18-alpine`);
  - Eshu: built from that commit by `up --build`.
- **Host:** the pinned Neo4j digest is amd64-only, so it ran emulated on an
  arm64 host at a load average of about 17.7. Every absolute time below is from
  that host, not native.
- **Exit:** 1, with 9/10 steps. The one failing step is the per-tool step, which
  failed on the two #7215 rows below. The stack was torn down afterwards.

The earlier run at `4aa1e23e0` was on NornicDB. It predates #7183, #7193 and
#7191, and this run supersedes it and its per-row table.

## Result

- 166 tools listed to the scoped personal token; 167 calls (one tool has two
  cases); **165/167 passed**.
- **Two rows fail on a product defect, tracked as #7215.** On Neo4j,
  `find_infra_resources` (`POST /api/v0/infra/resources/search`) and
  `analyze_infra_relationships` (`POST /api/v0/infra/relationships`) answer
  `backend_timeout` ("graph query exceeded its deadline") to a scoped token.
  The graph is nearly empty, so execution is not the problem; planning is.
  Measured on the kept stack with `EXPLAIN`/`PROFILE` (Neo4j's own `Time:`,
  host load average 17.7, so these are ranges):
  - the scoped search query took 21.5-38.0 s to plan cold and 5.1-6.0 s warm,
    against a 10 s bounded read; the same query unscoped took about 6.0 s cold
    and 157 ms warm;
  - each scoped per-label relationships anchor took about 1 s to plan, and
    about 16 anchors share one 10 s budget.

  The cause is suspected, not yet proven (#7215): the scoped-grant predicate
  (`EXISTS` subqueries plus inline map disjunctions) repeated across every label
  branch. What is measured is that scoped planning is several times slower than
  unscoped on the same emulated, loaded host; no `PROFILE` proof of the
  predicate as the cause exists yet. The rows' expectations are left as they
  are: accepting a timeout would hide the defect.
- **133 calls must answer `ok`:** 132 did. The 133rd is `find_infra_resources`
  above.
- **30 tolerant calls** prove only that the route is mounted and not refused by
  the route policy:
  - 22 answered a typed `not_found` for a subject the fixture does not seed;
  - 7 are gated by the stack profile: `unsupported_capability` x4,
    `ask_default_off` x1 (`ESHU_ASK_ENABLED` unset, matched on its "ask is not
    enabled" body) and `component_registry_unavailable` x2
    (`ESHU_COMPONENT_HOME` unset);
  - the 30th is `analyze_infra_relationships` above.
- **4 ledger calls** answered the route-policy 403 with a tool description that
  discloses it: 2 pending row filtering (`get_service_changed_since`,
  `search_registry_bundles`) and 2 shared-key only (`execute_cypher_query`,
  `visualize_graph_query`).
- **Since this run:** #7194 promoted `POST /api/v0/code/bundles` off the pending
  ledger, so `search_registry_bundles` is now an allowlisted row that must
  answer `ok` (a scoped caller reads only `visibility = 'public'` packages, and
  the fixture seeds none, so the expected answer is an empty page). **That row
  has not been live-run since its promotion**: the run above recorded it as the
  ledger 403. The live figures in this document (165/167, 132 of 133, 4 ledger)
  are that run's and are kept as measured. The current static split is
  134 `ok`, 30 tolerant, 3 ledger (1 pending row filtering, 2 shared-key
  only).
- **The rows promoted off the pending ledger since the NornicDB run now pass
  live:**
  - `get_index_status` (#7193): `ok`;
  - `trace_resource_to_code` and `trace_exposure_path` (#7191): `ok`;
  - `explain_dependency_path` (#7191): `not_found`, which it accepts because
    its endpoints are unseeded;
  - `analyze_code_relationships[who_modifies]` (#7183): `not_found`, with
    the all-scope control below.
- **The token was scoped, not shared:** the stack has no `ESHU_API_KEY`, and
  `GET /api/v0/auth/profile` showed roles
  `["e2e_catalog_sweep_reader","owner"]` with the permission catalog enforced.

### All-scope controls

The five tolerant rows that answered `not_found` and name the granted
repository among their arguments are replayed through the all-scope console session. The replay
uses the request the MCP dispatcher actually sends (`resolveRoute`'s body and
query, emitted by `TestCatalogSweepPolicy`), not the raw tool arguments.
All five answered the same typed 404:

```text
analyze_code_relationships[who_modifies] POST /api/v0/code/relationships: scoped=not_found all-scope=404 SAME
calculate_cyclomatic_complexity[default] POST /api/v0/code/complexity: scoped=not_found all-scope=404 SAME
get_file_content[default] POST /api/v0/content/files/read: scoped=not_found all-scope=404 SAME
get_file_lines[default] POST /api/v0/content/files/lines: scoped=not_found all-scope=404 SAME
trace_route_callers[default] POST /api/v0/code/routes/callers: scoped=not_found all-scope=404 SAME
```

The first Neo4j run failed the `who_modifies` control with `400 entity_id or
name is required`. That was a harness defect, not a product one. The dispatcher
sends `target` as `entity_id` (and drops `repo_id` for this query type, #7216),
so for `who_modifies` the replay carries no repository and proves only that the
entity is absent for every caller. The old replay posted the raw arguments. The other 17 tolerant `not_found`
rows name a `sweep-seed-missing` id that no seed creates, so they claim only
the weaker result.

### Negative control

The scoped `list_indexed_repositories` returned only `e2e-seed-repo-default`.
The all-scope session listed both it and `e2e-seed-repo-ungranted`. Per
single-repository tool (granted / ungranted via the scoped token / ungranted
via the all-scope session):

| tool | granted | ungranted | all-scope (ungranted) |
| --- | --- | --- | --- |
| get_repo_summary | ok | not_found | 200 |
| get_repo_context | ok | not_found | 200 |
| get_repo_story | ok | not_found | 200 |
| get_repository_stats | ok | not_found | 200 |
| get_repository_coverage | ok | not_found | 200 |
| get_repository_freshness | ok | not_found | 200 |

## Shape-A empty-grant check, fixed and rerun

`assertMcpToolCallRowFiltered` (and the twin check in the leakage module) read
`structuredContent.repositories`, but the MCP payload is the truth envelope, so
the list is at `structuredContent.data.repositories` and the zero-rows assertion
could not fail. Both now read `data` through `repositoryListFromEnvelope` and
throw on an unrecognised shape. `bash scripts/run-auth-mcp-e2e.sh --module
shapeA` at `4aa1e23e0`: exit 0, 9/9 steps, including
`shapeA_mcp_tool_call_row_filtered`: "166 tools listed; scoped personal token
(empty grant) correctly saw 0 repositories (total=0)". The seeded repository
exists in the graph, so the fixed check reads a real envelope and finds none.

That rerun was on NornicDB. On Neo4j at `7664283f3`, the full suite
(`bash scripts/run-auth-mcp-e2e.sh`, no module) passed 40/40 steps. That
includes `shapeA_mcp_tool_call_row_filtered` ("scoped personal token (empty
grant) correctly saw 0 repositories (total=0)") and
`leakage_cross_scope_row_filter_non_vacuous`. It also passed
`scripts/verify-auth-mcp-e2e-manifest.sh` against the named baseline (40 steps,
in order, within the runtime bound). The SSO suite (`scripts/run-auth-e2e.sh`)
passed 24/24 on the same Neo4j stack definition.
`scripts/verify-auth-mcp-e2e-sensitivity.sh` first failed to boot. Neo4j went
unhealthy under the inherited 5 s x 10 healthcheck: the pinned digest is
amd64-only, so it ran emulated on an arm64 host with load average 17. After the
e2e `neo4j` service got `start_period: 120s` and 30 retries, the gate passed
(real gate PASS, mutated FAIL on the gate, restored PASS). The Eshu images were
unchanged; only the healthcheck changed.

## Seed and limits

The seed consists of the graph Repository nodes (written with
`docker compose exec neo4j cypher-shell`), one repository-catalog scope per
repository, one `state_snapshot` scope, and a role granting every feature and
data class on the granted repository and the state scope. There is no indexed
content, so the 22 `not_found` rows prove only that the route is mounted and
not refused by the route policy, not a populated answer. The 4 routes refused in
this run are the closed `pendingRowFilteringRoutes` and `sharedKeyOnlyRoutes` ledgers.

## Per-tool table

Run at `7664283f3` on Neo4j:

```text
tool                                             route                                                                    expected                                                  actual                          verdict
-----------------------------------------------  -----------------------------------------------------------------------  --------------------------------------------------------  ------------------------------  -------
analyze_code_relationships[find_callers]         POST /api/v0/code/relationships/story                                    success (ok)                                              ok                              PASS
analyze_code_relationships[who_modifies]         POST /api/v0/code/relationships                                          success (ok|not_found)                                    not_found                       PASS
analyze_infra_relationships                      POST /api/v0/infra/relationships                                         success (ok|not_found|scope_not_found|service_not_found)  backend_timeout                 FAIL
analyze_pre_change_impact                        POST /api/v0/impact/pre-change                                           success (ok)                                              ok                              PASS
ask                                              POST /api/v0/ask                                                         success (ok|ask_default_off)                              ask_default_off                 PASS
build_evidence_citation_packet                   POST /api/v0/evidence/citations                                          success (ok)                                              ok                              PASS
calculate_cyclomatic_complexity                  POST /api/v0/code/complexity                                             success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
check_documentation_evidence_packet_freshness    GET /api/v0/documentation/evidence-packets/sweep-seed-missing/freshness  success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
compare_code_paths                               POST /api/v0/code/call-chain/compare                                     success (ok|unsupported_capability)                       unsupported_capability          PASS
compare_environments                             POST /api/v0/compare/environments                                        success (ok)                                              ok                              PASS
compose_replatforming_plan                       POST /api/v0/replatforming/plans                                         success (ok)                                              ok                              PASS
count_ci_cd_run_correlations                     GET /api/v0/ci-cd/run-correlations/count                                 success (ok)                                              ok                              PASS
count_container_image_identities                 GET /api/v0/supply-chain/container-images/identities/count               success (ok)                                              ok                              PASS
count_documentation_findings                     GET /api/v0/documentation/findings/count                                 success (ok)                                              ok                              PASS
count_infra_resources                            GET /api/v0/infra/resources/count                                        success (ok)                                              ok                              PASS
count_package_registry_packages                  GET /api/v0/package-registry/packages/count                              success (ok)                                              ok                              PASS
count_repositories_by_language                   GET /api/v0/repositories/by-language                                     success (ok)                                              ok                              PASS
count_sbom_attestation_attachments               GET /api/v0/supply-chain/sbom-attestations/attachments/count             success (ok)                                              ok                              PASS
count_secrets_iam_posture                        GET /api/v0/secrets-iam/posture-summary                                  success (ok)                                              ok                              PASS
count_security_alert_reconciliations             GET /api/v0/supply-chain/security-alerts/reconciliations/count           success (ok)                                              ok                              PASS
count_supply_chain_impact_findings               GET /api/v0/supply-chain/impact/findings/count                           success (ok)                                              ok                              PASS
derive_visualization_packet                      POST /api/v0/visualizations/derive                                       success (ok)                                              ok                              PASS
dispatch_cfg_summary                             POST /api/v0/code/flow/cfg-summary                                       success (ok)                                              ok                              PASS
dispatch_pdg_summary                             POST /api/v0/code/flow/pdg-summary                                       success (ok)                                              ok                              PASS
dispatch_reaching_def                            POST /api/v0/code/flow/reaching-def                                      success (ok)                                              ok                              PASS
dispatch_taint_path                              POST /api/v0/code/flow/taint-path                                        success (ok)                                              ok                              PASS
execute_cypher_query                             POST /api/v0/code/cypher                                                 403 disclosed (shared_key_only)                           route_denied_403 (disclosed)    PASS
execute_language_query                           POST /api/v0/code/language-query                                         success (ok)                                              ok                              PASS
explain_dependency_path                          POST /api/v0/impact/explain-dependency-path                              success (ok|not_found)                                    not_found                       PASS
explain_iac_management_status                    POST /api/v0/iac/management-status/explain                               success (ok)                                              ok                              PASS
explain_supply_chain_impact                      GET /api/v0/supply-chain/impact/explain                                  success (ok)                                              ok                              PASS
export_cloud_runtime_drift_packet                GET /api/v0/investigations/drift/packet                                  success (ok)                                              ok                              PASS
export_deployable_unit_packet                    GET /api/v0/investigations/deployable-unit/packet                        success (ok)                                              ok                              PASS
export_supply_chain_impact_packet                GET /api/v0/investigations/supply-chain/impact/packet                    success (ok)                                              ok                              PASS
find_blast_radius                                POST /api/v0/impact/blast-radius                                         success (ok)                                              ok                              PASS
find_change_surface                              POST /api/v0/impact/change-surface                                       success (ok)                                              ok                              PASS
find_code                                        POST /api/v0/code/search                                                 success (ok)                                              ok                              PASS
find_code_divergence                             POST /api/v0/code/divergence/findings                                    success (ok|unsupported_capability)                       unsupported_capability          PASS
find_cross_repo_dead_code                        POST /api/v0/code/dead-code/cross-repo                                   success (ok)                                              ok                              PASS
find_dead_code                                   POST /api/v0/code/dead-code                                              success (ok)                                              ok                              PASS
find_dead_iac                                    POST /api/v0/iac/dead                                                    success (ok)                                              ok                              PASS
find_function_call_chain                         POST /api/v0/code/call-chain                                             success (ok)                                              ok                              PASS
find_infra_resources                             POST /api/v0/infra/resources/search                                      success (ok)                                              backend_timeout                 FAIL
find_most_complex_functions                      POST /api/v0/code/complexity                                             success (ok)                                              ok                              PASS
find_symbol                                      POST /api/v0/code/symbols/search                                         success (ok)                                              ok                              PASS
find_unmanaged_resource_owners                   POST /api/v0/replatforming/ownership-packets                             success (ok)                                              ok                              PASS
find_unmanaged_resources                         POST /api/v0/iac/unmanaged-resources                                     success (ok)                                              ok                              PASS
get_answer_narration_status                      GET /api/v0/status/answer-narration                                      success (ok)                                              ok                              PASS
get_capability_catalog                           GET /api/v0/capabilities                                                 success (ok)                                              ok                              PASS
get_changed_since                                GET /api/v0/freshness/changed-since                                      success (ok)                                              ok                              PASS
get_ci_cd_run_correlation_inventory              GET /api/v0/ci-cd/run-correlations/inventory                             success (ok)                                              ok                              PASS
get_code_relationship_story                      POST /api/v0/code/relationships/story                                    success (ok)                                              ok                              PASS
get_collector_extraction_readiness               GET /api/v0/collector-extraction-readiness/pagerduty                     success (ok)                                              ok                              PASS
get_collector_readiness                          GET /api/v0/status/collector-readiness                                   success (ok)                                              ok                              PASS
get_component_extension_diagnostics              GET /api/v0/component-extensions/dev.eshu.collector.aws/diagnostics      success (ok|component_registry_unavailable)               component_registry_unavailable  PASS
get_container_image_identity_inventory           GET /api/v0/supply-chain/container-images/identities/inventory           success (ok)                                              ok                              PASS
get_documentation_evidence_packet                GET /api/v0/documentation/findings/sweep-seed-missing/evidence-packet    success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_documentation_finding_inventory              GET /api/v0/documentation/findings/inventory                             success (ok)                                              ok                              PASS
get_ecosystem_overview                           GET /api/v0/ecosystem/overview                                           success (ok)                                              ok                              PASS
get_entity_content                               POST /api/v0/content/entities/read                                       success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_entity_context                               GET /api/v0/entities/sweep-seed-missing/context                          success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_fact_schema_version                          GET /api/v0/fact-schema-versions/terraform_state_resource                success (ok)                                              ok                              PASS
get_file_content                                 POST /api/v0/content/files/read                                          success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_file_lines                                   POST /api/v0/content/files/lines                                         success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_freshness_causality                          GET /api/v0/status/freshness-causality                                   success (ok)                                              ok                              PASS
get_generation_lifecycle                         GET /api/v0/freshness/generations                                        success (ok)                                              ok                              PASS
get_graph_summary_packet                         POST /api/v0/ecosystem/graph-summary                                     success (ok)                                              ok                              PASS
get_hosted_governance_status                     GET /api/v0/status/governance                                            success (ok)                                              ok                              PASS
get_hosted_readiness                             GET /api/v0/status/hosted-readiness                                      success (ok)                                              ok                              PASS
get_iac_management_status                        POST /api/v0/iac/management-status                                       success (ok)                                              ok                              PASS
get_incident_context                             GET /api/v0/incidents/sweep-seed-missing/context                         success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_index_status                                 GET /api/v0/index-status                                                 success (ok)                                              ok                              PASS
get_infra_resource_inventory                     GET /api/v0/infra/resources/inventory                                    success (ok)                                              ok                              PASS
get_ingester_status                              GET /api/v0/status/ingesters/repository                                  success (ok)                                              ok                              PASS
get_operator_control_plane                       GET /api/v0/status/operator-control-plane                                success (ok)                                              ok                              PASS
get_package_registry_package_inventory           GET /api/v0/package-registry/packages/inventory                          success (ok)                                              ok                              PASS
get_relationship_evidence                        GET /api/v0/evidence/relationships/sweep-seed-missing                    success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_replatforming_rollups                        POST /api/v0/replatforming/rollups                                       success (ok)                                              ok                              PASS
get_repo_context                                 GET /api/v0/repositories/$REPO/context                                   success (ok)                                              ok                              PASS
get_repo_story                                   GET /api/v0/repositories/$REPO/story                                     success (ok)                                              ok                              PASS
get_repo_summary                                 GET /api/v0/repositories/$REPO/stats                                     success (ok)                                              ok                              PASS
get_repository_coverage                          GET /api/v0/repositories/$REPO/coverage                                  success (ok)                                              ok                              PASS
get_repository_freshness                         GET /api/v0/repositories/$REPO/freshness                                 success (ok)                                              ok                              PASS
get_repository_language_inventory                GET /api/v0/repositories/language-inventory                              success (ok)                                              ok                              PASS
get_repository_stats                             GET /api/v0/repositories/$REPO/stats                                     success (ok)                                              ok                              PASS
get_sbom_attestation_attachment_inventory        GET /api/v0/supply-chain/sbom-attestations/attachments/inventory         success (ok)                                              ok                              PASS
get_security_alert_reconciliation_inventory      GET /api/v0/supply-chain/security-alerts/reconciliations/inventory       success (ok)                                              ok                              PASS
get_semantic_capability_status                   GET /api/v0/status/semantic-extraction                                   success (ok)                                              ok                              PASS
get_service_changed_since                        GET /api/v0/freshness/services/changed-since                             403 disclosed (pending_row_filtering)                     route_denied_403 (disclosed)    PASS
get_service_context                              GET /api/v0/services/sweep-seed-missing/context                          success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_service_intelligence_report                  GET /api/v0/services/sweep-seed-missing/intelligence-report              success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_service_story                                GET /api/v0/services/sweep-seed-missing/story                            success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_supply_chain_impact_inventory                GET /api/v0/supply-chain/impact/inventory                                success (ok)                                              ok                              PASS
get_surface_inventory                            GET /api/v0/surface-inventory                                            success (ok)                                              ok                              PASS
get_vulnerability_scanner_read_contract          GET /api/v0/supply-chain/vulnerability-scanner/contract                  success (ok)                                              ok                              PASS
get_workload_context                             GET /api/v0/workloads/sweep-seed-missing/context                         success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
get_workload_story                               GET /api/v0/workloads/sweep-seed-missing/story                           success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
inspect_call_graph_metrics                       POST /api/v0/code/call-graph/metrics                                     success (ok)                                              ok                              PASS
inspect_code_inventory                           POST /api/v0/code/structure/inventory                                    success (ok)                                              ok                              PASS
inspect_code_quality                             POST /api/v0/code/quality/inspect                                        success (ok)                                              ok                              PASS
investigate_change_surface                       POST /api/v0/impact/change-surface/investigate                           success (ok)                                              ok                              PASS
investigate_code_divergence                      POST /api/v0/code/divergence/investigate                                 success (ok|unsupported_capability)                       unsupported_capability          PASS
investigate_code_topic                           POST /api/v0/code/topics/investigate                                     success (ok)                                              ok                              PASS
investigate_contract_impact                      POST /api/v0/impact/contracts                                            success (ok)                                              ok                              PASS
investigate_dead_code                            POST /api/v0/code/dead-code/investigate                                  success (ok)                                              ok                              PASS
investigate_deployment_config                    POST /api/v0/impact/deployment-config-influence                          success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
investigate_hardcoded_secrets                    POST /api/v0/code/security/secrets/investigate                           success (ok)                                              ok                              PASS
investigate_import_dependencies                  POST /api/v0/code/imports/investigate                                    success (ok)                                              ok                              PASS
investigate_resource                             POST /api/v0/impact/resource-investigation                               success (ok)                                              ok                              PASS
investigate_service                              GET /api/v0/investigations/services/sweep-seed-missing                   success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
list_admission_decisions                         GET /api/v0/evidence/admission-decisions                                 success (ok)                                              ok                              PASS
list_advisory_evidence                           GET /api/v0/supply-chain/advisories/evidence                             success (ok)                                              ok                              PASS
list_aws_runtime_drift_findings                  POST /api/v0/aws/runtime-drift/findings                                  success (ok)                                              ok                              PASS
list_ci_cd_run_correlations                      GET /api/v0/ci-cd/run-correlations                                       success (ok)                                              ok                              PASS
list_cloud_resource_inventory                    GET /api/v0/cloud/inventory                                              success (ok)                                              ok                              PASS
list_cloud_runtime_drift_findings                POST /api/v0/cloud/runtime-drift/findings                                success (ok)                                              ok                              PASS
list_codeowners_ownership                        GET /api/v0/codeowners/ownership                                         success (ok)                                              ok                              PASS
list_collector_extraction_readiness              GET /api/v0/collector-extraction-readiness                               success (ok)                                              ok                              PASS
list_collectors                                  GET /api/v0/status/collectors                                            success (ok)                                              ok                              PASS
list_component_extensions                        GET /api/v0/component-extensions                                         success (ok|component_registry_unavailable)               component_registry_unavailable  PASS
list_container_image_identities                  GET /api/v0/supply-chain/container-images/identities                     success (ok)                                              ok                              PASS
list_container_image_tag_history                 GET /api/v0/images/tag-history                                           success (ok)                                              ok                              PASS
list_dead_letter_work_items                      POST /api/v0/admin/dead-letters/query                                    success (ok)                                              ok                              PASS
list_documentation_facts                         GET /api/v0/documentation/facts                                          success (ok)                                              ok                              PASS
list_documentation_findings                      GET /api/v0/documentation/findings                                       success (ok)                                              ok                              PASS
list_fact_schema_versions                        GET /api/v0/fact-schema-versions                                         success (ok)                                              ok                              PASS
list_indexed_repositories                        GET /api/v0/repositories                                                 success (ok)                                              ok                              PASS
list_ingesters                                   GET /api/v0/status/ingesters                                             success (ok)                                              ok                              PASS
list_investigation_workflows                     GET /api/v0/investigation-workflows                                      success (ok)                                              ok                              PASS
list_kubernetes_correlations                     GET /api/v0/kubernetes/correlations                                      success (ok)                                              ok                              PASS
list_observability_coverage_correlations         GET /api/v0/observability/coverage/correlations                          success (ok)                                              ok                              PASS
list_package_registry_correlations               GET /api/v0/package-registry/correlations                                success (ok)                                              ok                              PASS
list_package_registry_dependencies               GET /api/v0/package-registry/dependencies                                success (ok)                                              ok                              PASS
list_package_registry_packages                   GET /api/v0/package-registry/packages                                    success (ok)                                              ok                              PASS
list_package_registry_versions                   GET /api/v0/package-registry/versions                                    success (ok)                                              ok                              PASS
list_query_playbooks                             GET /api/v0/query-playbooks                                              success (ok)                                              ok                              PASS
list_reducer_input_invalid_facts                 POST /api/v0/admin/input-invalid-facts/query                             success (ok)                                              ok                              PASS
list_relationship_edges                          POST /api/v0/relationships/edges                                         success (ok)                                              ok                              PASS
list_repositories_by_language                    GET /api/v0/repositories/by-language                                     success (ok)                                              ok                              PASS
list_repository_files                            GET /api/v0/repositories/$REPO/tree                                      success (ok)                                              ok                              PASS
list_sbom_attestation_attachments                GET /api/v0/supply-chain/sbom-attestations/attachments                   success (ok)                                              ok                              PASS
list_secrets_iam_identity_trust_chains           GET /api/v0/secrets-iam/identity-trust-chains                            success (ok)                                              ok                              PASS
list_secrets_iam_posture_gaps                    GET /api/v0/secrets-iam/posture-gaps                                     success (ok)                                              ok                              PASS
list_secrets_iam_privilege_posture_observations  GET /api/v0/secrets-iam/privilege-posture-observations                   success (ok)                                              ok                              PASS
list_secrets_iam_secret_access_paths             GET /api/v0/secrets-iam/secret-access-paths                              success (ok)                                              ok                              PASS
list_security_alert_reconciliations              GET /api/v0/supply-chain/security-alerts/reconciliations                 success (ok)                                              ok                              PASS
list_semantic_code_hints                         GET /api/v0/semantic/code-hints                                          success (ok)                                              ok                              PASS
list_semantic_documentation_observations         GET /api/v0/semantic/documentation-observations                          success (ok)                                              ok                              PASS
list_service_catalog_correlations                GET /api/v0/service-catalog/correlations                                 success (ok)                                              ok                              PASS
list_supply_chain_impact_findings                GET /api/v0/supply-chain/impact/findings                                 success (ok)                                              ok                              PASS
list_terraform_config_state_drift_findings       POST /api/v0/terraform/config-state-drift/findings                       success (ok)                                              ok                              PASS
list_work_item_evidence                          GET /api/v0/work-items/evidence                                          success (ok)                                              ok                              PASS
plan_developer_change                            POST /api/v0/impact/developer-change-plan                                success (ok)                                              ok                              PASS
propose_terraform_import_plan                    POST /api/v0/iac/terraform-import-plan/candidates                        success (ok)                                              ok                              PASS
report_code_divergence                           POST /api/v0/code/divergence/report                                      success (ok|unsupported_capability)                       unsupported_capability          PASS
resolve_entity                                   POST /api/v0/entities/resolve                                            success (ok)                                              ok                              PASS
resolve_investigation_workflow                   POST /api/v0/investigation-workflows/resolve                             success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
resolve_query_playbook                           POST /api/v0/query-playbooks/resolve                                     success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
search_entity_content                            POST /api/v0/content/entities/search                                     success (ok)                                              ok                              PASS
search_file_content                              POST /api/v0/content/files/search                                        success (ok)                                              ok                              PASS
search_registry_bundles                          POST /api/v0/code/bundles                                                403 disclosed (pending_row_filtering)                     route_denied_403 (disclosed)    PASS
search_semantic_context                          POST /api/v0/search/semantic                                             success (ok)                                              ok                              PASS
trace_deployment_chain                           POST /api/v0/impact/trace-deployment-chain                               success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
trace_exposure_path                              POST /api/v0/impact/trace-exposure-path                                  success (ok)                                              ok                              PASS
trace_resource_to_code                           POST /api/v0/impact/trace-resource-to-code                               success (ok)                                              ok                              PASS
trace_route_callers                              POST /api/v0/code/routes/callers                                         success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
visualize_graph_query                            POST /api/v0/code/visualize                                              403 disclosed (shared_key_only)                           route_denied_403 (disclosed)    PASS

165/167 calls passed

all-scope controls (granted-repository not_found rows):
  analyze_code_relationships[who_modifies] POST /api/v0/code/relationships: scoped=not_found all-scope=404 SAME
  calculate_cyclomatic_complexity[default] POST /api/v0/code/complexity: scoped=not_found all-scope=404 SAME
  get_file_content[default] POST /api/v0/content/files/read: scoped=not_found all-scope=404 SAME
  get_file_lines[default] POST /api/v0/content/files/lines: scoped=not_found all-scope=404 SAME
  trace_route_callers[default] POST /api/v0/code/routes/callers: scoped=not_found all-scope=404 SAME
```
