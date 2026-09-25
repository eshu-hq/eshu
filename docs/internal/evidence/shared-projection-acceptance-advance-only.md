# Shared-Projection Acceptance Is Advance-Only

Issue #6679: the shared-projection acceptance upsert was last-writer-wins. Its
`ON CONFLICT (scope_id, acceptance_unit_id, source_run_id) DO UPDATE SET
generation_id = EXCLUDED.generation_id` let a writer holding an older
generation, committing after a writer holding a newer one, move the accepted
generation backwards. The shared-projection lane keeps only intents whose
acceptance key maps to the accepted generation, so a rolled-back row marks the
newer generation's intents stale and lets older intents project as current
truth.

## Change

`upsertSharedProjectionAcceptanceBatchSuffix` now carries a conflict filter:

```sql
WHERE shared_projection_acceptance.generation_id = EXCLUDED.generation_id
   OR EXISTS (
       SELECT 1
       FROM scope_generations AS incoming
       JOIN scope_generations AS stored
         ON stored.generation_id = shared_projection_acceptance.generation_id
       WHERE incoming.generation_id = EXCLUDED.generation_id
         AND (incoming.observed_at, incoming.generation_id)
             > (stored.observed_at, stored.generation_id)
   )
RETURNING scope_id, acceptance_unit_id, source_run_id
```

- Ordering is `scope_generations (observed_at, generation_id)`, the same total
  order `priorGenerationIDSQL` uses.
- Same generation: applied. A retry refreshes `accepted_at`/`updated_at` and
  is not counted as stale.
- Missing generation row: `EXISTS` is false, so the row is left alone. The
  stored side cannot be missing for a committed row, because the FK is
  `ON DELETE CASCADE`. The incoming side is FK-checked on insert. The only way
  to reach this branch is a generation row the statement snapshot cannot see,
  and keeping the stored row is the safe direction there.
- The only writer is `SharedProjectionAcceptanceStore.Upsert` /
  `UpsertReportingStale`, called from `SharedIntentAcceptanceWriter` (code-call,
  repo-dependency and every other shared lane; `CodeCallIntentWriter` is an
  alias). Acceptance keys are deduplicated by
  `buildSharedProjectionAcceptanceRows`, so no batch carries a duplicate key.

Concurrency semantics come from the PostgreSQL docs,
<https://www.postgresql.org/docs/current/transaction-iso.html> (Read
Committed) and <https://www.postgresql.org/docs/current/sql-insert.html>. A
would-be updater "will wait for the first updating transaction to commit or roll
back", then "the search condition of the command (the WHERE clause) is
re-evaluated to see if the updated version of the row still matches". For
upserts, "if a conflict originates in another transaction whose effects are not
yet visible to the INSERT, the UPDATE clause will affect that row". The
`condition` "is evaluated last, after a conflict has been identified as a
candidate to update", and "if a row was locked but not updated because an ON
CONFLICT DO UPDATE ... WHERE clause condition was not satisfied, the row will
not be returned". The guard therefore needs no extra lock. Skipped rows stay
locked until commit, which matches the old behaviour, where every conflicting
row was locked by its update.

## Proof

RED on base `2ae147cf9`, run against a local `postgres:18-alpine`:

```text
ESHU_POSTGRES_TEST_DSN=... go test ./internal/storage/postgres \
  -run 'TestSharedProjectionAcceptance(Sequential|Concurrent).*Live' -count=1 -v
--- FAIL: TestSharedProjectionAcceptanceSequentialStaleWriteLive
    accepted generation after stale write = "gen-6679-old", want "gen-6679-new"
--- FAIL: .../absent/new-holds-lock    trial 0: accepted = "gen-6679-old"
--- PASS: .../absent/old-holds-lock    50/50
--- FAIL: .../preseeded/new-holds-lock trial 0: accepted = "gen-6679-old"
--- PASS: .../preseeded/old-holds-lock 50/50
rc=1
```

GREEN on the head: the sequential test passes. All four lock-wait variants
(row absent or preseeded, with G_new or G_old holding the uncommitted lock and
the other writer seen in `pg_stat_activity` with `wait_event_type = 'Lock'`
before the holder commits) end on G_new in 50/50 trials each, 200 in total.
`TestSharedIntentAcceptanceWriterStaleWriteCounterLive` drives the production
writer through G_new, a late G_old (counted once) and a G_new retry (not
counted). It exited with `rc=0`. Mutation check: dropping the equal-generation
branch makes both the same-generation `updated_at` assertion and the counter
assertion (`= 2, want 1`) fail.

No-Regression Evidence: `EXPLAIN (ANALYZE, BUFFERS)` inside `BEGIN`/`ROLLBACK`
on a seeded table with 200 scopes, 4,001 `scope_generations` rows and 4,000
acceptance rows, all at generation 10. The statement was one production-shaped
500-row batch: 167 advancing (gen 15), 167 same-generation (gen 10) and 166
stale (gen 5). Both correlated lookups use `scope_generations_pkey`, and the
same-generation branch short-circuits the `OR` (333 SubPlan loops for 500 rows).
Over 3 runs each, execution took 6.8, 9.8 and 10.0 ms for the new statement and
9.8, 12.0 and 12.4 ms for the old one, so there was no measurable regression.
Shared buffer hits went from 7,388 to 7,723 (+335, one PK probe per side per
subplan loop). Planning time went from 0.48 ms to 2.2 ms per statement. Batch
size, worker count, transaction scope and lock order are unchanged.

```text
Insert on shared_projection_acceptance (actual time=0.272..8.319 rows=334.00 loops=1)
   Conflict Resolution: UPDATE
   Conflict Arbiter Indexes: shared_projection_acceptance_pkey
   Conflict Filter: ((shared_projection_acceptance.generation_id = excluded.generation_id) OR EXISTS(SubPlan 1))
   Rows Removed by Conflict Filter: 166
   Tuples Inserted: 0
   Conflicting Tuples: 500
   Buffers: shared hit=7723 dirtied=4 written=4
   ->  Values Scan on "*VALUES*" (actual time=0.003..0.345 rows=500.00 loops=1)
   SubPlan 1
     ->  Nested Loop (actual time=0.003..0.003 rows=0.50 loops=333)
           Join Filter: (ROW(incoming.observed_at, incoming.generation_id) > ROW(stored.observed_at, stored.generation_id))
           Rows Removed by Join Filter: 0
           Buffers: shared hit=1998
           ->  Index Scan using scope_generations_pkey on scope_generations incoming (actual time=0.002..0.002 rows=1.00 loops=333)
                 Index Cond: (generation_id = excluded.generation_id)
                 Index Searches: 333
                 Buffers: shared hit=999
           ->  Index Scan using scope_generations_pkey on scope_generations stored (actual time=0.001..0.001 rows=1.00 loops=333)
                 Index Cond: (generation_id = shared_projection_acceptance.generation_id)
                 Index Searches: 333
                 Buffers: shared hit=999
 Planning:
   Buffers: shared hit=282
 Planning Time: 2.219 ms
 Trigger for constraint shared_projection_acceptance_generation_id_fkey: time=1.384 calls=167
```

Observability Evidence: a new counter,
`eshu_dp_shared_acceptance_stale_writes_total{domain}`. It is labelled only by
the bounded reducer domain set and is recorded by `SharedIntentAcceptanceWriter`
from the keys missing in `RETURNING`. Each write call that skips at least one
row emits one WARN line, `shared acceptance stale write skipped; stored
generation is newer`, carrying `acceptance.scope_id`, `acceptance.unit_id`,
`acceptance.source_run_id`, `acceptance.generation_id` (for the first skipped
key), `acceptance.stale_count` and `pipeline_phase=shared`. The existing
`eshu_dp_shared_acceptance_upserts_total` and
`eshu_dp_shared_acceptance_upsert_duration_seconds` are unchanged.
