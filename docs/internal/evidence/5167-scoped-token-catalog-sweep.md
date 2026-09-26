# #5167: scoped-token full-catalog MCP sweep, live run

Acceptance item: "Every MCP tool succeeds with a personal token whose role
covers it, on a live stack (full-catalog sweep, scripted)", plus the other half,
that a ledger route refuses a scoped token and says why.

Command: `bash scripts/run-auth-mcp-e2e.sh --module catalog-sweep` (see
[Scoped-Token MCP Catalog Sweep](../../public/run-locally/mcp-catalog-sweep.md)),
run twice with `ESHU_E2E_PROJECT_NAME=eshu-e2e-auth-mcp-5167close`, then the full
suite once (`ESHU_E2E_PROJECT_NAME=eshu-e2e-auth-mcp-5167closefull bash
scripts/run-auth-mcp-e2e.sh`). All figures below are author-run and
author-reported, from one host, not CI results.

- **Stack commit:** `2eb226f58` (`origin/main`, clean worktree,
  `claude/5167-final-sweep`). That includes #7222 (the e2e stack on Neo4j and the
  route-class guard), #7226 (#7215), #7221 (#7216), #7239 (#7231) and #7194
  (`search_registry_bundles`). The graph is **Neo4j**.
- **Images:**
  - graph: `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
    (the digest `docker-compose.neo4j.yml` pins; unchanged since the earlier runs);
  - Postgres: `postgres:18-alpine@sha256:6c538e7206ea40ff740ef27883529390a690b6ead6ba96b44c67a9f7c638e8fd`;
  - Eshu and MCP server: built from that commit by `up --build`, so they have
    local image IDs and no registry digest (`eshu-e2e-auth-mcp-5167close-eshu`
    `6e773f1de0c2`, `...-mcp-server` `fdba68c41a09`).
- **Host and platform:** an Apple Silicon (arm64) host running OrbStack. In the
  full-suite stack `docker exec ... neo4j uname -m` answered `x86_64`, so the
  Neo4j container ran **emulated amd64**; the Eshu image manifest is arm64
  (native). That platform was read on the third (full-suite) stack only; the
  compose file is the same for all three runs. The host was shared with other
  work: the 1-minute load average was 22.3 at the start of sweep run 1, 16.6 at
  the start of run 2 and 4.3 at the start of the full suite. Absolute times are
  neither native nor from an idle host.
- **Exit:** 0 on all three runs.
  - Sweep run 1: 10/10 steps in 61.7 s (the per-tool step 44.3 s).
  - Sweep run 2: 10/10 steps in 59.3 s (the per-tool step 42.9 s).
  - Full suite: 40/40 steps in 28.7 s; `scripts/verify-auth-mcp-e2e-manifest.sh`
    on that report answered "pass (40 steps, in order, all matching status,
    within runtime bound)", exit 0.
  - The two sweep runs' 167 rows matched row for row (a diff of the PASS/FAIL
    lines of the two tables was empty).
  - A first attempt of run 1 stopped at the `npx tsc` typecheck before any stack
    started, because the fresh worktree had no `node_modules`. It was rerun after
    `npm ci`, and that attempt is not counted.
  - All three stacks were torn down with `down -v` (the runner's own teardown).

## Result

- 166 tools listed to the scoped personal token; 167 calls (one tool has two
  cases); **167/167 passed**, in both runs.
- **`count_infra_resources` (#7231) now passes:** `GET /api/v0/infra/resources/count`
  answered `ok` to the scoped token in both runs. The previous run, at
  `136d75646`, answered `backend_timeout` on it, the only failure of 166/167.
  #7239 split the scoped infra count and inventory Cypher by graph dialect, which
  is the fix. The row's expectation was never loosened: it is still `success
  (ok)`. This sweep does not re-measure the plan times the earlier run took, only
  that the route now answers within its deadline.
- **The four rows earlier runs could not pass now pass live:**
  - `find_infra_resources` (`POST /api/v0/infra/resources/search`): `ok`, "Returned
    0 result(s)" (#7226, #7215);
  - `analyze_infra_relationships` (`POST /api/v0/infra/relationships`): `not_found`,
    a typed 404 for the unseeded entity (accepted; #7226, #7215);
  - `search_registry_bundles` (`POST /api/v0/code/bundles`, promoted off the
    pending ledger by #7194): **`default` returned an empty `ok` page to the
    scoped token**. A scoped caller reads only `visibility = 'public'` packages
    and the fixture seeds none, so an empty page is the expected answer;
  - `analyze_code_relationships[who_modifies]` (`POST /api/v0/code/relationships`):
    `not_found`, dispatched with `name` and `repo_id` (#7221), with the all-scope
    control below.
- **134 calls must answer `ok`:** 134 did, 0 short (this count includes
  `count_infra_resources`).
- **30 tolerant calls** prove only that the route is mounted and not refused by
  the route policy:
  - 23 answered a typed `not_found` for a subject the fixture does not seed;
  - 7 are gated by the stack profile: `unsupported_capability` x4,
    `ask_default_off` x1 (`ESHU_ASK_ENABLED` unset, matched on its "ask is not
    enabled" body) and `component_registry_unavailable` x2
    (`ESHU_COMPONENT_HOME` unset).
- **3 ledger calls** answered the route-policy 403 with a tool description that
  discloses it: 1 pending row filtering (`get_service_changed_since`) and 2
  shared-key only (`execute_cypher_query`, `visualize_graph_query`).
- **Rows promoted off the pending ledger since the NornicDB run pass live:**
  - `get_index_status` (#7193): `ok`;
  - `trace_resource_to_code` and `trace_exposure_path` (#7191): `ok`;
  - `explain_dependency_path` (#7191): `not_found`, which it accepts because
    its endpoints are unseeded;
  - `search_registry_bundles` (#7194): an empty `ok` page, above.
- **The token was scoped, not shared:** the stack has no `ESHU_API_KEY`, and the
  run's `catalog-sweep_token_is_scoped_not_shared` step showed the token
  resolving through roles `["e2e_catalog_sweep_reader","owner"]` with the
  permission catalog enforced.
- **Full suite:** 40/40 steps in 28.7 s, exit 0, including
  `shapeA_mcp_tool_call_row_filtered` ("166 tools listed; scoped personal token
  (empty grant) correctly saw 0 repositories (total=0)") and
  `leakage_cross_scope_row_filter_non_vacuous`.

### All-scope controls

The five tolerant rows that answered `not_found` and name the granted
repository among their arguments are replayed through the all-scope console
session. The replay uses the request the MCP dispatcher actually sends
(`resolveRoute`'s body and query, emitted by `TestCatalogSweepPolicy`), not the
raw tool arguments. All five answered the same typed 404:

```text
analyze_code_relationships[who_modifies] POST /api/v0/code/relationships: scoped=not_found all-scope=404 SAME
calculate_cyclomatic_complexity[default] POST /api/v0/code/complexity: scoped=not_found all-scope=404 SAME
get_file_content[default] POST /api/v0/content/files/read: scoped=not_found all-scope=404 SAME
get_file_lines[default] POST /api/v0/content/files/lines: scoped=not_found all-scope=404 SAME
trace_route_callers[default] POST /api/v0/code/routes/callers: scoped=not_found all-scope=404 SAME
```

Since #7221 the `who_modifies` dispatch is `{"entity_id":"","name":"sweepTarget",
"query_type":"who_modifies","repo_id":"<granted repository>"}` (read from the
run's policy file), so its control now proves the granted repository lacks the
entity, not only that the entity is absent for every caller. The other 18
tolerant `not_found` rows name a `sweep-seed-missing` id that no seed creates, so
they claim only the weaker result.

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

## Earlier run (history): 166/167 at `136d75646`

The immediately preceding run, on Neo4j at `136d75646` (`claude/5167-e2e-neo4j`,
rebased onto `origin/main` at `f55447c45`, project
`eshu-e2e-auth-mcp-5167final`, run twice), passed 166/167 and exited 1, with 9/10
steps in 84.8 s and 81.2 s. It ran on the emulated-amd64 Neo4j at a load average
of about 3.5-5. It predates #7239, and this run supersedes its figures.

- Its single failure was `count_infra_resources` (`GET /api/v0/infra/resources/count`):
  `backend_timeout` ("graph query exceeded its deadline") to a scoped token, in
  both runs, tracked in #7231. It was a sibling of #7215 that #7226 did not
  cover: the aggregate path rendered the scoped grant predicate into each of 27
  per-label `UNION ALL` branches and issued four such statements. `EXPLAIN` in
  `cypher-shell` on the kept stack measured cold plans of 4.8-5.2 s each
  (total 5076 ms, by provider 4966, by environment 5189, by label 4816) and 275 ms
  with the grant clause removed. The exact deadline accounting was not traced.
  #7239 fixed it, and it passes above.
- The other rows matched the current run: the four rows above already passed,
  with 133 of 134 `ok`, 30 tolerant and 3 ledger.
- At `136d75646` the full suite passed 40/40 in 29.3 s.

## Earlier run (history): 165/167 at `7664283f3`

The first Neo4j run, at a local pre-rebase commit that was never pushed (the
sweep itself is in #7204, `348861541`; the Neo4j stack change is in this PR),
passed 165/167 at a load average of about 17.7. It predates #7226, #7221 and the
#7194 promotion, and this run supersedes its figures.

- Its two failures were #7215: scoped `find_infra_resources` and
  `analyze_infra_relationships` answered `backend_timeout`. Measured then with
  `EXPLAIN`/`PROFILE`, the scoped search query took 21.5-38.0 s to plan cold and
  5.1-6.0 s warm against a 10 s read, and each scoped per-label relationships
  anchor about 1 s to plan with about 16 anchors sharing one budget. #7226
  fixed both; they pass above.
- `search_registry_bundles` was then still a ledger row (the disclosed 403),
  and `count_infra_resources` passed.
- The `who_modifies` control first failed with `400 entity_id or name is
  required`: the replay posted the raw arguments and the dispatcher renames
  `target`. That was a harness defect, fixed by replaying the dispatched
  request.
- Its counts were 132 of 133 `ok`, 30 tolerant (22 `not_found`, 7 gated, and
  the failing `analyze_infra_relationships`), 4 ledger.
- An earlier run at `4aa1e23e0` was on NornicDB and predates #7183, #7193 and
  #7191.

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

At `136d75646` on Neo4j the full suite passed 40/40 again, with
`shapeA_mcp_tool_call_row_filtered` ("166 tools listed; scoped personal token
(empty grant) correctly saw 0 repositories (total=0)") and
`leakage_cross_scope_row_filter_non_vacuous` both PASS, and
`scripts/verify-auth-mcp-e2e-manifest.sh` on that report answered "pass (40
steps, in order, all matching status, within runtime bound)", exit 0.

The earlier rerun was on NornicDB. On Neo4j at `7664283f3`, the full suite
(`bash scripts/run-auth-mcp-e2e.sh`, no module) passed 40/40 steps. That
includes `shapeA_mcp_tool_call_row_filtered` ("scoped personal token (empty
grant) correctly saw 0 repositories (total=0)") and
`leakage_cross_scope_row_filter_non_vacuous`. It also passed
`scripts/verify-auth-mcp-e2e-manifest.sh` against the named baseline (40 steps,
in order, within the runtime bound). The SSO suite (`scripts/run-auth-e2e.sh`)
passed 24/24 on the same Neo4j stack definition.
`scripts/verify-auth-mcp-e2e-sensitivity.sh` first failed to boot. Neo4j went
unhealthy under the inherited 5 s x 10 healthcheck. The Neo4j container ran
emulated amd64 on an arm64 host with load average 17. After the
e2e `neo4j` service got `start_period: 120s` and 30 retries, the gate passed
(real gate PASS, mutated FAIL on the gate, restored PASS). The Eshu images were
unchanged; only the healthcheck changed.

## Seed and limits

The seed consists of the graph Repository nodes (written with
`docker compose exec neo4j cypher-shell`), one repository-catalog scope per
repository, one `state_snapshot` scope, and a role granting every feature and
data class on the granted repository and the state scope. There is no indexed
content, so the 23 `not_found` rows prove only that the route is mounted and
not refused by the route policy, not a populated answer. The 3 routes refused in
this run are the closed `pendingRowFilteringRoutes` and `sharedKeyOnlyRoutes` ledgers.

## Per-tool table

Run at `2eb226f58` on Neo4j (first run; the second run's rows are identical):

```text
tool                                             route                                                                    expected                                                  actual                          verdict
-----------------------------------------------  -----------------------------------------------------------------------  --------------------------------------------------------  ------------------------------  -------
analyze_code_relationships[find_callers]         POST /api/v0/code/relationships/story                                    success (ok)                                              ok                              PASS
analyze_code_relationships[who_modifies]         POST /api/v0/code/relationships                                          success (ok|not_found)                                    not_found                       PASS
analyze_infra_relationships                      POST /api/v0/infra/relationships                                         success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
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
find_infra_resources                             POST /api/v0/infra/resources/search                                      success (ok)                                              ok                              PASS
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
search_registry_bundles                          POST /api/v0/code/bundles                                                success (ok)                                              ok                              PASS
search_semantic_context                          POST /api/v0/search/semantic                                             success (ok)                                              ok                              PASS
trace_deployment_chain                           POST /api/v0/impact/trace-deployment-chain                               success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
trace_exposure_path                              POST /api/v0/impact/trace-exposure-path                                  success (ok)                                              ok                              PASS
trace_resource_to_code                           POST /api/v0/impact/trace-resource-to-code                               success (ok)                                              ok                              PASS
trace_route_callers                              POST /api/v0/code/routes/callers                                         success (ok|not_found|scope_not_found|service_not_found)  not_found                       PASS
visualize_graph_query                            POST /api/v0/code/visualize                                              403 disclosed (shared_key_only)                           route_denied_403 (disclosed)    PASS

167/167 calls passed

all-scope controls (granted-repository not_found rows):
  analyze_code_relationships[who_modifies] POST /api/v0/code/relationships: scoped=not_found all-scope=404 SAME
  calculate_cyclomatic_complexity[default] POST /api/v0/code/complexity: scoped=not_found all-scope=404 SAME
  get_file_content[default] POST /api/v0/content/files/read: scoped=not_found all-scope=404 SAME
  get_file_lines[default] POST /api/v0/content/files/lines: scoped=not_found all-scope=404 SAME
  trace_route_callers[default] POST /api/v0/code/routes/callers: scoped=not_found all-scope=404 SAME
```

## Stack change evidence

No-Regression Evidence: `docker-compose.e2e.yaml` is the test-only stack behind the auth, MCP and SSO e2e suites. It is not a runtime deployment profile. It now runs `neo4j:2026-community` (digest-pinned through `docker-compose.neo4j.yml`) in place of NornicDB. These results are author-reported from live runs on that stack. At `7664283f3` the full auth/MCP suite passed 40/40 and the SSO suite passed 24/24. Both runs predate the healthcheck window (`start_period: 120s`, `retries: 30`); the sensitivity gate passed after it was added. At `2eb226f58` the catalog sweep passed 167/167 twice and the full suite passed 40/40 (the earlier 166/167 at `136d75646` failed only on `count_infra_resources`, fixed by #7239). The sweep cases were restated for route promotions, as the sections above describe. Besides the backend swap, the stack drops the host graph ports, renames the graph volume and gives db-migrate a neo4j dependency. It changes no production path.

No-Observability-Change: the change touches only test-stack wiring. It adds no runtime metric, span, log key, status field or worker behaviour. Suite results are reported through the existing e2e runner report and step lines.
