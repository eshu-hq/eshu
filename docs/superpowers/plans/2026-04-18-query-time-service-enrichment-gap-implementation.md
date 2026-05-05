# Query-Time Service Enrichment Gap Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the Go E2E MCP accuracy and contract gap documented in the 2026-04-18 ADR so service investigations return correct IDs, service/runtime facts, deployment provenance, cloud dependencies, and bounded-latency traces.

**Architecture:** Keep authoritative workload, instance, platform, deployment-source, and cloud-resource identity in reducer-materialized graph facts. Keep volatile service evidence such as hostnames, OpenAPI surface, docs routes, and hostname-based consumers in query-time enrichment, but make the contracts explicit, bounded, and observable so service responses are accurate first, performant second, and reliable under fan-out pressure.

**Tech Stack:** Go, PostgreSQL, Neo4j, OpenTelemetry, MkDocs

---

## Current Flow Summary

- Reducer projection materializes `Workload`, `WorkloadInstance`, `DeploymentSource`, `RuntimePlatform`, and cloud/platform relationships in `go/internal/reducer`.
- Service and workload reads begin in `go/internal/query/entity.go`, which still start from repo-linked graph context and can leak repo-centric `entry_points`.
- Query-time enrichment in `go/internal/query/service_query_enrichment.go` and `go/internal/query/service_evidence.go` adds hostnames, observed environments, API surface, consumer repositories, provisioning chains, and deployment evidence.
- Deployment tracing fans out from `go/internal/query/impact_trace_deployment.go` into graph lookups, cross-repo content searches, GitOps/controller discovery, provisioning-chain synthesis, and delivery-path story building.
- MCP tools route through `go/internal/mcp/dispatch.go`, so parity bugs can be caused by route/body construction even when lower-level handlers already support the right semantics.

## Root-Cause Summary

- Two P0 failures are malformed Cypher generation defects caused by a shared projection helper contract.
- The search parity gap is split across three layers: MCP request mapping, HTTP handler validation, and repo-scoped versus cross-repo reader wiring.
- Service context remains partially repo-centric because repo launch functions and service-facing entrypoints are conflated.
- Deployment/provenance evidence mostly already exists, but some of it is dropped, compressed, or never elevated into service-story responses.
- `trace_deployment_chain` is reliability-sensitive because it ignores caller knobs and performs expensive cross-repo fan-out sequentially.

## Non-Negotiable Guardrails

- Preserve the semantic split between repo-level launch functions and service-level entrypoints. Do not silently redefine one field to mean both.
- Do not widen cross-repo search without explicit caps, deterministic ranking, and targeted observability.
- Keep environment comparison graph-authoritative. If compare requires materialized instances or cloud edges, return an explicit unsupported or partial state rather than inference theater.
- Any new service-story or trace enrichment must emit enough telemetry and structured logs for operators to distinguish graph miss, content miss, timeout, and partial enrichment.

## Chunk 1: P0 Query Contract And Cypher Correctness

**Files:**
- Modify: `go/internal/query/entity.go`
- Modify: `go/internal/query/code_relationships.go`
- Modify: `go/internal/query/language_query_entities.go`
- Modify: `go/internal/mcp/dispatch.go`
- Test: `go/internal/query/entity_content_python_resolve_test.go`
- Test: `go/internal/query/entity_content_fallback_test.go`
- Test: `go/internal/query/code_relationships_graph_test.go`
- Test: `go/internal/mcp/dispatch_test.go`

- [ ] **Step 1: Write or extend the failing tests first**

Run:

```bash
cd go && go test ./internal/query -run 'TestResolveEntity|TestRelationshipGraphRowCypherAvoidsDuplicateRepoNameAndVariableReuse' -count=1
cd go && go test ./internal/mcp -run 'TestResolveRouteMaps|TestDispatchTool' -count=1
```

Expected: failures should prove the malformed Cypher or incorrect tool-body construction before code changes land.

- [ ] **Step 2: Fix the shared Cypher projection contract**

Implement the smallest correct change that removes the duplicated leading or trailing comma contract between:

- `resolveEntity`
- `relationshipGraphRowCypher`
- `graphSemanticMetadataProjection`

The post-change rule must be simple and shared: either callers own comma placement or the helper does, but never both.

- [ ] **Step 3: Re-run the focused correctness gate**

Run:

```bash
cd go && go test ./internal/query ./internal/mcp -count=1
```

Expected: PASS for the resolve-entity and code-relationship contract tests.

## Chunk 2: P0 Search Semantics, Code Search Parity, And Complexity Lookup

**Files:**
- Modify: `go/internal/query/content_handler.go`
- Modify: `go/internal/query/content_reader.go`
- Modify: `go/internal/query/content_reader_names.go`
- Modify: `go/internal/query/code.go`
- Modify: `go/cmd/eshu/find.go`
- Test: `go/internal/query/content_handler_search_test.go`
- Test: `go/internal/query/content_reader_cross_repo_test.go`
- Test: `go/internal/query/code_search_metadata_test.go`
- Test: `go/internal/query/code_call_graph_contract_test.go`
- Test: `go/internal/query/openapi_test.go`
- Test: `go/cmd/eshu/find_test.go`

- [ ] **Step 1: Lock the intended semantics in tests**

Write or update tests for these rules:

- content search with `repo_id` stays repo-scoped
- content search with omitted repo scope uses a bounded cross-repo path
- `repo_ids` alias behavior is explicit and documented
- code search does not reject valid omitted-scope callers if the contract now allows cross-repo search
- complexity lookup distinguishes exact `entity_id` miss from scoped name lookup

Run:

```bash
cd go && go test ./internal/query -run 'TestContentHandlerSearch|TestHandleComplexity|TestServeOpenAPI' -count=1
cd go && go test ./cmd/eshu -run 'TestRunFind' -count=1
```

Expected: initial failures should show contract drift between callers and handlers.

- [ ] **Step 2: Align MCP, HTTP, and CLI behavior**

Implement one explicit contract across the stack:

- MCP dispatch should map search tools into the supported handler shape
- HTTP handlers should choose repo-scoped or bounded any-repo readers consistently
- CLI `find` commands should stop sending ignored or misleading fields
- complexity lookup should use a deterministic fallback path when the caller provides a function name plus optional `repo_id`

- [ ] **Step 3: Update OpenAPI and rerun the search gate**

Run:

```bash
cd go && go test ./internal/query ./cmd/eshu -count=1
```

Expected: PASS, with OpenAPI reflecting the final supported search semantics.

## Chunk 3: P1 Service Runtime Facts, Entry-Point Accuracy, And Query-Time Evidence Hardening

**Files:**
- Modify: `go/internal/query/entity.go`
- Modify: `go/internal/query/repository.go`
- Modify: `go/internal/query/service_query_enrichment.go`
- Modify: `go/internal/query/service_evidence.go`
- Modify: `go/internal/reducer/projection.go`
- Modify: `go/internal/reducer/workload_materializer.go`
- Test: `go/internal/query/workload_context_test.go`
- Test: `go/internal/query/service_query_enrichment_test.go`
- Test: `go/internal/query/service_evidence_test.go`
- Test: `go/internal/reducer/projection_test.go`
- Test: `go/internal/reducer/workload_materializer_test.go`

- [ ] **Step 1: Write failing tests for entrypoint and runtime-fact separation**

Add or update tests to prove:

- repo context may still expose repo launch functions
- service context does not leak repo-centric `entry_points`
- service-facing entrypoint fields come only from service evidence or authoritative runtime facts
- reducer materialization keeps instance, platform, and environment facts stable for compare and trace paths

Run:

```bash
cd go && go test ./internal/query -run 'TestGetWorkloadContext|TestGetServiceContext|TestEnrichServiceQueryContext' -count=1
cd go && go test ./internal/reducer -run 'TestBuildProjectionRows|TestWorkloadMaterializer' -count=1
```

Expected: failures should point at the repo-entrypoint leakage or missing runtime-fact expectations.

- [ ] **Step 2: Make service context explicitly service-oriented**

Implement these rules:

- preserve repo launch points only in repo-oriented responses
- expose service-facing entrypoints through explicit service fields or story sections
- keep hostnames, docs routes, and API surface in query-time enrichment
- keep materialized workload instances and runtime platforms authoritative for environment-aware responses

- [ ] **Step 3: Re-run the service-context gate**

Run:

```bash
cd go && go test ./internal/query ./internal/reducer -count=1
```

Expected: PASS for workload/service context, service evidence, and reducer-materialization tests.

## Chunk 4: P2 Cross-Repo Consumer Accuracy And Bounded Enrichment

**Files:**
- Modify: `go/internal/query/deployment_trace_support_helpers.go`
- Modify: `go/internal/query/content_handler.go`
- Modify: `go/internal/query/content_reader_names.go`
- Modify: `go/internal/query/service_query_enrichment.go`
- Test: `go/internal/query/deployment_trace_support_helpers_test.go`
- Test: `go/internal/query/content_handler_search_test.go`
- Test: `go/internal/query/service_query_enrichment_test.go`

- [ ] **Step 1: Add failing tests for dual consumer views**

Test for both:

- graph-derived consumers and provisioning candidates
- hostname-aware or name-aware cross-repo content consumers outside the graph candidate set

Run:

```bash
cd go && go test ./internal/query -run 'TestLoadConsumerRepositoryEnrichment|TestLoadProvisioningSourceChains|TestContentHandlerSearch' -count=1
```

Expected: failures should identify missing cross-repo consumer coverage or inconsistent repo-scope handling.

- [ ] **Step 2: Bound and rank cross-repo enrichment**

Implement bounded fan-out rules:

- cap service-name and hostname search cardinality
- rank consumer hits deterministically
- deduplicate graph and content evidence into one stable response
- keep provisioning-chain discovery graph-seeded wherever possible

- [ ] **Step 3: Re-run the consumer-accuracy gate**

Run:

```bash
cd go && go test ./internal/query -run 'TestLoadConsumerRepositoryEnrichment|TestLoadProvisioningSourceChains|TestEnrichServiceQueryContext' -count=1
```

Expected: PASS, with stable consumer ordering and explicit evidence kinds.

## Chunk 5: P3 API Surface And Network Entry Accuracy

**Files:**
- Modify: `go/internal/query/service_evidence.go`
- Modify: `go/internal/query/service_query_enrichment.go`
- Modify: `go/internal/query/impact_trace_deployment.go`
- Modify: `go/internal/query/impact_trace_deployment_k8s.go`
- Modify: `go/internal/query/openapi_components.go`
- Modify: `go/internal/query/openapi_paths_entities.go`
- Modify: `go/internal/query/openapi_paths_impact.go`
- Test: `go/internal/query/service_evidence_test.go`
- Test: `go/internal/query/service_query_enrichment_test.go`
- Test: `go/internal/query/impact_trace_deployment_test.go`
- Test: `go/internal/query/openapi_test.go`

- [ ] **Step 1: Write failing tests for public versus internal network evidence**

Add tests that separate:

- public hostnames and docs-route entrypoints
- API surface derived from OpenAPI content
- internal K8s service or deployment-path evidence

Run:

```bash
cd go && go test ./internal/query -run 'TestLoadServiceQueryEvidence|TestBuildDeploymentTraceResponse|TestServeOpenAPI' -count=1
```

Expected: failures should expose missing or conflated network-entry semantics.

- [ ] **Step 2: Tighten network and API evidence extraction**

Implement the narrowest accurate model:

- keep OpenAPI parsing permissive but deterministic
- surface public entrypoints only when evidence supports them
- avoid claiming internal network paths unless the K8s or ingress evidence is authoritative enough
- update response schemas only after the query shape is stable

- [ ] **Step 3: Re-run the API/network gate**

Run:

```bash
cd go && go test ./internal/query ./cmd/api ./cmd/mcp-server -count=1
```

Expected: PASS, with OpenAPI and runtime transport tests aligned.

## Chunk 6: P4 And P5 Deployment Trace Reliability, Provenance Elevation, And Cloud Dependency Completion

**Files:**
- Modify: `go/internal/query/impact_trace_deployment.go`
- Modify: `go/internal/query/impact_trace_deployment_controllers.go`
- Modify: `go/internal/query/impact_trace_deployment_gitops_helpers.go`
- Modify: `go/internal/query/repository_controller_artifacts.go`
- Modify: `go/internal/query/repository_workflow_artifacts.go`
- Modify: `go/internal/query/repository_deployment_overview.go`
- Modify: `go/internal/query/repository_story.go`
- Modify: `go/internal/query/compare.go`
- Modify: `go/internal/reducer/infrastructure_platform_extractor.go`
- Modify: `go/internal/reducer/workload_materializer.go`
- Test: `go/internal/query/impact_trace_deployment_test.go`
- Test: `go/internal/query/impact_trace_deployment_argocd_test.go`
- Test: `go/internal/query/impact_trace_deployment_gitops_helpers_test.go`
- Test: `go/internal/query/repository_controller_artifacts_test.go`
- Test: `go/internal/query/repository_workflow_artifacts_test.go`
- Test: `go/internal/query/repository_deployment_overview_test.go`
- Test: `go/internal/query/compare_test.go`
- Test: `go/internal/reducer/infrastructure_platform_extractor_test.go`

- [ ] **Step 1: Write failing tests for bounded trace fan-out and provenance elevation**

Add tests that prove:

- `direct_only`, `max_depth`, and `include_related_module_usage` actually gate work
- service and repo stories elevate controller and workflow provenance already present in evidence
- compare remains explicit when materialized instances or cloud-resource edges are missing

Run:

```bash
cd go && go test ./internal/query -run 'TestBuildDeploymentTraceResponse|TestCompareEnvironments|TestBuildRepositoryDeploymentOverview|TestRepositoryControllerArtifacts' -count=1
cd go && go test ./internal/reducer -run 'TestExtractInfrastructurePlatformRows|TestWorkloadMaterializer' -count=1
```

Expected: failures should identify ignored request knobs, missing provenance elevation, or incomplete environment materialization.

- [ ] **Step 2: Make the trace path bounded and story-rich**

Implement these rules:

- honor trace request knobs before any expensive fan-out
- keep graph-seeded deployment-source and cloud-resource facts authoritative
- elevate ArgoCD, Helm, GitHub Actions, Jenkins, and Ansible evidence into service or repo stories where the evidence already exists
- preserve explicit unsupported or partial states for compare and trace when required materialized facts are absent

- [ ] **Step 3: Re-run the deployment and compare gate**

Run:

```bash
cd go && go test ./internal/query ./internal/reducer -count=1
```

Expected: PASS for deployment trace, repository story, compare, and reducer infrastructure tests.

## Chunk 7: P6 Provisioning Chains, Artifact Lineage, And Response-Surface Completion

**Files:**
- Modify: `go/internal/query/deployment_trace_support_helpers.go`
- Modify: `go/internal/query/repository_config_artifacts.go`
- Modify: `go/internal/query/repository_config_artifacts_loader.go`
- Modify: `go/internal/query/service_deployment_evidence.go`
- Modify: `go/internal/query/entity.go`
- Modify: `go/internal/query/openapi_components.go`
- Modify: `go/internal/query/openapi_paths_entities.go`
- Modify: `go/internal/query/openapi_paths_impact.go`
- Test: `go/internal/query/deployment_trace_support_helpers_test.go`
- Test: `go/internal/query/repository_config_artifacts_test.go`
- Test: `go/internal/query/service_query_enrichment_test.go`
- Test: `go/internal/query/impact_trace_deployment_test.go`
- Test: `go/internal/query/openapi_test.go`

- [ ] **Step 1: Add failing tests for lineage and response completeness**

Test for:

- provisioning chains compactly linking repo, module, and deployment evidence
- artifact or image lineage appearing only when evidence is explicit
- service-story and trace responses carrying the right elevated evidence without dropping critical context

Run:

```bash
cd go && go test ./internal/query -run 'TestLoadProvisioningSourceChains|TestBuildDeploymentTraceResponse|TestEnrichServiceQueryContext|TestServeOpenAPI' -count=1
```

Expected: failures should show missing lineage or mismatched response schemas.

- [ ] **Step 2: Complete the response surfaces**

Implement the final response-shaping work:

- carry forward provisioning-chain and artifact-lineage evidence that already exists
- avoid bloating service-story responses with raw repo internals
- update OpenAPI contracts only after the response fields are stable

- [ ] **Step 3: Re-run the lineage and response-contract gate**

Run:

```bash
cd go && go test ./internal/query -count=1
```

Expected: PASS, with response schemas and tests aligned.

## Chunk 8: P7 Observability, MCP Contract Hardening, And QA-vs-E2E Validation

**Files:**
- Modify: `go/internal/mcp/dispatch.go`
- Modify: `go/internal/mcp/tools_test.go`
- Modify: `go/internal/query/status.go`
- Modify: `go/internal/runtime/metrics.go`
- Modify: `go/internal/telemetry/contract.go`
- Modify: `go/internal/query/service_query_enrichment.go`
- Modify: `go/internal/query/impact_trace_deployment.go`
- Modify: `docs/docs/adrs/2026-04-18-query-time-service-enrichment-gap.md`
- Test: `go/internal/mcp/dispatch_test.go`
- Test: `go/internal/mcp/server_test.go`
- Test: `go/cmd/mcp-server/runtime_surface_test.go`
- Test: `go/cmd/api/runtime_surface_test.go`
- Test: `go/internal/runtime/status_server_test.go`

- [ ] **Step 1: Add failing tests for parity and observability seams**

Write or extend tests for:

- MCP tool-to-route parity
- runtime transport surfaces exposing the expected admin and OpenAPI contracts
- service-enrichment and trace paths emitting explicit partial or timeout-classification state where applicable

Run:

```bash
cd go && go test ./internal/mcp ./cmd/mcp-server ./cmd/api ./internal/runtime -count=1
```

Expected: failures should expose missing parity or missing status and observability seams.

- [ ] **Step 2: Add top-level enrichment observability**

Implement:

- top-level spans around service-story, service-context, and trace enrichment
- structured logs that distinguish graph miss, content miss, timeout, and partial enrichment
- status or metrics wiring that helps operators see incomplete enrichment without treating the runtime as unhealthy

- [ ] **Step 3: Run the full local verification ladder and external parity compare**

Run:

```bash
cd go && go test ./internal/mcp ./internal/query ./internal/reducer ./cmd/api ./cmd/mcp-server -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Expected: all tests pass, docs build succeeds, and repo hygiene is clean.

- [ ] **Step 4: Re-run the QA versus E2E MCP comparison**

Validate against the ADR acceptance cases:

- `resolve_entity`
- `get_service_story`
- `get_service_context`
- `get_repo_summary`
- `search_file_content`
- `find_code`
- `analyze_code_relationships`
- `calculate_cyclomatic_complexity`
- `trace_deployment_chain`
- `compare_environments`

Record which gaps are closed, which are intentionally partial, and update the ADR with code-backed evidence only.

## Execution Notes

- Commit after each chunk if the implementation lands cleanly and tests pass.
- Prefer one semantics decision per chunk. Do not mix contract changes, story-surface changes, and observability additions in the same commit unless the tests require them together.
- If P5 or P6 expose additional missing extractor facts rather than response-shaping gaps, stop and open a follow-up ADR instead of smuggling a new architecture track into this one.
