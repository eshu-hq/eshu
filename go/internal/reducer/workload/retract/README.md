# retract

Stale repository-edge retract for `workload_materialization`, added for #7285.

## Why it exists

The canonical projector's `repository_cleanup` used to `DETACH DELETE` the
`Repository` node on every non-delta attempt. That cleared stale `DEFINES`
edges, but it also cleared every reducer and cross-scope edge on the node, and
a projector retry or liveness re-drive never re-runs those writers. The
projector now `MERGE`s the node in place. The only writer of `DEFINES` and of
the repository-side `EXPOSES_ENDPOINT` edge, `workload_materialization`, now
removes its own stale edges.

## What it owns

| piece | file | what it does |
|---|---|---|
| `KeepList`, `KeepLists` | `retract.go` | the current workload and endpoint ids per repository |
| `FullGenerationRepositoryIDs` | `retract.go` | repositories whose repository fact is not `delta_generation` |
| `RepositoryEdges` | `retract.go` | the two keep-list `DELETE rel` statements, batched |
| `Executor`, `CountingExecutor`, `ErrUncounted` | `retract.go` | graph port and the optional delete-count capability |
| `Observe` | `observe.go` | metric and completion log |

## Contract

- Both statements anchor on `(repo:Repository {id: row.repo_id})`, which uses
  the `repository_id` uniqueness constraint. They then expand only the typed
  edge, never `REPO_CONTAINS`.
- They delete only edges whose `evidence_source` equals the caller's source
  (`finalization/workloads`). Another writer's `DEFINES` and every edge of
  another repository survive.
- Keep-lists are always bound as explicit lists. A missing key or a null value
  makes `NOT (x IN null)` null, and the statement deletes nothing. An empty list
  deletes every edge the source wrote for the repository: this is the
  zero-candidate path.
- The caller runs it after the current edges committed, and only for
  repositories from `FullGenerationRepositoryIDs`. A delta generation reads
  partial facts and never retracts.
- It is idempotent. A retry deletes nothing new. Same-scope races cannot happen,
  because the reducer queue's platform-graph conflict key serializes
  `workload_materialization` per scope.

## Telemetry

`Observe` adds measured deletes to
`eshu_dp_reconciliation_drift_retractions_total{domain="workload_materialization",
write_phase="defines_retract"|"repository_endpoint_retract", kind="edge"}`.
It does this only when the executor chain implements `CountingExecutor`; in
production that is `cmd/reducer`'s `reducerCypherExecutor`, forwarded by the
graph-write backpressure gate.

Every run also logs `workload repository edge retract completed` with these
fields:

- `scope_id`
- `generation_id`
- `repository_count`
- `kept_workload_count`
- `kept_endpoint_count`
- `defines_deleted`
- `repository_endpoint_edges_deleted`
- `deletes_counted`
- `duration_s`

Evidence: `docs/internal/evidence/7285-repository-cleanup-keeps-reducer-edges.md`.
