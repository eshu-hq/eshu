# Graph Orphan Sweep Runner

## Purpose

Marks and deletes aged zero-relationship graph nodes in bounded cycles,
so retracted entities leave no orphaned residue. Moved here from the
flat `maintenance` package under #7648.

## Ownership boundary

Owns the sweep loop and the single-owner lease discipline. Does not own
the orphan mark/delete Cypher (the storage `Sweeper` implementation),
the `eshu_dp_graph_orphan_nodes` gauge (registered independently by
the reducer), or the `Service` side-runner startup loop in the reducer
root.

## Exported surface

- `Runner`, `Config` — the sweep loop and its bounds
- `Policy` — orphan TTL, batch/count limits, labels
- `Result` — per-label counts, marked, deleted, skipped
- `Sweeper` — storage port (`SweepOrphanNodes`)
- `PartitionLeaseManager` — local mirror of the root lease contract
- `ErrSweeperRequired` — missing-wiring marker

See `doc.go` for the full contract.

## Dependencies

- `internal/telemetry` — log attributes and phase/failure-class keys
- `pkg/log` — structured error logging

Never `internal/reducer`.

## Telemetry

None of its own. The runner records structured completion/failure logs
(`phase=reduction`, `failure_class=graph_orphan_sweep_error` on
failure), both carrying `lease_ttl_seconds`: the configured lease TTL
on completion, and the TTL that would have guarded the cycle on
failure. A failed deferred release logs a WARN carrying the TTL after
which the unreleased lease expires server-side.

## Gotchas / invariants

- The runner MUST claim its single-owner partition lease before
  sweeping when a `LeaseManager` is wired.
- The 10m lease TTL outlasts the graph write budget with margin
  (#7047); if a cycle ever outgrows a fixed TTL, the renewal
  precedent is a TTL/2 same-owner re-claim (#4449), not a longer TTL.
- The release runs through a cancellation-proof context with a 10s
  bound, so shutdown cannot hang on a dead backend nor strand the
  lease (#6747 shape A).

## Related docs

- `docs/public/observability/telemetry-coverage.md`
