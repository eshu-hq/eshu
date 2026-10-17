# #7427 PR3: drop the terms generation foreign key

Refs #7427. Migration 191 drops the `generation_id` foreign key of
`eshu_search_index_terms` (a hash-partitioned parent of 64 leaves) to
`scope_generations`. The `scope_id` keys and the documents and stats
generation keys stay. This is option B' of the arbiter ruling: PR1 pre-deletes
a generation's terms and documents before the prune
([7427-search-index-predelete.md](7427-search-index-predelete.md)), PR2 makes
the freshness check fail closed and sweeps orphans
([7427-search-index-orphan-guard.md](7427-search-index-orphan-guard.md)), and
PR3 removes the terms statement that the cascade ran under the retention scope
lock.

## Summary

| Item | Before (PR2 head) | After (PR3) |
| --- | --- | --- |
| Terms statement under the scope lock | one cascade `DELETE ... WHERE $1 = generation_id` per pruned generation, a walk of the whole hash leaf | none |
| Locked `DELETE FROM scope_generations`, terms pre-deleted, 105-page leaves | terms trigger 4.3-5.6 ms warm (13.6 ms cold), statement 4.8-6.1 ms | no terms trigger, statement 0.26-0.34 ms |
| Late terms write for a pruned generation | rejected, SQLSTATE 23503 | stored; read-invisible; census lists it, sweep deletes it |
| Migration lock hold | n/a | 8.2-16 ms (two measurement windows, below), ACCESS EXCLUSIVE on `scope_generations`, the parent and 64 leaves |

## Root cause

Root-Cause Evidence: the cascade statement has no `scope_id` predicate, so it
cannot prune to one leaf. After the pre-delete it finds no row and still walks
the whole leaf (cost-B): 7,671 buffers on PR1's fixture, 99,998 on the
investigator's 10M-row leaf (352-467 ms). The composite-key alternative was
disproven in PR1. Only removing the statement removes cost-B.

## The constraint and its lock set

The key has two names in the field, on the parent and on all 64 leaves:

- `eshu_search_index_terms_generation_id_fkey`: fresh install (003b).
- `eshu_search_index_terms_shadow_generation_id_fkey`: an install that went
  through the 039a cutover. Proven by replaying 039a from an unpartitioned
  table on a scratch database: the parent and 64 inherited leaf keys carry the
  shadow name.

The migration finds the key in `pg_constraint` (`contype = 'f'`,
`conparentid = 0`, `confrelid` = `scope_generations`, a `generation_id`
column, `conrelid` = the parent or one of its `pg_inherits` leaves), never by
name, leaf keys first and the parent's last. Dropping the parent's key drops
the 64 inherited ones; a key a leaf carries on its own is dropped too.

Lock set of `ALTER TABLE eshu_search_index_terms DROP CONSTRAINT ...` on
PostgreSQL 18.6 (`BEGIN; ALTER; SELECT FROM pg_locks; ROLLBACK`): ACCESS
EXCLUSIVE on the parent (1), on each leaf (64) and on `scope_generations` (1).
No index lock. While a session holds `scope_generations`, `pg_locks` shows the
plain ALTER granted on the parent and the 64 leaves and waiting on
`scope_generations`.

## Why the migration is a lock-ordered DO block

The plain ALTER holds the parent and the leaves while it waits for
`scope_generations`. The retention prune holds `scope_generations` and then
deletes terms (the cascade, and the pre-delete batch whose eligibility CTE
reads `scope_generations` first). PostgreSQL 18.6 reported
`deadlock detected (SQLSTATE 40P01)`; the repro is
[7427-drop-terms-generation-fk-deadlock.sql](7427-drop-terms-generation-fk-deadlock.sql).
The migrator retries only 55P03. While it waits, the plain form also blocks
every terms reader and writer for up to the 5 s `lock_timeout`.

The shipped form follows #7420 PR2's rule: wait only for the busiest table and
only while holding nothing; take every other table with `NOWAIT`.

1. Find the key in `pg_constraint`. If there is none, do nothing: no table
   lock (the local supervisor re-applies every definition at each start).
2. `LOCK TABLE scope_generations IN ACCESS EXCLUSIVE MODE`: waits, bounded by
   the runner's 5 s `lock_timeout`, holding nothing.
3. `LOCK TABLE eshu_search_index_terms IN ACCESS EXCLUSIVE MODE NOWAIT`: locks
   the parent and its 64 leaves, never waits, so it cannot close a cycle.
4. `EXECUTE format('ALTER TABLE %s DROP CONSTRAINT IF EXISTS %I', ...)`.

`scope_generations` is the table to wait for: fact inserts (their key check),
the freshness check, the census and retention all touch it, so a `NOWAIT` on
it would fail on almost every attempt. Terms statements are short.
`migration_lock_shape.py check` exits 0 (DDL only); a seeded `LOCK` plus
`DELETE` exits 1.

## Measured lock-wait timeline

Scratch database `t7427c`, production schema, 200 scopes x 4 generations x
1,000 terms (800,000 rows, about 105 pages per leaf), PostgreSQL 18.6, shared
host. Psql sessions, shipped block:

| Other session | Migration result |
| --- | --- |
| prune order (row lock on the generation, 2 s, then the cascade delete) | waited holding nothing; dropped the key 7 ms after the prune ended; the prune's cascade ran |
| writer order (terms insert in an open transaction, 2 s) | waited holding nothing; dropped the key 10 ms after the writer ended |
| a transaction holding only terms (open read) | 55P03 at once (`could not obtain lock on relation "eshu_search_index_terms"`) |

Performance Evidence: exclusive-lock hold time, from the
`scope_generations` grant to commit end, 7 uncontended runs: 8.171, 8.295,
9.682, 9.269, 9.397, 8.407 and 8.894 ms. The independent reviewer measured
10.6-15.6 ms of hold and 11.4-16.2 ms for the whole statement (warm), timed
from before the catalog lookup; both windows end at commit. The drop is
catalog-only, so the hold follows the leaf count, not the row count.

Per-attempt outcome beside continuous load (40 attempts, 100 ms apart): a
terms writer (one autocommit insert every 5 ms) alone, 38 dropped and 2 55P03;
the writer plus the orphan census back to back, 24 dropped and 16 55P03. No
attempt deadlocked, no writer or census statement failed, the slowest insert
was 20-29 ms. The runner retries 55P03 with backoff from 5 s to 15 s within a
3-minute budget (about 12 attempts). If attempts fail independently, as with
the short writer and census statements here, a 40% failure rate leaves about
0.4^12 that every attempt fails. That estimate holds only for uncorrelated
holders. One long holder fails every attempt it overlaps, and autovacuum on
a terms leaf is such a holder (next section).

### Autovacuum on a terms leaf

The reviewer of this change reproduced it: while an autovacuum worker runs on
any of the 64 leaves (forced onto `eshu_search_index_terms_p05`), `LOCK TABLE
eshu_search_index_terms IN ACCESS EXCLUSIVE MODE NOWAIT` fails at once with
55P03 naming that leaf. Three attempts each failed in about 4 ms and the
worker kept running. A `NOWAIT` request never waits, so it never makes
autovacuum cancel itself; only a lock request that waits does, after
`deadlock_timeout` (a plain waiting `LOCK` on that leaf cancelled the worker
in 1.000 s). A manual `VACUUM` of a leaf gives the same immediate 55P03. The
PR1 pre-delete and the writers' churn make autovacuum on terms leaves common,
so these failures are correlated: every retry inside one vacuum fails.

Follow-up option, out of scope here: wait for SHARE UPDATE EXCLUSIVE on the
terms parent (which would make a regular autovacuum cancel itself) before the
`NOWAIT` step. Consider it only if post-deploy attempt counts
(`bootstrap.postgres.migration.lock_wait` lines for 191) show autovacuum
collisions.

`TestTermsForeignKeyDropLiveBoundsConcurrentWriters` on the live test schema:

| Case | Migration | Slowest terms insert | Slowest census |
| --- | --- | --- | --- |
| no other holder | dropped on attempt 2, 28.6 ms | 36.7 ms | 55.6 ms |
| a transaction holds a `scope_generations` row lock for 1.5 s | waited 1.506 s, then 55P03 (an insert queued behind it held terms) | 1.506 s | 1.511 s |

While the block waits for `scope_generations`, every new user of that table
queues behind it: the bound is the shorter of the longest conflicting holder
and the 5 s `lock_timeout`, plus the 8-16 ms hold. Roll out when no long
transaction holds `scope_generations`.

## The locked cascade before and after

Same fixture, each generation's terms deleted first (the pre-delete's state),
`SET plan_cache_mode = force_generic_plan`, `BEGIN; EXPLAIN (ANALYZE, BUFFERS)
DELETE FROM scope_generations WHERE generation_id = ...; ROLLBACK`, three
generations:

| | terms trigger | statement |
| --- | --- | --- |
| key present | 13.638 / 4.310 / 5.554 ms | 24.557 / 4.781 / 6.074 ms |
| key dropped | not fired | 9.774 / 0.343 / 0.264 ms |

The first run of each state is cold. The trigger cost follows the leaf's
pages (PR1: 32.2 ms and 7,671 buffers on a 7,671-page leaf), so on production
leaves the saving under the scope lock is that walk per pruned generation.

## Live proof

| Test | What it proves |
| --- | --- |
| `TestTermsForeignKeyDropLiveLeavesNoGenerationForeignKey` | After bootstrap no key from the parent or any leaf references `scope_generations`; 65 `scope_id` keys, the documents keys and the stats key stay; a terms row for a missing generation is accepted. RED before 191: the 65 keys listed. |
| `TestTermsForeignKeyDropLiveHandlesBothConstraintNames` | Both names are dropped, and a leaf-only key; a re-run locks no table. RED with the plain form: the no-op re-run took ACCESS EXCLUSIVE on the parent and all leaves. |
| `TestTermsForeignKeyDropLiveIsDeadlockFree` | Prune order and writer order: the waiting migration holds 0 terms locks, the other session's terms statement proceeds and commits, the key is gone. Terms-only holder: 55P03 (`coordination.IsLockNotAvailable`) in under 2 s, all 65 keys kept, a retry drops them. RED with the plain form: `the waiting migration holds 65 terms locks, want 0`. |
| `TestTermsForeignKeyDropLiveBoundsConcurrentWriters` | The table above: no load error, every attempt succeeds or returns 55P03 with all 65 keys kept. |
| `TestTermsForeignKeyDropLiveTwoAppliersBothSucceed` | Two runs find the key, both wait behind a `scope_generations` holder, and both succeed: the second one's `DROP CONSTRAINT IF EXISTS` finds no key. Without `IF EXISTS` the second fails with 42704. The lookup lists leaf keys before the parent's, so without the `conparentid = 0` filter an inherited leaf key comes first and every drop test fails at bootstrap with `cannot drop inherited constraint`. |
| `TestTermsForeignKeyDropFailsCleanlyWhenItsLockStaysBusyLive` | Tracked migrator, 200 ms `lock_timeout`, 1.5 s budget. A terms-parent holder: `could not obtain lock on relation "eshu_search_index_terms"` after 2 attempts (1.0 s). A `scope_generations` holder: `canceling statement due to lock timeout` (1.4 s). Each time: 55P03, a `lock_wait` WARN, 65 keys kept, no relation lock left on the connection, no ledger row; after the holder ends a later run drops the key and records it. Removing `NOWAIT` fails the first case. |
| `TestGenerationRetentionPrunesWithoutTheTermsForeignKeyLive` | One generation pre-deleted earlier, one with all 10,000 terms present: the pass prunes both, no terms row stays, the census reads 0, the active generation is whole. |
| `TestGenerationRetentionLateTermsWriterLeavesASweptInvisibleOrphanLive` | After the prune a late writer adds two terms for the pruned generation. RED before 191: `violates foreign key constraint "eshu_search_index_terms_generation_id_fkey" (SQLSTATE 23503)`. After: the census lists one terms orphan, `EshuSearchIndexStore.Search` returns only the active document and nothing for the orphan-only term, the sweep deletes the 2 rows, the census reads 0. |

The PR1 and PR2 retention, drain and orphan live tests now run on the dropped
schema and pass (16 in `search/index/retention`, 7 retention and freshness
tests in `storage/postgres`). `dropGenerationForeignKeys` in the orphan tests
now asserts the terms key is already gone and drops only the documents key.
`TestOrphanCensusLiveReadsZeroWithTheForeignKeysPresent` is renamed
`TestOrphanCensusLiveReadsZeroOnACleanTree`.

## What replaces the cascade

Hermetic guard `TestNoCodePathReliesOnTheTermsGenerationForeignKey`
(`go/internal/storage/postgres/terms_foreign_key_guard_test.go`), with
its seeded RED/GREEN pair `TestTermsForeignKeyGuardCatchesPlantedViolations`:

- Every statement in production Go or the migrations that deletes or
  truncates `scope_generations` is listed. Today there is one:
  `deleteScopeGenerationsForRetentionQuery`. A planted unlisted `DELETE`, a
  `TRUNCATE` list and a migration `DELETE` fail it.
- That statement is referenced only from `pruneCandidates`, which runs only
  from `PruneSupersededGenerations`, which calls `predeleteSearchIndex`, then
  `admit` (drained generations only), then `pruneCandidates`. A reorder, a
  missing pre-delete and a second caller fail it.
- No production statement names either dropped constraint name, and the
  search document writer does not branch on 23503.

Limit: the guard reads each declaration's string literals. A statement
split across constants, or built at run time from parts that each lack the
table name, passes it. Review new `scope_generations` delete paths by hand.

Readers: `TestSearchIndexReadersJoinTheActiveGeneration` (PR2) holds every
search index reader to the active-generation join. A deleted scope still
removes its terms through the retained `scope_id` key.

A reused generation id is the one case in which orphan terms can be read.
Some collectors derive the id from content (for example a Confluence page
version or a Terraform state serial), so a pruned id can come back as a new
generation. If a late writer left terms under the old id, those rows join
the active generation again and can count toward `document_frequency` until
the new generation's search writer runs `clearInitialSearchIndexTerms`, which
deletes the generation's terms before its first page. A hit still needs a
document row of that generation.

## Operator runbook

Arbiter ruling: migration 191 ships alone, as the lock-ordered `DO` block. It
needs no operational quiesce: stopping retention or scaling the resolution
engine is not a step. The measured facts it rests on:

- Hold: 8.2-9.7 ms over 7 runs from the `scope_generations` grant to commit
  end; the reviewer measured 10.6-15.6 ms from before the catalog lookup
  (catalog-only either way).
- Deadlock-free against the prune order and the writer order: the block waits
  for `scope_generations` while it holds nothing.
- Under continuous load, 38 of 40 attempts dropped at once beside a terms
  writer and 24 of 40 beside the writer plus the census; every other attempt
  was a 55P03 that the runner retries (about 12 attempts in its 3-minute
  budget). No deadlock.
- Budget exhaustion fails cleanly
  (`TestTermsForeignKeyDropFailsCleanlyWhenItsLockStaysBusyLive`): db-migrate
  exits with `apply eshu_search_index_terms_drop_generation_fk: lock retry
  budget ... exhausted ... (SQLSTATE 55P03)`, all 65 keys stay, no lock is
  left, the ledger records nothing, and a later run applies it.

Deploy dependency: the image that carries 191 must also carry #7427 PR1 (the
search index pre-delete and the drained-only admission) and PR2 (the
fail-closed freshness check, the orphan census and the sweep). Those are what
remove a pruned generation's terms once the key is gone.

1. Deploy the image (PR1, PR2 and 191 together) and let db-migrate run. Each
   55P03 logs `bootstrap.postgres.migration.lock_wait`; a later success logs
   `bootstrap.postgres.migration.lock_recovered`. The applied migration logs
   `postgres schema migration recorded` with
   `path=go/internal/storage/postgres/migrations/191_eshu_search_index_terms_drop_generation_fk.sql`
   and `duration_ms`.
2. Post-deploy census, after the first retention poll interval: run
   [7427-drop-terms-generation-fk-postdeploy.sql](7427-drop-terms-generation-fk-postdeploy.sql)
   in psql. Check 1, the `pg_constraint` query, must return zero rows: no key
   from the terms parent or any leaf to `scope_generations`. Check 2 must
   return the ledger row for 191. Check 3 runs the shipped census walk on
   every terms leaf and prints `pairs=... orphan_pairs=...`. On the scratch
   fixture it printed `pairs=800 orphan_pairs=0`, and `orphan_pairs=2` after
   two generation rows were deleted by hand (33 ms). Also read the gauge
   `max by (table) (eshu_dp_search_index_orphan_generations)`: terms reads 0,
   or falls to 0 after the next sweep
   (`eshu_dp_search_index_orphan_rows_swept_total{table="eshu_search_index_terms"}`
   counts what it removed); documents stays 0 while its key exists.
3. Record the gauge before and after, the check 1 result and the migrator's
   timing line on #7427.

Contingency only: if db-migrate keeps failing with 55P03 beyond the retry
budget, something held a lock the drop needs for the whole run. Look first
for autovacuum on a terms leaf:

```sql
SELECT pid, now() - xact_start AS running, query
FROM pg_stat_activity
WHERE backend_type = 'autovacuum worker'
  AND query LIKE '%eshu_search_index_terms_p%';
```

A regular autovacuum worker (`autovacuum: VACUUM ...eshu_search_index_terms_pNN`)
can be cancelled with `SELECT pg_cancel_backend(<pid>);`; then rerun
db-migrate before the next worker starts on a leaf. Leave an anti-wraparound
vacuum (`... (to prevent wraparound)`) to finish, then rerun. Otherwise find
the holder in `pg_stat_activity` and `pg_locks`, rerun db-migrate when it
ends, and only then consider quieting the writers, for example by scaling the
resolution engine to zero replicas for the run. Production refuses
`ESHU_GENERATION_RETENTION_ENABLED=false` (the reducer's startup check and
the Helm validation reject it outside local profiles), so that is not a
config flip.

If a reducer without PR1 prunes after the drop, it leaves that generation's
terms; the PR2 census lists them and the sweep removes them, and readers never
see them.

## Rollback

A code-only rollback is safe to the PR2 image. To the PR1 image it works, but
a late writer behind a NULL active pointer can then leave orphans that nothing
sweeps (read-invisible, a storage leak). An image older than PR1 relies on the
cascade: run
[7427-drop-terms-generation-fk-reverse.sql](7427-drop-terms-generation-fk-reverse.sql)
first, in psql. It sets `lock_timeout = '5s'` (psql
sets none), checks for orphans, adds the key `NOT VALID` with the same lock
order (SHARE ROW EXCLUSIVE on the parent, 64 leaves and `scope_generations`),
then validates (SHARE UPDATE EXCLUSIVE on terms, ROW SHARE on
`scope_generations`). On the scratch fixture: census 64.7 ms, add 23.5 ms,
validate 88.9 ms; a rerun is a no-op. Its step 4 deletes the 191 ledger row
so a later roll-forward drops the key again.

## Signals

Observability Evidence: the migrator's `bootstrap.postgres.migration.lock_wait`
WARN and exhausted-budget error name the migration, the busy relation and
55P03. After the drop,
`eshu_dp_search_index_orphan_generations{table="eshu_search_index_terms"}` can
read above 0 between a late write and the next sweep, and
`eshu_dp_search_index_orphan_rows_swept_total{table="eshu_search_index_terms"}`
counts the cleanup (PR2). `eshu_dp_generation_retention_scope_lock_hold_seconds`
no longer includes the terms leaf walk. No-Observability-Change: no new metric,
span or log key; the documents gauge stays 0 while its key exists.

## Not checked

- The drop on a production-sized install (leaf count is the same 64; the
  hold is catalog-only, measured 8-16 ms here).
- How often autovacuum on a terms leaf collides with 191 in production.
- The reverse procedure on PostgreSQL older than 18.6.
- The contingency (quieting writers) on a real cluster; the arbiter ruled it
  is not a deploy step.
