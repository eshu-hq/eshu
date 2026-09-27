# #7285: repository cleanup keeps reducer edges

## Defect

`buildRepositoryCleanupStatements` ran `MATCH (r:Repository {id: $repo_id})
DETACH DELETE r` on every non-first, non-delta projector attempt. Two paths
re-run the projector after the generation's reducers already succeeded:

- `projector/service.go` sets `PreviousGenerationExists = true` when
  `attempt_count > 1`, so any retry, including a retry of a first generation,
  took the non-first branch.
- `recoverWedgedActiveGenerationsQuery` reopens a succeeded `source_local`
  projector row after the generation's reducer work drained.

The delete removed every edge on the Repository node. The reducers are never
re-armed: their re-enqueue is `ON CONFLICT (work_item_id) DO NOTHING`, and
completed shared-projection intents are not reopened. `DEFINES` and every other
reducer or cross-scope edge on the node stayed lost. On ops-qa that produced
the empty-shell workloads.

Root-Cause Evidence: `TestLiveRepositoryRetryKeepsReducerEdges` on
neo4j:2026-community at `origin/main` `049be71612`. After a same-generation
retry, the per-type reducer edge counts on the Repository went from 17 types /
20 edges to `map[]`. This held under both the atomic-group and phase-group
executors, and the Repository element id changed.

## Fix

- **Option B** (the arbiter ruling):
  - `repository_cleanup` keeps only the `lookup=path_conflict` retirement of a
    different-id Repository at the same path.
  - The by-id delete and `canonicalNodeRepositoryIDCleanupCypher` are gone.
  - The upsert is unchanged: `MERGE` + `SET` over the projector-owned
    properties.
- **B1:** `workload_materialization` now retracts its own stale `DEFINES` and
  repository-side `EXPOSES_ENDPOINT` edges, which only the node delete used to
  remove.
  - The retract anchors on the `repository_id` unique index and uses a
    per-repository keep-list, scoped by `evidence_source =
    'finalization/workloads'`.
  - It runs after `Materialize` commits, and on the zero-candidate path.
  - It covers only repositories whose repository fact is not
    `delta_generation`.
- **Test C decision:** pin, not full replace.
  - `TestRepositoryPropertyWritersStayInsideTheProjectorUpsert` parses every
    Cypher string literal under `go/internal` and `go/cmd`. It fails if any
    literal SETs a Repository property outside the upsert's own SET list, or
    replaces or merges the whole map.
  - The owned set is derived from `canonicalNodeRepositoryUpsertCypher`, so
    there is no allowlist. A seeded violation turns it red.
  - `SET r = {...}` was not adopted: its semantics are unverified on NornicDB,
    the default backend, and would wipe any property a later writer adds.
  - Remaining gap: a property left on an existing node by an older release
    persists. None is known, and ops-qa nodes were not inspected.

## Proof

The live tests are tagged `live_nornicdb_answer_truth`. They run on the local
Neo4j container `neo4j:2026-community@sha256:eabfbb04...` (2026.08.1) on
127.0.0.1:38687 with `NEO4J_AUTH=none`. The host was shared, with load average
54 to 86.

| Test | main `049be71612` | fix |
| --- | --- | --- |
| `TestLiveRepositoryRetryKeepsReducerEdges` (Test A, rows 1 and 4), atomic_group and phase_group | FAIL: edges `map[]` | PASS |
| `TestLiveRepositoryEdgeRetractKeepsOnlyCurrentCandidates` (Test B) | FAIL: gen-2 own DEFINES `[api billing worker]`, want `[api]` | PASS |
| `TestLiveRepositoryRetryInterleavedWithWorkloadMaterialization` (row 2, six placements) | FAIL in all six | PASS |
| `TestLiveRepositoryZombieAttemptKeepsReducerEdges` (row 3, 1 serial and 4 racing trials) | FAIL on trial 0 | PASS |
| `TestLiveRepositoryFirstCreationMergeRaceKeepsOneNode` (row 5, 10 trials x 50 sessions; managed transactions retry the transient `DeadlockDetected` the two-endpoint stub MERGE raises, as production retries do) | PASS (guard) | PASS |
| `TestLiveRepositoryEdgeRetractRacesOtherScopeSameNamedWorkload` (row 6, 10 trials) | FAIL: retracting repo kept `[api]` | PASS |
| `TestLiveRepositoryRetryRefreshesProjectorProperties` (Test C, live) | FAIL: reducer `DEPENDS_ON` on the stub-created Repository = 0 | PASS: property map equals a fresh projection |

Row 7, a B1 retract racing a same-scope `workload_materialization`, cannot
happen. Both run in the same domain on the same scope, and the platform-graph
conflict key serializes them. `TestPlatformGraphConflictKeySameDomainSameScopeSerializes`
in `storage/postgres` pins that. No claim, lease, ack, heartbeat or fence SQL
changed: the diff over `storage/postgres/projector_queue*.go` and
`reducer_queue*.go` is empty.

Mutation checks:

- Treating a `delta_generation` repository as full fails the hermetic delta
  and mixed-scope tests. It also fails the live Test B at "delta gen-3 own
  DEFINES = [], want [api]".
- The Go Bolt driver encodes a nil `[]string` as an empty list, so a nil
  keep-list still retracts on Neo4j. A missing key or a null value makes
  `NOT (x IN null)` null, and the statement deletes nothing (cypher-shell
  probe). The writer always binds explicit lists.

Performance Evidence: the prove-the-theory shim ran on neo4j:2026-community 2026.08.1 against the largest-repository shape (12,403 files, 1,500 directories, 24,806 entities, 22 reducer edges), with statements copied verbatim from `origin/main` `049be71612`, interleaved with alternating first mover and fresh generation ids; a same-generation re-projection took a median of 1.525 s today and 1.053 s under B, B was faster in 15 of 15 pairs, and db hits per attempt fell from 530,470 to 454,616 (-14.3%), with `repository_cleanup` dropping from 12,458 db hits to 2; the B1 retract PROFILE is `NodeUniqueIndexSeek(Locking) repo:Repository(id)` then a typed `Expand(All)`, costing 21 (DEFINES) and 20 (EXPOSES_ENDPOINT) db hits on a node with 12,433 relationships, without walking REPO_CONTAINS.

Added per `workload_materialization` run:

- One repository-kind fact read (`LoadFactsForKinds` with `repository`). This
  is the read the zero-candidate path already made. It is now made on every
  run, and it is kind-filtered by Postgres `ListFactsByKind`.
- Two retract statements per 500 repositories.

Neither was separately timed.

Observability Evidence: actual deleted edge counts, read from the Bolt write summary through the new `retract.CountingExecutor` seam (`go/internal/reducer/workload/retract`) (implemented by `cmd/reducer` `reducerCypherExecutor` and forwarded by the backpressure gate), are recorded on the existing `eshu_dp_reconciliation_drift_retractions_total` with bounded `domain=workload_materialization`, `write_phase` `defines_retract` or `repository_endpoint_retract`, and `kind=edge`; every run logs `workload repository edge retract completed` with scope_id, generation_id, repository_count, kept_workload_count, kept_endpoint_count, defines_deleted, repository_endpoint_edges_deleted, deletes_counted and duration_s; `TestWorkloadMaterializationRecordsRepositoryEdgeRetractCounts` and `TestProductionWorkloadMaterializerCountsRepositoryEdgeRetracts` pin the metric and the production wiring, gate enabled and disabled.

## Not checked

- **NornicDB.** By the owner's rule these runs were Neo4j only. The live files
  are `class: ci` rows in `specs/live-tests.v1.yaml`, so the live-backend CI
  job runs them on the pinned NornicDB. Two risks are open there:
  - The zero-row `DELETE` store-size cost in `nornicdb-pitfalls.md`
    (NornicDB#296) applies to B1, which usually deletes nothing. It was
    measured for label-anchored shapes, never for an id-anchored one.
  - The phase-group executor path, and `UNWIND ... DELETE` through the reducer
    auto-commit path, are also untested on NornicDB.
- **Deadlock count.** Transient deadlock-detected counts for a projector atomic
  group racing reducer MERGEs, before and after the fix, were not measured.
- **Repair of existing shells.** Edges already lost do not heal under B. The
  re-run of the writers on ops-qa (`recover-generations` or a domain replay)
  is an owner-consented mutation and is out of scope here.
