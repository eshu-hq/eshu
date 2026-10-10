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
reducer or cross-scope edge on the node stayed lost. On the QA environment that produced
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
  - The keep-list is scope-wide: every workload and endpoint the scope
    generation's admitted candidates project to, taken before the intent's
    entity-key filter. The writes stay filtered. A keep-list is never built
    from an entity-filtered projection (see "#7304 fault-injection fix" below).
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
    (`fmt.Sprintf("(r:%s)", label)`), a Repository variable rebound through
    `UNWIND`, `collect` or a list comprehension, a procedure write
    (`apoc.create.setProperty`), a label-less match, or Cypher in test files.
  - The owned set is derived from `canonicalNodeRepositoryUpsertCypher`, so
    there is no allowlist. A seeded violation turns it red.
  - `SET r = {...}` was not adopted: its semantics are unverified on NornicDB,
    the default backend, and would wipe any property a later writer adds.
  - Remaining gap: a property left on an existing node by an older release
    persists. None is known, and QA nodes were not inspected.

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

Row 7, a B1 retract against a same-scope `workload_materialization`: two
same-scope intents cannot overlap, because the platform-graph conflict key
serializes them (`TestPlatformGraphConflictKeySameDomainSameScopeSerializes` in
`storage/postgres`). That closes the race, not the hazard. The first version of
B1 was order-dependent: an intent keyed to another repository retracted the
edges a sibling intent had written, one after the other, and the #7304
fault-injection gate caught it. Serialization cannot fix sequential order. The
scope-wide keep-list below does, and
`TestLiveRepositoryEdgeRetractKeepsScopeWorkloadsInEveryIntentOrder` pins both
orders plus a racing leg that does not rely on the queue fence. No claim,
lease, ack, heartbeat or fence SQL
changed: the diff over the non-test files matching
`storage/postgres/projector_queue*.go` and `reducer_queue*.go` is empty. The
only matching file this PR adds is the test-only
`reducer_queue_reenqueue_live_test.go`.

Mutation checks:

- Treating a `delta_generation` repository as full fails the hermetic delta
  and mixed-scope tests. It also fails the live Test B at "delta gen-3 own
  DEFINES = [], want [api]".
- Guard (hermetic): a nil reader, a dropped keep-list in the handler, dropped
  catalog wiring, the `RepositoryEdgeReader` line in `cmd/reducer/main.go`
  (`TestBuildReducerServiceWiresRepositoryEdgeReader`, RED with the line
  deleted: no guard read reaches the graph reader), and a dropped Go-side keep
  check each turn a test red. Live:
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

Observability Evidence: actual deleted edge counts, read from the Bolt write summary through the `retract.CountingExecutor` seam (`go/internal/reducer/workload/retract`, implemented by `cmd/reducer` `reducerCypherExecutor` and forwarded by the backpressure gate), are recorded on the new `eshu_dp_workload_repository_edge_retractions_total` by bounded `write_phase` (`defines_retract`, `repository_endpoint_retract`), kept off `eshu_dp_reconciliation_drift_retractions_total` because ordinary removal is not collector drift, and shown on the operator dashboard's Workload Repository Edge Retractions panel; every run logs `workload repository edge retract completed` with scope_id, generation_id, intent_id, entity_keys, retract_mode (`guarded`, `unguarded_no_reader`, `unguarded_read_failed`, `skipped_no_scope_truth`), repository_count, kept_workload_count, kept_endpoint_count, stale_defines, stale_repository_endpoint_edges, defines_deleted, repository_endpoint_edges_deleted, deletes_counted, read_error and duration_s, at warning level when the guard read failed or no reader was wired (`unguarded_no_reader`, which in production means the composition root dropped the guard, pinned by `TestBuildReducerServiceWiresRepositoryEdgeReader`); `TestWorkloadMaterializationRecordsRepositoryEdgeRetractCounts`, `TestObserveRecordsMeasuredDeletesOnly`, `TestObserveLogsGuardModeAndReadFailure`, `TestObserveLogsIntentAndSkippedMode` and `TestProductionWorkloadMaterializerCountsRepositoryEdgeRetracts` pin the metric, the log and the production wiring.

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

## #7304 CI fix: NornicDB commit-time UNIQUE conflict is a test-contract gap

The live-backend CI job's NornicDB leg failed
`TestLiveRepositoryFirstCreationMergeRaceKeepsOneNode` (matrix row 5, 50
concurrent first-creation MERGEs of one Repository id):
`Neo.ClientError.Transaction.TransactionCommitFailed` with
`commit failed: constraint violation: Constraint violation (UNIQUE on
Repository.[id]): Node with id=... already exists`. This is not a product
regression: on NornicDB a concurrent first-creation MERGE can lose at commit
instead of converging in place the way Neo4j does, and production never
surfaces it as a failure, because every canonical writer and reducer executor
is wired through a persistent `*cypher.RetryingExecutor`
(`cmd/reducer/executor_adapters.go`, `cmd/projector/executor_wiring.go`) whose
`classifyRetryableGraphWriteGroupError` recognizes a MERGE-shaped commit-time
UNIQUE conflict (`go/internal/storage/cypher/retryable_error.go`,
`isNornicDBCommitTimeUniqueConflict`) and replays the statement in-process;
if that local budget is exhausted the wrapped error still reports
`Retryable() == true`, and the projector/reducer requeue the whole intent,
whose MERGE replay matches the now-committed winner. The test called
`writer.Write` / `live.exec.ExecuteGroup` directly with no retry and
`t.Fatal`'d on any error, so it asserted a stronger, unproduced contract
(zero commit-time conflicts) instead of the actual production one (every
commit-time conflict on this write shape is retryable, and retrying
converges). The fix routes both write paths in that test through a
`*cypher.RetryingExecutor` (the exact production wrapper) and adds one more
bounded (5-attempt) durable-replay loop, gated on
`failure.IsRetryable` (`internal/projector/failure`, the live retry-decision
authority also named in `dead_letter_triage.go`), to mirror the durable-queue
requeue when the in-process budget is exhausted; a session whose error fails
that classification still fails the test immediately, so the invariant (one
node, `evidence_source = projector/canonical`) is unrelaxed and the test still
fails on a genuine regression. `TestRepositoryFirstCreationMergeRaceErrorIsClassifiedRetryable`
is a hermetic unit proof, using the exact error string from the #7304 CI log,
that this chain classifies it retryable. Neo4j needed zero durable replays in
10 local trials of 50 sessions each (`go test ./internal/reducer -tags
live_nornicdb_answer_truth -run TestLiveRepositoryFirstCreationMergeRaceKeepsOneNode
-v` against neo4j:2026-community on `bolt://127.0.0.1:38687`, PASS in 11.81s);
the NornicDB leg was not re-run here (owner rule: never start NornicDB
locally), so whether it needs the in-process retry, the durable-replay tier,
or neither remains to be observed from the next CI run's per-trial
`t.Logf("... sessions needed a durable replay ...")` output.

## #7304 fault-injection fix: the retract keep-list is scope-wide

### Defect

The Ifá fault-injection shard 1 digests disagreed: baseline and killworker
`repo_dependency` cells gave `64682b26…`, failgraphwrite and every main cell
gave `bad9985d0d382b11cc7ebe620de4e28f712a712cd6a20318e0495dc1f3195c49`. The
baseline graph was the wrong one: it lacked the fixture's
`Repository -DEFINES-> Workload` edge.

Root-Cause Evidence: the #7304 CI reducer log shows a
`workload_materialization` intent keyed `repo:<target repository>` in the
source scope logging `kept_workload_count=0 stale_defines=1 defines_deleted=1`.

- `CorrelatedWorkloadProjectionInputLoader` narrows candidates to the intent's
  entity keys. The first B1 built its keep-list from that narrowed projection,
  but retracted for every full-generation repository in the scope.
- Foreign keys in a scope are designed behaviour: the repo_dependency replay
  keys `PROVISIONS_DEPENDENCY_FOR` to the target repository, and the
  deployment_mapping replay falls back to `repo:<scope id>`.
- So a foreign-keyed intent kept nothing and deleted what a sibling intent
  wrote, and the final graph depended on which intent ran last. The fault cell
  reordered the intents (a resolution-not-ready deferral pushed the matching
  intent last), which is why only that cell was right.

### Fix (arbiter option B)

- The loader admits the whole scope candidate set once, then narrows the
  admitted set to the entity keys for the writes.
  `LoadWorkloadProjectionScopeInputs` returns both. Filtering and admission
  commute: the filter reads only repository identity, which admission does not
  change, so the writes are unchanged.
- The keep-list comes from the admitted scope set, through the same
  `BuildProjectionRowsWithInfrastructurePlatforms` builder the writes use. Its
  workload and endpoint ids depend only on the candidate, so the platform read
  is not repeated.
- The repository set stays `FullGenerationRepositoryIDs`. A disappeared
  workload is absent from the scope set, so any intent retracts it, including
  one keyed to a foreign repository.
- A loader that cannot supply the scope set (no
  `ScopeWorkloadProjectionInputLoader`) skips the retract and logs at warning
  with `retract_mode=skipped_no_scope_truth`. It never falls back to its
  filtered candidates. Production wires the correlated loader.
- The retract log now carries `intent_id` and `entity_keys`, and
  `workload materialization completed` carries both too. `kept_workload_count`
  and `kept_endpoint_count` now count the scope generation's admitted set, not
  what the intent wrote. The written count is that log's `workload_row_count`.
- An admission error on a candidate the intent's keys would have dropped now
  fails the intent. That is fail-closed, and it is a behaviour change.

### Proof

RED runs are on `c9f8af0c72`, GREEN on the fix, Neo4j only
(`neo4j:2026-community` 2026.09.0, `sha256:91fb0bf2…`).

| Test | `c9f8af0c72` | fix |
| --- | --- | --- |
| `TestWorkloadMaterializationKeepListIsScopeWideForEveryEntityKey` (real loader; keys: matching `workload:`, matching `repo:`, foreign `repo:`, `repo:<scope id>`, none; guarded and unguarded) | FAIL: foreign and scope-id rows delete the current edge; keep-lists `[]` | PASS |
| `TestBuildReducerServiceKeepsScopeWorkloadsForForeignKeyedIntent` (`cmd/reducer`, production composition root) | FAIL: 1 `DEFINES` DELETE | PASS |
| `TestLiveRepositoryEdgeRetractKeepsScopeWorkloadsInEveryIntentOrder` (both orders, 10 racing trials, disappearance) | FAIL: "matching then foreign" ends `DEFINES=[] EXPOSES_ENDPOINT=[]`; a race trial ends `DEFINES=[]` | PASS (1.18 s) |
| `TestWorkloadMaterializationForeignKeyedIntentRetractsTrueDisappearance` | PASS (the old code over-retracts) | PASS |
| `TestWorkloadMaterializationDeferralAndFailedWriteIssueNoRetract` | PASS | PASS |
| `TestWorkloadMaterializationSkipsRetractWithoutScopeTruth` | n/a (new capability) | PASS |

Seeded mutations, each RED:

- keep-list from the filtered candidates: the key-table test and the
  composition-root test;
- repository set restricted by the entity keys (option A): the disappearance
  test;
- `delta_generation` treated as full: the existing delta, mixed-scope and
  `FullGenerationRepositoryIDs` tests;
- a loader without scope truth falling back to its candidates: the skip test.

Performance Evidence: `load_inputs` on a 50-repository scope with an intent keyed to one repository, 400 calls per run, six interleaved base/fix pairs with alternating first mover: median 0.000794 s on `c9f8af0c72` and 0.000995 s on the fix (+0.20 ms, admission of the 49 candidates the filter used to drop); the whole handler with an in-memory executor went from 0.000824 s to 0.001092 s (+0.27 ms, adding the keep-list projection). A git scope holds one repository, where the two paths do the same work.

## Not checked

- **Ifá fault-injection shard 1, locally.** Not run:
  `scripts/verify-ifa-fault-injection.sh` hardcodes
  `ESHU_GRAPH_BACKEND=nornicdb`, and the owner's rule is never to start
  NornicDB locally. The local mechanism proof stands in: the live Neo4j
  both-orders-plus-race test, the real-loader key table and the seeded
  mutations above. CI shard 1 is the digest check: all three
  `repo_dependency` cells (baseline, killworker, failgraphwrite) must equal
  `bad9985d0d382b11cc7ebe620de4e28f712a712cd6a20318e0495dc1f3195c49`.
- **deployable_unit_correlation retract.** `retractDeployableUnitEdges` builds
  its rows from the intent's entity keys, the option-A shape. Whether it has
  the never-fires gap is not checked here and is out of this PR's scope.
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
  re-run of the writers on the QA environment (`recover-generations` or a domain replay)
  is an owner-consented mutation and is out of scope here.
