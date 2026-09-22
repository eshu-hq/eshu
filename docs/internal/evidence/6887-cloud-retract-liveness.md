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

### Admission-drain fence (review of PR #6892, finding F1)

`reducer_cloud_resource_identity` — the only oracle the cloud-family
live-check reads — is reducer output written by the separate
`cloud_inventory_admission` work item. Nothing orders that item against
another scope's `*_resource_materialization`: `reducerClaimReadinessRequirementsSQL`
gates neither domain, and the projector flips `active_generation_id` in
`Ack` without waiting for the reducer. So a scope whose active generation's
admission has not drained has no admission rows yet, and every uid it still
holds would read dead to another scope's retract. The live tests never saw
it because each seeds the surviving scope's admission rows directly.

Fix (`postgres.LiveAdmissionCloudUIDs`): before the admission probe, refuse
with `ErrCloudAdmissionUndrained` while any active-generation
`cloud_inventory_admission` work item is nonterminal (`pending`, `claimed`,
`running`, `retrying`, `dead_letter`). The chunk rolls back, the handler
fails, and the durable queue retries the whole intent once the admission
has landed. A skip instead of an error would leak forever: a uid absent in
both G and G+1 is never a candidate again. A fallback to the scope's last
drained generation was rejected too: it misses a uid that is new in the
undrained generation. Dead-lettered admissions block the same way and name
their scope, since their facts never land until an operator redrives them.

Theory shim (private Postgres 18, rolled-back transaction, `ANALYZE` after
seeding 2,000 scopes x 10 generations = 20,000 `cloud_inventory_admission`
work items, 19,997 `succeeded`, 2 `pending` and 1 `dead_letter` on active
generations): the predicate is an `Index Scan using
fact_work_items_stage_domain_status_idx` with `status = ANY(...)` as an
index condition, 8 shared buffers on the work-item side and 9 on the
`ingestion_scopes_active_generation_idx` nested-loop side, 17 buffers and
0.075 ms execution in total; the terminal items are never scanned. Script:
`scratchpad/6887-shim.sql` (session-local, not committed).

Proof: `TestCloudResourceLivenessRefusesUndrainedAdmissionLive` — every
nonterminal status on the active generation returns the sentinel naming the
scope; `succeeded` and `superseded` answer normally and the uid admitted
only by the superseded generation reads dead; a nonterminal item on a
non-active generation does not block. RED before the fence (undefined
sentinel, then dead read), GREEN after.

Operator signal: the sentinel text (scope/generation=status) lands in the
failed work item's `failure_message` and the reducer's handler-failure log;
no new metric. A scope stuck in `dead_letter` is already the operator's
queue alarm and now also names itself in every blocked retract.

## Performance Evidence: battery EXPLAIN, live shape, and live end-to-end (NornicDB + Postgres)

- Live end-to-end (`TestLiveCloudRetractEndToEnd`, private Postgres 18 +
  pinned NornicDB, compose project `wt6887` on ports 15532/7788): 2
  candidates → locks + admission-drain fence + global live-check + 1 ledger
  release + 1 uid-anchored graph delete + commit. The figure is the chunk
  critical-section duration from the retract log (`graph node owner retract
  chunk completed ... candidate_uids=2 retracted_uids=1 duration=...`), not
  the whole-test wall: 6.9 ms and 32.7 ms on the two runs after the final
  code edit (2026-09-22; the second run followed a fresh migration, cold
  caches), 79 ms and 67 ms (replay) on the earlier host run this section
  first recorded. Same data shape every time; the spread is host and cache
  state, and every run is far below the 500-lock chunk's transaction budget.
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
  without its delete); a failed delete logs the family and sorted uid set of
  the ledger-vs-graph ghost and returns an error naming the delete step
  (`TestGateRetractDeadUIDsLogsGhostOnDeleteError`); nil-ledger skips the
  retract (fail-closed, never an over-delete); chunk bound holds.
- `TestCloudResourceLivenessRefusesUndrainedAdmissionLive` (live PG): the
  admission-drain fence above — undrained active admission refuses, drained
  answers, non-active nonterminal items are ignored.
- `TestLiveCloudRetractEndToEnd` (live PG + live graph): multi-scope
  adversarial layout — node live in scope B survives, history-only node is
  deleted and ledger-released, replay reconverges deterministically.
- Handler tests (AWS/Azure-shaped + EC2): predecessor-only uids become
  candidates; first generation, unwired seams, and empty diffs stay silent;
  retract failure fails the intent for durable retry.
- Writer tests: uid-anchored `MATCH` + `DETACH DELETE`, no `MERGE`, no
  `evidence_source` predicate, UNWIND batching, sequential dispatch,
  fail-closed nil executor.
