# Performance & Observability Evidence — Issue #3652

Review-bot follow-up to merged PR #3648 (write-timeout backpressure executor +
dead-letter backlog drain, issue #3560). Three correctness fixes on the graph
write and dead-letter recovery hot paths.

## Conflict domain

- **Graph write permit pool** (`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT`): one bounded
  semaphore in `cypher.BackpressureExecutor` shared by every reducer graph
  writer. A slow backend holds permits, blocks additional writers at the write
  boundary, and slows intake (closed-loop backpressure) instead of converting
  transient slowness into a `graph_write_timeout` dead-letter flood.
- **`fact_work_items` terminal rows** during a dead-letter backlog drain: the
  rows a drain resets from `dead_letter`/`failed` back to `pending`.

## P1 — BackpressureExecutor must not advertise ExecuteGroup when inner lacks it

`graphbackpressure.Wrap` now returns `cypher.ExecuteOnlyBackpressureExecutor`
(no `ExecuteGroup` method) when the inner executor does not implement
`GroupExecutor`. With `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=false` the inner
executor is `ExecuteOnlyExecutor`, so `SemanticEntityWriter.WriteSemanticEntities`
now correctly takes the sequential fallback instead of hitting
`errInnerNoExecuteGroup`. When inner does implement `GroupExecutor`, the grouped
permit-bounded path is preserved.

Worker/lease settings: unchanged. Permit pool size = `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT`.

## P2 — bound all reducer graph writers, not just the semantic executor

`buildReducerService` builds ONE shared `cypher.BackpressureGate` via
`reducerGraphWriteGate` and applies it to every reducer graph write path:
handler edge writers, canonical writers, shared projection, secrets/IAM, orphan
sweep, and the semantic entity writer all derive from the gate-wrapped base
`neo4jExec`, and the workload + infrastructure-platform materializers (the
separate `reducer.CypherExecutor` `ExecuteCypher` path) derive from the
gate-wrapped `cypherExec` (see "P2-followup" below). All paths share one permit
pool — not per-wrapper sub-pools that would each admit `maxInFlight` writers.

This is not a serialization fix: `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` is a
configurable ceiling greater than one sized to backend headroom; a non-positive
value yields a nil gate that is a passthrough no-op preserving each inner
executor's interface.

## P1-followup — keep the permit OUTSIDE the semantic write timeout

The semantic path adds a per-statement `TimeoutExecutor`
(`ESHU_CANONICAL_WRITE_TIMEOUT`). The permit gate is now composed as the
OUTERMOST layer (`gate.boundExecutor(TimeoutExecutor{Inner: rawBase})`), so a
permit is acquired BEFORE the write timeout starts. Permit-wait therefore no
longer counts against `ESHU_CANONICAL_WRITE_TIMEOUT`: a saturated pool delays a
queued semantic write but never times it out, which is the whole point of the
backpressure. Previously the gate-wrapped base was passed INTO the timeout
adapter, so a queued write burned its write-timeout budget while waiting and
failed as `graph_write_timeout` — reintroducing the dead-letter flood. The
`cypher.BackpressureGate` was extracted from `BackpressureExecutor` so the
Executor path, grouped path, and materializer path all share one pool while each
sits at the correct outermost position in its own layering.

Regression: `TestSemanticPathPermitWaitIsOutsideWriteTimeout` (cmd/reducer)
saturates a single-permit pool and proves a queued write whose permit-wait
exceeds the timeout still sees a full, unexpired deadline at the backend and
succeeds. A throwaway inversion (timeout outside the gate) confirmed the test
fails as `graph_write_timeout` against the buggy layering.

## P2-followup — bound the materializer CypherExecutor path

The workload and infrastructure-platform materializers write through
`reducer.CypherExecutor.ExecuteCypher`, a separate adapter from the canonical
`cypher.Executor`. `WrapCypherExecutorWithGate` now wraps that path on the same
shared gate (`gate.boundCypherExecutor(cypherExec)`), so materializer writes draw
from the same permit pool as canonical and semantic writes instead of bypassing
the bound entirely.

Regression: `TestReducerGraphWriteGateSharesOnePoolAcrossExecutorAndMaterializer`
(cmd/reducer) mixes Executor and materializer writes against one pool and proves
the combined in-flight count never exceeds the ceiling.

## P3 — bound the drain UPDATE with a LIMIT subquery

`ReplayFailedWorkItems` now selects the bounded template when `filter.Limit > 0`:

```
WHERE work_item_id IN (
    SELECT work_item_id FROM fact_work_items
    WHERE status IN ('dead_letter', 'failed') <predicate>
    ORDER BY work_item_id
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
```

`Limit=100` against thousands of `retry_exhausted` rows now resets exactly 100
rows to `pending` instead of resetting every matching row and reporting only 100
IDs. `FOR UPDATE SKIP LOCKED` keeps the bound a true cap under concurrent drains
rather than a serialization point: two concurrent drains lock disjoint row sets
and both make forward progress. `ORDER BY work_item_id` makes the selected set
deterministic so repeated bounded drains advance.

Before: row UPDATEs per drain call = all matching terminal rows (unbounded).
After: row UPDATEs per drain call = min(Limit, matching rows).

## Performance Evidence

No-Regression Evidence: All changes are correctness fixes on paths that were
previously broken or no-ops. P1 replaced a guaranteed `errInnerNoExecuteGroup`
failure with the working sequential fallback. P2 made the reducer-wide knob
actually bound non-semantic writers it previously ignored. P1-followup moved the
permit OUTSIDE the semantic write timeout so a saturated pool no longer converts
permit-wait into spurious `graph_write_timeout` dead letters (the dead-letter
flood the knob exists to prevent). P2-followup bounded the materializer
`ExecuteCypher` path that previously bypassed the pool. P3 replaced an unbounded
UPDATE plus a Go scan-loop cap with a DB-side `LIMIT`, strictly reducing rows
mutated per drain call. No write throughput is lost on the happy path: a
non-positive `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` yields a nil gate (passthrough),
and unlimited replay still uses the original single-statement template. The
`BackpressureGate` extraction is behavior-preserving for the existing
`BackpressureExecutor` API. Verified with `go test ./internal/storage/cypher
./internal/storage/postgres ./internal/recovery ./internal/graphbackpressure
./cmd/reducer -count=1 -race` — 1832 passed, 0 failed (concurrency tests run
under the race detector).

## Observability

No-Observability-Change: The `GraphWriteBackpressureEngaged` counter and
`GraphWriteBackpressureWaitDuration` histogram introduced in #3648 are unchanged
and remain wired through `graphbackpressure.NewObserver` onto the shared gate. P2
and P2-followup make those signals fire for every reducer graph writer that hits
the shared bound — canonical, semantic, and materializer paths — not only the
semantic entity path, so an operator watching the engaged counter and wait
histogram at 3 AM sees backpressure across all reducer writes (the materializer
permit-wait is labelled `materialize_cypher`). P1-followup means a queued
semantic write under backpressure now records a backpressure-wait signal rather
than a `graph_write_timeout` failure, so the operator sees "delayed by
backpressure" instead of "dead-lettered". The drain path's existing
`BacklogDepthBefore` count continues to report the backlog a drain set out to
move; the bounded UPDATE makes `Replayed` an honest count of rows actually reset
rather than a truncated view of a larger silent mutation.
