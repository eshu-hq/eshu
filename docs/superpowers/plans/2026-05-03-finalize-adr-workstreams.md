# Finalize ADR Workstreams

> **For agentic workers:** REQUIRED: Use `superpowers:executing-plans` before implementing a chunk from this plan. Use `golang-engineering` for Go changes, `humanizer` for documentation edits, `eshu-diagnostic-rigor` for runtime evidence, `cypher-query-rigor` for graph query/write behavior, and `concurrency-deadlock-rigor` for workflow coordinator, reducer, queue, and claim work.

**Goal:** Close the ADRs that are already shipped, update the ADRs that are partially complete, and split the remaining work into chunks that subagents can own without stepping on each other.

**Architecture:** Treat ADRs as stable decision records. Use this plan for sprint-level execution, file ownership, test gates, and subagent dispatch. Close an ADR only when the code, docs, tests, and operational evidence match the decision.

**Tech Stack:** Go, PostgreSQL, Neo4j, NornicDB, Docker Compose, Helm, OpenAPI, MkDocs

## Current Read

Four ADRs are close to completion or already implemented enough to close with small documentation updates:

- `2026-04-18-bootstrap-relationship-backfill-quadratic-cost.md`
- `2026-04-20-embedded-local-backends-desktop-mode.md`
- `2026-04-28-reducer-throughput-and-nornicdb-concurrency-plan.md`
- parts of `2026-04-20-embedded-local-backends-implementation-plan.md`

Five workstreams still contain material implementation work:

- Multi-source correlation DSL and collector readiness
- Embedded local backends implementation plan
- IaC reachability and refactor impact
- Workflow coordinator runtime contract
- Workflow coordinator claiming, fencing, and convergence

## Execution Waves

### Wave 1: ADR And Documentation Reconciliation

This wave is documentation-heavy and can run in parallel. It should finish before larger code chunks begin so every worker has the same map.

#### Chunk 1A: Bootstrap Backfill And Reducer Throughput ADR Closeout

**Owner type:** documentation worker

**Files:**

- `docs/docs/adrs/2026-04-18-bootstrap-relationship-backfill-quadratic-cost.md`
- `docs/docs/adrs/2026-04-28-reducer-throughput-and-nornicdb-concurrency-plan.md`
- `docs/docs/adrs/README.md`
- `docs/docs/services/resolution-engine.md`

**Tasks:**

1. Mark the bootstrap relationship backfill ADR closed or completed if the implementation evidence still matches the current code.
2. Move remaining straggler replay automation into a follow-up note instead of leaving the ADR half-open.
3. Mark the reducer throughput and NornicDB concurrency ADR closed for PR #129-level work.
4. Update `resolution-engine.md` if it still says NornicDB reducer workers default to `1`; current code and docs should describe the NornicDB worker default as bounded by CPU up to 8, with claim size tied to worker count.
5. Update the ADR index row states.

**Verification:**

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

#### Chunk 1B: Embedded Local Backends Status Split

**Owner type:** documentation worker

**Files:**

- `docs/docs/adrs/2026-04-20-embedded-local-backends-desktop-mode.md`
- `docs/docs/adrs/2026-04-20-embedded-local-backends-implementation-plan.md`
- `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`
- `docs/docs/adrs/README.md`
- consistency check only: `docs/docs/reference/capability-conformance-spec.md`
- consistency check only: `docs/docs/reference/graph-backend-installation.md`
- consistency check only: `docs/docs/reference/nornicdb-tuning.md`
- consistency check only: `docs/docs/reference/local-testing.md`
- consistency check only: `go/cmd/eshu/nornicdb_release_manifest.json`

**Tasks:**

1. Close the desktop-mode ADR direction if local host, local authoritative sidecar, truth labels, and capability ports are represented.
2. Mark implementation-plan chunks 1 through 4 shipped if local evidence still matches.
3. Split chunk 3.5 into sidecar lifecycle shipped and release-backed NornicDB promotion blocked.
4. Keep NornicDB candidate status as accepted with conditions until release asset, pinning, signature, and conformance gates are complete.
5. Make the blocker language plain: NornicDB support is real, but promotion is not complete until Eshu consumes an acceptable release or pinned build across the required matrix.

**Verification:**

```bash
cd go && go test ./cmd/eshu ./cmd/ingester ./internal/query ./internal/storage/cypher ./internal/storage/neo4j -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

#### Chunk 1C: DSL And IaC ADR Status Reconciliation

**Owner type:** documentation worker with Go-aware review

**Files:**

- `docs/docs/adrs/2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `docs/docs/adrs/2026-04-24-iac-usage-reachability-and-refactor-impact.md`
- `docs/docs/adrs/README.md`
- optional stale execution plans under `docs/superpowers/plans/`

**Tasks:**

1. Mark DSL substrate, rule packs, deployable-unit handler, and reducer contracts as implemented only where code proves them.
2. Keep real AWS scanner, Terraform-state scanner, webhook freshness ingress, and true cross-source Git plus state plus cloud joins open.
3. Mark dead-IaC materialized rows, `/api/v0/iac/dead`, MCP `find_dead_iac`, product-truth fixtures, local-authoritative finalization, and pagination as complete where verified.
4. Keep graph neighborhood, refactor impact, integrity routes, CloudFormation, Crossplane, deeper Helm, ApplicationSet templating, and conservative Kubernetes orphan semantics open.
5. Avoid making "completed" mean "we have a plan"; completed means shipped and verified.

**Verification:**

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

### Wave 2: Independent Product Surface Work

These chunks can run in parallel after Wave 1. They touch separate product surfaces, but each one must update OpenAPI, MCP, CLI, docs, and ADR status where applicable.

#### Chunk 2A: Graph Neighborhood Phase 0

**Owner type:** query/API worker

**Files:**

- likely `go/internal/query/*graph*`
- likely `go/internal/mcp/*`
- likely `go/cmd/eshu/*`
- `docs/docs/reference/http-api.md`
- `docs/docs/reference/mcp-reference.md`
- `docs/docs/reference/cli-reference.md`
- OpenAPI sources under `go/internal/query/`

**Acceptance:**

- `POST /api/v0/graph/neighborhood` returns code-only and content-backed IaC neighborhoods through existing ports.
- MCP exposes `get_dependency_neighborhood`.
- CLI exposes a matching command or documented JSON passthrough.
- Responses include incoming edges, outgoing edges, paths, findings, blast radius, truth labels, limitations, and truncation.
- Handlers depend on query ports, not concrete Neo4j or NornicDB adapters.

**Tests:**

```bash
cd go && go test ./internal/query ./internal/mcp ./cmd/eshu -run 'Neighborhood|OpenAPI|MCP|Analyze' -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

#### Chunk 2B: IaC Impact And Integrity Routes

**Owner type:** query/API worker

**Files:**

- likely `go/internal/query/*iac*`
- likely `go/internal/mcp/*`
- likely `go/cmd/eshu/*`
- `docs/docs/reference/http-api.md`
- `docs/docs/reference/mcp-reference.md`
- `docs/docs/reference/cli-reference.md`

**Acceptance:**

- Add `/api/v0/iac/impact`, `/api/v0/iac/integrity`, and `/api/v0/iac/relationships`.
- MCP exposes `trace_iac_impact`, `find_broken_iac_references`, and `get_iac_relationships`.
- Tests cover exact, derived, ambiguous, missing, unsupported, and paginated results.
- Impact supports path selectors first. Semantic selector breadth needs an explicit decision before implementation.

**Tests:**

```bash
cd go && go test ./internal/query ./internal/mcp ./cmd/eshu -run 'IaC|Impact|Integrity|Relationships|OpenAPI' -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

#### Chunk 2C: Remaining Dead-IaC Family Completion

**Owner type:** parser/reducer worker

**Files:**

- likely `go/internal/parser/*`
- likely `go/internal/relationships/*`
- likely `go/internal/query/content_relationships_*`
- likely `go/internal/reducer/*`
- fixtures under the relevant testdata directories
- `docs/docs/adrs/2026-04-24-iac-usage-reachability-and-refactor-impact.md`

**Acceptance:**

- CloudFormation nested references and intrinsic references have positive, negative, ambiguous, and missing-path coverage.
- Crossplane XRD, Composition, and claim references have the same coverage.
- ApplicationSet template and generator behavior is represented.
- Helm values and template references are covered at the agreed parser or renderer level.
- Kubernetes orphan detection remains conservative: do not mark a resource exactly dead solely because no inbound edge was observed.

**Decision needed before coding:** parser-only first pass or renderer-backed proof for Helm and Kustomize.

**Tests:**

```bash
cd go && go test ./internal/parser ./internal/relationships ./internal/query ./internal/reducer -run 'CloudFormation|Crossplane|ApplicationSet|Helm|Kubernetes|IaC' -count=1
git diff --check
```

#### Chunk 2D: Multi-Source Collector Readiness

**Owner type:** collector/storage worker

**Files:**

- likely `go/internal/collector/*`
- `go/internal/facts/models.go`
- likely `go/internal/storage/postgres/facts*.go`
- likely `go/internal/storage/postgres/workflow_control*.go`
- likely `go/internal/runtime/*`
- likely `go/cmd/ingester/*`
- `docs/docs/reference/fact-envelope-reference.md`
- `docs/docs/adrs/2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`

**Acceptance:**

- AWS, Terraform-state, and webhook collector inputs can create scopes and generations without assuming Git.
- Duplicate deliveries are idempotent.
- Failures surface in status and telemetry.
- Collectors emit evidence and facts; they do not write canonical graph truth directly.

**Decision needed before coding:** exact scanner scope for cloud inputs and whether this chunk uses fixtures only or a live-account proof path.

**Tests:**

```bash
cd go && go test ./internal/scope ./internal/facts ./internal/collector ./internal/storage/postgres ./cmd/ingester -count=1
cd go && go vet ./internal/scope ./internal/facts ./internal/collector ./internal/storage/postgres ./cmd/ingester
git diff --check
```

#### Chunk 2E: True Cross-Source Correlation Slice

**Owner type:** reducer/correlation worker

**Files:**

- likely `go/internal/correlation/*`
- likely `go/internal/reducer/*`
- correlation fixtures and verifier scripts
- `docs/docs/adrs/2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `docs/docs/adrs/2026-04-20-multi-source-reducer-and-consumer-contract.md`

**Acceptance:**

- Fixtures prove Git, CI/CD, IaC config, Terraform state, and observed AWS evidence converge for ECS, ECR, and ALB cases.
- Positive, negative, and ambiguous cases are covered.
- Shared deploy repositories stay provenance-only unless explicit correlation keys justify materialization.
- Fixture intent, reducer graph truth, and API or query truth agree.

**Decision needed before coding:** whether admitted candidates remain candidate truth only or whether materialization consumes them in this slice.

**Tests:**

```bash
cd go && go test ./internal/correlation ./internal/reducer ./internal/query -run 'Correlation|Deployable|AWS|TerraformState' -count=1
./scripts/verify_correlation_dsl_compose.sh
git diff --check
```

### Wave 3: Workflow Coordinator Correctness And Promotion

Chunks 3A and 3B should be handled before any active deployment promotion. They may be split between workers only if they coordinate schema and type changes carefully.

#### Chunk 3A: Workflow Work-Item Identity Completion

**Owner type:** workflow/storage worker

**Files:**

- `go/internal/workflow/types.go`
- `go/internal/workflow/types_test.go`
- `go/internal/storage/postgres/workflow_control.go`
- `go/internal/storage/postgres/workflow_control_test.go`
- `schema/data-plane/postgres/014_workflow_control_plane.sql`
- `docs/docs/adrs/2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`

**Acceptance:**

- `workflow.WorkItem` and `workflow_work_items` carry `acceptance_unit_id` and `source_run_id` at minimum.
- Add `source_system` if it is required by the ADR and current flow.
- Enqueue, scan, validation, fake rows, schema SQL, and bootstrap definition tests all agree.
- ADR status moves from claim substrate partial to identity represented, with reconciliation still pending until Chunk 3B lands.

**Tests:**

```bash
cd go && go test ./internal/workflow ./internal/storage/postgres -run 'Test.*WorkItem|TestWorkflowControl' -count=1
```

#### Chunk 3B: Exact-Tuple Reconciliation Gate

**Owner type:** workflow/storage worker

**Files:**

- `go/internal/storage/postgres/workflow_run_reconciliation.go`
- `go/internal/storage/postgres/workflow_run_reconciliation_test.go`
- `go/internal/storage/postgres/workflow_run_reconciliation_integration_test.go`
- `docs/docs/adrs/2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`

**Acceptance:**

- Reconciliation joins on `scope_id`, `acceptance_unit_id`, `source_run_id`, and `generation_id`.
- Regression tests prove same scope and generation with the wrong acceptance unit does not complete the run.
- Regression tests prove same scope and generation with the wrong source run does not complete the run.
- ADR status says convergence joins use the authoritative phase-state tuple.

**Tests:**

```bash
cd go && go test ./internal/storage/postgres -run 'TestWorkflowControlStore.*ReconcileWorkflowRuns' -count=1
```

#### Chunk 3C: First-Class Claim Release

**Owner type:** workflow/storage worker

**Files:**

- `go/internal/workflow/types.go`
- `go/internal/workflow/types_test.go`
- `go/internal/storage/postgres/workflow_control.go`
- `go/internal/storage/postgres/workflow_control_test.go`
- `docs/docs/adrs/2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`

**Acceptance:**

- Add `ReleaseClaim` or an equivalent fenced mutation.
- Stale token and stale owner releases are rejected.
- Release returns work to pending without pretending the item failed.
- Audit rows preserve the release event.

**Tests:**

```bash
cd go && go test ./internal/workflow ./internal/storage/postgres -run 'Test.*ReleaseClaim|Test.*Claim' -count=1
```

#### Chunk 3D: Family Fairness Scheduler

**Owner type:** coordinator/workflow worker

**Files:**

- likely `go/internal/workflow/*`
- likely `go/internal/coordinator/*`
- `go/internal/storage/postgres/workflow_control.go`
- `go/internal/storage/postgres/workflow_control_test.go`
- `docs/docs/adrs/2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`

**Acceptance:**

- Weighted round-robin across collector families.
- Deterministic FIFO behavior inside each family.
- Fairness cannot bypass fencing.
- Existing claim selector remains a safe per-family primitive.

**Tests:**

```bash
cd go && go test ./internal/workflow ./internal/coordinator ./internal/storage/postgres -run 'Test.*Fairness|Test.*ClaimNextEligible' -count=1
```

#### Chunk 3E: Coordinator-Owned Git Claim Integration

**Owner type:** coordinator/collector worker

**Files:**

- `go/internal/coordinator/service.go`
- `go/internal/coordinator/service_test.go`
- Git collector and ingester runtime files after tracing entrypoints
- `docs/docs/deployment/service-runtimes.md`
- `docs/docs/reference/environment-variables.md`
- `docs/docs/adrs/2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`

**Acceptance:**

- Production defaults do not flip.
- A claim-enabled Git collector can claim bounded work, heartbeat, emit facts, complete, release, and fail with fencing.
- The current ingester path remains available until a deliberate cutover.
- Status and telemetry let an operator see whether the coordinator is stuck, slow, failing, or idle.

**Tests:**

```bash
cd go && go test ./cmd/ingester ./cmd/workflow-coordinator ./internal/coordinator ./internal/collector ./internal/storage/postgres -count=1
```

#### Chunk 3F: Deployment Promotion Gate

**Owner type:** DevOps worker

**Files:**

- `docker-compose.yaml`
- `docker-compose.neo4j.yml`
- `deploy/helm/eshu/values.yaml`
- `deploy/helm/eshu/values.schema.json`
- `deploy/helm/eshu/templates/deployment-workflow-coordinator.yaml`
- `docs/docs/deployment/*`
- `docs/docs/reference/environment-variables.md`
- workflow coordinator ADRs

**Acceptance:**

- Helm schema either intentionally remains dark-only, or active mode is allowed only with guarded claims and explicit collector instances.
- Compose has an explicit active proof path.
- Documentation says exactly what is safe to enable and what remains experimental.
- No deployment defaults are promoted before Chunks 3A through 3E pass.

**Tests:**

```bash
helm lint deploy/helm/eshu
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

## NornicDB Promotion Track

This track should stay separate from general local-backend documentation because it depends on release and compatibility evidence.

**Files:**

- `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`
- `docs/docs/reference/graph-backend-installation.md`
- `docs/docs/reference/nornicdb-tuning.md`
- `go/cmd/eshu/nornicdb_release_manifest.json`

**Acceptance:**

- Eshu consumes an acceptable upstream release asset or documented pinned build.
- Darwin and Linux coverage is represented in the manifest or intentionally documented as unavailable.
- Conformance tests pass against the selected build.
- Docs do not claim NornicDB is fully promoted until the manifest, installation instructions, tuning docs, and ADR agree.

**Tests:**

```bash
cd go && go test ./cmd/eshu ./internal/storage/cypher ./internal/query -run 'Nornic|GraphBackend|Conformance' -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

## Suggested Subagent Dispatch

Start with Wave 1 in parallel:

- Worker 1: Chunk 1A
- Worker 2: Chunk 1B
- Worker 3: Chunk 1C

Then run Wave 2 in parallel with separate write ownership:

- Worker 4: Chunk 2A
- Worker 5: Chunk 2B
- Worker 6: Chunk 2C
- Worker 7: Chunk 2D, after scanner-scope decision
- Worker 8: Chunk 2E, after materialization decision

Run workflow coordinator work in a stricter order:

- Worker 9: Chunks 3A and 3B together, or two workers with one schema owner and one reconciliation owner after the schema owner lands.
- Worker 10: Chunk 3C.
- Worker 11: Chunk 3D.
- Worker 12: Chunk 3E.
- Worker 13: Chunk 3F only after 3A through 3E pass.

## Decisions To Make Before Coding

1. For Helm and Kustomize reachability, should the next slice be parser-only first or renderer-backed?
2. For cloud and Terraform-state collector readiness, should we build fixture-only inputs first, or include a live-account proof path?
3. For correlation materialization, should admitted candidates stay candidate truth for this slice, or should materialized graph truth consume them?
4. Resolved for workflow coordinator identity: `source_system` belongs on the work item with `acceptance_unit_id` and `source_run_id`.

## Final Verification Gate

Before calling the branch ready, run the focused gates from every touched chunk and then:

```bash
cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./internal/query ./internal/mcp -count=1
cd go && go test ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer ./cmd/workflow-coordinator ./internal/runtime ./internal/status ./internal/storage/postgres -count=1
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

If a Compose or local-authoritative proof is required for a chunk, capture the exact command, backend, and result in the PR notes and relevant ADR status table.
