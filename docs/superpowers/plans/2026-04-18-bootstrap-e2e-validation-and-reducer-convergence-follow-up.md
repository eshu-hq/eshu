# Bootstrap E2E Validation And Reducer Convergence Follow-Up Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the current E2E validation ADR implementation for deferred bootstrap backfill and reopen observability, then capture the next reducer full-convergence optimization track in a separate ADR so the monster-repo and queue-fairness work is not lost.

**Architecture:** Keep the current ADR narrowly focused on bootstrap correctness, bootstrap-helper performance, reopen semantics, and telemetry. Treat the remaining reducer full-convergence bottlenecks as a separate architecture problem with its own ADR covering Neo4j query-shape optimization and reducer scheduling fairness.

**Tech Stack:** Go, PostgreSQL, Neo4j, OpenTelemetry, MkDocs

---

## Chunk 1: Finish Current ADR Runtime And Test Alignment

**Files:**
- Modify: `go/cmd/bootstrap-index/main.go`
- Modify: `go/cmd/bootstrap-index/main_test.go`
- Modify: `go/internal/storage/postgres/ingestion.go`
- Modify: `go/internal/telemetry/instruments.go`
- Test: `go/cmd/bootstrap-index/main_test.go`
- Test: `go/internal/storage/postgres/ingestion_test.go`

- [ ] **Step 1: Write or update the failing tests first**

Run targeted tests for the bootstrap and storage seams before any code edit:

```bash
cd go && go test ./cmd/bootstrap-index -count=1
cd go && go test ./internal/storage/postgres -run 'TestIngestionStoreBackfillAllRelationshipEvidenceSkipsUnknownTargetGenerations|TestReducerQueueReopenSucceededResetsSucceededWorkItemToPending' -count=1
```

Expected: if the branch is not aligned yet, failures should point at removed terminal-wait assumptions or missing telemetry wiring.

- [ ] **Step 2: Implement the minimal runtime changes**

Ensure the code matches the current ADR claims:

- remove the stale “repair runner catches stragglers” comment
- wire OTEL instruments into deferred backfill and reopen
- keep reopen semantics limited to already-succeeded `deployment_mapping` rows
- keep bootstrap tests aligned with the removed terminal wait

- [ ] **Step 3: Re-run focused tests**

Run:

```bash
cd go && go test ./cmd/bootstrap-index ./internal/storage/postgres ./internal/reducer -count=1
```

Expected: PASS

## Chunk 2: Finalize The Current ADR Document

**Files:**
- Modify: `docs/docs/adrs/2026-04-18-e2e-validation-atomic-writes-deferred-backfill.md`
- Modify: `docs/docs/adrs/2026-04-18-bootstrap-relationship-backfill-quadratic-cost.md`

- [ ] **Step 1: Make the ADR match the implementation exactly**

Cover these points explicitly:

- bootstrap-helper duration versus full reducer convergence
- operator restarts are still required for monster repos
- reopen-window stragglers are not automatically replayed today
- telemetry additions are present in the same changeset
- avoid claiming precise straggler identification unless the code records it directly

- [ ] **Step 2: Verify doc correctness against the code**

Cross-check the ADR against:

- `go/cmd/bootstrap-index/main.go`
- `go/internal/storage/postgres/ingestion.go`
- `go/internal/reducer/cross_repo_resolution.go`
- `go/internal/recovery/replay.go`

## Chunk 3: Draft The Follow-Up Reducer Convergence ADR

**Files:**
- Create: `docs/docs/adrs/2026-04-18-reducer-full-convergence-optimization.md`

- [ ] **Step 1: Write the ADR draft**

The new ADR should be explicitly separate from the bootstrap ADR and frame the remaining problem as:

- monster-repo Neo4j transaction duration
- unlabelled `MATCH` patterns causing full scans and lock amplification
- shared FIFO reducer claim starvation for fast domains
- underutilized hardware because reducer workers block on a few pathological repos

The ADR should compare at least two approaches:

- Neo4j query-shape and transaction optimization first
- reducer queue fairness and domain-aware scheduling first

Recommendation: start with Neo4j query-shape and lock-footprint reduction, then revisit fairness once the true hot-path cost is lower.

- [ ] **Step 2: Include explicit success metrics**

Define measurable targets such as:

- reducer full-convergence wall clock
- queue oldest age
- stuck transaction frequency
- worker utilization
- per-domain drain latency

## Chunk 4: Verify Docs And Repo Hygiene

**Files:**
- Verify only

- [ ] **Step 1: Run the docs gate**

Run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: PASS

- [ ] **Step 2: Run the repo hygiene gate**

Run:

```bash
git diff --check
```

Expected: no whitespace or patch-format errors

- [ ] **Step 3: Summarize outcomes**

Capture:

- which runtime tests passed
- whether docs built cleanly
- which files now represent the current ADR versus the next ADR

