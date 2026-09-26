# Shared-Projection Acceptance Is Advance-Only

Issue #6679: the shared-projection acceptance upsert was last-writer-wins. Its
`ON CONFLICT (scope_id, acceptance_unit_id, source_run_id) DO UPDATE SET
generation_id = EXCLUDED.generation_id` let a writer holding an older
generation, committing after a writer holding a newer one, move the accepted
generation backwards. The shared-projection lane keeps only intents whose
acceptance key maps to the accepted generation, so a rolled-back row marks the
newer generation's intents stale and lets older intents project as current
truth.

## What the first fix got wrong (review F1)

The first version guarded the update with an `EXISTS` over two
`scope_generations` rows. That subquery reads the statement snapshot. Read
Committed re-checks an update's condition against the target row's newest
version after a lock wait, but an updating command "does not see effects of
those commands on other rows in the database"
(<https://www.postgresql.org/docs/current/transaction-iso.html#XACT-READ-COMMITTED>).

Take a multi-row upsert B that blocks on its first key X. Meanwhile
transaction C commits an older generation and an acceptance row for B's
second key Y, both after B's snapshot. When B reaches Y, the stored
generation is invisible to the subquery, `EXISTS` is false, and B's newer
write is skipped as "stale". Y stays on the older generation. That is the
#6679 defect again, and the stale counter reports it as benign. The earlier
claim here, that an invisible generation row makes keeping the stored row
"the safe direction", was wrong for the stored side.

## Design

- **The ordering key lives on the row.** Migration 125 adds
  `shared_projection_acceptance.generation_ingested_at TIMESTAMPTZ NULL`, a
  copy of `scope_generations.ingested_at`. There is no backfill (see "Why
  the backfill migration was removed"). The upsert fills the column for each incoming
  row with a per-row PK subquery. The incoming generation is committed before
  its reducer work is enqueued, so it is visible, and the FK still rejects an
  unknown one.
- **The guard is row-local for every row the new binary wrote.** It compares
  `EXCLUDED` (statement constants) with the target row's own columns. Only a
  NULL (legacy) stored key is resolved from `scope_generations`:

  ```sql
  WHERE shared_projection_acceptance.generation_id = EXCLUDED.generation_id
     OR (EXCLUDED.generation_ingested_at, EXCLUDED.generation_id)
        > (COALESCE(
               shared_projection_acceptance.generation_ingested_at,
               (SELECT stored_generation.ingested_at
                FROM scope_generations AS stored_generation
                WHERE stored_generation.generation_id = shared_projection_acceptance.generation_id),
               '-infinity'::timestamptz
           ),
           shared_projection_acceptance.generation_id)
  RETURNING scope_id, acceptance_unit_id, source_run_id
  ```

  For a non-NULL stored key, that is exactly what Read Committed and
  ON CONFLICT re-check after the lock wait (<https://www.postgresql.org/docs/current/sql-insert.html#SQL-ON-CONFLICT>:
  the condition "is evaluated last, after a conflict has been identified as a
  candidate to update"; a row "locked but not updated because an ON CONFLICT
  DO UPDATE ... WHERE clause condition was not satisfied ... will not be
  returned").
- **The ordering is `(ingested_at, generation_id)`**, not `observed_at`.
  Activation and supersession order by `ingested_at` (#6686). A generation
  observed earlier but ingested later is the one the projector activates, so
  ordering by `observed_at` would call its writes stale.
- **Branch outcomes:**
  - Same generation: applied, refreshing `accepted_at`/`updated_at` and
    filling a missing key. Not counted as stale.
  - NULL stored key on a legacy row (written before 125): COALESCE evaluates
    only the arguments it needs
    (<https://www.postgresql.org/docs/current/functions-conditional.html>), so
    only these rows pay a PK probe for the stored generation's `ingested_at`.
    That generation was committed before the deploy, so before any new-binary
    statement's snapshot. The probe therefore resolves to the value a backfill
    would have stored, and the guard stays advance-only. The SET fills the
    key, so the row heals on its first applied write.
  - NULL stored key whose generation the probe cannot see (an old-binary row,
    during a rolling deploy, at a generation ingested after this statement's
    snapshot): `'-infinity'` makes the write advance, as before #6679. Without
    it, the row comparison is NULL
    (<https://www.postgresql.org/docs/current/functions-comparisons.html#ROW-WISE-COMPARISON>)
    and a newer write would be dropped.
  - Stale non-NULL key during a rolling deploy: an old binary that updates a
    row which already carries a key sets `generation_id` but leaves
    `generation_ingested_at` at the previous generation's value, so until the
    next same-generation write refreshes it, a late writer holding a
    generation that sorts between the two can still pass the guard.
  - NULL incoming key (a generation the statement snapshot cannot see): fails
    the comparison, so it is skipped and counted, never applied blind.
- **Batches take locks in one order.** Rows are sorted by primary key in Go
  (`buildSharedProjectionAcceptanceRows`), and the statement adds
  `ORDER BY ... COLLATE "C"` (byte order, matching Go). Without the sort, map
  iteration handed each batch a random key order, and two overlapping
  batches could deadlock (40P01), aborting the whole intents+acceptance
  transaction.
- **Writers:** `SharedIntentAcceptanceWriter` is the only writer. The
  code-call and repo-dependency lanes reach it (`CodeCallIntentWriter` is an
  alias).

Deviations from the design ruling, both measured:

1. **Scalar subquery instead of the LEFT JOIN.** The planner hashed the
   `LEFT JOIN scope_generations` over the whole generations table: a
   100,000-row seq scan per 500-row batch, 13.2-21.2 ms. The per-row PK
   subquery is 500 index probes whatever the table size. The semantics are
   the same.
2. **No backfill.** The first version of this change shipped a separate
   backfill migration, which has since been removed (next section). Before
   main took migrations 122-124, this branch numbered the column migration
   122 and the backfill 123, and the remote backfill run below measured them
   under those numbers. Only the column migration, now 125, ships.

## Why the backfill migration was removed

The removed backfill migration filled `generation_ingested_at` in one
statement with `FOR UPDATE SKIP LOCKED`. A remote production-size run (AWS
r7a.4xlarge, PostgreSQL 18.6, n=1 per size) measured it under 4 live writer
clients and 1 reader. It was sent the way the migration runner sends it, one
file as one implicit transaction:

| Acceptance rows | Backfill duration | WAL | Table before, then after (heap and indexes) |
|---|---|---|---|
| 1M | 31.4 s | 0.83 GB | 304 MB, then 627 MB |
| 5M | 190.6 s (3m11s) | 5.69 GB | 1,499 MB, then 3,091 MB |
| 10M | 394.4 s (6m34s) | 16.71 GB | 2,998 MB, then 6,152 MB |

- **It stalled every writer for the whole run.** All four writer sessions
  sat in `Lock/transactionid` for essentially the entire run (431, 2,639 and
  5,464 wait samples). The single transaction holds every row lock it takes
  until it ends. `SKIP LOCKED` stops the backfill waiting on writers, but
  does nothing for writers waiting on it. Readers were unaffected (PK lookups at most
  15.6 ms).
- **The table doubled permanently.** Every update was non-HOT: the default
  fillfactor leaves no room on packed pages. `VACUUM` did not shrink it, and
  reclaiming the space needs `VACUUM FULL` or `REINDEX`.
- **It could overrun the bootstrap Job.** At 10M rows it exceeds the Job's
  roughly 4-minute budget for migrations; break-even is about 6M rows.
- **Chunking would not have been enough.** A candidate that commits between
  chunks cut the writer stall (at most 0.36 s at 10M), but still took 374 s
  at 10M, wrote 25.9 GB of WAL, and doubled the table.

So the backfill was dropped. 125 (catalog-only: 36 ms, 37 ms and 62 ms at 1M,
5M and 10M rows remotely) is the only migration. NULL keys are resolved lazily in
the guard (above), so legacy rows cost nothing unless they are written, and
nothing is ever rewritten in bulk.

## Proof

Two environments. The live tests and the plan-shape checks ran on a local
`postgres:18-alpine` (PostgreSQL 18). The backfill and migration timings and
both EXPLAIN tables ran on a remote host: AWS r7a.4xlarge (16 vCPU, 128 GiB),
`postgres:18-alpine` (PostgreSQL 18.6) in Docker on EBS.

- **Post-snapshot interleaving.**
  `TestSharedProjectionAcceptancePostSnapshotStoredGenerationLive` runs L
  holding X, B's production two-row upsert blocked on X (observed in
  `wait_event_type = 'Lock'`), then C committing a late generation plus Y
  before L releases. 10 trials per case.
  - RED on the snapshot-bound `EXISTS` guard, `rc=1`:
    `newer-incoming: trial 0: Y = "gen-6679-late-newer-incoming-0", stale = 1 rows; want Y = "gen-6679-new"`.
    The `older-incoming` mirror passed 10/10.
  - GREEN on the row-local guard: both cases 10/10, `rc=0`.
- **Earlier proofs, unchanged and GREEN.**
  - The sequential test.
  - The 4 × 50 lock-wait matrix (row absent or preseeded, either generation
    holding the lock): 200/200 trials end on the newer generation (ledger:6679-concurrent-out-of-order-g-new).
  - The stale-write counter through the production writer: a late older
    write counts 1, and a same-generation retry does not count.
- **Deadlocks.**
  `TestSharedIntentAcceptanceWriterReversedBatchesDoNotDeadlockLive` runs two
  production writers over the same 200 keys, one reversed.
  - GREEN: 20/20 trials, no 40P01, every key on the newer generation (ledger:6679-reversed-batches-no-deadlock).
  - RED with the Go sort and the SQL `ORDER BY` removed:
    `trial 1 writer 1 deadlocked: ... deadlock detected (SQLSTATE 40P01)`.
- **Ordering key.** `TestSharedProjectionAcceptanceOrdersByIngestedAtLive`: a
  generation observed earlier but ingested later wins.
- **Legacy NULL-key rows**, in `shared_projection_acceptance_contention_live_test.go`.
  Run against 8e785b9d7's guard (restored from git) versus the new guard:
  - `TestSharedProjectionAcceptanceLegacyNullKeyRejectsStaleLive/older-incoming`.
    A NULL-key row at G_new receives a late G_old write.
    - RED on 8e785b9d7, `rc=1`:
      `legacy row: generation = "gen-6679-old", stale = 0; want "gen-6679-new" kept and 1 stale write`.
    - GREEN on the new guard: G_new kept, 1 stale write, and the key is left
      NULL, because a rejected write does not touch the row.
  - `.../same-generation-fills-key`: a same-generation retry applies and
    fills the key with G_new's `ingested_at`. GREEN on both guards.
  - `TestSharedProjectionAcceptanceLegacyNullKeyAdvancesLive`: a NULL-key row
    at G_old advances to G_new and gets its key. GREEN on both.
  - `TestSharedProjectionAcceptanceLegacyNullKeyInvisibleGenerationAdvancesLive`.
    During B's lock wait, C commits a generation plus a NULL-key row (the
    old-binary shape). The generation is invisible to B's snapshot, and B's
    G_new write advances with its key, 5/5. GREEN on both guards.
    - Mutation check: removing `'-infinity'` makes it RED, `rc=1`:
      `trial 0: Y = "gen-6679-invisible-0", stale = 1 ... want "gen-6679-new" advanced`.
      The fallback is load-bearing.
- **Hermetic tests.** Rows sort by primary key (RED without the sort: map
  order differed at attempt 0); an unmapped stale key is labelled `unknown`;
  the migration checksum manifest and golden digest cover 125 (146 definitions).
- **CI enrollment.** The reducer contention gate now runs the nine live
  tests under `-race` with `ESHU_REQUIRE_ACCEPTANCE_MONOTONIC_PROOF=1`, so an
  unset DSN fails there instead of skipping. The enrollment guard
  `TestReducerContentionPostgresProofsRunInTheReducerContentionGate` fails if
  the flag or any test name leaves the workflow (seeded RED for both).
  Lane-style local run (at 8e785b9d7): 77 s of test time under `-race` at
  host load average about 45.

Performance Evidence: remote host, the shipped statement against the
statements it replaces. 500-row batch (about 167 advancing, 167
same-generation and 166 stale rows), `EXPLAIN (ANALYZE, BUFFERS)` inside
`BEGIN`/`ROLLBACK`, 45 windows per statement per state with the statement
order rotated, medians in ms, on a quiet host. "Unguarded" is main's
pre-change statement, `EXISTS` the first fix, and row-local the 8e785b9d7
shape.

All stored keys NULL (legacy rows; every conflicting row probes
`scope_generations`):

| Acceptance rows | Unguarded | `EXISTS` | Shipped |
|---|---|---|---|
| 1M | 33.07 | 25.85 | 29.20 |
| 5M | 32.06 | 23.80 | 26.67 |
| 10M | 33.34 | 25.72 | 28.60 |

All stored keys filled:

| Acceptance rows | Unguarded | Row-local | Shipped |
|---|---|---|---|
| 1M | 31.52 | 24.14 | 24.39 |
| 5M | 33.73 | 24.54 | 25.31 |
| 10M | 33.14 | 24.59 | 25.43 |

- The shipped statement is faster than main's pre-change statement in both
  states: 3.9-5.4 ms faster with NULL keys, 7.1-8.4 ms faster with filled
  keys.
- While legacy keys are NULL it is 11-13% (2.9-3.4 ms) slower than the
  intermediate `EXISTS` design. That state is transient: each row heals on
  its first accepted write. With filled keys it matches the row-local shape
  within noise (+0.25 to +0.84 ms; per-shape spread is about 4-6 ms).
- Migration 125 alone at 10M rows, under 4 writers and 1 reader: 0.038 s and
  0.043 s, catalog-only, with no lock wait sampled and no writer transaction
  over 1 s.
- 4-writer soak at 10M rows (150 s, about 1.5M updates per arm) against a
  control running the pre-change statement: +14.2 MB (+0.42%) total, which is
  the 8-byte key on the 1.36M rows it filled. Nothing is rewritten in bulk.
- Limits: synthetic writers, literal VALUES (custom plan; a generic plan was
  not checked), and the rolling deploy with an old binary writing
  concurrently was not measured.

Local plan-shape check (4,000 acceptance rows, one run each; shape only, not
timing):

- All keys NULL: the stored-generation probe, `Index Scan using
  scope_generations_pkey`, runs `loops=333`, once per conflicting row that
  is not a same-generation retry.
- All keys filled: the same probe is `(never executed)`.
- Both states remove 166 stale rows.

Earlier local medians for the 8e785b9d7 shape, 1M rows at load average 35-49:
9.85 ms final versus 9.86 ms for `EXISTS`, with about 9,223 versus 9,884
buffers. The rejected LEFT JOIN shape measured 13.2-21.2 ms. Batch size,
worker count, transaction scope and lock order are unchanged, except that
lock order is now deterministic.

Migration cost: only 125 remains, catalog-only. Locally it took 19 ms;
remotely 36-62 ms at 1M-10M rows, with one writer transaction and one reader
briefly queued behind the ACCESS EXCLUSIVE.

Plan of the 8e785b9d7 row-local shape at 1M scale (local):

```text
 Insert on shared_projection_acceptance (actual time=1.167..7.861 rows=334.00 loops=1)
   Conflict Resolution: UPDATE
   Conflict Arbiter Indexes: shared_projection_acceptance_pkey
   Conflict Filter: ((shared_projection_acceptance.generation_id = excluded.generation_id) OR (shared_projection_acceptance.generation_ingested_at IS NULL) OR (ROW(excluded.generation_ingested_at, excluded.generation_id) > ROW(shared_projection_acceptance.generation_ingested_at, shared_projection_acceptance.generation_id)))
   Rows Removed by Conflict Filter: 166
   Tuples Inserted: 0
   Conflicting Tuples: 500
   Buffers: shared hit=9223 dirtied=5 written=5
   ->  Subquery Scan on "*SELECT*" (actual time=0.991..1.061 rows=500.00 loops=1)
         Buffers: shared hit=2003
         ->  Sort (actual time=0.990..1.017 rows=500.00 loops=1)
               Sort Key: "*VALUES*".column1 COLLATE "C", "*VALUES*".column2 COLLATE "C", "*VALUES*".column3 COLLATE "C"
               Sort Method: quicksort  Memory: 72kB
               Buffers: shared hit=2003
               ->  Values Scan on "*VALUES*" (actual time=0.026..0.791 rows=500.00 loops=1)
                     Buffers: shared hit=2000
                     SubPlan 1
                       ->  Index Scan using scope_generations_pkey on scope_generations generation (actual time=0.001..0.001 rows=1.00 loops=500)
                             Index Cond: (generation_id = "*VALUES*".column4)
                             Index Searches: 500
                             Buffers: shared hit=2000
 Planning:
   Buffers: shared hit=284
 Planning Time: 1.707 ms
```

Observability Evidence: `eshu_dp_shared_acceptance_stale_writes_total{domain}`
is recorded by `SharedIntentAcceptanceWriter` from the keys missing in
`RETURNING`. The domain set is the bounded reducer domains plus `unknown` for a
key with no intent, which is unreachable today. Each write call that skips at
least one row emits one WARN line, `shared acceptance stale write skipped;
stored generation is newer`, carrying `acceptance.scope_id`,
`acceptance.unit_id`, `acceptance.source_run_id`, `acceptance.generation_id`
(the first skipped key), `acceptance.stale_count` and `pipeline_phase=shared`.
Migration progress uses the existing `bootstrap.postgres.migration.applying`
and `bootstrap.postgres.migration.recorded` events, with `duration_ms` for 125.
Legacy-key healing needs no new signal: it happens inside ordinary upserts
already covered by `eshu_dp_shared_acceptance_upserts_total`. The existing `eshu_dp_shared_acceptance_upserts_total` and
`eshu_dp_shared_acceptance_upsert_duration_seconds` are unchanged.
