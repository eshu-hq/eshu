# #8261 Unordered fact_work_items Lockers

#8205 put the recovery replays, the admin replays and the reducer claim-time
supersede sweep into one row lock order, `work_item_id COLLATE "C"`. Its
review listed five more statements that lock many `fact_work_items` rows in no
fixed order. This note records a two-session repro for each one on `main`.
Every runtime statement among the five reached SQLSTATE 40P01, so each now
takes its locks in the shared order. The fifth, the refinalize reset, cannot
join a row-lock cycle, so it was read and not reproduced.

## Results

All runs used Postgres 18.6 in a local scratch container, with the full
bootstrap schema in an isolated schema per test. The database collation was
`en_US.utf8`, the `postgres` image default. The held-row and stress tests add
the #8205 background: 400,000 rows in 2,000 scopes, mostly `succeeded`, plus
10,000 projector dead letters, all `ANALYZE`d. Main is `26be17f76`.

| statement | meets | held-row probe, main | held-row probe, fix | 150-round stress, main | stress, fix |
| --- | --- | --- | --- | --- | --- |
| poison recovery (`RecoverDeadLettersQuery`) | scoped and stage-only recovery replays | 40P01 2 of 2 (production domain names); 40P01 2 of 2 (mixed-case keys); one-way 2 of 2 (lower-case keys) | one-way 6 of 6 | 5 x 40P01 (production-shaped ids) | 0 |
| retention cascade (`PruneSupersededGenerations`) | claim sweep, scoped and stage-only replays, both start orders | 40P01 6 of 6 | one-way 6 of 6 | 14 x 40P01, 13 rounds failing accounting | 0 |
| admin skip (`SkipRepositoryWorkItems`) | recovery replay, admin replay, claim sweep, both start orders | 40P01 6 of 6 | one-way 6 of 6 | 98 x 40P01 | 0 |
| projector Ack obsolete supersede (`supersedeProjectorObsoleteGenerationsQuery`) | scoped and stage-plus-class projector replays | one-way 2 of 2 (hex generation ids); 40P01 2 of 2 (mixed-case ids, a theoretical shape) | one-way 4 of 4 | 38 x 40P01 (mixed-case ids), 2 rounds failing accounting | 0 |
| refinalize reset (#8207) | none possible | not run (see below) | not run | not run | not run |

Each count is one observed run. A stress count depends on timing; the held-row
probe does not.

## Production Id Shapes

The retention cascade and the admin skip reproduce with any ids: their order
comes from the heap and from `updated_at`, not from collation. Poison
recovery and projector Ack reproduce only when the database's default
collation orders two work ids differently from C. A read-only check of a
deployed replica (aggregate counts only) found:

- The database collation is `en_US.UTF-8` (libc provider), not C.
- 14,047 work rows (13,230 reducer, 817 projector) and 818 generations. None
  of the work ids or generation ids contains an upper-case letter. On that
  data the default-collation rank and the C rank agree for every row, both
  table-wide and within each scope.
- Reducer work ids have the shape
  `reducer_<scope>_<generation>_<domain>_<entity key>`
  (`reducerWorkItemID`, `reducer_queue_helpers.go`). The generation and the
  entity key are lower-case hex; `:` and `/` become `_`.

glibc `en_US` ignores `_` and `-` at the first comparison level; C sorts `_`
below every lower-case letter. Among the 72 reducer domain names in
`go/internal/reducer/contract/domain.go`, 2 pairs sort differently:
`codeowners_ownership` sorts before `code_taint_evidence` and
`code_value_flow_refresh` under `en_US`, and after them under C. The replica
holds `codeowners_ownership` rows in 795 places and `code_value_flow_refresh`
rows too. One scope's newest generation with dead letters in both domains is
therefore a production shape. The poison repro and stress use exactly those
domain names with hex entity keys, and reach 40P01 on main.

Projector work ids are `projector_<scope>_<generation>`, so Ack's order
within one scope is the order of the generation ids. The built-in collectors'
generation ids (lower-case hex StableIDs, or
`terraform_state:<scope>:<lineage>:serial:<n>`) sort the same under both
collations, and the replica shows no divergence. An extension host supplies
its own generation id, which can be any string. The Ack deadlock is therefore
a theoretical shape: reproduced with mixed-case generation ids, not seen in
production data. The fix still follows the shared convention, because it is
cheap (below) and removes the dependency on generation-id spelling.

### Why each one deadlocked

- **Poison recovery.** The candidate CTE orders by `dead.work_item_id` in the
  database's default collation, and the UPDATE locks rows in that order
  (nested loop over the CTE, primary-key probe). With the production domain
  names above and the `codeowners_ownership` row held, poison waited on it
  holding nothing, the replay took the `code_taint_evidence` and
  `code_value_flow_refresh` rows and waited on it, and the release closed the
  cycle. An upper-case entity key does the same. With lower-case keys in one
  domain the two orders agree, and the pair waited one way. The sweep is disjoint by
  predicate: it takes rows with a newer active generation, and poison takes
  rows with no newer generation.
- **Retention cascade.** A prune locks only scope and generation rows until
  its last statement. That `DELETE FROM scope_generations` cascades to the
  generation's work rows through the FK trigger: `generation_id` references
  `scope_generations` `ON DELETE CASCADE` (`migrations/005_fact_work_items.sql`).
  EXPLAIN of the trigger's statement on the populated fixture
  (`DELETE FROM ONLY fact_work_items WHERE $1 = generation_id`) shows a Bitmap
  Index Scan of `fact_work_items_scope_generation_idx` (2,002 index searches,
  a skip scan) under a Bitmap Heap Scan, so rows are deleted in heap order.
  The sweep has no generation-status check, so it retires a superseded
  generation's pending, failed and dead-letter reducer rows. The replays take
  dead letters of any generation. Rows inserted in reverse id order deadlocked
  every pairing.
- **Admin skip.** The UPDATE locks the selected rows in the selection's
  `updated_at DESC` order. A claim that loses the cycle ends the reducer run.
- **Projector Ack.** The supersede locks one projector row
  (`projector_<scope>_<generation>`) per obsolete generation, in generation
  order under the default collation. Within one scope the C order of those ids
  is the C order of the generation ids. The built-in collectors' generation ids
  (lower-case hex StableIDs, `terraform_state:<scope>:<lineage>:serial:<n>`)
  sort the same both ways, and those pairs waited one way. An extension host
  supplies its own generation id, which can be any string. With mixed-case ids
  the pair deadlocked. Production `deadlock_timeout` (1 s) is below Ack's
  `lock_timeout` (2 s), so a cycle surfaces as 40P01, not 55P03.
- **Refinalize reset.** `AcquireReducerClaimFence` takes `LOCK TABLE
  fact_work_items IN EXCLUSIVE MODE` before any write in its transaction,
  with a 1 s `lock_timeout` and retries (#8207, `claim_fence.go`). EXCLUSIVE
  conflicts with ROW SHARE and ROW EXCLUSIVE. While refinalize holds row locks,
  no other transaction holds or takes one; before the fence it holds none. It
  cannot close a row-lock cycle.

## Fix

Each statement locks its rows first in `work_item_id COLLATE "C"` order and
only then updates or deletes them. The pattern and its reasoning are those of
#8205 (`docs/internal/evidence/8205-replay-lock-order.md`).

- Poison recovery: a `poison_locked` CTE takes the selected ids, rechecks
  `status = 'dead_letter'` and the recovery budget, orders by
  `work_item_id COLLATE "C"` and takes `FOR NO KEY UPDATE`. The UPDATE touches
  `work_item_id = ANY(ARRAY(SELECT work_item_id FROM poison_locked))` and keeps
  its status recheck. The selection and its default-collation `ORDER BY` stay,
  so the batch chosen is unchanged.
- Retention: `lockPrunedGenerationWorkItemsQuery` runs right before the
  scope_generations delete in the same transaction. It selects the pruned
  generations' work rows by generation id and takes `FOR UPDATE` in C order.
  `FOR UPDATE` is the lock the cascade delete takes, so nothing upgrades. The
  relock already holds the generation rows `FOR UPDATE`, so no work row of a
  pruned generation can be inserted in between. Its time counts in the
  `delete_scope_generations` phase.
- Admin skip: a `locked` CTE joins `selected`, rechecks the selected status
  and eligibility, and takes `FOR NO KEY UPDATE OF work` in C order
  (`LockRows` above the `Sort`). The UPDATE joins only `locked`, which
  carries each row's selected status, and keeps both rechecks. The `selected`
  CTE (scope, status set, `ORDER BY updated_at DESC, work_item_id ASC LIMIT
  100`) and the final `ORDER BY updated_at DESC, work_item_id ASC` are
  unchanged, so the operator still gets the same deterministic set in the same
  order. A row another worker changes between the selection and the lock is
  left out of the response, as the UPDATE's recheck already did on main.
- Projector Ack: `locked_obsolete_work` joins the generations the existing
  SKIP LOCKED step holds, rechecks stage, scope, the current-generation guard
  and status, and takes `FOR NO KEY UPDATE OF stale` in C order. The UPDATE
  touches only those ids and keeps every generation guard. The current
  generation's rows are never locked or superseded (`stale.generation_id <>
  $3` in both steps).

None of the steps uses `SKIP LOCKED`: each statement must still reach every
eligible row.

Ack's `lock_timeout` is unchanged.
`TestProjectorAckSupersedeKeepsLockTimeoutLive` holds one obsolete projector
row and runs the supersede under Ack's default timeout. It gave up with 55P03
after 2.005 s on the fix and 2.006 s on main, and left every row as it was.

## EvalPlanQual And Mutations

Under Read Committed a waiting row lock rechecks only the locking level's own
quals on the newest row version. Each test changes the first row in lock order
in an open transaction, starts the statement, waits until it blocks, then
commits the change.

| test | change | fix | main |
| --- | --- | --- | --- |
| `TestPoisonRecoveryDropsRowChangedBeforeItsLockLive` | claimed | row kept claimed, 2 others re-driven | same (UPDATE recheck) |
| same | recovery budget spent, still dead letter | row kept, 2 others re-driven | FAIL: the row is re-driven again |
| `TestRetentionCascadeDeletesRowChangedBeforeItsLockLive` | replayed to pending | all 3 rows pruned | same |
| `TestSkipRepositoryDropsRowChangedBeforeItsLockLive` | claimed; replayed to pending | row kept, 2 others dead-lettered | same (UPDATE recheck) |
| `TestProjectorAckSupersedeDropsRowClaimedBeforeItsLockLive` | claimed | row and its generation kept, 2 others superseded | same (UPDATE recheck) |

The budget recheck in the poison lock step is new. On main, a row another
sweep re-drove and that dead-lettered again between the snapshot and the lock
was re-driven twice in one sweep.

Mutations of the fix, each run once:

| mutation | result |
| --- | --- |
| poison: lock-step budget recheck removed | budget case FAIL |
| poison: both status rechecks removed | claimed case FAIL |
| poison: lock order in the default collation (`COLLATE "C"` removed) | held-row probe FAIL 4 of 6 (production domain names and mixed-case keys) |
| retention: lock-step `ORDER BY` removed | held-row probe FAIL 6 of 6 |
| skip: lock-step and UPDATE status rechecks removed | both EPQ cases FAIL |
| skip: lock-step `ORDER BY` removed | held-row probe FAIL 6 of 6 |
| Ack: lock-step and UPDATE status rechecks removed | claimed case FAIL (row and generation superseded) |
| Ack: lock-step `ORDER BY` removed | held-row probe FAIL 2 of 4 (the mixed-case pairs) |

The hermetic shape tests fail on main because no lock step exists there:
`TestRecoverDeadLettersLocksRowsInWorkItemOrder` (poison package),
`TestGenerationRetentionLocksWorkRowsInWorkItemOrderBeforeDelete`,
`TestSkipRepositoryWorkItemsLocksSelectedRowsInWorkItemOrder` (admin store) and
`TestSupersedeProjectorObsoleteGenerationsLocksWorkInWorkItemOrder`.

### Stress method

Each stress round reseeds 18 rows, starts every statement on its own
connection after a random delay below 160 ms, and checks accounting. A
test-schema-only BEFORE UPDATE trigger sleeps 2 ms per updated row, and
`deadlock_timeout` stays at 1 s. Without the delay the replays' lock steps
take every row in about a millisecond, before poison's 116 ms selection
finishes, and main showed 0 deadlocks. Without opposite leading rows both
collations start at the same row, and main also showed 0. The poison stress
ids rotate the three production domain names above with hex keys; the Ack
stress generation ids alternate `Z..` and `a..`. In both, the two orders
share no leading row.

## Performance

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of each shipped statement
on the 400,000-row fixture, inside `BEGIN`/`ROLLBACK`, `jit = off`, one
warm-up and 9 measured repetitions, medians in ms. The skip statement was
captured through `SkipRepositoryWorkItems` itself. Main's SQL came from
`origin/main` (`26be17f76`). The runs went fix, main, fix on a shared host
(load average 1 to 4). None of the plans seq-scans `fact_work_items`.

| statement and shape | main | fix | delta |
| --- | --- | --- | --- |
| poison recovery, 18 eligible dead letters | 112.4 | 106.1, 111.0 | none (noise) |
| poison recovery, 200 eligible (one batch) | 117.7 | 114.9, 118.2 | none (noise) |
| retention lock plus delete, 18 work rows | 5.69 | 9.71, 9.73 | +4.0 |
| retention lock plus delete, 2,000 work rows | 11.30 | 17.00, 16.12 | +5.3 |
| admin skip, 18 rows | 0.568 | 0.609 | +0.04 |
| admin skip, 100 of 400 rows (limit) | 2.725 | 2.957 | +0.23 (+8 %) |
| Ack supersede, no obsolete generation | 0.029 | 0.042, 0.033 | none (noise) |
| Ack supersede, 3 obsolete generations | 0.213 | 0.230, 0.233 | +0.02 |
| Ack supersede, 18 obsolete generations | 0.743 | 0.792, 0.807 | +0.06 |

Whole retention pass (`GenerationRetentionResult.Duration`, the prune
transaction, real `PruneSupersededGenerations`, 9 repetitions after a
warm-up, medians):

| work rows of the pruned generation | main | fix | `delete_scope_generations` phase, main / fix |
| --- | --- | --- | --- |
| 18 | 37.4 | 41.5, 41.6 | 5.3 / 9.9 |
| 2,000 | 42.2 | 49.7, 53.0 | 11.6 / 17.4 |

- Poison recovery's time is its unchanged selection: it walks
  `fact_work_items_pkey` and filters 400,000 rows (about 110 ms). The lock
  step adds a sort and a primary-key probe per selected row.
- Retention's work rows per generation are small in production: on the
  deployed replica, active generations hold p50 18, p99 21 and at most 34
  work rows; superseded ones p50 8 and at most 20.
- Retention's lock finds the rows by `generation_id`, which leads no index.
  The planner skip-scans `fact_work_items_scope_generation_idx` (2,002 index
  searches, one per distinct scope, 4.2 ms here). That is the same lookup the
  FK cascade already makes, so the step about doubles it. The cost grows with
  the number of scopes, not with the size of the pruned generation. Adding
  the known `scope_id` set would make it a range scan, but a work row whose
  `scope_id` differed from its generation's scope would then be deleted by
  the cascade without the ordered lock. The step keeps the exact cascade
  predicate. A pass runs on the retention poll interval, not on a claim.
- Lock hold: the locked set is the set the cascade already locked (same
  `generation_id` predicate), held until the prune commits, as before. The
  hold starts one statement earlier: the `delete_scope_generations` phase grew
  from 5.3 to 9.9 ms (18 rows) and from 11.6 to 17.4 ms (2,000 rows).
- Claims and the sweep: claim candidate selection uses `FOR UPDATE SKIP
  LOCKED`, so it never waits on these rows. The claim-time sweep waited on the
  same rows on main, behind the cascade, until the prune committed; it now
  waits that much longer at most. No worker count, batch or retry changed.
- Rejected alternative: deleting the work rows explicitly, in C order, before
  the generation delete. One statement
  (`DELETE ... WHERE work_item_id = ANY(ARRAY(SELECT ... ORDER BY
  work_item_id COLLATE "C" FOR UPDATE))`) cost 9.1 ms (18 rows) and 17.3 ms
  (2,000 rows) with the generation delete, against 9.0 and 16.3 ms shipped:
  the FK trigger still runs its own lookup (4.1 ms, finding no row).
  Committed batches before the prune would delete work rows of a generation
  the pass may still keep if a later step fails; batches inside the prune
  transaction hold every lock until commit anyway.
- The admin skip's first shape (`= ANY(ARRAY(...))` plus `FROM selected`)
  cost 4.3 ms for 100 rows: the UPDATE nested-looped the `selected` CTE and
  removed 9,900 rows by join filter. The shipped shape carries
  `selected_status` in the lock step and joins only `locked`.
- Ack's steady state, with no obsolete generation, is unchanged. Ack runs once
  per projector generation.

Claim latency: none of the four statements runs in a claim. Poison recovery
and retention run in their own reducer maintenance loops, the skip is an
operator request, and the supersede runs inside projector Ack.

## Concurrency

- Conflict domain: `fact_work_items` rows that two multi-row lockers can both
  hold. These are dead letters (poison, replays, retention, Ack), failed rows
  (skip, replays, sweep, Ack) and older-generation pending rows (skip, sweep,
  retention, Ack).
- Lock mode and order: `FOR NO KEY UPDATE` (poison, skip, Ack) or
  `FOR UPDATE` (retention), all in `work_item_id COLLATE "C"` order, taken in
  one statement before any row is changed.
- Hold time is unchanged: each lock step runs in the transaction that already
  held those rows until commit. Retention takes its work-row locks one
  statement earlier than the cascade did, in the same final step.
- Worker and lease settings are unchanged. No worker count, batch size or
  retry changed, and no statement became serial.

## Observability

No-Observability-Change: no new runtime path, metric or status. Every
statement's 40P01 already reaches an operator. Poison recovery returns
`recover poison dead letters: ...` and counts
`eshu_dp_poison_liveness_failures_total`. Retention returns `prune superseded
generations: ...`, counts `eshu_dp_generation_retention_failures_total` with
reason `store_error`, and logs the cycle. Its new lock step is timed inside
the `delete_scope_generations` phase of
`eshu_dp_generation_retention_phase_duration_seconds`, so a slow lock wait
shows there. The admin skip answers 500 and the request span records the
error. Projector Ack fails with `ack projector work: supersede obsolete
terminal generations: ...` and the work retries. Postgres logs both statements
of a cycle and counts it in `pg_stat_database.deadlocks`; after this change
that counter should stop rising for these statements.

## Residual Risk And Not Checked

- A deployed environment: everything above ran on a local scratch container.
- Poison recovery's selection walks `fact_work_items_pkey` with a filter
  (about 116 ms on the 400,000-row fixture) to find the dead letters in id
  order. This change leaves that selection as it was.
- A claim that moves a pruned generation's row to `claimed` after retention's
  relock and before its delete is still deleted with the generation, as on
  main. This is not a lock-order question.
- The existing poison-package live proofs
  (`TestPoisonLivenessIntegration/BoundedArmIncrementsThenZero`,
  `TestRecoverPoisonDeadLettersQueryDoesNotClobberConcurrentReclaim`) fail on
  main and on this branch, with
  `column target.container_image_identity_v2_required does not exist`. Their
  hand-built schema predates that column, and no CI job sets their
  `ESHU_POISON_LIVENESS_PROOF_DSN`. The repros here use the full bootstrap
  schema instead.
- Projector Ack was driven through its supersede statement in a transaction
  with Ack's lock timeout, not through `ProjectorQueue.Ack`. Ack's earlier
  statements lock the scope row and Ack's own claimed row, which no replay
  locks.
