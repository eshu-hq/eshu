# #7427 PR2: fail-closed freshness and the search index orphan sweep

Refs #7427. PR3 drops the `eshu_search_index_terms` generation foreign key,
so the locked retention cascade no longer walks the whole terms leaf (see
[7427-search-index-predelete.md](7427-search-index-predelete.md)). After that
drop nothing in the database rejects a terms row for a generation that no
longer exists. This change closes the two ways such an orphan row could stay:

- c3: a writer whose generation was pruned now abandons (the freshness check
  fails closed).
- c8: a census finds orphan `(scope_id, generation_id)` pairs and a bounded
  sweep deletes their rows, once per retention poll interval.

PR2 changes no DDL. The foreign keys stay, so on a production schema the
census reads 0. The orphan tests drop the keys only inside their isolated
test schema.

## c3: the freshness check failed open

Root-Cause Evidence: `NewGenerationFreshnessCheck`
(`go/internal/storage/postgres/generation_freshness.go`) returned
`(true, nil)` when the scope's `active_generation_id` was NULL and the
intent's generation row was missing ("Preserve the legacy result for every
other no-active state"). A NULL pointer is a real state: the projector's
fail path sets it to NULL when the active generation fails, and retention
can then prune that scope's superseded generations. A late search-document
writer for a pruned generation was told "current" and wrote rows that only
the terms generation foreign key rejected. The branch with a non-NULL
pointer already returned `(false, nil)` for a missing row.

RED, unit (`TestGenerationFreshnessCheck`, before the fix):

```text
generation_freshness_test.go:184: current = true, want false
```

RED, live against the migrated schema
(`TestGenerationFreshnessFailsClosedForAPrunedGenerationLive`, base
`generation_freshness.go`):

```text
generation_retention_search_index_live_test.go:279: pruned generation behind a NULL pointer: check(scope-7427c3-023544.934211733-pruned) = (true, <nil>), want current=false not-yet-active=false
```

GREEN: both pass. The fix returns `(false, nil)` when the intent row is
missing, whatever the pointer. These results do not change:

| State | Result |
| --- | --- |
| intent is the active generation | `(true, nil)` |
| NULL pointer, intent pending (first activation) | `GenerationNotYetActiveError` (retry) |
| newer pending intent behind an active pointer | `GenerationNotYetActiveError` |
| NULL pointer, intent `active` or `completed` row | `(true, nil)` (legacy) |
| failed or superseded intent | `(false, nil)` |
| unknown scope | `(true, nil)` (kept) |

The unknown-scope result stays because the search index `scope_id` foreign
keys stay: `TestOrphanLiveUnknownScopeWriteIsRejected` drops the generation
foreign keys and shows that a terms or documents insert for an unknown scope
still fails with 23503 on `*_scope_id_fkey`.

Consumers of the check (`rg NewGenerationFreshnessCheck`): the reducer
runtime guard (`cmd/reducer/main.go`, every domain), the search document
handler, container image identity, cloud inventory admission, and the repo
dependency replay freshness. Each now treats a pruned generation behind a
NULL pointer as superseded, as it already did behind a non-NULL pointer. The
live consumer tests pass unchanged on this branch:
`TestReducerContentionGatePreActivationGenerationRunsAfterActivation`,
`TestReducerContentionGateOlderGenerationSupersessionStaysTerminal`,
`TestEshuSearchDocumentHandlerAbandonsSupersededGenerationLive`, the six
container image identity first-generation and epoch-barrier tests, and
`TestSearchIndexReadersJoinTheActiveGeneration`. This settles the
"Known scope, NULL active, missing intent" row that
[7916-first-generation-readiness.md](7916-first-generation-readiness.md) left
NOT_CHECKED.

## c8: the orphan census

Fixture: database `t7427b_census` on the shared scratch PostgreSQL 18.6
(127.0.0.1:25432), production schema. 1,000 scopes x 31 generations with 50
terms and 5 documents each, plus a 10,015,500-row terms leaf
(`eshu_search_index_terms_p00`: 812 MB heap, 916 MB primary key) holding 20
active generations of 335,000 rows and three superseded targets. 11,550,000
terms and 1,155,000 documents in all. Then the target-g18 and target-g10
rows (2,800,000 terms) were deleted without VACUUM, as a pre-delete leaves
them, and in this scratch database only the generation foreign keys were
dropped and 40 generation rows deleted to make 40 orphans. 31,021 pairs per
table. Buffers and plan shape are the primary evidence; wall times are
indicative on a shared host.

The DISTINCT form named in the ruling,
`SELECT DISTINCT scope_id, generation_id FROM eshu_search_index_terms_p00 EXCEPT SELECT scope_id, generation_id FROM scope_generations`,
is not bounded: Seq Scan of the leaf, 10,015,500 rows, 104,479 buffers
(shared hit 3, read 104,476), 26,102 ms. PostgreSQL 18 skip scan
does not apply to DISTINCT.

Performance Evidence: the shipped census is a loose index scan per relation:
a recursive CTE whose step seeks the first primary-key entry after the last
pair (`(scope_id, generation_id) > (...) ORDER BY scope_id, generation_id
LIMIT 1`), then an anti-join on `scope_generations (scope_id,
generation_id)`. Plan: Index Only Scan on the leaf primary key, one Index
Search per pair; anti-join Index Scan on `scope_generations_pkey`. Measured
with the shipped statement text (PREPARE, `EXPLAIN (ANALYZE, BUFFERS)`):

| Run | Relations | Pairs walked | Orphans | Buffers | Execution |
| --- | --- | --- | --- | --- | --- |
| 1, after the deletes | 64 leaves + documents | 62,042 | 80 (40 per table) | hit 475,672, read 10,873 | 3,602.8 ms |
| 2, warm | same | 62,042 | 80 | hit 455,971 | 877.1 ms |
| 3, after VACUUM | same | 62,042 | 80 | hit 365,199 | 825.8 ms |

On the big leaf alone (331 pairs), the shipped census statement read 33,141
buffers in 44 ms with 2,800,000 dead index entries whose kill bits were set,
and 2,443 buffers in 6.1 ms after VACUUM. The scratch loose-scan query (the
same recursive walk and anti-join, without the orphan listing) read 1,787
buffers in its walk before the deletes, and its first walk over the unkilled
dead entries cost 2,800,330 heap fetches, 2,833,138 buffers and 7,060 ms,
once (its walk read 32,145 buffers on the next run); in production the pre-delete's from-start re-sweep walks
those entries first. The census cost follows pairs (about 6 to 8 buffers
per pair, half of it the anti-join probe) plus dead index entries until
VACUUM, not leaf rows. It runs at most once per retention poll interval
(default one hour).

## c8: the orphan sweep

The sweep reuses the pre-delete's batch loop and statement body (keyset
page plus ctid DELETE, 20,000 rows, per-generation advisory lock class 7427,
500 ms `lock_timeout`, from-start re-sweep, row cap and deadline). Only the
eligibility CTE differs: the pair is absent from `scope_generations` and the
scope's active pointer does not name it, re-checked in each batch's
snapshot.

No-Regression Evidence: first terms batch of a 500,000-row orphan generation
on the 10M-row leaf, forced generic plan: Index Scan using
`eshu_search_index_terms_p00_pkey`, Index Searches 1, Subplans Removed 63,
Tid Scan delete; 84,461 buffers (hit 79,997, read 4,464), 20,001 WAL
records. A late batch (cursor `tk-5000`): 84,439 buffers. PR1's pre-delete
batch measured 80,179 to 80,688. The eligibility check is 4 to 6 buffers
(Index Only Scan on `scope_generations_scope_generation_idx`, Index Scan on
`ingestion_scopes_active_generation_idx`). An existing generation passed as
an orphan: One-Time Filter false, 4 buffers, 0.094 ms, no row touched. Wall
times (5.4 s, 3.9 s) were dominated by cold reads on the shared host.

Behavior proofs (live, `search/index/retention/orphan_live_test.go`):

- Clean tree with the foreign keys present: census 0 for both tables.
- Only the missing generation loses rows (25,000 terms, 2,500 documents);
  active, pending and superseded generations keep all rows; a second sweep
  finds 0 orphans and runs no batch.
- An existing generation in any status, and a missing generation that the
  active pointer still names, is ineligible with 0 rows deleted.
- Two concurrent sweeps delete the 33,000 rows once, without 40P01.
- A row a late writer inserts behind the cursor is removed by the re-sweep
  (30,001 deleted). A row inserted after a sweep is found and swept by the
  next sweep.

Mutation checks, each failing a test: dropping the `scope_generations` test
from the eligibility CTE (`SweepOrphan(gen-old-...) = {... Outcome:drained
... TermsDeleted:1500 ...}, want ineligible`), dropping the active-pointer
test (`SweepOrphan(dangling active) = {... Outcome:drained ...}, want
ineligible`), and disabling the census anti-join (`census
eshu_search_index_terms = 3 orphans of 3 pairs, want 0 of 3`).

## Concurrency

- Conflict domain: one generation id's search index rows. The sweep runs in
  `Runner.RunOnce` after `PruneSupersededGenerations` returns, so it never
  holds or waits on a retention scope lock. Its batches take
  `pg_try_advisory_xact_lock(7427, hashtext(generation_id))` like the
  pre-delete and the prune, so a sweep, a pre-delete and a prune never
  delete one generation's rows at once, and a second sweeper skips.
- A late writer that inserts while the sweep runs either loses the row to
  the re-sweep or the next census, or (c3) is told its generation is
  superseded and abandons. A generation row written between the census and a
  batch makes that batch ineligible. A row the writer updated after the
  batch's snapshot keeps its new ctid, which the ctid DELETE does not match.
- Cadence: at most once per poll interval, so a draining backlog that runs
  passes back to back does not repeat the census
  (`TestGenerationRetentionRunnerSweepsOrphansOncePerPollInterval`).

## Observability

Observability Evidence: `eshu_dp_search_index_orphan_generations{table}`
(gauge, 0 on a clean tree), `eshu_dp_search_index_orphan_rows_swept_total{table}`,
phases `census_search_index_orphans` and `sweep_search_index_orphans` on
`eshu_dp_generation_retention_phase_duration_seconds`, failure reason
`search_index_orphan_sweep` on `eshu_dp_generation_retention_failures_total`,
logs `generation retention search index orphan sweep completed`
(pairs_scanned, orphan_generations, rows_swept, batches,
unswept_generations, census and sweep seconds), per generation
`generation retention search index orphan generation swept`, and ERROR
`generation retention search index orphan sweep failed`
(`failure_class=generation_retention_search_index_orphan_sweep_error`).
`TestGenerationRetentionRunnerRecordsOrphanSweep` pins them. When the census
succeeds and a sweep batch then fails, the gauge still takes the census
value and the committed batches' rows stay counted; a failed census leaves
the gauge at its last value
(`TestGenerationRetentionRunnerRecordsTheCensusWhenTheSweepFails`, RED before
the fix: `expected: 3 actual: 0`). At 3 AM: a gauge
above 0 on two consecutive hours means the sweep cannot drain; the log's
`unswept_generations` and the per-generation outcome say why. One named
cause is a dangling active pointer: the generation row is missing but
`ingestion_scopes.active_generation_id` still names it, so readers can see
those rows and the sweep leaves them; the per-generation line reads
`outcome=ineligible`. The census runs once per poll interval per reducer
replica.

## Not checked

- PR3's schema (terms generation foreign key dropped) on a production
  install: simulated only inside isolated test schemas and the scratch
  census database.
- Census cost at more than 31,021 pairs per table, and with autovacuum on.
- One orphan generation id listed under two scopes in one census is swept
  for the first scope only; the second waits for the next sweep.
