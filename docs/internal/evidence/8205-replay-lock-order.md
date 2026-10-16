# #8205 Replay Lock Order

Two concurrent operator replays of dead-lettered work could deadlock (SQLSTATE
40P01). The unbounded recovery replay (`RecoveryStore.ReplayFailedWorkItems`
with no `Limit`) and the admin replay (`buildMutatingWorkItemsQuery`) each
updated their rows in one statement. Each took row locks in its own scan
order. The planner picks a different index for each filter shape, so two
overlapping replays took shared rows in opposite orders. Postgres then aborted
one of them.

## Fix

Both replays now lock their rows first, in one global order, and only then
update them. The order is `work_item_id COLLATE "C"`, the order that
`AckBatch` and the own-gate wake (#7597) already use.

- Recovery (`replayFailedWorkItemsTemplate` in
  `go/internal/storage/postgres/projector_queue_sql.go`). A `locked` CTE runs
  the replay filter with `ORDER BY work_item_id COLLATE "C" FOR NO KEY
  UPDATE`. The UPDATE then touches only
  `work_item_id = ANY(ARRAY(SELECT work_item_id FROM locked)) AND status IN
  ('dead_letter', 'failed')`. The SET list and the skipped-count CTE are
  unchanged. Replays do not stamp `reopened_at`, before or after.
- Admin (`go/internal/query/admin/store/queries.go`). The `selected` CTE keeps
  its unlocked `updated_at DESC` LIMIT selection. The new
  `lockSelectedWorkItemsCTE` locks the selected rows that are still terminal,
  in the same order. The UPDATE joins `FROM locked JOIN selected` and keeps
  its terminal-status recheck. `selected.failure_class` still feeds the #7387
  replay event. The admin dead-letter mutation shares this builder and gets
  the same lock step.
- The bounded recovery replay (`Limit > 0`, `FOR UPDATE SKIP LOCKED`) is
  unchanged. It never waits, and it had 0 deadlocks in every run.

`FOR NO KEY UPDATE` is the lock an UPDATE takes when no key column changes, so
the UPDATE never upgrades a lock. `LockRows` sits above the `Sort` in every
plan below, so a row is locked only after every row before it in that order.

### Status recheck (EvalPlanQual)

Under Read Committed, a waiting row lock rechecks only the locking query's own
quals on the newest row version. The `locked` CTE carries the terminal-status
predicate, so a row that another transaction made `pending` or `claimed` after
the replay's snapshot is dropped there. The UPDATE's own status predicate
repeats the check. Mutation runs show the checks are independent:

| mutation | `TestRecoveryReplayDropsRowChangedBeforeItsLockLive` |
| --- | --- |
| none | PASS |
| `locked` CTE status predicate dropped | PASS (the UPDATE recheck drops the row) |
| UPDATE status predicate dropped | PASS (the lock recheck drops the row) |
| both dropped | FAIL: the claimed row is reset to `pending` and reported replayed |

The admin twin (`TestAdminReplayDropsRowClaimedBeforeItsLockLive`) fails the
same way with both admin rechecks dropped. Each predicate stays for a second
reason as well:

- The CTE predicate bounds what is locked. Without it, a stage-only replay
  would lock every reducer row, `claimed` ones included.
- The UPDATE predicate keeps the UPDATE on an index. Without it, the stage-only
  shape planned a sequential scan of `fact_work_items` (65 ms for 18 rows).

## RED And GREEN

The fixture: Postgres 18.6 (Docker, scratch container) and the full bootstrap
schema in an isolated schema. It holds 400,000 background rows in 2,000
scopes, mostly `succeeded`, plus a tail of 10,000 projector `dead_letter`
rows. Six target scopes hold 3 dead-lettered reducer rows each. `visible_at`
runs backwards against the scope order, and all tables are `ANALYZE`d. The
test asserts with EXPLAIN that the two unbounded replays reach their rows
through different indexes, so the RED cannot go vacuous:

```text
scoped replay scans fact_work_items via [Bitmap Index Scan:fact_work_items_scope_generation_idx Index Scan:fact_work_items_pkey]; stage-only via [Index Scan:fact_work_items_pkey Index Scan:fact_work_items_reducer_source_claim_idx]
```

`TestRecoveryReplayConcurrentReplaysNeverDeadlockLive` runs 150 rounds of
three concurrent replays (scoped plus `retry_exhausted`, stage-only, and
bounded `Limit 3` per scope in reverse scope order):

```text
main (a997e3962): 150 rounds in 2m30.617s: 40P01 scoped=48 stage-only=97 bounded=0; rounds failing exact accounting=0
                  concurrent replays deadlocked 145 times in 150 rounds, want 0   (FAIL)
fix:              150 rounds in 6.267s: 40P01 scoped=0 stage-only=0 bounded=0; rounds failing exact accounting=0   (PASS)
```

Exact accounting means the three replays together reported each of the 18
rows exactly once, and all 18 rows ended `pending`.

The deterministic two-session tests use a test-schema-only `pg_sleep` BEFORE
UPDATE trigger (150 ms per row) and `deadlock_timeout = 10s`. Replay A starts.
Replay B starts once A holds its first row lock. `pg_blocking_pids` is then
sampled three times.

| pair | main | fix |
| --- | --- | --- |
| recovery scoped, then recovery stage-only | A blocked by B and B by A, then 40P01 | B blocked by A only; A 18 rows, B 0 |
| recovery scoped, then admin stage-only | mutual block, 40P01 | one-way, 18 + 0 |
| admin stage-only, then recovery scoped | 40P01 | one-way, 18 + 0 |
| admin id list, then admin stage-only | one-way (both admin replays already lock in `selected` order) | one-way, 18 + 0 |

The admin-versus-admin pair is GREEN-only. Two admin replays share one
selection order on main as well, and no inverted admin pair was reproduced.
The admin fixture runs `updated_at` forward with the scope order, so on main
the admin UPDATE visits s5 first while the scoped recovery replay visits s0
first.

The hermetic shape tests are RED on main, because the lock step does not
exist there:

- `TestReplayFailedWorkItemsLocksRowsInWorkItemOrder`,
  `TestReplayFailedWorkItemsSharesTheBatchAckLockOrder` and
  `TestReplayFailedWorkItemsBoundedKeepsSkipLocked` derive the lock step from
  the shipped template and use a prefix guard on the rendered query.
- `TestBuildMutatingWorkItemsQueryLocksSelectedRowsInWorkItemOrder` does the
  same for the admin builder.

`TestActivationWakeInterleaveLive` (#7597 wake against Fail, admin replay and
claim) is unchanged on both trees: 200 iterations, 0 x 40P01 and 0 lost wakes.

## Performance

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on the fixture above.
Each statement ran inside `BEGIN`/`ROLLBACK`; 9 alternating repetitions after
one warm-up; medians in ms. The SQL text came from the shipped builders, and
main's template came from `origin/main`. The host was shared (load average 6
to 12 on 16 cores). Shown with `jit = off`. With `jit = on` every row is
within 0.1 ms, except the drain, which is 45.7 (main) and 45.6 (fix) ms.

| shape | rows | main | fix | delta | seq scan of fact_work_items |
| --- | --- | --- | --- | --- | --- |
| scoped + class | 18 | 0.335 | 0.356 | +0.02 | none |
| stage-only | 18 | 0.376 | 0.420 | +0.04 | none |
| broad drain, stage-only | 2,018 | 43.0 | 43.3 | +0.3 (+0.7 %) | none |
| scoped, 2,018 dead letters present | 18 | 0.368 | 0.409 | +0.04 | none |
| admin stage-only, Limit 100 | 18 | 0.461 | 0.500 | +0.04 | none |
| admin stage-only, Limit 100 | 100 of 2,018 | 3.41 | 3.82 | +0.41 (+12 %) | none |

The admin cost is one extra pkey probe per selected row (the lock step). The
admin replay is an operator action with a 100-row default limit.

### `IN` versus `= ANY(ARRAY(...))` in the UPDATE

The two UPDATE shapes behave differently because of the lock CTE's row
estimate. On the fixture the planner estimates the CTE at 6,310 rows (18
actual).

With `work_item_id IN (SELECT work_item_id FROM locked)` (the diagnosis
shim), the planner hash-joins the locked ids against a bitmap scan of every
terminal row in the table:

```text
->  Hash Semi Join  (cost=348.40..11592.64 rows=174 width=322) (actual time=1.962..1.969 rows=18.00 loops=1)
      ->  Bitmap Heap Scan on fact_work_items fact_work_items_1  (cost=128.83..11343.60 rows=10321 width=26) (actual time=0.132..1.327 rows=10018.00 loops=1)
```

With `= ANY(ARRAY(...))` (shipped), the CTE runs as an InitPlan, so every lock
is taken before the first update, and the UPDATE probes the primary key once
per id:

```text
->  Index Scan using fact_work_items_pkey on fact_work_items fact_work_items_1  (cost=0.42..84.41 rows=1 width=266) (actual time=0.069..0.078 rows=18.00 loops=1)
      Index Cond: (work_item_id = ANY ((InitPlan 4).col1))
```

Both shapes on two data shapes (`jit = off`, medians in ms, same method as
above). "Tail" is the 10,000 projector `dead_letter` rows that are not part of
the replay. "No tail" sets them to `succeeded` and runs `VACUUM ANALYZE`,
which matches the diagnosing lane's fixture of dead letters only in the
replayed set.

| shape | tail: IN | tail: ANY | no tail: IN | no tail: ANY |
| --- | --- | --- | --- | --- |
| scoped 18 rows | 0.389 | 0.356 | 0.411 | 0.366 |
| stage-only 18 rows | 2.275 | 0.420 | 0.426 | 0.419 |
| broad drain 2,018 rows | 46.8 | 43.3 | 48.1 | 45.8 |

With the tail, IN scans every terminal row in the table; with JIT on, its
drain is 60.1 ms because the plan cost passes `jit_above_cost`. Without the
tail, the two shapes tie. The diagnosing lane reported the opposite for the
broad drain (about 52 ms with `ANY(ARRAY)` against 57 ms with IN, on its own
data shape). Its plans were not recorded, and the gap did not reproduce here
on either shape.

The tail shape is the one that matters. IN's cost is set by every terminal
row in `fact_work_items`, whatever stage, scope or class they belong to. A
production queue keeps such a backlog: dead letters under review, the
manual-review classes no drain may replay, and other stages. `ANY(ARRAY)`'s
cost is set only by the rows the replay locks.

### `AS MATERIALIZED`

`AS MATERIALIZED` is not load-bearing. In all six shapes the plan node
signature is the same with and without it, for both builders, and the times
match within noise.

The lock step is never folded into the UPDATE. PostgreSQL 18 inlines a CTE
only when `!contain_dml(cte->ctequery)` holds (`SS_process_ctes` in
`src/backend/optimizer/plan/subselect.c`, REL_18_STABLE). `contain_dml`
returns true for a query with `rowMarks`, with the comment "We reject SELECT
FOR UPDATE/SHARE as well". A `FOR NO KEY UPDATE` CTE is therefore always
planned as its own CTE node, with `LockRows` above `Sort`. The query-shape
tests fail if the `ORDER BY work_item_id COLLATE "C"` or the
`FOR NO KEY UPDATE` is removed from either lock step.

## Concurrency

- Conflict domain: `fact_work_items` rows in `dead_letter` or `failed`.
- Lock mode: `FOR NO KEY UPDATE`, taken in `work_item_id COLLATE "C"` order
  by the lock step.
- Hold time: one autocommit statement (recovery), or one statement before the
  replay-event insert (admin). This is about 45 ms for a 2,018-row drain.
- Writers that meet these rows:
  - Another unbounded replay or admin mutation: same order, so the second
    waits one way (proven above).
  - Bounded replay: `SKIP LOCKED`, never waits.
  - `AckBatch` and the own-gate in-flight lock: lock `claimed` or `running`
    rows, which no replay locks. They share the same order anyway
    (`TestReplayFailedWorkItemsSharesTheBatchAckLockOrder`).
  - Own-gate wake: `SKIP LOCKED` on dead letters, never waits.
  - Reducer `Fail` (`failReducerWorkQuery`): one autocommit UPDATE of one
    `claimed`/`running` row by primary key. A one-row locker holds nothing
    while it waits, so it cannot close a cycle. Its follow-up wake statement
    is `SKIP LOCKED`.
- Reducer claim-time supersede sweep (`supersedeInactiveReducerGenerationsCTE`
  in `reducer_generation_filter_sql.go`): a residual, out of this change's
  files. It is a plain blocking UPDATE with no ORDER BY over
  `pending`/`retrying`/`failed`/`dead_letter` rows of inactive generations, so
  it still locks in scan order. In a scratch shim (18 dead letters on an
  inactive generation, a per-row sleep trigger, the claim first, then the
  replay), the results were:
  - 400,000 background rows, where the sweep scans
    `fact_work_items_stage_domain_status_idx`:
    - main: the scoped replay deadlocked the claim, and the claim statement
      got the 40P01. The stage-only replay waited one way; on main it walks
      the same `visible_at` order as the sweep.
    - fix: both replay variants deadlock with the sweep, and the replay gets
      the 40P01.
  - 18 rows only, both sides seq-scanning: one-way on both trees when the
    heap order matches id order. On the fix, a reversed heap order deadlocks.

  The hazard predates this change, but the set of replay variants that can
  hit it moves. Removing it needs the sweep to take the shared order or
  `SKIP LOCKED`, on the reducer claim hot path.
- Retention deletes and the refinalize reset: NOT_CHECKED.

## Observability

No-Observability-Change: a replay is an operator request, and its 40P01
already reaches the operator. The runtime recovery route answers 422 with the
error text (`deadlock detected (SQLSTATE 40P01)`). The admin replay answers
500, and the request span records the error. Postgres logs both statements
of the cycle and counts it in `pg_stat_database.deadlocks`. This change
removes the replay-against-replay cycle and adds no new runtime path, so no
metric is added. Rerun a replay that failed with 40P01.

## Not Checked

- A deployed environment: everything above ran on a local scratch container.
- The smallest table size at which the replay plans diverge.
- Retention deletes and the refinalize reset as waiters on replayed rows.
- An inverted admin-versus-admin pair (no plan was found that inverts two
  admin replays on main).
