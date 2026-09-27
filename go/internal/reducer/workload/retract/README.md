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
| `RepositoryEdges` | `retract.go` | guarded or keep-list retract, batched |
| `Reader`, guard reads and id-scoped deletes, `Mode*` | `guard.go` | read current targets, compute the stale set, delete only it |
| `Executor`, `CountingExecutor`, `ErrUncounted` | `retract.go` | graph port and the optional delete-count capability |
| `Run`, `Observe` | `observe.go` | the intent a retract ran for; metric and completion log |

## Guard (NornicDB#296)

A zero-row relationship `DELETE` on NornicDB costs proportional to store size
(`docs/public/reference/nornicdb-pitfalls.md`). In steady state nothing is
stale, so an unconditional retract would pay that cost on every run.

- With a `Reader` (production: the reducer's shared graph query runner),
  `RepositoryEdges` reads each repository's current `DEFINES` and
  repository-side `EXPOSES_ENDPOINT` targets. The read has the same
  `MATCH`/`WHERE` as the keep-list delete, minus the keep-list. It computes the
  stale set in Go and deletes exactly those `(repo_id, target_id)` pairs. The
  delete is anchored on both ids and still checks `evidence_source`. No stale
  pair means no `DELETE` statement (`retract_mode=guarded`).
- A row the read returns for a repository outside the keep-lists, or with a
  blank id, is ignored, so an over-broad read cannot widen the delete.
- No reader (`unguarded_no_reader`) or a failed read (`unguarded_read_failed`)
  runs the two keep-list statements unconditionally. That fails toward
  deleting: a redundant delete is only slow, a skipped one leaves stale edges
  permanently. A failed read never fails the intent. Both modes log at WARN;
  `unguarded_no_reader` in production means the reader was not wired.
- On Neo4j 2026.09.0 the read plans `NodeUniqueIndexSeek` on `repository_id`
  then `Expand(All)` over `DEFINES` (17 db hits on a 12,403-file repository).
  The guarded delete seeks the target's unique index (`workload_id` /
  `endpoint_id`) and expands from that side (7 db hits).
- Remove the guard with the other NornicDB#296 guards once the pin moves past
  that fix.

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
- The keep-lists are a fact of the scope generation, not of one intent: the
  workloads and endpoints every admitted candidate in the scope projects to,
  taken before the intent's entity-key filter. A keep-list is never built
  from an entity-filtered projection: an intent keyed to another repository
  would then delete what a sibling intent wrote, and the graph would depend on
  intent order (#7304 fault-injection failure). A caller that cannot supply
  the scope set skips the retract with `ModeSkippedNoScopeTruth`.
- It is idempotent. A retry deletes nothing new. The reducer queue's
  platform-graph conflict key serializes `workload_materialization` per scope,
  but correctness does not rely on it: with scope-wide keep-lists no intent's
  deletes overlap any intent's writes, so any order or interleaving ends at
  the same graph.

## Telemetry

`Observe` adds measured deletes to
`eshu_dp_workload_repository_edge_retractions_total{write_phase="defines_retract"|"repository_endpoint_retract"}`.
It does this only when the executor chain implements `CountingExecutor`; in
production that is `cmd/reducer`'s `reducerCypherExecutor`, forwarded by the
graph-write backpressure gate. This is ordinary workload or endpoint removal,
so it has its own counter and its own operator-dashboard panel instead of
sharing `eshu_dp_reconciliation_drift_retractions_total`, which means
collector disagreement.

Every run also logs `workload repository edge retract completed` with these
fields:

- `scope_id`
- `generation_id`
- `intent_id`, `entity_keys` (the intent that ran the retract; its keys bound
  what it writes, never what the retract keeps)
- `retract_mode` (`guarded`, `unguarded_no_reader`, `unguarded_read_failed`,
  `skipped_no_scope_truth`)
- `repository_count` (for a skipped retract, the repositories left unretracted)
- `kept_workload_count`, `kept_endpoint_count` (the scope generation's admitted
  workloads and endpoints, not what this intent wrote; the written counts are
  `workload_row_count` and `endpoint_row_count` on `workload materialization
  completed`)
- `stale_defines`, `stale_repository_endpoint_edges` (found by the guard read)
- `defines_deleted`
- `repository_endpoint_edges_deleted`
- `deletes_counted`
- `read_error` (the log is a warning when this is set, and also for
  `unguarded_no_reader` and `skipped_no_scope_truth`)
- `duration_s`

Evidence: `docs/internal/evidence/7285-repository-cleanup-keeps-reducer-edges.md`.
