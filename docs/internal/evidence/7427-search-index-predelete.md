# #7427 PR1: search index pre-delete before the retention prune

Refs #7427. This note records why generation retention now deletes a
superseded generation's `eshu_search_index_terms` and
`eshu_search_index_documents` rows in committed batches before its prune
transaction, what that buys, and what it does not buy yet.

## Summary

- Before: those rows went only through the `scope_generations` foreign-key
  cascade inside the prune transaction, under the generation's
  `ingestion_scopes` row lock. The terms cascade cost is cost-A (delete the
  generation's rows) plus cost-B (walk the whole hash leaf to find them).
- PR1 (this change): the pre-delete removes the rows first (cost-A moves out
  of the lock) and re-sweeps them (the dead-index-entry walk moves out of the
  lock). The prune admits only generations that drained.
- PR1 does NOT make the locked cascade fast. The foreign keys stay, so the
  terms cascade still runs `DELETE ... WHERE $1 = generation_id` on the
  partitioned parent under the lock, and that statement still walks the
  whole leaf (cost-B, O(leaf pages)) even when it finds no row. Only PR3,
  which drops the terms generation foreign key so no terms statement runs
  under the lock, removes cost-B. The composite-FK alternative was tested and
  disproven (below).

## Definitions (from the investigator's reports)

- cost-A: per-row delete work of the generation's own rows, about 1 buffer
  per deleted row (3,309 ms at 18% of the 10M-row leaf, of which about
  0.84 s was the leaf scan).
- cost-B: locating the rows with no `scope_id` predicate: a full-leaf walk,
  O(leaf pages) = 99,998 pages on the 10M-row leaf, independent of how many
  rows are deleted, and planned over all 64 leaves on the partitioned parent.

Root-Cause Evidence: the cascade the FK issues is
`DELETE FROM "public"."eshu_search_index_terms" WHERE $1 OPERATOR(pg_catalog.=) "generation_id"`
(the auto_explain `Query Text` of every cascade measured below). It has no
`scope_id` predicate, so it cannot prune to one leaf and it cannot use the
leaf primary key `(scope_id, generation_id, term_key, document_id)` as an
equality prefix. Its plan depends on leaf statistics (Seq Scan, lossy Bitmap,
or a PK skip scan over every dead entry), and every variant visits about the
whole leaf.

## Investigator files (DB p7427, PostgreSQL 18.6; numbers verbatim)

Fixture: one 10M-row terms leaf (`eshu_search_index_terms_p00`, 99,998
pages); target generations g5, g10, g18 hold 5%, 10% and 18% of it
(500,000 / 1,000,000 / 1,800,000 terms; 50,000 / 100,000 / 180,000
documents). The investigator's raw outputs were not retained; the
figures below are copied from the investigator's reports, by case.

### First theory: the current single-column FK on `generation_id`

| Case | State | Terms cascade plan | Figures |
| --- | --- | --- | --- |
| stale stats | never-analyzed leaf | Seq Scan at every share (5/10/18%) | g18: 1,715 ms |
| analyzed | after ANALYZE | Bitmap via PK skip scan at 5% (Index Searches 21), Seq Scan at 10% and 18% | g18: 3,293 ms |
| pre-deleted, stale stats | after the 20k ctid pre-delete, stats from before it (the realistic state) | Seq Scan, Rows Removed 8,200,000 | 99,998 buffers, 467 ms at 18% |
| pre-deleted, re-analyzed | after the pre-delete, fresh ANALYZE without VACUUM | PK skip scan visiting every dead index entry | 1,818,749 hits + 17,775 reads, 631 ms |
| vacuumed | after VACUUM, stats still stale | Seq Scan even for g5 | 99,998 buffers, 352-356 ms |
| vacuumed and analyzed | after VACUUM then ANALYZE | Index Scan | 4.1 ms |
| leaf index on generation_id | rejected "leaf index on (generation_id)" theory | fair test (index first, then pre-delete): lossy bitmap over 99,998-100,429 pages | 415-424 ms |

The first leaf-index attempt built the index after the pre-delete, so it
held no dead entries; that result is optimistic and is not used.

### Second theory: composite FK `(scope_id, generation_id)` (DISPROVEN)

The old FK dropped, a composite FK to
`scope_generations (scope_id, generation_id)` added, so the cascade carries
`scope_id`.

| Case | State | Plan | Figures |
| --- | --- | --- | --- |
| composite (a) | stats without targets, rows present, no pre-delete | plain PK Index Scan, about 2 buffers/row | 1.0M / 2.0M / 3.6M buffers; 542 / 1,061 / 2,101 ms at 5/10/18% |
| composite (b) | fresh ANALYZE, rows present, no pre-delete | lossy Bitmap at 5% (602,486 buffers), Bitmap at 10% (1,108,354), Seq Scan of the leaf at 18% (1,900,780) | about 5.3 s wall under host load |
| composite (c) | after the 20k ctid pre-delete, stats from (b) | Bitmap 102,486 buffers 341 ms (5%), Bitmap 108,354 / 433 ms (10%), Seq Scan 100,779 / 432 ms (18%) | forced generic plan at g18: Bitmap over 1,800,002 dead TIDs, 103,139 buffers, 361 ms |
| composite (d) | after the pre-delete THEN VACUUM and ANALYZE of the leaf | | 4-6 buffers, 0.5-1.8 ms |
| composite, ANY form | three-generation `WHERE generation_id = ANY(...)` form, without / with the pre-delete | | g5 vacuumed empty 4 buffers; g10 intact 1,094,933; g18 pre-deleted with stale stats 100,780 |
| composite, insert tax | insert write tax of the composite FK, 20k-row batches | | +5% FK trigger time, no WAL increase |

Why it is disproven: with rows present the composite FK still deletes the
whole generation under the lock (cost-A, composite (a), composite (b)); after the
pre-delete it still walks about the whole leaf's pages at every share
(composite (c)). Only composite (d) is cheap, and it needs a VACUUM plus ANALYZE that
retention cannot schedule (that VACUUM took 152 s under load). composite (d) is not
the PR3 shape. The PR3 shape is "terms generation FK dropped, no terms
statement runs under the lock at all", which was never measured as such
because it removes the statement; its safety net is this PR's `drained=true`
admission plus the PR2 census sweeper.

### The batch statement (the shape PR1 ships)

Performance Evidence: the ctid-array keyset batch
`DELETE ... WHERE t.scope_id=$1 AND t.generation_id=$2 AND t.ctid = ANY(ARRAY(SELECT ctid ... ORDER BY term_key, document_id LIMIT $5))`
at 20,000 rows measured 1.12 us/row, 3.0 buffers/row, 23 ms, 1.08 MB WAL and
FPI 0 per batch (investigator's report; raw output not retained), with Index Scan on
the leaf PK, Index Searches 1, and runtime pruning Subplans Removed 63 under
a generic plan; the first and a late batch cost the same. A
`WHERE scope_id=$1 AND generation_id=$2` delete without the ctid page was
planned by the same planner and scanned the leaf heap, which is why the
re-sweep never runs under the lock.

## This change's own measurements

Host: the shared scratch PostgreSQL 18.6 (127.0.0.1:25432, aarch64), other
agents running; buffers and plan shape are the primary evidence, wall times
are indicative. Database `t7427`, production schema.

### Plan of the shipped statement (generic plan)

The shipped terms statement adds the in-statement eligibility CTE (status
superseded, superseded_at set, not the scope's active pointer, no claimed,
running or retrying work item). Fixture: one leaf with a 400,000-row
superseded generation, a 50,000-row pending generation and a 335,000-row
active generation (785,000 rows). `SET plan_cache_mode = force_generic_plan`,
`EXPLAIN (ANALYZE, BUFFERS, WAL)`:

| Batch | Page scan | Delete | Total buffers | WAL |
| --- | --- | --- | --- | --- |
| first (cursor `('', '')`) | Index Scan using `eshu_search_index_terms_p00_pkey`, Index Searches 1, Subplans Removed 63, 20,176 hits | Tid Scan on `_p00`, Subplans Removed 63, 60,000 hits | 80,179 | 20,000 records, 1,080,000 bytes |
| late (cursor at offset 300,000) | same plan, 20,177 hits | same | 80,180 | 20,000 records, 1,080,000 bytes |
| pending generation passed as a candidate | One-Time Filter on the eligibility InitPlan; page and delete return 0 rows | | 3 (eligibility read) | none |

Re-measured after the live-work probe dropped its `scope_id` term (same
fixture, freshly re-seeded, so the first touch after a checkpoint wrote
full-page images): first batch 80,688 buffers, 20,000 WAL records with 510
FPI, 5,234,498 bytes, 40.6 ms; late batch 80,660 buffers, 480 FPI,
4,988,152 bytes, 40.4 ms; same plan (Index Scan on the leaf PK, Index
Searches 1, Subplans Removed 63).

No-Regression Evidence: first and late batches cost the same (80,179 vs
80,180 buffers; 80,688 vs 80,660 after the predicate change), so the walk does not degrade as the cursor advances; the
eligibility CTE adds 3 buffers per batch.

### Locked cascade before and after (same fixture, forced generic plan)

`BEGIN; EXPLAIN (ANALYZE, BUFFERS) DELETE FROM scope_generations WHERE generation_id = 'x-old'; ROLLBACK;`
with auto_explain on nested statements. "Before" is main's behavior (no
pre-delete). "After" ran the shipped batch statements in committed
transactions first (20 full terms batches, one short page, one from-start
re-sweep; documents likewise).

| | terms FK trigger | terms cascade buffers | documents FK trigger | documents buffers | statement total |
| --- | --- | --- | --- | --- | --- |
| before | 219.279 ms | 407,671 (400,000 rows deleted + 7,671 leaf pages) | 19.481 ms | 41,119 | 249.341 ms |
| after | 32.248 ms | 7,671 (Seq Scan of `_p00`, 0 rows, Rows Removed 385,000) | 2.948 ms | 1,119 | 43.650 ms |

After the pre-delete the terms cascade still reads every page of the leaf
(7,671 = `pg_relation_size('eshu_search_index_terms_p00') / 8192`): that is
cost-B, and it remains until PR3. The active generation kept all 335,000
terms rows.

Pre-delete batches outside the lock: terms 22 batches (400,000 rows, 678 ms
total, p50 28 ms, the from-start re-sweep 87.5 ms because it walks the
400,000 dead index entries once); documents 4 batches (40,000 rows, 82 ms).

### Behavior proof

- RED on `origin/main` (ef5703ec2):
  `TestGenerationRetentionPredeletesSearchIndexBeforeTheScopeLockLive`
  failed with `terms rows of the pruned generation at the targeted re-lock = 45000, want 0`.
  GREEN on this branch: 0 rows at the re-lock, `RowsPruned` reports 45,000
  terms and 4,500 documents.
- Mutation checks, each failing its test: dropping the status / active
  pointer predicates (c4), dropping the live-work exclusion (c5), dropping the
  generation predicate from the page and delete (c7, 54,000 terms deleted
  instead of 45,000), ending the drain on a short page without the re-sweep
  (c6, 30,000 deleted instead of 30,001), and skipping the advisory lock (D4).

### Cost of the second candidate read

The pre-delete reads its candidates with the shipped selection (weaker row
locks) before the prune transaction runs the selection again, so each pass
now runs that query twice. Measured by the independent reviewer of this
change (PREPARE plus EXPLAIN ANALYZE, three runs each) on 5,000 scopes x 31
generations = 155,000 generations with 155,000 work items: the shipped
selection took 1,134-1,152 ms and 3,205,077 buffers; the pre-delete's
candidate read took 1,134-1,146 ms and 3,205,077 buffers. At that scale PR1
adds about 1.1 s of query time to every pass. It runs before the prune
transaction opens and holds only its own short-lived FOR NO KEY UPDATE row
locks, which fact inserts do not wait on, so it adds nothing to
`eshu_dp_generation_retention_scope_lock_hold_seconds`; it is pure extra work
per pass.

### Live-work probe in each batch

Each batch re-checks live work with the selection's own predicate,
`work.generation_id = $2 AND work.status IN ('claimed', 'running', 'retrying')`,
with no `scope_id` term: `fact_work_items` has no `(scope_id, generation_id)`
foreign key, so a work row's `scope_id` is not guaranteed to equal its
generation's. Plan on this change's scratch database with 5,000 scopes x 31
generations and 155,000 work items (forced generic plan): Index Scan using
`fact_work_items_status_idx`, Index Searches 1, 625 buffers, 0.64 ms; its
cost follows the live work rows, not the table.

### Accounting

- Events: the recount under the lock runs after the pre-delete, so it sees
  the drained tables empty. Each pruned generation's retention event adds the
  rows its pre-delete removed, so per pruned generation the event and
  `eshu_dp_generation_retention_rows_pruned_total` agree. RED before the fix
  (live test, one generation with 45,000 terms):
  `event row_counts[eshu_search_index_terms] = 0, RowsPruned[eshu_search_index_terms] = 45000`.
  Rows pre-deleted for a generation the pass did not prune appear only on the
  counter (documented in `metrics-reducer-storage.md`).
- Duration: `eshu_dp_generation_retention_duration_seconds` stays the prune
  transaction; the pre-delete reports through its own phases and the batches
  histogram (`TestGenerationRetentionDurationExcludesThePredelete`).

## Concurrency

- Conflict domain: one generation's search index rows. Every batch and the
  prune transaction take `pg_try_advisory_xact_lock(7427, hashtext(generation_id))`
  and never wait for it, so a batch (primary-key order) and the FK cascade
  (physical order) never delete one generation's rows at once. In the
  investigator's interleaving (one session holds batch-deleted rows of g5 in
  an open transaction while another deletes the g5 generation row) the
  cascade took 4,369.618 ms; the advisory lock makes that overlap a skip
  instead. Passes stay concurrent across generations.
- A batch sets `lock_timeout` 500 ms (below `deadlock_timeout` 1 s) and
  yields on 55P03 or 40P01; the generation stays undrained for that pass.
- The candidate read is the shipped selection with `FOR NO KEY UPDATE ...
  SKIP LOCKED` in its own transaction, rolled back before the first batch: no
  retention row lock spans a batch, a scope held `FOR UPDATE` by another pass
  or an Ack is skipped as the prune will skip it, and a fact insert's FK
  `KEY SHARE` neither waits on nor skips the read.
- Live proofs: two concurrent drains of one generation delete its 33,000
  rows exactly once with no 40P01; a held advisory lock makes a drain skip
  with zero rows deleted; a drain cancelled after one batch converges on
  rerun; a row inserted behind the cursor is removed by the re-sweep.
- `TestGenerationRetentionChunkConcurrentInsertWaitsLive` (#7981) flaked on
  `origin/main` (3 of 8 runs: the selection skips the scope while an insert
  holds its FK `KEY SHARE`); the pre-delete shifted the timing (6 of 8). The
  test now retries passes that skipped the scope; its assertions are
  unchanged (8 of 8 pass).

## Observability

Observability Evidence: phases `predelete_search_index` and
`sweep_search_index` on `eshu_dp_generation_retention_phase_duration_seconds`;
rows on `eshu_dp_generation_retention_rows_pruned_total{table="eshu_search_index_terms"|"eshu_search_index_documents"}`;
skip reasons `search_index_undrained` and `advisory_lock_busy` on
`eshu_dp_generation_retention_skipped_total`; batches per pass on
`eshu_dp_generation_retention_search_index_batches`; the per-generation log
`generation retention search index pre-delete completed` (scope_id,
generation_id, outcome, drained, batches, rows per table, duration); the
cycle log field `search_index_batches`. At 3 AM:
`eshu_dp_generation_retention_scope_lock_hold_seconds` still includes the
terms cascade's leaf walk until PR3; the skip counter's reason says why a
generation waited.

## Not checked

- PR3's shape (no terms statement under the lock): not measurable in PR1.
