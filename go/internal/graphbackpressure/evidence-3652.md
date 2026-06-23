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

`buildReducerService` now wraps the base `neo4jExec` via
`boundReducerGraphWrites` before any writer is derived, so handler edge writers,
canonical writers, shared projection, secrets/IAM, orphan sweep, workload
materializers, and the semantic entity writer all draw from one shared in-flight
permit pool. The previous second `graphbackpressure.Wrap` on only the semantic
executor was removed so there is exactly one semaphore, not two independent ones.

This is not a serialization fix: `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` is a
configurable ceiling greater than one sized to backend headroom; a non-positive
value is a passthrough no-op that preserves the inner executor's interface.

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

No-Regression Evidence: All three changes are correctness fixes on paths that
were previously broken or no-ops. P1 replaced a guaranteed
`errInnerNoExecuteGroup` failure (semantic materialization failing outright when
the knob was enabled with grouped writes off) with the working sequential
fallback. P2 made the documented reducer-wide knob actually bound non-semantic
writers it previously ignored, removing a redundant second semaphore (fewer
allocations, one pool). P3 replaced an unbounded UPDATE (all matching rows) plus
a Go scan-loop cap with a DB-side `LIMIT`, strictly reducing the rows mutated per
drain call. No write throughput is lost on the happy path: a non-positive
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` remains a passthrough, and unlimited replay still
uses the original single-statement template. Verified with
`go test ./internal/storage/cypher ./internal/graphbackpressure ./cmd/reducer
./internal/storage/postgres -count=1 -race` (concurrency tests run under the race
detector).

## Observability

No-Observability-Change: The `GraphWriteBackpressureEngaged` counter and
`GraphWriteBackpressureWaitDuration` histogram introduced in #3648 are unchanged
and remain wired through `graphbackpressure.NewObserver`. P1/P2 make those signals
now fire for every reducer graph writer that hits the shared bound, not only the
semantic entity path, so an operator watching the engaged counter and wait
histogram at 3 AM sees backpressure across all reducer writes. The drain path's
existing `BacklogDepthBefore` count (read before the bounded replay) continues to
report the backlog a drain set out to move; the bounded UPDATE makes
`Replayed` an honest count of rows actually reset rather than a truncated view of
a larger silent mutation.
