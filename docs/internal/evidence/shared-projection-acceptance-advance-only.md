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
  `shared_projection_acceptance.generation_ingested_at TIMESTAMPTZ NULL`, and
  migration 126 backfills it from `scope_generations.ingested_at`. The upsert
  fills the column for each incoming row with a per-row PK subquery. The
  incoming generation is committed before its reducer work is enqueued, so it
  is visible, and the FK still rejects an unknown one.
- **The guard is row-local.** It compares only `EXCLUDED` (statement
  constants) with the target row's own columns:

  ```sql
  WHERE shared_projection_acceptance.generation_id = EXCLUDED.generation_id
     OR shared_projection_acceptance.generation_ingested_at IS NULL
     OR (EXCLUDED.generation_ingested_at, EXCLUDED.generation_id)
        > (shared_projection_acceptance.generation_ingested_at, shared_projection_acceptance.generation_id)
  RETURNING scope_id, acceptance_unit_id, source_run_id
  ```

  That is exactly what Read Committed and ON CONFLICT re-check after the lock
  wait (<https://www.postgresql.org/docs/current/sql-insert.html#SQL-ON-CONFLICT>:
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
  - NULL stored key (a pre-#6679 binary during a rolling deploy, or a row the
    backfill skipped): sorts older than anything and is advanced.
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
2. **The backfill is its own file, 126, not part of 125.** A migration file
   runs as one multi-statement simple Query, which PostgreSQL executes as one
   transaction (`schemaConnectionExecutor.execContextWithLockTimeout` passes
   the whole file to `ExecContext`). In one file, 125's ACCESS EXCLUSIVE lock
   would be held through the rewrite. Measured: 16-19 s per 1M rows,
   blocking every acceptance reader and writer. For the same reason, batching
   UPDATEs inside one file would not release locks. 126 uses
   `FOR UPDATE SKIP LOCKED`, so it never waits on a writer's row and cannot
   deadlock one. A skipped row keeps a NULL key and is fixed by its next
   write.

## Proof

All runs used a local `postgres:18-alpine`. No remote host was available for
this change.

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
    holding the lock): 200/200 trials end on the newer generation.
  - The stale-write counter through the production writer: a late older
    write counts 1, and a same-generation retry does not count.
- **Deadlocks.**
  `TestSharedIntentAcceptanceWriterReversedBatchesDoNotDeadlockLive` runs two
  production writers over the same 200 keys, one reversed.
  - GREEN: 20/20 trials, no 40P01, every key on the newer generation.
  - RED with the Go sort and the SQL `ORDER BY` removed:
    `trial 1 writer 1 deadlocked: ... deadlock detected (SQLSTATE 40P01)`.
- **Ordering key.** `TestSharedProjectionAcceptanceOrdersByIngestedAtLive`: a
  generation observed earlier but ingested later wins.
- **Backfill.** `TestSharedProjectionAcceptanceGenerationKeyBackfillLive` runs
  the shipped 126 file with one row locked by another transaction. Every
  other row is filled, and the locked row is skipped without waiting. A rerun
  after release fills it. A NULL-key row is advanced.
- **Hermetic tests.** Rows sort by primary key (RED without the sort: map
  order differed at attempt 0); an unmapped stale key is labelled `unknown`;
  the migration checksum manifest and golden digest cover 125/126.
- **CI enrollment.** The reducer contention gate now runs the seven live
  tests under `-race` with `ESHU_REQUIRE_ACCEPTANCE_MONOTONIC_PROOF=1`, so an
  unset DSN fails there instead of skipping. The enrollment guard
  `TestReducerContentionPostgresProofsRunInTheReducerContentionGate` fails if
  the flag or any test name leaves the workflow (seeded RED for both).
  Lane-style local run: 77 s of test time under `-race` at host load average
  about 45.

No-Regression Evidence: `EXPLAIN (ANALYZE, BUFFERS)` inside `BEGIN`/`ROLLBACK`
of the final statement, compared with the first fix's `EXISTS` statement on
the same data. The batch is production-shaped, 500 rows: 167 advancing,
167 same-generation and 166 stale. The stale rows are removed by the conflict
filter in every run. The host was heavily loaded (load average 35-49), so
medians over interleaved runs are the fair comparison:

| Scale | Metric | Final (row-local) | Previous (`EXISTS`) |
|---|---|---|---|
| 5,000 scopes, 100,000 generations, 1M acceptance rows | Execution, median of 15 | 9.85 ms | 9.86 ms |
| same | Shared buffer hits | about 9,223 | about 9,884 |
| 200 scopes, 4,000 generations, 4,000 acceptance rows | Execution, median of 15 | 17.34 ms | 20.68 ms |
| same | Shared buffer hits | 7,201 | 7,693 |

The rejected LEFT JOIN shape measured 13.2-21.2 ms at 1M scale. The guard
itself is now a tuple comparison; the first fix spent two PK probes per
conflicting row. Batch size, worker count, transaction scope and lock order
are unchanged, except that lock order is now deterministic.

Migration cost on 1M acceptance rows and 100,000 generations, locally:

- 125 `ALTER TABLE ... ADD COLUMN`: 19 ms (catalog only).
- 126 backfill: 15.0 s, `UPDATE 1000000`. A rerun is `UPDATE 0` in 0.6 s.
- The table grew from 163 MB to 335 MB of dead tuples until autovacuum.

The chart's schema-bootstrap Job leaves about 4 minutes for migration work,
so this fits well below that for tables up to several million rows. A
production-size run on the remote corpus copy is still owed before rollout.

Final plan at 1M scale:

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
and `bootstrap.postgres.migration.recorded` events, with `duration_ms` for 125
and 126. The existing `eshu_dp_shared_acceptance_upserts_total` and
`eshu_dp_shared_acceptance_upsert_duration_seconds` are unchanged.
