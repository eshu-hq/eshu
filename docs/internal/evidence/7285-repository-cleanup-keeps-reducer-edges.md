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
  - It is guarded (review F1): with the reducer's graph reader wired
    (`RepositoryEdgeReader`), it first reads the repository's current targets,
    computes the stale set in Go, and deletes only those
    `(repo_id, target_id)` pairs by id. Nothing stale means no `DELETE`. No
    reader, or a failed read, runs the keep-list `DELETE` (fail toward
    deleting) and logs a warning.
- **Test C decision:** pin, not full replace.
  - `TestRepositoryPropertyWritersStayInsideTheProjectorUpsert` is a literal
    scan of every Go string literal under `go/internal` and `go/cmd` (non-test
    files). It binds a Repository variable through `(r:Repository`,
    `WHERE r:Repository` and `WITH r AS alias` chains, and fails on
    `SET r.p =`, `SET r.p +=`, `SET r[$key] =`, `SET r =` or `SET r +=` outside
    the upsert's own SET list.
  - It is not proof that no other writer exists. It cannot see Cypher split
    across a Go string concatenation, a non-literal label
    (`fmt.Sprintf("(r:%s)", label)`), a procedure write
    (`apoc.create.setProperty`), a label-less match, or Cypher in test files.
  - The owned set is derived from `canonicalNodeRepositoryUpsertCypher`, so
    there is no allowlist. A seeded violation turns it red.
  - `SET r = {...}` was not adopted: its semantics are unverified on NornicDB,
    the default backend, and would wipe any property a later writer adds.
  - Remaining gap: a property left on an existing node by an older release
    persists. None is known, and ops-qa nodes were not inspected.

## Proof

The live tests are tagged `live_nornicdb_answer_truth`. The first round ran on
`neo4j:2026-community@sha256:eabfbb04...` (2026.08.1) on 127.0.0.1:38687 with
`NEO4J_AUTH=none`, on a shared host (load average 54 to 86). After the review
fixes, all seven re-ran green on `neo4j:2026-community@sha256:91fb0bf2...`
(2026.09.0) on 127.0.0.1:38721, rebased on `origin/main` `be7fa7432e`.

| Test | main `049be71612` | fix |
| --- | --- | --- |
| `TestLiveRepositoryRetryKeepsReducerEdges` (Test A, rows 1 and 4, ruling item 5), atomic_group and phase_group | FAIL: edges `map[]` | PASS; `get_workload_context` answers the defining `repo_id` |
| `TestLiveRepositoryEdgeRetractKeepsOnlyCurrentCandidates` (Test B), guarded and unguarded legs | FAIL: gen-2 own DEFINES `[api billing worker]`, want `[api]` | PASS; the guarded steady-state pass sends 0 retract `DELETE` statements |
| `TestLiveRepositoryRetryInterleavedWithWorkloadMaterialization` (row 2, six placements) | FAIL in all six | PASS |
| `TestLiveRepositoryZombieAttemptKeepsReducerEdges` (row 3, 1 serial and 4 racing trials) | FAIL on trial 0 | PASS |
| `TestLiveRepositoryFirstCreationMergeRaceKeepsOneNode` (row 5, 10 trials x 50 sessions; managed transactions retry the transient `DeadlockDetected` the two-endpoint stub MERGE raises, as production retries do) | PASS (guard) | PASS |
| `TestLiveRepositoryEdgeRetractRacesOtherScopeSameNamedWorkload` (row 6, 10 trials) | FAIL: retracting repo kept `[api]` | PASS |
| `TestLiveRepositoryRetryRefreshesProjectorProperties` (Test C, live) | FAIL: reducer `DEPENDS_ON` on the stub-created Repository = 0 | PASS: property map equals a fresh projection |

Ruling item 4 is `TestReducerQueueReprojectionReEnqueueAdmitsNothingLive`
(`storage/postgres`, `postgres:18-alpine` 18.6, disposable schema). After the
generation's reducer rows succeed, re-enqueueing the same intents reports
`Count == 0` and leaves both rows succeeded with `attempt_count` and
`updated_at` unchanged. An enqueue that reopens succeeded rows turns it red
(`Count = 2`).

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
- Guard (hermetic): a nil reader, a dropped keep-list in the handler, dropped
  catalog wiring, and a dropped Go-side keep check each turn a test red. Live:
  a guarded delete that matches nothing leaves gen-2 at
  `[api billing worker]`.
- API truth: deleting the `DEFINES` edge before the call makes
  `get_workload_context` answer `repo_id ""`, and Test A fails.
- The Go Bolt driver encodes a nil `[]string` as an empty list, so a nil
  keep-list still retracts on Neo4j. A missing key or a null value makes
  `NOT (x IN null)` null, and the statement deletes nothing (cypher-shell
  probe). The writer always binds explicit lists.

Performance Evidence: the prove-the-theory shim ran on neo4j:2026-community 2026.08.1 against the largest-repository shape (12,403 files, 1,500 directories, 24,806 entities, 22 reducer edges), with statements copied verbatim from `origin/main` `049be71612`, interleaved with alternating first mover and fresh generation ids; a same-generation re-projection took a median of 1.525 s today and 1.053 s under B, B was faster in 15 of 15 pairs, and db hits per attempt fell from 530,470 to 454,616 (-14.3%), with `repository_cleanup` dropping from 12,458 db hits to 2; the B1 retract PROFILE is `NodeUniqueIndexSeek(Locking) repo:Repository(id)` then a typed `Expand(All)`, costing 21 (DEFINES) and 20 (EXPOSES_ENDPOINT) db hits on a node with 12,433 relationships, without walking REPO_CONTAINS.

Added per `workload_materialization` run:

- One repository-kind fact read (`LoadFactsForKinds` with `repository`). This
  is the read the zero-candidate path already made. It is now made on every
  run, and it is kind-filtered by Postgres `ListFactsByKind`.
- Two guard reads per 500 repositories. On Neo4j 2026.09.0 the `DEFINES` read
  plans `NodeUniqueIndexSeek` on `repository_id` then `Expand(All)`, 17 db hits
  on a 12,403-file repository.
- A `DELETE` only when a stale edge exists. It seeks `workload_id` /
  `endpoint_id` and expands from that side (7 db hits). In steady state there is
  none, so the NornicDB#296 zero-row `DELETE` cost is not paid.

None was separately timed. The fallback keep-list `DELETE` (no reader, or a
failed read) still pays the zero-row cost on NornicDB.

Observability Evidence: actual deleted edge counts, read from the Bolt write summary through the `retract.CountingExecutor` seam (`go/internal/reducer/workload/retract`, implemented by `cmd/reducer` `reducerCypherExecutor` and forwarded by the backpressure gate), are recorded on the new `eshu_dp_workload_repository_edge_retractions_total` by bounded `write_phase` (`defines_retract`, `repository_endpoint_retract`), kept off `eshu_dp_reconciliation_drift_retractions_total` because ordinary removal is not collector drift, and shown on the operator dashboard's Workload Repository Edge Retractions panel; every run logs `workload repository edge retract completed` with scope_id, generation_id, retract_mode (`guarded`, `unguarded_no_reader`, `unguarded_read_failed`), repository_count, kept_workload_count, kept_endpoint_count, stale_defines, stale_repository_endpoint_edges, defines_deleted, repository_endpoint_edges_deleted, deletes_counted, read_error and duration_s, at warning level when the guard read failed; `TestWorkloadMaterializationRecordsRepositoryEdgeRetractCounts`, `TestObserveRecordsMeasuredDeletesOnly`, `TestObserveLogsGuardModeAndReadFailure` and `TestProductionWorkloadMaterializerCountsRepositoryEdgeRetracts` pin the metric, the log and the production wiring.

## Review follow-ups

| Finding | Resolution |
| --- | --- |
| F1 (P1) zero-row `DELETE` per run | Guarded read-then-delete, above; pitfalls page lists it as guarded. |
| F2 (P2) drift counter overloaded | Dedicated counter and dashboard panel; the drift series is unchanged. |
| F3 (P2) Test C scan evasions | `+=`, aliases, label predicates and dynamic keys caught; remaining limits listed above. |
| F4 (P2) ruling items 4 and 5 | Both implemented, above. |
| P3-1 orphan comment line | Reflowed in `generation_liveness_sql.go`; comment only. |
| P3-2 stale GitLab evidence note | Updated. |
| P3-3 strict `delta_generation` decode | Recorded, not changed: the only producer writes a JSON bool. A string `"true"` would read as full here and delta in the projector. |
| P3-4 `deletes_counted` with no Bolt counter | Recorded: NornicDB's `relationshipsDeleted` reporting is unverified, so the counter could under-report. `stale_*` log fields come from the guard read and do not depend on it. |
| P3-5 delta with empty paths | Recorded: the projector treats it as full, the retract skips it; stale edges wait for the next full or reconciliation generation. |

## Not checked

- **NornicDB.** By the owner's rule these runs were Neo4j only. The live files
  are `class: ci` rows in `specs/live-tests.v1.yaml`, so the live-backend CI
  job runs them on the pinned NornicDB. Two risks are open there:
  - The guard avoids the zero-row `DELETE` (NornicDB#296) in steady state,
    but its reads and the fallback keep-list `DELETE` were not timed on
    NornicDB.
  - The phase-group executor path, and `UNWIND ... DELETE` through the reducer
    auto-commit path, are also untested on NornicDB.
- **MCP transport and the liveness sweep.** Item 5 drives the HTTP handler
  the `get_workload_context` MCP tool dispatches to, not `tools/call` itself.
  The liveness re-drive (`recoverWedgedActiveGenerationsQuery`) was not run end
  to end; item 4 pins the enqueue contract it depends on.
- **Deadlock count.** Transient deadlock-detected counts for a projector atomic
  group racing reducer MERGEs, before and after the fix, were not measured.
- **Repair of existing shells.** Edges already lost do not heal under B. The
  re-run of the writers on ops-qa (`recover-generations` or a domain replay)
  is an owner-consented mutation and is out of scope here.
