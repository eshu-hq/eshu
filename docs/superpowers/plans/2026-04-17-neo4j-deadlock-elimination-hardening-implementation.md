# Neo4j Deadlock Elimination Hardening Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the current readiness-gating deadlock fix so missed readiness publications self-heal, code-call writes use smaller managed Neo4j transaction groups, and the system has proof-grade tests and operator telemetry for blocked and repaired readiness.

**Architecture:** Keep bounded-unit readiness gating as the primary correctness contract for `code_entities_uid`, add a reducer-side repair path that republishes missing readiness only when it is already true, and tighten `DomainCodeCalls` into smaller `ExecuteGroup(...)` chunks without abandoning managed transaction retries. Verification must prove that edge work does not start before semantic readiness and that repair restores progress after a missed publication.

**Tech Stack:** Go, PostgreSQL, Neo4j, OTEL telemetry, MkDocs

---

## Scope And Guardrails

1. Do not remove or weaken readiness gating for `code_calls`, `inheritance_edges`, or `sql_relationships`.
2. Do not introduce timeout-based bypass logic that lets blocked work ignore readiness.
3. Do not replace managed Neo4j grouped execution with bare `Execute(...)` for code-call optimization.
4. Do not turn repair into a second generic workflow engine.
5. Preserve concurrency across unrelated repositories, runs, and domains.

## Current File Boundaries

These files already define the hardening seam and should anchor the work:

- `docs/superpowers/specs/2026-04-17-neo4j-deadlock-elimination-hardening-design.md`
- `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`
- `go/internal/reducer/graph_projection_phase.go`
- `go/internal/storage/postgres/graph_projection_phase_state.go`
- `go/internal/projector/runtime.go`
- `go/internal/reducer/semantic_entity_materialization.go`
- `go/internal/reducer/code_call_projection_runner.go`
- `go/internal/reducer/shared_projection_worker.go`
- `go/internal/reducer/shared_projection_runner.go`
- `go/internal/storage/neo4j/edge_writer.go`
- `go/cmd/reducer/main.go`

## Planned File Structure

### Create

- `go/internal/reducer/graph_projection_phase_repair.go`
- `go/internal/reducer/graph_projection_phase_repair_test.go`

### Modify

- `go/internal/reducer/graph_projection_phase.go`
- `go/internal/storage/postgres/graph_projection_phase_state.go`
- `go/internal/storage/postgres/graph_projection_phase_state_test.go`
- `go/internal/reducer/code_call_projection_runner.go`
- `go/internal/reducer/code_call_projection_runner_test.go`
- `go/internal/reducer/shared_projection_worker.go`
- `go/internal/reducer/shared_projection_worker_test.go`
- `go/internal/reducer/shared_projection_runner.go`
- `go/internal/storage/neo4j/edge_writer.go`
- `go/internal/storage/neo4j/edge_writer_test.go`
- `go/cmd/reducer/main.go`
- `go/cmd/reducer/main_test.go`
- `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`
- `docs/docs/reference/local-testing.md` only if verification commands change

## Chunk 1: Extend The Readiness Contract For Repair

**Files:**
- Create: `go/internal/reducer/graph_projection_phase_repair.go`
- Create: `go/internal/reducer/graph_projection_phase_repair_test.go`
- Modify: `go/internal/reducer/graph_projection_phase.go`
- Modify: `go/internal/storage/postgres/graph_projection_phase_state.go`
- Modify: `go/internal/storage/postgres/graph_projection_phase_state_test.go`
- Test: `go/internal/reducer/graph_projection_phase_repair_test.go`
- Test: `go/internal/storage/postgres/graph_projection_phase_state_test.go`

- [ ] **Step 1: Write the failing repair contract tests**

Add tests that pin the repair-facing contract:

```go
func TestGraphProjectionPhaseRepairCandidateValidate(t *testing.T) {
	candidate := GraphProjectionPhaseRepairCandidate{
		Key: GraphProjectionPhaseKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
		},
		Phase: GraphProjectionPhaseSemanticNodesCommitted,
	}
	if err := candidate.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
```

Run:

```bash
cd go && go test ./internal/reducer -run 'TestGraphProjectionPhaseRepair' -count=1
```

Expected: FAIL because the repair types do not exist yet.

- [ ] **Step 2: Write the failing Postgres repair lookup tests**

Add tests for store helpers that enumerate missing readiness candidates and
upsert repair rows idempotently.

```go
func TestGraphProjectionPhaseStateStoreListMissingRepairCandidates(t *testing.T) {
	db := newGraphProjectionPhaseStateTestDB()
	store := NewGraphProjectionPhaseStateStore(db)

	candidates, err := store.ListMissingRepairCandidates(
		context.Background(),
		GraphProjectionPhaseSemanticNodesCommitted,
		GraphProjectionKeyspaceCodeEntitiesUID,
		100,
	)
	if err != nil {
		t.Fatalf("ListMissingRepairCandidates() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
}
```

Run:

```bash
cd go && go test ./internal/storage/postgres -run 'TestGraphProjectionPhaseStateStore.*Repair' -count=1
```

Expected: FAIL because the repair lookup helpers do not exist yet.

- [ ] **Step 3: Implement the repair contract and store helpers**

Implement:

1. `GraphProjectionPhaseRepairCandidate`
2. `GraphProjectionPhaseRepairDecision`
3. `Validate()` helpers for repair inputs
4. Postgres store helpers for:
   - enumerating missing readiness candidates
   - looking up exact phase state
   - upserting repaired readiness rows
5. small helper functions that keep repair-specific SQL out of the runner code

Keep the store focused: lookup, list candidates, and publish repairs. Do not
mix runner logic into the store.

- [ ] **Step 4: Verify the persistence and contract layer**

Run:

```bash
cd go && go test ./internal/reducer ./internal/storage/postgres -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/internal/reducer/graph_projection_phase.go go/internal/reducer/graph_projection_phase_repair.go go/internal/reducer/graph_projection_phase_repair_test.go go/internal/storage/postgres/graph_projection_phase_state.go go/internal/storage/postgres/graph_projection_phase_state_test.go
git commit -m "feat: add graph projection readiness repair contract"
```

## Chunk 2: Add Reducer-Side Repair And Observability

**Files:**
- Modify: `go/internal/reducer/graph_projection_phase_repair.go`
- Modify: `go/internal/reducer/graph_projection_phase_repair_test.go`
- Modify: `go/internal/reducer/shared_projection_runner.go`
- Modify: `go/internal/reducer/code_call_projection_runner.go`
- Modify: `go/cmd/reducer/main.go`
- Modify: `go/cmd/reducer/main_test.go`
- Test: `go/internal/reducer/graph_projection_phase_repair_test.go`
- Test: `go/cmd/reducer/main_test.go`

- [ ] **Step 1: Write the failing repair-loop tests**

Add tests that define the required behavior:

```go
func TestGraphProjectionPhaseRepairRepublishesMissingSemanticReadiness(t *testing.T) {
	store := &fakeGraphProjectionPhaseRepairStore{...}
	publisher := &recordingGraphProjectionPhasePublisher{}
	repairer := GraphProjectionPhaseRepairer{
		Store:     store,
		Publisher: publisher,
	}

	repaired, err := repairer.RunOnce(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if repaired != 1 {
		t.Fatalf("repaired = %d, want 1", repaired)
	}
}
```

Add a second test proving stale generations are not repaired.

Run:

```bash
cd go && go test ./internal/reducer -run 'TestGraphProjectionPhaseRepair' -count=1
```

Expected: FAIL because the repairer behavior does not exist yet.

- [ ] **Step 2: Write the failing reducer wiring tests**

Add tests ensuring the reducer service wires the repairer exactly once and
exposes non-nil repair dependencies.

Run:

```bash
cd go && go test ./cmd/reducer -run 'TestBuildReducerService.*Repair' -count=1
```

Expected: FAIL because main wiring does not yet include repair.

- [ ] **Step 3: Implement the reducer repairer**

Implement a focused repair loop:

1. load missing readiness candidates
2. ignore stale or invalid candidates
3. publish only missing readiness that is already true
4. emit structured logs for blocked, repaired, and failed publication cases

Keep the loop bounded by a `limit` and a poll interval. Do not let it scan the
entire world each cycle.

- [ ] **Step 4: Wire repair into reducer main**

In `go/cmd/reducer/main.go`:

1. create the repair store and repairer once
2. inject it into the reducer service alongside the shared and code-call runners
3. keep it independent from the main reducer executor path

- [ ] **Step 5: Add observability**

At minimum add:

1. structured log for readiness publish failure
2. structured log for repaired readiness row
3. counter or aggregate for repaired rows per cycle
4. log for blocked readiness with bounded-unit identifiers

Do not invent a new telemetry subsystem. Reuse the current logger and OTEL
instrument plumbing already present in reducer services and runners.

- [ ] **Step 6: Verify reducer repair behavior**

Run:

```bash
cd go && go test ./internal/reducer ./cmd/reducer -count=1
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go/internal/reducer/graph_projection_phase_repair.go go/internal/reducer/graph_projection_phase_repair_test.go go/internal/reducer/shared_projection_runner.go go/internal/reducer/code_call_projection_runner.go go/cmd/reducer/main.go go/cmd/reducer/main_test.go
git commit -m "feat: add readiness repair loop for reducer"
```

## Chunk 3: Tighten DomainCodeCalls Managed Transaction Chunking

**Files:**
- Modify: `go/internal/storage/neo4j/edge_writer.go`
- Modify: `go/internal/storage/neo4j/edge_writer_test.go`
- Test: `go/internal/storage/neo4j/edge_writer_test.go`

- [ ] **Step 1: Write the failing edge-writer chunking tests**

Add tests that pin the desired behavior for `DomainCodeCalls` only:

```go
func TestEdgeWriterWriteEdgesCodeCallsUsesMultipleExecuteGroupChunks(t *testing.T) {
	exec := &recordingGroupExecutor{}
	writer := NewEdgeWriter(exec, 2)
	writer.CodeCallGroupSize = 2

	err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := exec.groupCalls, 2; got != want {
		t.Fatalf("ExecuteGroup calls = %d, want %d", got, want)
	}
}
```

Add a companion test proving `DomainRepoDependency` still uses one grouped
execution for the same input size.

Run:

```bash
cd go && go test ./internal/storage/neo4j -run 'TestEdgeWriterWriteEdges.*Group' -count=1
```

Expected: FAIL because the writer has no domain-aware grouped chunk size.

- [ ] **Step 2: Implement code-call grouped chunking**

In `edge_writer.go`:

1. add a code-call-specific group chunk size field
2. default it conservatively, smaller than the generic grouped path
3. split `DomainCodeCalls` statements into multiple `ExecuteGroup(...)` calls
4. preserve existing behavior for all other domains
5. preserve fallback sequential behavior only for executors without
   `GroupExecutor`

Important: do not replace grouped execution with bare `Execute(...)` for
`DomainCodeCalls`. The optimization is smaller managed groups, not a loss of
managed transaction semantics.

- [ ] **Step 3: Verify the Neo4j write optimization**

Run:

```bash
cd go && go test ./internal/storage/neo4j -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add go/internal/storage/neo4j/edge_writer.go go/internal/storage/neo4j/edge_writer_test.go
git commit -m "feat: reduce code call neo4j transaction group size"
```

## Chunk 4: Add Contention Proof And Sync Docs

**Files:**
- Modify: `go/internal/reducer/code_call_projection_runner_test.go`
- Modify: `go/internal/reducer/shared_projection_worker_test.go`
- Modify: `go/internal/storage/neo4j/edge_writer_test.go` if a harness is needed
- Modify: `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`
- Modify: `docs/docs/reference/local-testing.md` only if the verification gate changes

- [ ] **Step 1: Write the failing contention-proof tests**

Add tests that intentionally create the bad interleaving we care about:

1. code-call work pending while semantic readiness is unavailable
2. selector cycles repeatedly
3. no edge write occurs
4. readiness becomes available
5. the next cycle processes the edge work

Sketch:

```go
func TestCodeCallProjectionRunnerDoesNotStartEdgeWritesBeforeSemanticReadiness(t *testing.T) {
	reader := &fakeCodeCallIntentStore{...}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	readiness := &stepReadinessLookup{readyAfter: 2}
	runner := CodeCallProjectionRunner{...}

	_, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got := len(writer.writeCalls); got != 0 {
		t.Fatalf("len(writeCalls) = %d, want 0 before readiness", got)
	}
}
```

Run:

```bash
cd go && go test ./internal/reducer -run 'TestCodeCallProjectionRunnerDoesNotStartEdgeWritesBeforeSemanticReadiness|TestSharedProjectionWorkerDoesNotStartSQLOrInheritanceWritesBeforeSemanticReadiness' -count=1
```

Expected: FAIL until the tests correctly model the current repair and gating behavior.

- [ ] **Step 2: Add repair recovery proof**

Add a test that simulates:

1. semantic write success
2. readiness publish failure
3. downstream work blocked
4. repair loop restores readiness
5. downstream work resumes

Run:

```bash
cd go && go test ./internal/reducer -run 'TestGraphProjectionPhaseRepairRestoresProgressAfterMissedPublish' -count=1
```

Expected: PASS after repair loop implementation.

- [ ] **Step 3: Run the full facts-first verification gate**

Run:

```bash
cd go && go test ./internal/projector ./internal/reducer ./internal/storage/postgres -count=1
cd go && go test ./cmd/projector ./cmd/reducer ./internal/storage/neo4j -count=1
cd go && go vet ./internal/projector ./internal/reducer ./internal/storage/postgres ./cmd/projector ./cmd/reducer
git diff --check
```

Expected: PASS

- [ ] **Step 4: Sync the ADR with the hardening work**

Update the ADR so it records:

1. repair/reconciliation as the liveness hardening path
2. code-call grouped chunking as a secondary optimization
3. contention-proof tests as part of the validation story

Do not rewrite the root decision. This is hardening, not a new architectural
direction.

- [ ] **Step 5: Run docs verification if docs changed**

Run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go/internal/reducer/code_call_projection_runner_test.go go/internal/reducer/shared_projection_worker_test.go go/internal/storage/neo4j/edge_writer_test.go docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md docs/docs/reference/local-testing.md
git commit -m "test: prove deadlock elimination hardening behavior"
```

## Final Verification Checklist

- readiness gating remains the primary correctness contract
- missed readiness publications self-heal through repair
- repair never revives stale generations
- blocked work remains pending and does not bypass readiness
- `DomainCodeCalls` uses smaller managed Neo4j transaction groups
- non-code-call domains preserve current grouped execution behavior
- contention-proof tests show no early edge execution
- full Go test gate passes
- `go vet` passes
- `git diff --check` passes
- docs build passes if docs changed

Plan complete and saved to `docs/superpowers/plans/2026-04-17-neo4j-deadlock-elimination-hardening-implementation.md`. Ready to execute?
