# Neo4j Deadlock Elimination Hardening Design

**Date:** 2026-04-17  
**Status:** Draft for review  
**Related ADR:** `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`  
**Related implementation plan:** `docs/superpowers/plans/2026-04-17-neo4j-deadlock-elimination-implementation.md`

## Summary

Keep the current readiness-gating architecture as the primary deadlock
elimination mechanism, and harden it so the system is both safe and operable.

This next pass adds four things:

1. An exact durable repair path for missed readiness publications
2. A contention-proof test that demonstrates edge work does not start early
3. Operator-visible telemetry for blocked and repaired readiness state
4. Smaller managed Neo4j transaction groups for `DomainCodeCalls` writes

The core decision does not change: unsafe producer/consumer overlap on the
`code_entities_uid` keyspace is the root problem, so bounded-unit readiness
remains the primary correctness contract. Smaller code-call write groups are
added as a secondary optimization, not as the elimination mechanism.

## Goals

1. Preserve the current architectural fix that prevents code-entity edge
   consumers from racing semantic node writers for the same bounded unit.
2. Eliminate the current liveness hole where a successful Neo4j write followed
   by a failed readiness publish can leave work blocked indefinitely.
3. Add proof that the system does not start code-entity edge work before the
   semantic phase is durably complete.
4. Reduce `DomainCodeCalls` transaction footprint without giving up managed
   Neo4j transaction retry semantics.
5. Make blocked and repaired readiness state visible to operators.

## Non-Goals

1. Do not replace readiness gating with batch splitting alone.
2. Do not add a global serializer for Neo4j writes.
3. Do not add timeout-based bypasses that let blocked edge work ignore
   readiness and run anyway.
4. Do not broaden this into a generic workflow engine for every projection
   domain.
5. Do not change the acceptance contract away from freshness-only semantics.

## Current Flow

For one bounded unit:

1. Projector writes canonical nodes into Neo4j.
2. Projector publishes `canonical_nodes_committed` into Postgres.
3. Reducer semantic materialization writes semantic nodes into Neo4j.
4. Reducer publishes `semantic_nodes_committed` into Postgres.
5. `code_calls`, `inheritance_edges`, and `sql_relationships` are selected only
   when accepted generation matches and semantic readiness exists.

This removes the producer/consumer overlap that caused the observed deadlock
class. The remaining weakness is not deadlock correctness. It is liveness: a
successful graph write can become invisible to the readiness contract if the
publish step fails afterward.

## Problem Statement

The current implementation solves the unsafe-overlap problem, but it still has
three operational gaps:

1. **Missed readiness publication can stall work.**
   If the canonical or semantic writer succeeds and the readiness upsert fails,
   downstream consumers remain blocked even though the graph is already in the
   correct state.
2. **We lack contention-proof verification.**
   We have unit coverage for gating behavior but not a proof-oriented test that
   holds the semantic writer path open while edge work is pending.
3. **Code-call edge writes are still transactionally large.**
   Even after readiness unlocks the work, `DomainCodeCalls` still routes all
   batches through one `ExecuteGroup(...)` call. That preserves retries but can
   still create large lock footprints and higher tail latency than needed.

## Design

### 1. Keep readiness gating as the correctness contract

No architectural reversal. The following domains remain gated on
`semantic_nodes_committed` for the same
`scope_id + acceptance_unit_id + source_run_id + generation_id`:

1. `code_calls`
2. `inheritance_edges`
3. `sql_relationships`

`platform_infra`, `repo_dependency`, and `workload_dependency` remain ungated.

This preserves useful concurrency across unrelated repositories, runs, and
domains while preventing unsafe overlap on the `code_entities_uid` keyspace.

### 2. Add durable readiness repair

Introduce an exact repair path that repopulates missing readiness rows when the
graph write succeeded but the readiness publish did not.

Recommended shape:

1. Add a Postgres-backed `graph_projection_phase_repair_queue`.
2. When canonical or semantic readiness publication fails **after** a
   successful graph write, enqueue the exact bounded-unit readiness slice that
   failed to publish.
3. Run a reducer-side repair loop that:
   - lists due repair rows
   - skips stale generations
   - skips rows whose readiness already exists
   - republishes only the exact missing readiness row
   - backs off failed republishes without dropping the repair row

Important constraints:

1. Repair must only publish a phase that is already true. It is a missed-signal
   recovery path, not a speculative publisher.
2. Repair must be safe to run repeatedly.
3. Repair must not mark edge work complete directly. It only restores readiness.
4. Repair must be bounded and incremental so it can run continuously without
   becoming a second ingestion pipeline.
5. Repair must not become a generic scanner over accepted generations when the
   exact failed readiness slice is already known at publish-failure time.

### 3. Add readiness-block and repair telemetry

Instrumentation for this hardening pass should answer:

1. How many bounded units are blocked on missing readiness?
2. How old is the oldest blocked unit by phase and keyspace?
3. How many readiness rows were repaired?
4. How often does primary publication fail?

Minimum signals:

1. Structured logs when work is blocked on readiness
2. Structured logs when readiness repair publishes a missing phase
3. Counter for repaired readiness rows
4. Counter for readiness publication failures
5. Gauge or derived metric for oldest blocked readiness age

### 4. Add code-call-specific managed transaction chunking

This is the secondary optimization.

Current behavior:

1. `EdgeWriter.WriteEdges(...)` builds all code-call batches
2. If the executor supports `GroupExecutor`, all batches go through one
   `ExecuteGroup(...)` call

Target behavior for `DomainCodeCalls` only:

1. Keep `ExecuteGroup(...)` so we retain managed retry semantics.
2. Do not send all code-call statements as one giant grouped transaction.
3. Split the statements into smaller grouped chunks, each executed through its
   own `ExecuteGroup(...)` call.

Why this shape:

1. It reduces per-transaction lock footprint.
2. It preserves the retry behavior we already rely on from managed
   transactions.
3. It narrows the optimization to the domain that showed the highest deadlock
   pressure.

This is not the elimination mechanism. It is a post-readiness optimization that
should improve runtime behavior after the correctness contract has already
removed the unsafe overlap.

## Proposed Components

### Repair path

Likely additions:

1. `go/internal/reducer/graph_projection_phase_repair.go`
2. `go/internal/reducer/graph_projection_phase_repair_test.go`
3. `go/internal/storage/postgres/graph_projection_phase_repair_queue.go`
4. Reducer service wiring in `go/cmd/reducer/main.go`

Responsibilities:

1. Enqueue exact readiness repair rows when primary publication fails
2. Decide whether queued repair is still valid
3. Publish the missing row
4. Emit repair telemetry

### Edge-writer optimization

Likely changes:

1. `go/internal/storage/neo4j/edge_writer.go`
2. `go/internal/storage/neo4j/edge_writer_test.go`

Responsibilities:

1. Add domain-aware grouped chunk size for `DomainCodeCalls`
2. Preserve existing behavior for all other domains
3. Preserve `GroupExecutor` retry semantics

### Contention-proof validation

Likely additions:

1. focused reducer-side concurrency test near
   `go/internal/reducer/code_call_projection_runner_test.go`
2. focused shared-projection test near
   `go/internal/reducer/shared_projection_worker_test.go`
3. if needed, a Neo4j adapter-level execution harness in
   `go/internal/storage/neo4j/edge_writer_test.go`

Responsibilities:

1. Hold semantic readiness unavailable while edge work is pending
2. Prove edge work is skipped, not partially started
3. Release readiness and prove edge work resumes normally
4. Confirm generation scoping remains exact

## Data Flow And Failure Handling

### Happy path

1. Canonical write succeeds.
2. Canonical readiness publishes.
3. Semantic write succeeds.
4. Semantic readiness publishes.
5. Edge selectors see readiness and process work.
6. Code-call edges write in smaller grouped transactions.

### Publication failure after successful graph write

1. Graph write succeeds.
2. Readiness publication fails.
3. Exact repair row is enqueued durably.
4. Downstream work is blocked.
5. Repair loop republishes the missing row.
6. Downstream work resumes on the next selection cycle.

### Invalid behavior we explicitly forbid

1. Running edge work without readiness after a timeout
2. Marking blocked intents stale or completed just because readiness is absent
3. Publishing readiness speculatively before the graph write success boundary

## Edge Cases

1. Canonical readiness exists but semantic readiness does not.
   This remains a normal blocked state, not an error.
2. Semantic readiness is missing because of a failed publish, not a failed
   semantic write.
   Repair must recover this.
3. A newer accepted generation supersedes an older blocked generation.
   Repair must not revive stale work.
4. Multiple repair passes encounter the same missing row.
   Upsert must remain idempotent.
5. Readiness exists for one generation but edge work belongs to another.
   Selection must remain generation-exact.
6. Large code-call edge sets create many transaction chunks.
   The write path must still preserve ordering and retry behavior.

## Testing Strategy

### Unit tests

1. Repair publishes missing readiness only when the bounded unit is still
   authoritative.
2. Repair does nothing for stale generations.
3. Repair is idempotent.
4. Code-call chunking changes only `DomainCodeCalls`.
5. Other domains keep their current grouped execution behavior.

### Concurrency and proof tests

1. Simulate semantic work pending while code-call intents are present.
   Expected: no edge write starts early.
2. Release semantic readiness.
   Expected: code-call work is selected and completes.
3. Simulate readiness publication failure after successful semantic write.
   Expected: repair loop restores readiness and work resumes.

### Verification gate

1. `go test ./internal/projector ./internal/reducer ./internal/storage/postgres -count=1`
2. `go test ./cmd/projector ./cmd/reducer ./internal/storage/neo4j -count=1`
3. `go vet ./internal/projector ./internal/reducer ./internal/storage/postgres ./cmd/projector ./cmd/reducer`
4. `git diff --check`
5. strict MkDocs build if docs change

## Trade-Offs

### Why this is the strongest path

1. It keeps the architectural correctness fix.
2. It closes the liveness hole instead of ignoring it.
3. It adds proof rather than relying on intuition.
4. It improves write-path behavior without reverting to a contention-only story.

### Costs

1. Additional reducer-side coordination and repair code
2. More observability surface
3. More tests and slightly more runtime complexity

These costs are acceptable because the deadlock issue is a correctness and
operability problem, not just a micro-optimization problem.

## Recommendation

Proceed with a hardening pass that:

1. keeps readiness gating as the primary elimination contract
2. adds missed-publication repair
3. adds contention-proof verification
4. adds operator telemetry for blocked and repaired readiness
5. adds code-call-specific managed transaction chunking as a secondary
   optimization

This gives the best outcome: architectural correctness, liveness recovery,
runtime improvement, and proof that the system behaves correctly under
contention.
