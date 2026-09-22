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

Fix, in four parts (rounds 1 and 2 of the post-rebase review):

1. Refuse, never skip. `LiveAdmissionCloudUIDs` returns
   `reducercontract.ErrCloudAdmissionUndrained` (wrapped with
   `scope/generation=status` for the first five) while any active-generation
   `cloud_inventory_admission` work item is nonterminal — `pending`,
   `claimed`, `running`, `retrying`, `failed`, `dead_letter`, the same list
   `generation_liveness_sql.go` uses for the same table. A skip would leak
   forever (a uid absent in G and G+1 is never a candidate again); a
   fallback to the scope's last drained generation misses a uid new in the
   undrained one. Dead-lettered admissions block and name their scope.
2. One statement, one snapshot. The chunk transaction is READ COMMITTED,
   so a fence statement followed by the admission probe would see two
   snapshots and a scope's pointer could flip onto an undrained generation
   between them. `liveAdmissionCloudUIDsFencedSQL` returns the fence rows and
   the admission rows from a single `UNION ALL` statement, so they cannot
   disagree, without REPEATABLE READ and its serialization retries.
3. Cheap refusal. `CloudResourceRetracter.RetractDeadCloudResourceNodes`
   runs `postgres.RequireCloudAdmissionDrained` in its own rolled-back
   transaction before the first per-uid lock, so a routine multi-scope race
   costs one index probe and no 500-lock acquisition on the write path
   (`TestCloudResourceRetracterRefusesBeforeLockingWhenAdmissionUndrained`:
   zero `LockUIDs` calls, zero deletes, one rolled-back transaction).
4. Non-counting deferral. The handler classifies the refusal
   (`reducer.ClassifyCloudRetractError`) as `cloud_admission_not_ready`, a
   `Retryable()` readiness class enrolled in
   `nonCountingReducerRetryFailureClasses`, so the queue retries the intent
   without eroding its attempt budget; an unclassified refusal would have
   counted toward `maxAttempts` and dead-lettered a healthy scope's node
   writes under continuous multi-scope ingest. Pinned by
   `TestReducerQueueFailDefersCloudAdmissionReadinessPastAttemptBudget`
   (AttemptCount 42 past MaxAttempts 3 stays `retrying` in that class),
   `TestEveryReadinessFailureClassIsEnrolled` (the go/ast guard sees the
   class beside its method in `internal/reducer/cloud_resource_retract.go`)
   and `TestReadinessDomainsWithoutAClaimGateAreTheKnownSet` (placed nowhere:
   three domains return it and the awaited condition is deployment-wide, so
   no single-scope CTE row can express it).

The EC2 family needs no fence: `LiveEC2PostureUIDs` reads
`ec2_instance_posture`, a collector-emitted kind that lands with the
generation at ingest. Only the cloud family chose a reducer-derived oracle,
which is exactly why only it needed the fence.

Theory shims (private Postgres 18, rolled-back transactions after `ANALYZE`;
scripts `scratchpad/6887-shim*.sql`, session-local, not committed):

| shape | statement | plan | buffers | time |
| --- | --- | --- | --- | --- |
| 2,000 scopes x 10 generations = 20,000 admission items (19,997 `succeeded`, 2 `pending`, 1 `dead_letter`) | pre-lock fence (`undrainedCloudAdmissionSQL`) | Index Scan `fact_work_items_stage_domain_status_idx`, `status = ANY` as index cond, nested loop on `ingestion_scopes_active_generation_idx` | 17 | 0.075 ms |
| same, plus 200,000 admission facts (100 per active scope); 500 candidates, 250 alive; drained | fenced probe (`liveAdmissionCloudUIDsFencedSQL`) | fence branch 2 buffers; alive branch Seq Scan `ingestion_scopes` -> Nested Loop `fact_records_scope_generation_idx` | 204,079 | 199.8 ms |
| same | original alive-only probe (pre-fence statement) | identical alive plan | 204,077 | 201.3 ms |
| same, 3 undrained | fenced probe | same, 253 rows | — | 123.2 ms |
| same, 2 candidates | alive-only probe | Bitmap Index Scan `fact_records_cloud_retract_admission_uid_idx` (migration 118), hash join over Seq Scan `ingestion_scopes` | 130 | <1 ms |

Reading: the fence adds two buffers to the in-transaction probe and nothing
else; fenced and unfenced statements plan the alive branch identically. The
alive branch itself abandons the migration-118 partial index once the
candidate array reaches lock-chunk size (the planner estimates ~1,000 rows
per array element) and walks the scopes instead: ~200 ms and ~204k buffers
per 500-candidate chunk at 2,000 scopes x 100 admitted facts, held under up
to 500 per-uid advisory locks. That is a property of the probe this PR
already carries, not of the fence, and it bounds the "per-candidate O(1)"
argument below to the index path. Recorded as #6946 with the exact input
shape; not widened into this PR.

Proof: `TestCloudResourceLivenessRefusesUndrainedAdmissionLive` — every
nonterminal status on the active generation returns the sentinel naming the
scope; `succeeded` and `superseded` answer normally and the uid admitted
only by the superseded generation reads dead; a nonterminal item on a
non-active generation does not block. RED before the fence (undefined
sentinel, then dead read), GREEN after.

Operator signal: the refusal lands as `failure_class =
cloud_admission_not_ready` on the retrying work item with the sentinel text
(`scope/generation=status`) in `failure_message`, and in the reducer's
handler-failure log; no new metric. A scope stuck in `dead_letter` is
already the operator's queue alarm and now also names itself in every
deferred retract.

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
