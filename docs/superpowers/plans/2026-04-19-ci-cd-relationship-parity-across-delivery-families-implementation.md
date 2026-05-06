# CI/CD Relationship Parity Across Delivery Families Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the CI/CD relationship parity gap documented in the 2026-04-19 ADR by making Go E2E accurate for service materialization, delivery-path synthesis, consumer discovery, and operator-safe deployment tracing across Jenkins, GitHub Actions, ArgoCD, Terraform, Terragrunt, Docker Compose, Ansible, and CloudFormation families.

**Architecture:** Keep authoritative workload identity and deployment-source identity in reducer materialization. Keep volatile evidence such as hostnames, OpenAPI routes, hostname-based consumers, and trace storytelling in query-time enrichment, but gate every new inference with classification, provenance, confidence, and bounded fan-out. Preserve the infrastructure relationship wins E2E already has while fixing the service synthesis gaps that still leave legacy controller-driven services invisible.

**Tech Stack:** Go, PostgreSQL, Neo4j, OpenTelemetry, MkDocs

---

## Non-Negotiable Guardrails

- Do not add company-specific repo names, hostnames, or service names to tests. Extend the generic fixture ecosystems under `tests/fixtures/relationship_platform` and related generic fixture directories only.
- Do not regress existing E2E strengths in Terraform, Terragrunt, ArgoCD, Ansible, or Docker relationship surfacing while adding service synthesis.
- Do not materialize a service workload from "controller file exists" alone. Classification and confidence are mandatory.
- Do not let query-time hostname or consumer search become unbounded. Cap, rank, and emit provenance.
- Preserve the semantic split between repo-level launch functions and service-level entrypoints.

## Current System Seams

- Reducer workload candidate formation lives in:
  - `go/internal/reducer/candidate_loader.go`
  - `go/internal/reducer/workload_deployment_sources.go`
  - `go/internal/reducer/projection.go`
  - `go/internal/reducer/workload_materializer.go`
  - `go/internal/reducer/workload_materialization_handler.go`
- Repo-level delivery path synthesis lives in:
  - `go/internal/query/service_deployment_evidence.go`
  - `go/internal/query/repository_deployment_overview.go`
  - `go/internal/query/repository_deployment_overview_story.go`
  - `go/internal/query/repository_story.go`
- Service-query enrichment and deployment trace safety live in:
  - `go/internal/query/service_query_enrichment.go`
  - `go/internal/query/service_evidence.go`
  - `go/internal/query/deployment_trace_support_helpers.go`
  - `go/internal/query/impact_trace_deployment.go`
  - `go/internal/query/impact_trace_deployment_lineage.go`
- MCP and search-contract stability lives in:
  - `go/internal/mcp/dispatch.go`
  - `go/internal/query/content_handler.go`
  - `go/internal/query/code.go`
  - `go/internal/query/code_relationships.go`
  - `go/internal/query/code_cypher.go`

## Parallel Execution Map

Start these three tracks in parallel because they have disjoint primary write sets:

1. **Track A: Reducer workload candidate expansion and classification**
2. **Track B: Repo-level delivery-path synthesis and story parity**
3. **Track C: MCP/query contract stabilization**

After those land, run:

4. **Track D: Service enrichment and deployment-trace integration**
5. **Track E: Generic fixture acceptance matrix, observability, docs, and final parity verification**

## Chunk 1: Track C - MCP/Query Contract Stabilization

**Owner:** Subagent C

**Files:**
- Modify: `go/internal/mcp/dispatch.go`
- Modify: `go/internal/mcp/dispatch_test.go`
- Modify: `go/internal/query/content_handler.go`
- Modify: `go/internal/query/content_handler_search_test.go`
- Modify: `go/internal/query/code.go`
- Modify: `go/internal/query/code_cypher.go`
- Modify: `go/internal/query/code_cypher_test.go`
- Modify: `go/internal/query/code_relationships.go`
- Modify: `go/internal/query/code_relationships_graph_test.go`

- [ ] **Step 1: Lock the broken contract in tests**

Run:

```bash
cd go && go test ./internal/mcp -run 'TestResolveRoute|TestDispatchTool' -count=1
cd go && go test ./internal/query -run 'TestContentHandlerSearch|TestRelationshipGraphRowCypher|TestResolveEntity' -count=1
```

Expected: failing or incomplete coverage should capture current route/body mismatches, Cypher formatting defects, and repo-scope search semantics.

- [ ] **Step 2: Fix tool/request mapping and shared Cypher contracts**

Implement the smallest correct changes so:

- MCP tools map `repo_id` and `repo_ids` intentionally instead of drifting
- `resolve_entity` and code relationship helpers do not emit malformed Cypher
- cross-repo content search is explicit, bounded, and testable
- complexity lookup can distinguish exact-ID lookup from scoped name lookup
- `find_most_complex_functions` is treated as a separate behavior from single-entity cyclomatic lookup so one decision does not block the other

- [ ] **Step 3: Re-run the contract gate**

Run:

```bash
cd go && go test ./internal/mcp ./internal/query -count=1
```

Expected: PASS for MCP dispatch, content search, and code-query contract tests.

## Chunk 2: Track A - Reducer Workload Candidate Expansion And Classification

**Owner:** Subagent A

**Files:**
- Modify: `go/internal/reducer/candidate_loader.go`
- Modify: `go/internal/reducer/workload_deployment_sources.go`
- Modify: `go/internal/reducer/projection.go`
- Modify: `go/internal/reducer/workload_materializer.go`
- Modify: `go/internal/reducer/workload_materialization_handler.go`
- Modify: `go/internal/reducer/candidate_loader_test.go`
- Modify: `go/internal/reducer/workload_materializer_test.go`
- Modify: `go/internal/reducer/workload_materialization_handler_test.go`
- Modify: `go/internal/reducer/projection_test.go`

- [ ] **Step 1: Write failing reducer tests for candidate formation**

Add or extend tests to prove:

- cross-repo Argo deployment evidence can seed a workload candidate for a real service
- Jenkins-only utility repos do not become services
- workload kind classification can produce `service`, `job`, `utility`, or `infrastructure`
- candidate materialization records source attribution and confidence

Run:

```bash
cd go && go test ./internal/reducer -run 'TestExtractWorkloadCandidates|TestWorkloadMaterializer|TestWorkloadMaterializationHandler|TestInferWorkloadKind' -count=1
```

Expected: failures should show that repo-local K8s/Argo-only candidate creation is too narrow and that confidence/classification are missing.

- [ ] **Step 2: Implement gated candidate expansion**

Implement:

- cross-repo Argo deployment evidence as a first-class candidate source
- controller/runtime candidate scoring for Jenkins, CloudFormation, Dockerfile, and Docker Compose
- negative controls so utility repos stay non-service
- explicit confidence and provenance attached to newly materialized workloads
- land this in two slices:
  - candidate/projection gating first in `candidate_loader.go`, `workload_deployment_sources.go`, and `projection.go`
  - canonical write-path updates second in `workload_materializer.go` and `workload_materialization_handler.go`

- [ ] **Step 3: Re-run reducer-focused tests**

Run:

```bash
cd go && go test ./internal/reducer -count=1
```

Expected: PASS, with no false-positive workload creation in generic utility fixtures.

## Chunk 3: Track B - Repo-Level Delivery Path Synthesis And Story Parity

**Owner:** Subagent B

**Files:**
- Modify: `go/internal/query/service_deployment_evidence.go`
- Modify: `go/internal/query/repository_deployment_overview.go`
- Modify: `go/internal/query/repository_deployment_overview_story.go`
- Modify: `go/internal/query/repository_story.go`
- Modify: `go/internal/query/repository_deployment_overview_test.go`
- Modify: `go/internal/query/repository_deployment_overview_workflow_delivery_test.go`
- Modify: `go/internal/query/repository_story_delivery_parity_test.go`

- [ ] **Step 1: Write failing delivery-path tests first**

Add or extend tests for:

- Jenkins pipeline delivery paths
- CloudFormation serverless delivery paths
- dual delivery when Jenkins and CloudFormation coexist
- GitOps delivery path elevation from cross-repo Argo evidence
- Docker Compose represented as development/runtime path, not default production deployment

Run:

```bash
cd go && go test ./internal/query -run 'TestBuildRepositoryDeploymentOverview|TestRepositoryStoryDeliveryParity|TestRepositoryDeploymentOverviewWorkflowDelivery' -count=1
```

Expected: failures should show missing or under-modeled delivery paths and missing story parity.

- [ ] **Step 2: Implement delivery-path synthesis**

Implement:

- structured `jenkins_pipeline` delivery paths
- structured `cloudformation_serverless` delivery paths
- GitOps delivery-path synthesis from cross-repo Argo evidence
- dual/multi-delivery aggregation without collapsing distinct controllers
- story text that stays operator-readable and provenance-backed

- [ ] **Step 3: Re-run delivery-path tests**

Run:

```bash
cd go && go test ./internal/query -run 'TestBuildRepositoryDeploymentOverview|TestRepositoryStoryDeliveryParity|TestRepositoryDeploymentOverviewWorkflowDelivery' -count=1
```

Expected: PASS, with delivery-path coverage expanded and existing workflow stories preserved.

## Chunk 4: Track D - Service Enrichment, Hostname Safety, And Consumer Discovery Integration

**Owner:** Main rollout or integration subagent after Tracks A/B/C merge

**Files:**
- Modify: `go/internal/query/service_query_enrichment.go`
- Modify: `go/internal/query/service_evidence.go`
- Modify: `go/internal/query/deployment_trace_support_helpers.go`
- Modify: `go/internal/query/impact_trace_deployment.go`
- Modify: `go/internal/query/impact_trace_deployment_lineage.go`
- Modify: `go/internal/query/service_query_enrichment_test.go`
- Modify: `go/internal/query/service_evidence_test.go`
- Modify: `go/internal/query/deployment_trace_support_helpers_test.go`
- Modify: `go/internal/query/impact_trace_deployment_test.go`
- Modify: `go/internal/query/workload_context_test.go`

- [ ] **Step 1: Write failing integration tests**

Add or extend tests to prove:

- service context exposes service entrypoints, not repo-launch artifacts
- noisy code literals are filtered from public hostnames
- consumer repositories include graph-derived and bounded content-derived evidence
- trace output remains deterministic, bounded, and provenance-rich
- delivery paths from Chunk 3 and workloads from Chunk 2 both show up in service-facing responses

Run:

```bash
cd go && go test ./internal/query -run 'TestEnrichServiceQueryContext|TestLoadConsumerRepositoryEnrichment|TestImpactTraceDeployment|TestGetWorkloadContext' -count=1
```

Expected: failures should show trace noise, missing integration between reducer facts and query synthesis, or unstable consumer ranking.

- [ ] **Step 2: Integrate the service-facing query path**

Implement:

- a normalization pass first:
  - service entrypoint separation from repo entrypoints
  - hostname filtering with operator-safe heuristics and provenance
  - bounded consumer discovery inputs combining graph and content evidence
- then a response-composition pass:
  - integration of materialized workloads and synthesized delivery paths into trace and service story output
  - stable merge behavior between repository-backed delivery paths and trace-backed paths
  - preservation of provenance when K8s matching is partial or weak

- [ ] **Step 3: Re-run service integration tests**

Run:

```bash
cd go && go test ./internal/query -count=1
```

Expected: PASS for service enrichment, trace, and consumer-enrichment suites.

## Chunk 5: Track E - Generic Acceptance Matrix, Observability, And Final Verification

**Owner:** Main rollout with review subagents

**Files:**
- Modify: `tests/fixtures/relationship_platform/expected_relationships.yaml`
- Modify: `tests/fixtures/relationship_platform/service-edge-api/...` as needed
- Modify: `tests/fixtures/relationship_platform/service-worker-jobs/...` as needed
- Modify: `tests/fixtures/relationship_platform/delivery-argocd/...` as needed
- Modify: `tests/fixtures/relationship_platform/delivery-legacy-automation/...` as needed
- Modify: `go/internal/relationships/relationship_platform_fixture_test.go`
- Modify: `go/internal/query/relationship_platform_workflow_fixture_test.go`
- Modify: `go/internal/query/repository_context_relationship_overview_test.go`
- Modify: `go/internal/reducer/cross_repo_resolution_controller_config_test.go`
- Modify: `docs/docs/adrs/2026-04-19-ci-cd-relationship-parity-across-delivery-families.md` if implementation findings change status or consequences

- [ ] **Step 1: Build a generic acceptance matrix**

Extend the generic fixtures so they cover these families without company data:

- mixed GHA + Argo + Terraform service
- Jenkins + GitOps service
- Jenkins utility repo negative control
- Jenkins + CloudFormation dual-delivery repo
- Ansible role dependency repo
- Docker Compose dependency repo
- Terragrunt/Terraform provisioning repo

- [ ] **Step 2: Add acceptance assertions**

Verify the generic fixtures can prove:

- service materialization where appropriate
- no false-positive utility-service materialization
- delivery-path parity across controller families
- provisioning relationship preservation
- bounded hostname and consumer synthesis

- [ ] **Step 3: Run the final verification gate**

Run:

```bash
cd go && go test ./internal/reducer ./internal/query ./internal/mcp ./internal/relationships -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: PASS for targeted Go packages and strict docs build.

## Recommended Subagent Dispatch Order

### Parallel Wave 1

- [ ] Dispatch **Subagent A** for Chunk 2 only
- [ ] Dispatch **Subagent B** for Chunk 3 only
- [ ] Dispatch **Subagent C** for Chunk 1 only

### Parallel Wave 2

- [ ] Integrate Wave 1 branches locally
- [ ] For Track A, merge the candidate/projection slice before the canonical-write slice
- [ ] Execute Chunk 4 after merging A and B outputs

### Parallel Wave 3

- [ ] Dispatch a fixture/acceptance worker for Chunk 5 once Chunk 4 is stable
- [ ] Dispatch spec reviewer and code-quality reviewer subagents for each completed chunk

## Review And Merge Discipline

- After each chunk, run a **spec-compliance review subagent** first
- Then run a **code-quality review subagent**
- Only after both approve should the chunk be marked complete
- Do not push or merge until Chunk 5 final verification passes

## Completion Criteria

- `workload_count` and service-story behavior are correct for generic Jenkins/GitOps fixtures
- utility repos remain non-service
- repo summaries expose controller-accurate delivery paths
- deployment trace output is bounded, operator-safe, and provenance-backed
- existing Terraform/Terragrunt/ArgoCD relationship coverage is preserved
- MCP/query contract tests are green
- strict docs build passes
