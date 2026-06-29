# MCP Prompt Surface Audit

This audit tracks the documented prompt surface for #285. A prompt is
prompt-ready when the docs point to a first-class MCP or HTTP tool with a clear
scope contract, bounded result size, deterministic paging where lists are
returned, envelope metadata, and a drilldown path for exact evidence.

Normal prompt suites must not use `execute_cypher_query`. Raw Cypher belongs
only in diagnostics sections and local graph debugging.

## Current Status

| Prompt family | Primary route | Prompt-ready status | Notes |
| --- | --- | --- | --- |
| Symbol or implementation lookup | `find_symbol`, `find_code` | Ready | Use repository scope and `limit` when broad names are possible. |
| Broad code behavior investigation | `investigate_code_topic` | Ready | Start here for behavior prompts before drilling into symbols, relationships, or source lines. |
| Structural code inventory | `inspect_code_inventory` | Ready | Covers functions, classes, dataclasses, decorated methods, documented functions, class methods, `super()` calls, and function counts per file. |
| Import dependency prompts | `investigate_import_dependencies` | Ready | Covers imports by file, module importers, package imports, direct Python file import cycles, and cross-module calls. |
| One-symbol callers/callees/imports | `get_code_relationship_story` | Ready | Resolves ambiguity first, then returns bounded direct or transitive relationship rows. |
| Class hierarchy and overrides | `analyze_code_relationships` | Ready | Use `query_type=class_hierarchy` or `query_type=overrides`; raw Cypher examples are diagnostics-only. |
| Call-chain prompts | `find_function_call_chain` | Ready | Use `max_depth`; compatibility through `analyze_code_relationships` is still supported. |
| Recursive and hub-function prompts | `inspect_call_graph_metrics` | Ready | Requires repository scope, returns canonical `functions` rows, reports recursion or hub-degree evidence, and includes truncation metadata. |
| Code quality and refactoring prompts | `inspect_code_quality`, `find_most_complex_functions`, `calculate_cyclomatic_complexity` | Ready | Prefer `inspect_code_quality` for list-style prompts because it returns source handles and truncation. |
| Dead-code prompts | `investigate_dead_code`, `find_cross_repo_dead_code` | Ready | Use the investigation packet first for one repository; use `find_cross_repo_dead_code` when another repository or service boundary may keep a producer symbol live. `find_dead_code` remains the lower-level candidate scan. |
| Hardcoded-secret prompts | `investigate_hardcoded_secrets` | Ready | Returns redacted evidence only, with suppression notes and paging. |
| Repository explanation and context | `get_repo_story`, `get_repo_context` | Ready | Use `get_repo_story` for the narrative repository dossier and `get_repo_context` for durable drilldown after story or search results identify the repository. |
| Service explanation and onboarding | `get_service_story`, `investigate_service` | Ready | Use story first for the normal dossier path; use investigation first when coverage must be inspected before answering. |
| Deployment chain prompts | `trace_deployment_chain` | Ready | Read `deployment_fact_summary`, `deployment_facts`, `controller_overview`, and `runtime_overview` before lower-level rows. |
| Deployment configuration prompts | `investigate_deployment_config` | Ready | Covers image tags, runtime settings, resource limits, values layers, rendered targets, and read-first file handles. |
| Resource, queue, database, and cloud-resource prompts | `investigate_resource` | Ready | Resolves ambiguity before returning workload users, provenance paths, source handles, and next calls. |
| Environment comparison prompts | `compare_environments` | Ready | Returns story, summary, per-side resources, evidence, limitations, and side-specific truncation. |
| Evidence citation prompts | `build_evidence_citation_packet` | Ready | Accepts explicit citation handles; current runtime hydrates file and entity handles; caps input handles and hydrated citations. |
| Visualization prompts | `derive_visualization_packet` | Ready | Derives bounded service-story, evidence-citation, or incident-context visualization packets from a source response already returned by an authorized route/tool. |
| Source and content reads | `get_file_content`, `get_file_lines`, `get_entity_content`, `search_file_content`, `search_entity_content` | Ready | Use after story, investigation, or search tools identify portable handles. |
| Runtime and indexing status | `get_index_status`, `list_ingesters`, `get_ingester_status` | Ready | Job-id based MCP status tools are not advertised. |
| Package registry prompts | `list_package_registry_packages`, `list_package_registry_versions` | Ready | Use `limit` and package/version scope. |
| IaC cleanup and AWS management prompts | `find_dead_iac`, `find_unmanaged_resources`, `get_iac_management_status`, `explain_iac_management_status`, `propose_terraform_import_plan` | Ready | Import-plan generation stays read-only and safety-gated. |

## Prompt Suite Guardrails

- Prefer the named tool in the table before any raw content search or Cypher.
- Pass the narrowest known scope: `repo_id`, `service_name`, `workload_id`,
  environment, resource id, source file, target file, or module name.
- List-style calls must set or accept default `limit` and `offset`; callers
  must read `truncated` or `next_offset` before claiming complete coverage.
- File-shaped drilldowns must use `repo_id + relative_path` or `entity_id`.
  Server-local paths are not portable prompt contracts.
- Use `build_evidence_citation_packet` after story or investigation tools return
  handles instead of guessing which files to cite.
- Use `derive_visualization_packet` after a service story, evidence-citation,
  or incident-context answer when a client needs renderable nodes and edges.
- Use `inspect_call_graph_metrics` for recursive and hub-function prompts
  instead of diagnostics-only Cypher.

## Evidence

No-Regression Evidence: this audit is documentation-only. It cross-checks
`docs/public/guides/starter-prompts.md`, `docs/public/use-cases.md`,
`docs/public/guides/mcp-guide.md`, `docs/public/reference/mcp-reference.md`,
`docs/public/reference/mcp-cookbook.md`, and
`docs/public/reference/mcp-tool-contract-matrix.md` against the current
`ReadOnlyTools` prompt surface. Strict docs proof:
`uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`.
Focused contract proof:
`go test ./internal/mcp -run TestMCPPromptEpicDocsDoNotAdvertiseClosedGaps -count=1`.

No-Observability-Change: this does not change runtime, API, MCP dispatch, graph
queries, or telemetry. Existing handler spans, MCP envelope negotiation, and
bounded response fields remain the operator signals for the documented tools.

## Aggregate Fan-Out Audit (#574)

This section tracks the audit requested by
[#574](https://github.com/eshu-hq/eshu/issues/574): every list-shaped MCP tool
and HTTP route is inventoried and classified by whether it forces callers to
fan out across repositories, services, resources, or files to answer a bounded
summary question.

The reference case is the repository language inventory question. Answering
"how many TypeScript repos exist?" used to require listing every repository
and then calling `get_repository_coverage` per repository. That was fixed by
adding server-side Postgres aggregates and the MCP tool / HTTP route pair
shipped alongside them. The pattern other aggregates must follow lives at:

- `go/internal/query/content_reader_language_inventory.go`
- `go/internal/mcp/tools_repository_language.go`
- `go/internal/query/repository.go` (route registration)
- `go/internal/query/openapi_paths_repositories.go`

### Classification Buckets

| Label | Trigger |
| --- | --- |
| `already-bounded` | Returns a count or pre-aggregated inventory in O(1) DB calls. |
| `needs-read-model-aggregate` | Source-of-truth lives in a Postgres reducer table (`fact_records` keyed by `fact_kind`) or `content_files`. Add a `Count*` / `*Inventory` Reader method, MCP tool, and HTTP route mirroring the language-inventory pattern. |
| `needs-indexed-graph-aggregate` | Source-of-truth lives only in the graph (NornicDB). Must use a hot-path-eligible Cypher shape (`PatternIncomingCountAgg`, `PatternOutgoingCountAgg`, `PatternEdgePropertyAgg`, cookbook Area 5 grouped count) with a graph index on the grouping property, and prove hot-path eligibility before merging. |
| `server-side-fanout-bounded` | Caller passes handles; backend fans out but the input list is hard-capped and per-handle work is bounded. Acceptable when the cap is enforced and the per-handle timeout is documented. |
| `drilldown-only` | Single-entity or already-scoped search; an aggregate would not change the contract. |

### Already-Bounded Surfaces (No Action)

| Surface | Backend | Notes |
| --- | --- | --- |
| `count_repositories_by_language` / `GET /api/v0/repositories/by-language` | Postgres `content_files` aggregate | Worked example. |
| `get_repository_language_inventory` / `GET /api/v0/repositories/language-inventory` | Postgres `content_files` aggregate | Worked example. |
| `get_ecosystem_overview` / `GET /api/v0/ecosystem/overview` | Graph; single Cypher with composed `WITH` counts | Top-level repo / workload / platform / instance counts. |
| `get_repo_context` / `GET /api/v0/repositories/{repo_id}/context` | Graph + Postgres read-model | Single repository scope; bounded counts via `go/internal/query/repository_context_counts.go`. |
| `list_service_catalog_correlations` / `GET /api/v0/service-catalog/correlations` | Postgres `fact_records` | Filtered list per scope or provider; no aggregate question forces fan-out. |
| `list_kubernetes_correlations` / `GET /api/v0/kubernetes/correlations` | Postgres `fact_records` | Filtered list per cluster, workload, namespace, image, or digest scope; no aggregate question forces fan-out. |
| `list_observability_coverage_correlations` / `GET /api/v0/observability/coverage/correlations` | Postgres `fact_records` | Filtered list per scope, provider, coverage signal, observability object, target resource, or target service; no aggregate question forces fan-out. |
| `list_indexed_repositories` / `GET /api/v0/repositories` | Graph + Postgres catalog | Bounded list with deterministic ordering. |
| `compare_environments` / `POST /api/v0/compare/environments` | Graph | Already returns side-by-side aggregate summary. |
| `get_repo_summary` / `get_repo_story` | Graph + Postgres | Single repository scope. |

### Server-Side Fan-Out, Bounded

| Surface | Backend | Notes |
| --- | --- | --- |
| `build_evidence_citation_packet` / `POST /api/v0/evidence/citations` | Graph + Postgres | Caller passes explicit handle list; the tool documents the input cap and per-handle hydration. No action. |
| `derive_visualization_packet` / `POST /api/v0/visualizations/derive` | Caller-supplied source response | Pure derivation from an already-authorized answer response; no graph/content read or fan-out. No action. |

### Needs Read-Model Aggregate (Postgres `fact_records`)

These surfaces have a reducer-owned Postgres source of truth with the right
partial indexes already in `go/internal/storage/postgres/schema_fact_records.go`.
A grouped count + inventory pair shipped behind the same envelope pattern as
the language inventory will eliminate the page-and-iterate workload.

| Surface | Kind | Backend (`fact_kind`) | Grouping dimensions | Follow-up |
| --- | --- | --- | --- | --- |
| `list_supply_chain_impact_findings` / `GET /api/v0/supply-chain/impact/findings` | list | `reducer_supply_chain_impact_finding` | `impact_status`, `priority_bucket`, `severity` (CVSS bucket), `repository_id`, `subject_digest` | [#683](https://github.com/eshu-hq/eshu/issues/683) |
| `list_container_image_identities` / `GET /api/v0/supply-chain/container-images/identities` | list | `reducer_container_image_identity` | `registry`, `namespace`, `repository_id`, `outcome` | [#684](https://github.com/eshu-hq/eshu/issues/684) |
| `list_ci_cd_run_correlations` / `GET /api/v0/ci-cd/run-correlations` | list | `reducer_ci_cd_run_correlation` | `outcome`, `environment`, `repository_id`, `provider` | [#685](https://github.com/eshu-hq/eshu/issues/685) |
| `list_documentation_findings`, `list_documentation_facts` / `GET /api/v0/documentation/findings`, `/facts` | list | reducer-owned documentation kinds (see `go/internal/query/documentation_read_model.go`) | `status`, `evidence_level`, `finding_kind`, `fact_kind`, `repository_id` | [#686](https://github.com/eshu-hq/eshu/issues/686) |
| `list_security_alert_reconciliations` / `GET /api/v0/supply-chain/security-alerts/reconciliations` | list | `reducer_security_alert_reconciliation` | `reconciliation_status`, `provider`, `provider_state`, `repository_id`, `package_id` | [#687](https://github.com/eshu-hq/eshu/issues/687) |
| `list_sbom_attestation_attachments` / `GET /api/v0/supply-chain/sbom-attestations/attachments` | list | `reducer_sbom_attestation_attachment` | `attachment_status`, `artifact_kind`, `subject_digest` | [#688](https://github.com/eshu-hq/eshu/issues/688) |

### Needs Indexed Graph Aggregate (NornicDB)

These surfaces have a graph source of truth. They need NornicDB hot-path-
eligible aggregate Cypher and a graph index on the grouping property, with
hot-path eligibility proved against the pinned binary before merge per
[Cypher Performance](cypher-performance.md) and the NornicDB-New hot-path
query cookbook checked out alongside the repo.

| Surface | Kind | Backend | Grouping dimensions | Follow-up |
| --- | --- | --- | --- | --- |
| `list_package_registry_packages`, `list_package_registry_dependencies`, `list_package_registry_correlations` / `GET /api/v0/package-registry/*` | list | Graph (`(:Package)`, `(:Package)-[:DEPENDS_ON]->(:Package)`) | `ecosystem`, `registry`, `namespace`, `package_manager`, consumer `repository_id` | [#689](https://github.com/eshu-hq/eshu/issues/689) |
| `find_infra_resources` / `POST /api/v0/infra/resources/search` | search | Graph (`infraLabelPredicate`) | `provider`, `kind` / `resource_type`, `account`, `environment`, `resource_service`, `resource_category` | [#690](https://github.com/eshu-hq/eshu/issues/690) |

### Drilldown-Only Surfaces (No Aggregate Needed)

Single-entity reads, scoped searches that already require canonical scope, or
content reads keyed by a portable handle. These stay as is. Examples:
`get_repo_context`, `get_workload_context`, `get_service_context`,
`get_entity_context`, `investigate_resource`, `get_relationship_evidence`,
`get_file_content`, `get_file_lines`, `get_entity_content`,
`search_file_content`, `search_entity_content`, `get_documentation_evidence_packet`,
`check_documentation_evidence_packet_freshness`, `explain_supply_chain_impact`,
`trace_deployment_chain`, `investigate_deployment_config`,
`find_blast_radius`, `find_change_surface`, `investigate_change_surface`,
`find_dead_iac`, `find_unmanaged_resources`, `get_iac_management_status`,
`explain_iac_management_status`, `propose_terraform_import_plan`,
`list_aws_runtime_drift_findings`, `list_cloud_runtime_drift_findings`.

### Aggregate Contract Requirements

Every follow-up issue inherits the same contract requirements from the
`eshu-mcp-call-rigor` and `cypher-query-rigor` project skills under
`.agents/skills/`:

- Scoped inputs with a defaulted `limit` and a hard maximum.
- Deterministic ordering.
- Server-side timeout via `context.Context`.
- Truncation metadata (`truncated`, `next_offset`).
- Structured truth envelope (`truth.level`, `truth.profile`,
  `truth.freshness.state`, `error`).
- Cheap-summary call first; payload-heavy drilldown second.
- For Postgres aggregates: `EXPLAIN (ANALYZE, BUFFERS)` evidence showing
  partial index use; before/after timing vs. the page-and-iterate path.
- For graph aggregates: NornicDB `PROFILE` or statement summary proving
  hot-path eligibility on a graph index seek, not a label scan.
- Telemetry: `eshu_dp_<aggregate>_duration_seconds` histogram +
  `eshu_dp_<aggregate>_errors_total` counter + OTEL span with
  `db.system`, `db.operation`, and `db.sql.table` (or graph anchor)
  attributes. Dimension *values* go in spans or logs, not in metric labels.

### Evidence

No-Regression Evidence: this section is documentation-only. It catalogs
existing list-shaped surfaces against the current `ReadOnlyTools` surface and
backing reducer tables. The classification was cross-checked by reading
`go/internal/storage/postgres/schema_fact_records.go`,
`go/internal/query/supply_chain_impact_findings.go`,
`go/internal/query/package_registry.go`, and
`go/internal/query/infra.go`. Strict docs proof:
`uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`.

No-Observability-Change: this section does not change runtime, API, MCP
dispatch, graph queries, or telemetry. Each follow-up issue carries its own
observability and performance evidence requirements.
