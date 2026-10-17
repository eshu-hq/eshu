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
| poison recovery (`RecoverDeadLettersQuery`) | scoped and stage-only recovery replays | 40P01 2 of 2 (mixed-case ids); one-way 2 of 2 (lower-case ids) | one-way 4 of 4 | 5 x 40P01 | 0 |
| retention cascade (`PruneSupersededGenerations`) | claim sweep, scoped and stage-only replays, both start orders | 40P01 6 of 6 | one-way 6 of 6 | 14 x 40P01, 13 rounds failing accounting | 0 |
| admin skip (`SkipRepositoryWorkItems`) | recovery replay, admin replay, claim sweep, both start orders | 40P01 6 of 6 | one-way 6 of 6 | 98 x 40P01 | 0 |
| projector Ack obsolete supersede (`supersedeProjectorObsoleteGenerationsQuery`) | scoped and stage-plus-class projector replays | one-way 2 of 2 (hex generation ids); 40P01 2 of 2 (mixed-case ids) | one-way 4 of 4 | 38 x 40P01, 2 rounds failing accounting | 0 |
| refinalize reset (#8207) | none possible | not run (see below) | not run | not run | not run |

Each count is one observed run. A stress count depends on timing; the held-row
probe does not.

### Why each one deadlocked

- **Poison recovery.** The candidate CTE orders by `dead.work_item_id` in the
  database's default collation, and the UPDATE locks rows in that order
  (nested loop over the CTE, primary-key probe). Under `en_US.utf8`, an
  upper-case entity key sorts after lower-case keys; under C it sorts first.
  Reducer work ids end with the entity key
  (`reducer_<scope>_<generation>_<domain>_<key>`). With keys `Nginx`, `api`
  and `mysql`, and `mysql` held, poison took `api` and waited on `mysql`,
  while the replay took `Nginx` and waited on `api`. With lower-case keys the
  two orders agree, and the pair waited one way. The sweep is disjoint by
  predicate: it takes rows with a newer active generation, and poison takes
  rows with no newer generation.
- **Retention cascade.** A prune locks only scope and generation rows until
  its last statement. That `DELETE FROM scope_generations` cascades to the
  generation's work rows through the FK trigger, which scans
  `fact_work_items_scope_generation_idx` as a bitmap and deletes in heap order.
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
  carries each row's selected status, and keeps both rechecks.
- Projector Ack: `locked_obsolete_work` joins the generations the existing
  SKIP LOCKED step holds, rechecks stage, scope, the current-generation guard
  and status, and takes `FOR NO KEY UPDATE OF stale` in C order. The UPDATE
  touches only those ids and keeps every generation guard. The current
  generation's rows are never locked or superseded (`stale.generation_id <>
  $3` in both steps).

None of the steps uses `SKIP LOCKED`: each statement must still reach every
eligible row.

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
| poison: lock order in the default collation (`COLLATE "C"` removed) | held-row probe FAIL 2 of 4 (the mixed-case pairs) |
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
finishes, and main showed 0 deadlocks. Without opposite leading rows
(`K00`/`k01`) both collations start at the same row, and main also showed 0.
The poison and Ack stress ids alternate `Z..` and `a..` keys, so the two
orders share no leading row.

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
- Retention's lock finds the rows by `generation_id`, which leads no index.
  The planner skip-scans `fact_work_items_scope_generation_idx` (2,002 index
  searches, one per distinct scope, 4.2 ms here). That is the same lookup the
  FK cascade already makes, so the step about doubles it. The cost grows with
  the number of scopes, not with the size of the pruned generation. Adding
  the known `scope_id` set would make it a range scan, but a work row whose
  `scope_id` differed from its generation's scope would then be deleted by
  the cascade without the ordered lock. The step keeps the exact cascade
  predicate. A pass runs on the retention poll interval, not on a claim.
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
