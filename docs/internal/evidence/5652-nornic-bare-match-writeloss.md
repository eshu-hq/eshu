# Issue #5652: UNWIND bare-MATCH SET silent write-loss on NornicDB v1.1.11

Confirmed and fixed the four AWS posture node writers — that fix is the entire
scope of this PR. Two further File/Directory writer shapes were flagged during
the same investigation but re-investigated separately and found NOT to be
production bugs (a harness artifact and a production-unreachable backend
defect); their proof and tests ship in a separate follow-up PR, summarized
under "Follow-up" below. All evidence for the posture fix is live read-back
against the pinned production image, not counter output — batched-write
counters (`PropertiesSet`, `ContainsUpdates`) are unreliable on this backend
and were never trusted as the sole signal.

Backend: `timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`
(the exact image `deploy/helm/eshu/values.yaml` pins for production), isolated
Compose project (`p0nornic5652`, host ports `48474`/`48687`), no-auth Bolt,
database `nornic`. Torn down after the investigation.

## Part 1: the four posture writers (confirmed broken, fixed, and shipped)

`go/internal/storage/cypher/{ec2_internet_exposure,ec2_block_device_kms_posture,rds_posture,s3_internet_exposure}_node_writer.go`
each shipped an `UNWIND $rows AS row MATCH (resource:CloudResource {uid:
row.uid}) SET ...` statement. Live proof (a full-coverage Go harness driving
each writer's exact production Cypher, `go/internal/storage/cypher/
posture_node_writers_live_test.go` is the committed version of this proof):

| writer | bare-MATCH SET (old) | MERGE control | two-phase fix (shipped) |
| --- | --- | --- | --- |
| ec2_internet_exposure | silent no-op (read-back nil) | persists | persists + never-creates |
| ec2_block_device_kms_posture | silent no-op | persists | persists + never-creates |
| rds_posture | silent no-op | persists | persists + never-creates |
| s3_internet_exposure | silent no-op | persists | persists + never-creates |

### Never-create analysis per writer

All four writers' doc comments state the same contract: they only ever
update an already-materialized `CloudResource` node and must never fabricate
one. Tracing the reducer wiring
(`go/internal/reducer/{ec2_internet_exposure,ec2_block_device_kms_posture,rds_posture,s3_internet_exposure}_materialization.go`)
shows each handler gates on
`GraphProjectionKeyspaceCloudResourceUID`/`GraphProjectionPhaseCanonicalNodesCommitted`
before running (`canonicalNodesReady`) — but that gate is scope+generation
level ("has the CloudResource node phase committed for this scope at all"),
not per-uid ("does THIS uid's CloudResource node exist"). A posture-eligible
resource (present in `ec2_instance_posture`/`rds_instance_posture`/
`s3_bucket_posture` facts) is not guaranteed to have been admitted as a
`CloudResource` by `aws_resource_materialization` in the same run — the doc
comments' explicit "a missing uid is a no-op at the MATCH" anticipates this.
`MATCH -> MERGE` is therefore NOT a safe blind swap for any of the four
writers: MERGE unconditionally creates on a miss.

### Fix: two-phase read-confirm-then-MERGE

`go/internal/storage/cypher/posture_node_existence.go` adds
`PostureExistenceReader` and `filterRowsToExistingCloudResourceUIDs`: a
separate read (`UNWIND $candidate_uids AS candidate_uid MATCH
(resource:CloudResource {uid: candidate_uid}) RETURN resource.uid AS
existing_uid` — reads are not subject to the SET no-op, and the UNWIND
binding/RETURN alias use distinct names per the variable-shadowing pitfall)
confirms which candidate uids already exist. The query does not `RETURN
DISTINCT`: `CloudResource.uid` is uniqueness-constrained
(`cloud_resource_uid_unique` / `nornicdb_cloud_resource_uid_lookup`), so a
`DISTINCT` hash-aggregation would only ever guard against a duplicate value in
`$candidate_uids`, and `filterRowsToExistingCloudResourceUIDs` already folds
every returned row into a Go set before checking membership, so a duplicate
row is deduplicated there for free. Live proof:
`TestLivePostureExistenceReaderDedupesDuplicateCandidateUIDs` in
`posture_node_existence_live_bench_test.go` drives a duplicate `uid` through
two rows plus a missing uid over the real Bolt driver and asserts the filtered
result keeps both rows for the confirmed uid and drops the missing one. Rows for unconfirmed uids are
dropped in Go before the write; the write statement itself switches its
anchor from `MATCH` to `MERGE`, so it always matches a uid this function
already confirmed exists and never creates. All four writer constructors
gained a `PostureExistenceReader` parameter, wired in
`cmd/reducer/canonical_graph_writers.go`/`cmd/reducer/main.go` to the
reducer's existing `query.GraphQuery` graph-read port (`graphReader`).

Live proof of the shipped code (not a throwaway harness):
`go/internal/storage/cypher/posture_node_writers_live_test.go`,
`TestLivePostureNodeWritersPersistAndNeverCreate` (gate:
`ESHU_CYPHER_BOLT_DSN`). For each of the four writers it seeds one existing
`CloudResource`, leaves a second uid absent, drives the real production
writer with both rows, and asserts by read-back in a separate transaction
that (a) the confirmed-existing uid's property persisted and (b) the
`CloudResource` count was unchanged and no phantom node exists for the
missing uid. Result: **all 4 PASS** against the pinned v1.1.11 image.

```text
=== RUN   TestLivePostureNodeWritersPersistAndNeverCreate
--- PASS: TestLivePostureNodeWritersPersistAndNeverCreate (0.09s)
    --- PASS: .../ec2_internet_exposure (0.04s)
    --- PASS: .../ec2_block_device_kms_posture (0.01s)
    --- PASS: .../rds_posture (0.01s)
    --- PASS: .../s3_internet_exposure (0.01s)
```

### Rejected candidate: UNIQUE constraint

A single-property `UNIQUE` constraint on `CloudResource.uid` was tried first
(shim harness, prior to this evidence note) and does NOT fix the no-op — the
constraint applies cleanly but the identical bare-MATCH SET still silently
drops. Not shipped.

### Static class-gate

`go/internal/storage/cypher/unwind_bare_match_set_gate_test.go`,
`TestNoUnwindBareMatchThenSetCyphersInPackage`, parses every non-test `.go`
file in the package with `go/parser`, extracts every string constant, and
fails if any contain `UNWIND ... MATCH (...) SET` with no `MERGE` anywhere in
the same statement. Verified red against the historical (pre-fix) constant
text and green against the shipped fix. The gate deliberately does not flag
the File multi-clause update statement (it contains a `MERGE` elsewhere in the
same statement); that statement's separate investigation is summarized under
"Follow-up" below.

## Performance: added existence-read cost

The two-phase fix adds a real Bolt round trip
(`filterRowsToExistingCloudResourceUIDs` -> `postureCloudResourceExistingUIDsCypher`)
to all four posture writers, and it runs once per `graphowner.LockOnlyGate`
chunk (`cmd/reducer/canonical_graph_writers.go` wires the writers behind
`LockOnlyGate`; `LockOnlyGate.writeChunk` in
`go/internal/graphowner/lock_only_gate.go` calls the writer's `Write*Nodes`
method — which is exactly where the existence read runs — between
`store.LockUIDs` and `tx.Commit`, i.e. while the chunk's per-uid Postgres
advisory locks are held). This section measures that added cost, since the
shipped benchmarks before this PR used the `echoingPostureExistenceReader`
fake and the shipped live test (`TestLivePostureNodeWritersPersistAndNeverCreate`)
proves only correctness.

Honest framing: this is not an output-preserving speedup to compare against a
prior number. The OLD bare-MATCH `SET` was fast but silently wrong (see Part
1 above); the two-phase fix trades a bounded amount of added latency and
lock-hold time for a write that actually persists. What follows proves that
added cost is small, bounded, and does not introduce contention or deadlock
risk of its own — not that the change is "faster."

### Benchmark: the real read, not a mock

`go/internal/storage/cypher/posture_node_existence_live_bench_test.go` drives
`filterRowsToExistingCloudResourceUIDs` against a `boltPostureExistenceReader`
wrapping the real Bolt driver connected to the pinned production image
(`timothyswt/nornicdb-cpu-bge:v1.1.11`, container `p0-5652-posture-nornic`,
`bolt://127.0.0.1:49687`), seeding one real `CloudResource` node per candidate
uid before the timed loop so every row is a confirmed-existing hit (the
common case; a miss is a no-op dictionary lookup, not an extra query). Three
runs, `-benchtime=30x` each:

```
go test ./internal/storage/cypher -run '^$' -bench 'BenchmarkPostureExistenceReaderLive' -benchtime=30x -v

run 1: N10=253172 ns/op   N500=877086 ns/op   N2000=3532940 ns/op   N500LargeStore=810300 ns/op
run 2: N10=240718 ns/op   N500=1011108 ns/op  N2000=2834899 ns/op  N500LargeStore=922132 ns/op
run 3: N10=282617 ns/op   N500=797794 ns/op   N2000=2617878 ns/op  N500LargeStore=1131722 ns/op

mean (ns/op): N10=258836   N500=895329   N2000=2995239   N500LargeStore=954718
mean (us/op): N10≈0.26     N500≈0.90     N2000≈3.00      N500LargeStore≈0.95
```

`N500ShippedBatchSize` is the number that matters: `cypher.DefaultBatchSize`
(500) is the SAME value `graphowner.lockChunkSize` uses to bound advisory-lock
chunk size, so 500 candidate rows is the worst-case number of rows this read
ever runs over inside a single Postgres advisory-lock hold. Mean **≈0.9ms**
per chunk, allocating ~410KB/8079 allocs (dominated by the Bolt driver's
per-row record decoding, not by this package's own code).

### Index-seek proof (PROFILE unavailable on this backend)

The pinned NornicDB v1.1.11 Bolt transport does not return `PROFILE`/`EXPLAIN`
plan metadata — confirmed again for this PR (both over Bolt and over the HTTP
`tx/commit` endpoint: `PROFILE ...`/`EXPLAIN ...` against this container both
return a normal result envelope with no plan/stats field at all), matching the
same limitation already documented for the #5410 SQL-relationships live
benchmark. In its place, `BenchmarkPostureExistenceReaderLive_N500LargeStore`
is the empirical substitute: it re-runs the shipped 500-candidate case with
5,000 unrelated distractor `CloudResource` nodes seeded first (10x the total
`CloudResource` population of the plain `N500ShippedBatchSize` case). An
index-seek anchor's cost is a function of candidate count, not total graph
size; a full `:CloudResource` label scan's cost is NOT.

Measured: `N500LargeStore` (955us mean, 10x the store) vs
`N500ShippedBatchSize` (895us mean, baseline store) — a 6.6% difference, well
within this benchmark's own run-to-run variance (797us-1011us, a ~27% spread
across three runs of the SAME case). Allocations are effectively identical
(417,686-417,689 B / 8079-8080 allocs across all three `N500LargeStore` runs
vs 409,809-410,174 B / 8079-8080 allocs for `N500ShippedBatchSize`). A full
label scan would show a cost that scales with total `CloudResource` count, not
a flat ~1x ratio at 10x the population. This is consistent with
`postureCloudResourceExistingUIDsCypher`'s anchor
(`MATCH (resource:CloudResource {uid: candidate_uid})`) hitting the
`cloud_resource_uid_unique` / `nornicdb_cloud_resource_uid_lookup`
index/constraint pair (`internal/graph.SchemaStatementsForBackend`) instead of
a `MergeScanFallback`-style full scan.

### Lock-hold and contention assessment

- **Bounded, single round trip.** `filterRowsToExistingCloudResourceUIDs` is
  called exactly once per `Write*Nodes` invocation, over the FULL chunk (up to
  `lockChunkSize` = `DefaultBatchSize` = 500 rows) in one `reader.Run` call —
  not once per uid. The measured ~0.9ms is the entire added lock-hold
  extension per chunk, not a per-row cost that could balloon under a large
  batch (`N2000Stress`, 4x the shipped batch size, scales to ~3.0ms — linear in
  candidate count, not the runaway growth a full scan would show).
- **Does not serialize concurrent writers on different uids.**
  `graphowner.LockOnlyGate` takes Postgres advisory locks keyed per-uid
  (`postgres.GraphNodeOwnerStore.LockUIDs`); two chunks touching disjoint uid
  sets take disjoint locks and never wait on each other regardless of how long
  either chunk's existence read takes. Only a chunk racing the SAME uid (e.g. a
  concurrent Gate-gated base-property write) waits, and only for this chunk's
  own now-slightly-longer critical section — bounded by the ~0.9ms measured
  above, not unbounded.
- **No deadlock.** The existence read is a plain read dispatched through the
  reducer's existing `query.GraphQuery` port (the same `graphReader` used for
  every other graph read in the reducer; wired identically whether or not
  `LockOnlyGate` is involved) — it does not itself acquire any Postgres
  advisory lock or NornicDB write lock, and it runs strictly AFTER
  `store.LockUIDs` has already succeeded and BEFORE `tx.Commit`. There is no
  second lock acquisition inside the critical section for this read to
  deadlock against; the critical section's lock-acquisition order is
  unchanged from before this PR (acquire uid locks, do graph work, commit) —
  only the graph work inside it got one read longer.

Benchmark Evidence: `go test ./internal/storage/cypher -run '^$' -bench
'BenchmarkPostureExistenceReaderLive' -benchtime=30x -v` against
`bolt://127.0.0.1:49687` (pinned `nornicdb-cpu-bge:v1.1.11`), 3 runs — see
tables above. `N500ShippedBatchSize` (the shipped-batch-size, worst-case
lock-hold shape) means ≈0.9ms/chunk; `N500LargeStore` vs
`N500ShippedBatchSize` (955us vs 895us, 6.6% apart at 10x the store) is the
index-seek-vs-full-scan proof in place of an unavailable `PROFILE` plan.

No-Regression Evidence: `TestLivePostureNodeWritersPersistAndNeverCreate`
(all 4 writers) and `TestLivePostureExistenceReaderDedupesDuplicateCandidateUIDs`
pass against the same pinned image, proving the added read does not change
writer correctness; `graphowner.LockOnlyGate`'s own lock-acquisition-order and
per-uid scoping are unchanged by this PR (this PR only lengthens what runs
inside an already-existing critical section, it does not touch
`LockOnlyGate`/`lock_only_gate.go` itself).

No-Observability-Change: the existence read flows through the reducer's
existing `query.GraphQuery` port (`internal/query.Neo4jReader.Run`), which
already emits an OTEL span (`tracer: "eshu/go/internal/query"`) and records
errors for every graph read the reducer issues, posture existence reads
included; no new metric, span, or log field was added because none is needed
for a call already covered by that pre-existing instrumentation.

## Follow-up: File/Directory edge shapes (investigated separately, no fix ships here)

While investigating the posture writers, two further `UNWIND` shapes in the
canonical File/Directory writer were flagged as suspect on the contaminated
posture stack: a `WITH`-chained multi-clause File update
(`canonicalNodeFileUpdateExistingCypher`) appearing to drop its post-`WITH`
`REPO_CONTAINS`/`CONTAINS` edge MERGEs, and the `UNWIND`-batched
`MATCH ... DELETE` refresh/retract statements
(`canonicalNodeRefreshCurrent*Cypher`) appearing to no-op.

Both were re-investigated on a fresh, uncontaminated v1.1.11 container in a
**separate PR** (the #5652 follow-up,
`docs/internal/evidence/5652-followup-file-directory-edge-writeloss-investigation.md`,
landed on its own branch — **not part of this posture PR's diff**), and neither
is a production bug:

- **File multi-clause update — not reproducible in production.** It did not
  reproduce across the production managed-transaction `ExecuteGroup` dispatch
  mode (the mode `buildFileStatementsForRows` actually runs under), auto-commit,
  or the real `CanonicalNodeWriter.Write` path, and the pre-existing
  `TestDeltaTombstoneGraphTruth` passes. The follow-up did NOT root-cause why
  the original probe reported dropped edges; the most likely mechanism is stack
  contamination (that probe ran on the same instance as an earlier abandoned
  `UNIQUE`-constraint experiment, a documented NornicDB drop/recreate corruption
  pitfall), but that was not confirmed — the discrepancy is tracked as #5671.
  No `buildFileStatementsForRows` rewrite ships.
- **Refresh/retract `DELETE` — real backend defect, production-unreachable.**
  The no-op reproduces only under the atomic `ExecuteGroup` transaction path.
  The production retract phase builds these statements with
  `OperationCanonicalRetract` and dispatches them through
  `PhaseGroupExecutor.executeSequentialRetractPhase`, which runs each as
  sequential per-statement auto-commit `Execute` and never `ExecuteGroup`
  (`PhaseGroupExecutor` intentionally does not implement `cypher.GroupExecutor`).
  This is pre-existing routing (#4367 / #5116 / #5128); the #4367 evidence file
  already classified `canonicalNodeRefreshCurrentFileImportEdgesCypher` as
  "Already safe. Live-claimed, no production fix." The upstream backend shapes
  are already tracked as #4902 and #5323.

The committed regression tests
(`TestFileUpdateExistingEdgesGraphTruth_ExistingFile` / `_BrandNewFile`,
`TestRefresh*EdgesGraphTruth`) and the static dispatch guard
(`TestPhaseGroupExecutorRetractPhaseNeverUsesExecuteGroup`) that pin these
conclusions ship in that follow-up PR, not here. This posture PR is scoped to
the Part 1 fix only.
