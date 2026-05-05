# Neo4j Deadlock Elimination Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the current Neo4j deadlock class by introducing durable code-entity keyspace readiness and gating edge consumers on committed node readiness without collapsing reducer concurrency to one-at-a-time execution.

**Architecture:** Keep `shared_projection_acceptance` as the freshness contract and add a second durable Postgres contract for graph-write readiness. Publish `canonical_nodes_committed` after projector canonical writes and `semantic_nodes_committed` after semantic reducer writes, both scoped by `(scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace)`. Then teach the code-call runner and the shared projection runner to skip not-ready code-entity edge work for the same bounded unit while preserving parallelism across other repositories, runs, and partitions.

**Tech Stack:** Go, PostgreSQL, Neo4j, OTEL telemetry, MkDocs

---

## Scope And Guardrails

This plan is intentionally narrow and opinionated:

1. Do not redefine `shared_projection_acceptance`. It already models freshness and authoritative generation selection.
2. Do not serialize all reducer work behind one global lock or one worker.
3. Do not rely on smaller Neo4j batches, ad-hoc retries, or “retry later” as the primary fix.
4. Do not widen this into a generic workflow engine. Solve the current `code_entities_uid` keyspace collision first.
5. Preserve concurrency across unrelated repositories, unrelated source runs, and unrelated shared projection domains.

## Current Code Boundaries

These are the files that define the current deadlock boundary and should anchor the refactor:

- `go/internal/storage/postgres/shared_projection_acceptance.go`
- `go/internal/storage/postgres/accepted_generation.go`
- `go/internal/storage/postgres/code_call_intent_writer.go`
- `go/internal/storage/postgres/shared_intents.go`
- `go/internal/projector/runtime.go`
- `go/cmd/projector/runtime_wiring.go`
- `go/internal/reducer/semantic_entity_materialization.go`
- `go/internal/reducer/code_call_projection_runner.go`
- `go/internal/reducer/shared_projection_runner.go`
- `go/internal/reducer/shared_projection.go`
- `go/internal/reducer/defaults.go`
- `go/internal/reducer/intent_emission.go`
- `go/internal/reducer/code_call_materialization_intents.go`
- `go/cmd/reducer/main.go`
- `go/internal/storage/neo4j/semantic_entity.go`
- `go/internal/storage/neo4j/canonical_node_writer.go`

## Planned File Structure

### Create

- `schema/data-plane/postgres/012_graph_projection_phase_state.sql`
- `go/internal/reducer/graph_projection_phase.go`
- `go/internal/reducer/graph_projection_phase_test.go`
- `go/internal/storage/postgres/graph_projection_phase_state.go`
- `go/internal/storage/postgres/graph_projection_phase_state_test.go`

### Modify

- `go/internal/projector/runtime.go`
- `go/internal/projector/runtime_test.go`
- `go/cmd/projector/runtime_wiring.go`
- `go/cmd/projector/runtime_wiring_test.go`
- `go/internal/reducer/semantic_entity_materialization.go`
- `go/internal/reducer/semantic_entity_materialization_test.go`
- `go/internal/reducer/code_call_projection_runner.go`
- `go/internal/reducer/code_call_projection_runner_test.go`
- `go/internal/reducer/shared_projection.go`
- `go/internal/reducer/shared_projection_runner.go`
- `go/internal/reducer/shared_projection_runner_test.go`
- `go/internal/reducer/defaults.go`
- `go/internal/reducer/defaults_test.go`
- `go/internal/reducer/code_call_materialization_intents.go`
- `go/internal/reducer/intent_emission.go`
- `go/cmd/reducer/main.go`
- `go/cmd/reducer/main_test.go`
- `go/internal/storage/postgres/schema.go`

## Target Contract

The implementation should introduce these concepts explicitly:

- `GraphProjectionKeyspace`: start with `code_entities_uid`
- `GraphProjectionPhase`: start with `canonical_nodes_committed` and `semantic_nodes_committed`
- `GraphProjectionPhaseKey`: `scope_id`, `acceptance_unit_id`, `source_run_id`, `generation_id`, `keyspace`
- `GraphProjectionPhaseStore`: durable Postgres upsert and lookup
- `GraphProjectionPhasePrefetch`: batch current-cycle lookups to avoid one query per intent row
- `GraphProjectionPhasePublisher`: projector and semantic reducer success hook
- `GraphProjectionReadinessLookup`: runner-facing predicate that answers “is this bounded unit ready for this edge domain?”

The first version only needs one keyspace and two phases. Keep the contract small.

## Chunk 1: Add Durable Graph Projection Phase State

**Files:**
- Create: `schema/data-plane/postgres/012_graph_projection_phase_state.sql`
- Create: `go/internal/reducer/graph_projection_phase.go`
- Create: `go/internal/reducer/graph_projection_phase_test.go`
- Create: `go/internal/storage/postgres/graph_projection_phase_state.go`
- Create: `go/internal/storage/postgres/graph_projection_phase_state_test.go`
- Modify: `go/internal/storage/postgres/schema.go`
- Test: `go/internal/reducer/graph_projection_phase_test.go`
- Test: `go/internal/storage/postgres/graph_projection_phase_state_test.go`

- [ ] **Step 1: Write the failing reducer contract tests**

```go
func TestGraphProjectionPhaseKeyValidate(t *testing.T) {
	key := GraphProjectionPhaseKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
```

Run:

```bash
cd go && go test ./internal/reducer -run 'TestGraphProjectionPhase' -count=1
```

Expected: FAIL because the contract types do not exist yet.

- [ ] **Step 2: Write the failing Postgres store tests**

```go
func TestGraphProjectionPhaseStateStoreUpsertAndLookup(t *testing.T) {
	db := newGraphProjectionPhaseStateTestDB()
	store := NewGraphProjectionPhaseStateStore(db)
	key := reducer.GraphProjectionPhaseKey{...}

	err := store.Upsert(context.Background(), []GraphProjectionPhaseState{{
		Key:       key,
		Phase:     reducer.GraphProjectionPhaseSemanticNodesCommitted,
		CommittedAt: now,
		UpdatedAt: now,
	}})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if ready, found, err := store.Lookup(context.Background(), key, reducer.GraphProjectionPhaseSemanticNodesCommitted); err != nil || !found || !ready {
		t.Fatalf("Lookup(...) = (%v, %v, %v), want (true, true, nil)", ready, found, err)
	}
}
```

Run:

```bash
cd go && go test ./internal/storage/postgres -run 'TestGraphProjectionPhaseStateStore' -count=1
```

Expected: FAIL because the table and store do not exist yet.

- [ ] **Step 3: Implement the phase contract and Postgres store**

Implement:

- `GraphProjectionKeyspaceCodeEntitiesUID`
- `GraphProjectionPhaseCanonicalNodesCommitted`
- `GraphProjectionPhaseSemanticNodesCommitted`
- `GraphProjectionPhaseKey.Validate()`
- `GraphProjectionPhaseState`
- SQL DDL keyed by `(scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)`
- `EnsureSchema`
- `Upsert`
- exact `Lookup`
- cycle-local `Prefetch`

Use `init()` bootstrap registration exactly like `shared_projection_acceptance.go`.

- [ ] **Step 4: Verify the new persistence layer**

Run:

```bash
cd go && go test ./internal/reducer ./internal/storage/postgres -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add schema/data-plane/postgres/012_graph_projection_phase_state.sql go/internal/reducer/graph_projection_phase.go go/internal/reducer/graph_projection_phase_test.go go/internal/storage/postgres/graph_projection_phase_state.go go/internal/storage/postgres/graph_projection_phase_state_test.go go/internal/storage/postgres/schema.go
git commit -m "feat: add durable graph projection phase state"
```

## Chunk 2: Publish Canonical And Semantic Readiness At The True Commit Boundaries

**Files:**
- Modify: `go/internal/projector/runtime.go`
- Modify: `go/internal/projector/runtime_test.go`
- Modify: `go/cmd/projector/runtime_wiring.go`
- Modify: `go/cmd/projector/runtime_wiring_test.go`
- Modify: `go/internal/reducer/semantic_entity_materialization.go`
- Modify: `go/internal/reducer/semantic_entity_materialization_test.go`
- Modify: `go/internal/reducer/defaults.go`
- Modify: `go/internal/reducer/defaults_test.go`
- Modify: `go/internal/reducer/intent_emission.go`
- Modify: `go/internal/reducer/code_call_materialization_intents.go`
- Modify: `go/cmd/reducer/main.go`
- Modify: `go/cmd/reducer/main_test.go`
- Test: `go/internal/projector/runtime_test.go`
- Test: `go/internal/reducer/semantic_entity_materialization_test.go`
- Test: `go/cmd/projector/runtime_wiring_test.go`
- Test: `go/cmd/reducer/main_test.go`

- [ ] **Step 1: Write the failing projector readiness publication test**

```go
func TestRuntimeProjectPublishesCanonicalNodesCommittedAfterCanonicalWrite(t *testing.T) {
	writer := &recordingCanonicalWriter{}
	publisher := &recordingGraphProjectionPhasePublisher{}
	runtime := Runtime{
		CanonicalWriter: writer,
		PhasePublisher:  publisher,
		IntentWriter:    &recordingIntentWriter{},
	}

	_, err := runtime.Project(ctx, scopeValue, generation, facts)
	if err != nil {
		t.Fatalf("Project() error = %v", err)
	}
	if len(publisher.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(publisher.calls))
	}
	if got := publisher.calls[0].Phase; got != reducer.GraphProjectionPhaseCanonicalNodesCommitted {
		t.Fatalf("Phase = %q, want canonical_nodes_committed", got)
	}
}
```

Run:

```bash
cd go && go test ./internal/projector -run 'TestRuntimeProjectPublishesCanonicalNodesCommittedAfterCanonicalWrite' -count=1
```

Expected: FAIL because projector runtime does not publish phase readiness yet.

- [ ] **Step 2: Write the failing semantic readiness publication test**

```go
func TestSemanticEntityMaterializationPublishesSemanticNodesCommitted(t *testing.T) {
	handler := SemanticEntityMaterializationHandler{
		FactLoader: factsLoader,
		Writer: writer,
		PhasePublisher: publisher,
	}

	_, err := handler.Handle(ctx, intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := publisher.calls[0].Phase; got != GraphProjectionPhaseSemanticNodesCommitted {
		t.Fatalf("Phase = %q, want semantic_nodes_committed", got)
	}
}
```

Run:

```bash
cd go && go test ./internal/reducer -run 'TestSemanticEntityMaterializationPublishesSemanticNodesCommitted' -count=1
```

Expected: FAIL because semantic materialization does not publish phase readiness yet.

- [ ] **Step 3: Implement publication at the real boundaries**

Implementation notes:

- Add an optional `PhasePublisher` to `projector.Runtime`.
- After `CanonicalWriter.Write(...)` succeeds, derive the bounded-unit context from repository facts and publish `canonical_nodes_committed`.
- Do not publish anything before the Neo4j write returns successfully.
- Extend `SemanticEntityMaterializationHandler` with an optional `PhasePublisher`.
- Reuse `ProjectionContext` instead of inventing a second bounded-unit identity type.
- If the semantic handler loads facts and finds no semantic rows, do not publish `semantic_nodes_committed`.
- Keep the publication outside the Neo4j transaction boundary but strictly after successful completion of the write call.

- [ ] **Step 4: Wire the new publisher through service construction**

Use the Postgres-backed publisher in:

- `go/cmd/projector/runtime_wiring.go`
- `go/cmd/reducer/main.go`

Update constructor tests so nil wiring still behaves safely and non-nil wiring is verified.

- [ ] **Step 5: Verify publication behavior**

Run:

```bash
cd go && go test ./internal/projector ./internal/reducer ./cmd/projector ./cmd/reducer -count=1
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go/internal/projector/runtime.go go/internal/projector/runtime_test.go go/cmd/projector/runtime_wiring.go go/cmd/projector/runtime_wiring_test.go go/internal/reducer/semantic_entity_materialization.go go/internal/reducer/semantic_entity_materialization_test.go go/internal/reducer/defaults.go go/internal/reducer/defaults_test.go go/internal/reducer/intent_emission.go go/internal/reducer/code_call_materialization_intents.go go/cmd/reducer/main.go go/cmd/reducer/main_test.go
git commit -m "feat: publish graph projection readiness after canonical and semantic writes"
```

## Chunk 3: Gate Code-Entity Edge Consumers On Semantic Readiness

**Files:**
- Modify: `go/internal/reducer/shared_projection.go`
- Modify: `go/internal/reducer/shared_projection_runner.go`
- Modify: `go/internal/reducer/shared_projection_runner_test.go`
- Modify: `go/internal/reducer/code_call_projection_runner.go`
- Modify: `go/internal/reducer/code_call_projection_runner_test.go`
- Modify: `go/cmd/reducer/main.go`
- Modify: `go/cmd/reducer/main_test.go`
- Test: `go/internal/reducer/shared_projection_runner_test.go`
- Test: `go/internal/reducer/code_call_projection_runner_test.go`
- Test: `go/cmd/reducer/main_test.go`

- [ ] **Step 1: Write the failing code-call gating test**

```go
func TestCodeCallProjectionRunnerSkipsAcceptanceUnitUntilSemanticNodesCommitted(t *testing.T) {
	reader := &fakeCodeCallIntentStore{...}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter: &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen: acceptedGenerationFixed("gen-1", true),
		ReadinessLookup: func(key GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool) {
			return false, false
		},
	}

	key, err := runner.selectAcceptanceUnitWork(context.Background())
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if key != (SharedProjectionAcceptanceKey{}) {
		t.Fatalf("key = %#v, want zero value while semantic readiness is missing", key)
	}
}
```

Run:

```bash
cd go && go test ./internal/reducer -run 'TestCodeCallProjectionRunnerSkipsAcceptanceUnitUntilSemanticNodesCommitted' -count=1
```

Expected: FAIL because the runner currently uses accepted generation only.

- [ ] **Step 2: Write the failing shared projection gating test**

```go
func TestSelectPartitionBatchSkipsSQLAndInheritanceRowsUntilSemanticNodesCommitted(t *testing.T) {
	result, err := SelectPartitionBatch(
		ctx,
		reader,
		DomainSQLRelationships,
		0,
		1,
		100,
		acceptedGenerationFixed("gen-1", true),
		nil,
		readinessLookup,
		nil,
	)
	if err != nil {
		t.Fatalf("SelectPartitionBatch() error = %v", err)
	}
	if len(result.LatestRows) != 0 {
		t.Fatalf("len(LatestRows) = %d, want 0 until semantic readiness exists", len(result.LatestRows))
	}
}
```

Run:

```bash
cd go && go test ./internal/reducer -run 'TestSelectPartitionBatchSkipsSQLAndInheritanceRowsUntilSemanticNodesCommitted' -count=1
```

Expected: FAIL because the shared partition selector has no readiness gate.

- [ ] **Step 3: Implement runner-facing readiness lookups and prefetch**

Add runner-level support for:

- `GraphProjectionReadinessLookup`
- `GraphProjectionReadinessPrefetch`
- domain helper for “requires semantic node readiness”
- code-call runner gate on `semantic_nodes_committed`
- shared projection gate only for `DomainInheritanceEdges` and `DomainSQLRelationships`

Important behavior:

- `DomainPlatformInfra`, `DomainRepoDependency`, and `DomainWorkloadDependency` must remain ungated.
- “not ready yet” must not mark intents stale or completed.
- the selector must keep scanning for another ready acceptance unit instead of immediately bailing on the first blocked unit when more work is available deeper in the pending slice.
- use prefetch to avoid one readiness query per row when scanning a batch.

- [ ] **Step 4: Add observability for blocked readiness**

Record a structured log or metric when a bounded unit is skipped because readiness is missing. The first implementation can reuse existing runner logger/instrument hooks; do not invent a full telemetry subsystem here.

- [ ] **Step 5: Wire the readiness lookup in reducer main**

Create the Postgres-backed lookup and prefetch once in `go/cmd/reducer/main.go` and pass it into both:

- `SharedProjectionRunner`
- `CodeCallProjectionRunner`

- [ ] **Step 6: Verify runner gating**

Run:

```bash
cd go && go test ./internal/reducer ./cmd/reducer -count=1
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go/internal/reducer/shared_projection.go go/internal/reducer/shared_projection_runner.go go/internal/reducer/shared_projection_runner_test.go go/internal/reducer/code_call_projection_runner.go go/internal/reducer/code_call_projection_runner_test.go go/cmd/reducer/main.go go/cmd/reducer/main_test.go
git commit -m "feat: gate code entity edge consumers on semantic readiness"
```

## Chunk 4: Regression Proof, Performance Guardrails, And Docs Sync

**Files:**
- Modify: `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`
- Modify: `docs/docs/reference/local-testing.md` only if verification commands or operator expectations changed
- Test: focused Go package gate from the local testing runbook
- Test: docs build if docs changed

- [ ] **Step 1: Add regression tests that prove the contract, not the implementation detail**

At minimum ensure these remain covered:

- projector publishes canonical readiness only after canonical write success
- semantic reducer publishes semantic readiness only after semantic write success
- code-call runner ignores not-ready acceptance units and later processes them once ready
- shared projection runner gates SQL and inheritance domains only
- freshness and readiness remain independent: accepted generation can exist while readiness does not

- [ ] **Step 2: Run the facts-first verification gate**

Run:

```bash
cd go && go test ./internal/projector ./internal/reducer ./internal/storage/postgres -count=1
cd go && go test ./cmd/projector ./cmd/reducer ./internal/storage/neo4j -count=1
cd go && go vet ./internal/projector ./internal/reducer ./internal/storage/postgres ./cmd/projector ./cmd/reducer
```

Expected: PASS

- [ ] **Step 3: Run repo hygiene and docs verification**

Run:

```bash
git diff --check
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: PASS

- [ ] **Step 4: Update the ADR with implementation reality**

Once code is merged in this branch, update the ADR so it references the concrete store, publisher, and gating contracts instead of future-tense prose.

- [ ] **Step 5: Commit**

```bash
git add docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md docs/docs/reference/local-testing.md
git commit -m "docs: sync deadlock elimination docs with implementation"
```

## Execution Notes

Use these rules during implementation:

1. Keep the new readiness store independent from acceptance/freshness. Mixing them back together recreates the original ambiguity.
2. Prefer helper extraction over duplicating repository context derivation in projector and reducer paths.
3. Keep new files small. If `code_call_projection_runner.go` or `shared_projection.go` starts to balloon, split readiness-specific logic into dedicated files before crossing 500 lines.
4. Preserve existing retry semantics on Neo4j writes. Do not replace managed transactional behavior with ad-hoc `Run()` calls to “simplify” the change.
5. If a test becomes hard to write, that is a design smell. Tighten the interface rather than mocking deeper internals.

## Final Verification Checklist

- `shared_projection_acceptance` still models freshness only
- `graph_projection_phase_state` models readiness only
- canonical and semantic readiness are published only after successful writes
- code-call, SQL, and inheritance edge consumers do not run before semantic readiness
- non-code-entity shared projection domains still flow concurrently
- no global reducer serialization was introduced
- all focused Go tests pass
- `go vet` passes for touched packages
- `git diff --check` passes

Plan complete and saved to `docs/superpowers/plans/2026-04-17-neo4j-deadlock-elimination-implementation.md`. Ready to execute.
