# Evidence: C-14 (#4367) projection-COST pilot — canonical, semantic, and documentation domains

## Scope

Three advisory `cost` gaps in `specs/replay-coverage-manifest.v1.yaml`
(C-13-derived, `projection:<reducer_domain>|cost`) covered with real R-16
deterministic cost-counting scenarios, one per reducer projection domain:

- `projection:code_graph_projection` — claimed against the EXISTING
  `TestCostBudget_NestedDirectoryTree` (`cost_counting_test.go`). No new test:
  the "code" family (`file`, `repository` kinds, admission-exempt) projects
  through `storage/cypher.CanonicalNodeWriter`, and that test already drives
  exactly that writer over the committed
  `nested-directory-tree.json` cassette — the repository/directory canonical
  writes it exercises ARE the code-graph canonical projection path
  (`canonical_code_graph` projection_hook, `code_graph_projection`
  reducer_domain). Verified by reading
  `specs/fact-kind-registry.v1.yaml:109-126` (family `code` ->
  `reducer_domain: code_graph_projection`) and confirming no reducer handler
  other than the canonical writer owns that domain.
- `projection:semantic_entity_materialization` — new scenario
  (`semantic_entity_cost_test.go`) driving
  `storage/cypher.SemanticEntityWriter.WriteSemanticEntities` (the writer
  `reducer.SemanticEntityMaterializationHandler.Handle` calls,
  `go/internal/reducer/semantic_entity_materialization.go`).
- `projection:documentation_materialization` — new scenario
  (`documentation_edges_cost_test.go`) driving
  `storage/cypher.EdgeWriter.WriteEdges` with domain
  `reducer.DomainDocumentationEdges` (the writer
  `reducer.DocumentationEdgeMaterializationHandler.Handle` calls,
  `go/internal/reducer/documentation_edge_materialization.go`).

All three are claimed; none needed a fake/hand-counted assertion.

## Instrument selection (why these two, not the writer's own return value)

Neither `SemanticEntityWriter` nor `EdgeWriter` exposes a domain-scoped
`eshu_dp_*` counter the way `CanonicalNodeWriter.recordAtomicWrite` does
directly on the writer. Both are still real production instrumentation, one
layer over:

- `storage/cypher.InstrumentedExecutor` (`instrumented.go`) wraps the executor
  the SemanticEntityWriter is built with in production
  (`go/cmd/reducer/observed_service_wiring.go` `buildObservedReducerService`
  constructs `instrumentedNeo4j := &sourcecypher.InstrumentedExecutor{Inner:
  neo4jExecutor, ...}` and threads it in as `neo4jExec`, which
  `go/cmd/reducer/main.go` and `neo4j_wiring.go`
  `semanticEntityExecutorForGraphBackend`/`semanticEntityWriterForGraphBackend`
  wire into the semantic-entity writer). It increments
  `eshu_dp_neo4j_batches_executed_total` once per UNWIND-shaped statement (a
  statement whose `Parameters` carry a `"rows"` key) on both `Execute` and
  `ExecuteGroup` (`recordStatementBatchMetrics`).
- `storage/cypher.EdgeWriter.Instruments` is set directly in production by
  `go/cmd/reducer/endpoint_presence_wiring.go` `newHandlerEdgeWriter`, which
  `go/cmd/reducer/main.go:298` wires as `DocumentationEdgeWriter:
  edgeWriterForHandlers`. `EdgeWriter.recordGroupedWrite`
  (`edge_writer.go`) increments `eshu_dp_shared_edge_write_groups_total` once
  per grouped `WriteEdges` transaction.

Both are genuine `eshu_dp_*` counters recorded by production code paths, read
off the real otel `ManualReader`, matching the AGENTS.md non-negotiable
invariant.

## RED -> GREEN evidence

Commands run from the worktree root with
`GOCACHE=$(pwd)/.gocache`:

```
cd go && go test ./internal/replay/costcounting/... -run \
  'TestCostBudget_SemanticEntityMaterialization$|TestCostBudget_DocumentationMaterialization$' -v -count=1
```

1. **RED (placeholder budget, discovery run).** Both `.cost-budget.json` files
   started with `999999` placeholder budgets. The positive tests logged the
   real observed counts:
   - `eshu_dp_neo4j_batches_executed_total=2` for two DIFFERENT-label
     semantic rows (Annotation, Function), `statements_executed=13`.
   - `eshu_dp_shared_edge_write_groups_total=1`,
     `statements_executed=2` for the documentation scenario.
   Running the N+1 negative controls at that point FAILED with "did NOT
   exceed budget 999999" — the loose placeholder proved nothing yet, which is
   the expected RED state for a not-yet-tightened budget.
2. **Design correction (RED, semantic N+1 was a no-op).** The initial
   different-label semantic fixture produced the SAME
   `eshu_dp_neo4j_batches_executed_total` for the positive scenario (one call,
   two rows -> 2 batches, one per label) and the N+1 scenario (two calls, one
   row each -> 2 batches) — the negative control proved nothing because
   `SemanticEntityWriter` already batches by label regardless of call count
   when labels differ. Fixed by changing the fixture to two SAME-label
   (`Annotation`) rows: positive now batches both rows into ONE UNWIND
   statement (`eshu_dp_neo4j_batches_executed_total=1`), while N+1 (one call
   per row) produces two separate single-row batches
   (`eshu_dp_neo4j_batches_executed_total=2`), so the control now genuinely
   proves the N+1 case exceeds the batched case.
3. **Exact budgets set from the observed GREEN counts:**
   - `semantic-entity-materialization.cost-budget.json`:
     `eshu_dp_neo4j_batches_executed_total: 1`, `statements_executed: 12`
     (1 write batch + 11 label-scoped retract statements, one per
     `semanticEntityPlans()` entry — retract statements key on
     `"repo_ids"`/label-scoped `MATCH`, not `"rows"`, so they do not increment
     the batches-executed counter).
   - `documentation-materialization.cost-budget.json`:
     `eshu_dp_shared_edge_write_groups_total: 1`, `statements_executed: 2`
     (one grouped `WriteEdges` transaction covering two Cypher routes: entity
     target, workload target).
4. **GREEN — positive + N+1 against the tightened budgets:**

```
=== NAME  TestCostBudget_SemanticEntityMaterialization
    eshu_dp_neo4j_batches_executed_total=1 (budget=1) statements_executed=12 (budget=12)
--- PASS: TestCostBudget_SemanticEntityMaterialization (0.00s)
=== NAME  TestCostBudget_SemanticEntityMaterialization_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_neo4j_batches_executed_total = 2 > budget 1 (N=2 rows)
--- PASS: TestCostBudget_SemanticEntityMaterialization_N1_ExceedsBudget (0.00s)
=== NAME  TestCostBudget_DocumentationMaterialization
    eshu_dp_shared_edge_write_groups_total=1 (budget=1) statements_executed=2 (budget=2)
--- PASS: TestCostBudget_DocumentationMaterialization (0.00s)
=== NAME  TestCostBudget_DocumentationMaterialization_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_shared_edge_write_groups_total = 2 > budget 1 (N=2 rows)
--- PASS: TestCostBudget_DocumentationMaterialization_N1_ExceedsBudget (0.00s)
=== NAME  TestCostBudget_NestedDirectoryTree
    eshu_dp_canonical_atomic_writes_total=4 (budget=4) statements_executed=5 (budget=5)
--- PASS: TestCostBudget_NestedDirectoryTree (0.00s)
=== NAME  TestCostBudget_N1_ExceedsBudget
    N+1 negative control passed: eshu_dp_canonical_atomic_writes_total = 12 > budget 4 (N=3 directories)
--- PASS: TestCostBudget_N1_ExceedsBudget (0.00s)
PASS
```

5. **False-green guard proof.** Temporarily set both new budgets' primary key
   to `0` and reran the positive tests: both failed with "exceeds budget 0:
   algorithmic regression detected" (the observed value of `1` is nonzero, so
   this proves the "budget too tight trips the gate" direction — the paired
   "budget=0 means instrument isn't recording" guard in the test code is the
   same `if x == 0 { t.Fatal(...) }` shape already proven by the existing
   canonical-writer test). Budgets restored to the exact GREEN values (1/12
   and 1/2) before committing.

## Commands run (full verification)

```
cd go && gofumpt -l ./internal/replay/costcounting/*.go   # no output: already formatted
cd go && go vet ./internal/replay/costcounting/
cd go && go test ./internal/replay/costcounting/ -race -count=1
cd go && go test ./internal/replaycoverage/ ./cmd/replay-coverage-gate/ -count=1
bash scripts/verify-ci-gates-registry.sh --drift
bash scripts/test-verify-ci-gates-registry.sh
cd go && go test ./cmd/replay-coverage-gate/ -update-dashboard -count=1
bash scripts/verify-replay-coverage-gate.sh --blocking
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main bash scripts/verify-performance-evidence.sh
git diff --check
```

## Gate summary

`replay-coverage-report.json` (blocking run):

- `"blocking": true`
- `projection` coverage: `0/27` (0.00%) -> `3/27` (11.11%)
- `TOTAL` gaps: `28` -> `25`
- `projection:code_graph_projection|cost`, `projection:semantic_entity_materialization|cost`,
  `projection:documentation_materialization|cost` all move from `[WARN] uncovered`
  to covered.

## No-Regression Evidence

Test-only and spec/doc additions; no production code paths changed.
`go test ./internal/replay/costcounting/ -race -count=1` (6/6 pass, includes
the 3 pre-existing canonical-writer tests unchanged) and
`go test ./internal/replaycoverage/ ./cmd/replay-coverage-gate/ -count=1`
(gate logic unit tests) both green. `scripts/verify-replay-coverage-gate.sh
--blocking` passes with `"blocking": true` and the three targeted gaps
resolved; the remaining 25 gaps are unrelated pre-existing C-14 backlog, not a
regression introduced here.

## No-Observability-Change Evidence

No new `eshu_dp_*` instrument was added and no production instrumentation
call site changed. The two new scenarios read PRE-EXISTING production
instruments (`eshu_dp_neo4j_batches_executed_total`,
`eshu_dp_shared_edge_write_groups_total`) that
`storage/cypher.InstrumentedExecutor` and `storage/cypher.EdgeWriter` already
record in production; this change only adds credential-free test coverage
asserting their values, per `telemetry-coverage-discipline`'s
`No-Observability-Change:` marker convention for changes that touch no
instrument definition or production emission call site.
