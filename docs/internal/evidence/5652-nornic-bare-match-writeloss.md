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
(resource:CloudResource {uid: candidate_uid}) RETURN DISTINCT resource.uid AS
existing_uid` — reads are not subject to the SET no-op, and the UNWIND
binding/RETURN alias use distinct names per the variable-shadowing pitfall)
confirms which candidate uids already exist. Rows for unconfirmed uids are
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
