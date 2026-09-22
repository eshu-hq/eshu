# #6887 cloud node-retract liveness and delete-shape evidence

## Theory shims (prove-the-theory-first)

Retract-time global live-check, single-uid shape against the parity battery
Postgres (`par6843-battery-postgres-1`, 2043 `reducer_cloud_resource_identity`
rows), `EXPLAIN (ANALYZE, BUFFERS)`:

- Without an expression index the planner uses
  `fact_records_scope_generation_idx` on `fact_kind` and filters the whole
  kind slice on `payload->>'cloud_resource_uid'`: cost 617.39, 6 buffers at
  2k rows. Cost scales with kind cardinality — at 150k rows/kind every
  candidate pays a 150k-row filter on the materialization write path.
- With the partial expression index (`migration 118`,
  `fact_records_cloud_retract_admission_uid_idx` on
  `(payload->>'cloud_resource_uid') WHERE fact_kind =
  'reducer_cloud_resource_identity' AND is_tombstone = FALSE`) the same probe
  is a point Index Scan: cost 12.76, `Index Searches: 1`, 4 buffers,
  independent of kind cardinality. The shim index was dropped after
  measurement; the battery is unmodified.

Graph delete shape against the parity battery NornicDB (`bolt://7687`,
database `nornic`, seeded `CloudResource` nodes), inside a rolled-back
transaction: `CREATE (:CloudResource {uid: 'probe-6887-scratch'})`, then the
shipped `UNWIND $rows AS row MATCH (n:CloudResource {uid: row.uid}) DETACH
DELETE n`, then count → 0, no errors, rolled back. `SHOW INDEXES` on the
battery is empty, so the point-lookup claim rests on the managed-deployment
`cloud_resource_uid_unique` constraint plus the `nornicdb_cloud_resource_uid_lookup`
index — the same lookup the write-path `MERGE (r:CloudResource {uid:
row.uid})` already rides, so the delete costs one MERGE-equivalent lookup per
uid, never a bare-label scan (#6822: bare-label `DETACH DELETE` costs
7.6–9.3 s at 1M nodes even with zero matches).

## Performance Evidence: battery EXPLAIN, live shape, and live end-to-end (NornicDB + Postgres)

- Live end-to-end (`TestLiveCloudRetractEndToEnd`, battery PG + battery
  NornicDB): 2 candidates → locks + global live-check + 1 ledger release + 1
  uid-anchored graph delete + commit in 79ms wall (`graph node owner retract
  chunk completed family=cloud_resource candidate_uids=2 retracted_uids=1
  duration=79.098291ms`). Replay repeats the same decision in 67ms.
- Unit-bounded: every chunk holds at most `lockChunkSize` (500) advisory
  locks per transaction (`TestGateRetractDeadUIDsChunksAtLockChunkSize`:
  501 candidates open exactly 2 transactions), so no transaction can exhaust
  the advisory-lock table (#5007 P2-1).
- Cypher batching: deletes batch at the writer batch size through sequential
  `Execute` only, never `ExecuteGroup` (#4367 under-apply precedent), and are
  NOT marked for the bounded-drain rewrite — `UNWIND` batching already bounds
  each execution to point lookups, and the drain rewrite's `ORDER BY
  elementId` would add a sort over them.
- No-regression on existing paths: the retract is additive and nil-safe —
  handlers without `NodeRetracter`/`PriorGeneration` wired execute zero new
  statements (proven by every pre-existing materialization test passing
  unchanged); the read path is untouched. The EC2 tuple live-check has no new
  index: it runs only when EC2 delete candidates exist and filters the
  posture-kind slice (same access shape as the pre-index admission probe);
  a dedicated tuple index ships only if the gate shows pain.

## Observability Evidence: per-chunk retract log plus handler completion counts

- New per-chunk `graph node owner retract chunk completed` INFO log
  (`family`, `candidate_uids`, `retracted_uids`, `duration`,
  `component=graphowner`): shows the conflict domain, diff pressure, delete
  yield, and critical-section hold time. Emitted only when candidates exist;
  the empty-diff case opens no transaction and logs nothing new.
- Handler completion logs (`aws/azure/gcp resource materialization
  completed`, `ec2 instance node materialization completed`) gain
  `retracted_node_count` and `retract_duration_seconds`; `EvidenceSummary`
  gains the `N dead node(s) retracted` clause. No new metric instruments:
  the counts ride the existing completion-log and contention-counter
  (`eshu_dp_cross_scope_ownership_contended_rows_total`) signals.

## Correctness proof matrix

- `TestCloudResourceLivenessLive` (live PG): cross-scope shared uid stays
  live; superseded-only, tombstoned, pending-generation, and never-existed
  uids read dead; EC2 arn-fallback and blank-type normalization read live;
  identity-free candidates read dead; ledger release deletes the row and is
  idempotent.
- `TestGateRetractDeadUIDs*` (unit): live candidates never released or
  deleted; all-alive commits without writes; empty input opens no
  transaction; deletes run in sorted uid order without mutating the input;
  liveness errors and delete errors both roll back (release never commits
  without its delete); nil-ledger skips the retract (fail-closed, never an
  over-delete); chunk bound holds.
- `TestLiveCloudRetractEndToEnd` (live PG + live graph): multi-scope
  adversarial layout — node live in scope B survives, history-only node is
  deleted and ledger-released, replay reconverges deterministically.
- Handler tests (AWS/Azure-shaped + EC2): predecessor-only uids become
  candidates; first generation, unwired seams, and empty diffs stay silent;
  retract failure fails the intent for durable retry.
- Writer tests: uid-anchored `MATCH` + `DETACH DELETE`, no `MERGE`, no
  `evidence_source` predicate, UNWIND batching, sequential dispatch,
  fail-closed nil executor.
