# 7230 Fingerprint Reap Plan Cliff

The content writer's repo-wide fingerprint reaps
(`reapStaleFingerprintSQL` and `reapStaleFingerprintBandSQL`) were
`NOT EXISTS` anti-joins against `content_entities`. Bootstrap grows those
tables from empty, so a repository is often reaped while the last `ANALYZE`
saw at most 100 repositories and not this one. The planner then estimates one
row on both sides and runs a Nested Loop Anti Join that rescans the
repository's entities once per side-table row. #7230 has the host symptom:
230–1,735 s per repository in `reap_stale_fingerprints`.

This note records the proof on PostgreSQL 16 (the reference host's version),
the rejected alternatives, the fix, and its before/after cost.

Source binding: "before" is `origin/main` `f55447c459`. "After" is branch
`perf/7230-reap-plan-cliff`.

## Shim

- Throwaway `postgres:16` container, PostgreSQL 16.15, `shared_buffers=1GB`,
  default `work_mem=4MB` and `hash_mem_multiplier=2`, autovacuum off so
  `ANALYZE` alone controls the statistics.
- The schema mirrors migrations 004, 035, 077, 104, 107 and 111: every
  `content_entities` btree the planner could pick, plus the fingerprint side
  tables. Entity ids use the real 29-byte `content-entity:e_<12 hex>` shape.
- The 1× repository has the issue's sizes: 45,808 entities, 4,995 fingerprinted
  functions, and 159,840 band rows. The 5× repository has 229,040 entities,
  24,975 functions, and 799,200 band rows.
- The stale database has 10 small repositories analyzed, then 1× and 5×
  repositories loaded unseen. The estimate for both is `rows=1`.
- The fresh database adds a 200-repository corpus and runs `ANALYZE`:
  2,961,056 entities, 328,204 fingerprint rows, 10,502,528 band rows, and 213
  repositories.
- Statements are `PREPARE`d with `$1` and run with `plan_cache_mode` forced to
  custom (what pgx gets first) or generic (what pgx may settle on after five
  executions). DELETEs run in `BEGIN`/`ROLLBACK`.
- "Churn" deletes every tenth function entity inside the transaction: 499
  stale ids at 1× and 2,497 at 5×.
- The laptop ran other jobs, at load averages 38–51, so loop counts, rows
  removed and buffers are the stable comparison. The before/after table is the
  median of five interleaved rounds that alternate which path runs first.

## Root cause reproduced

With the 1× repository unseen:

- The fingerprint reap ran as a Nested Loop Anti Join. Its inner Index Scan on
  `content_entities_repo_idx` estimated 1 row, ran with loops = 4,995, and
  removed 112,292,595 rows in the join filter. That took 4,986,040 shared hits
  and 23,326 ms.
- The band reap had the same plan, with the inner scan looping once per band
  row. It was cancelled at a 600 s `statement_timeout`.
- In the 5× stale state, the fingerprint reap alone exceeded a 300 s timeout.

The Go regression `TestContentWriterFingerprintReapScansOnceUnderStaleStatisticsLive`
failed on `f55447c459` for that reason: `Index Scan on "content_entities" loops=222`
for the fingerprint reap and `loops=7104` for the band reap.

## Alternatives rejected

- **`NOT IN (SELECT ce.entity_id ...)`**: 189 ms under stale statistics, but it
  moves the cliff to over-estimates. A hashed SubPlan is only planned when the
  estimated entity set fits `hash_mem`, and it never spills.
  - With the 1× repository analyzed and the table then grown five-fold, the
    estimate was 173,528 entities (9.7 MB, over 8 MB). The planner used an
    unhashed SubPlan over a Materialize with loops = 159,840: **230,999 ms**,
    against 188 ms for the old anti-join in the same state.
  - Under stale statistics at 5×, the hashed set measured 16 MB.
- **Skip both reaps on a repository's first generation**: `content.Materialization`
  carries no prior-generation signal.
  - A zero-rows probe would have to run before `upsertFingerprintBatches`,
    which writes the side rows first.
  - It would fix only first generations. A later generation reaped inside the
    window keeps the cliff.
  - The fix below already makes first generations two empty reads.
- **Reap only the ids Write already holds**: path deletes and purges remove
  entities by path, so the ids are not in hand. Write is also not one
  transaction.
  - The repo-wide reap is what converges orphans left by a failure between
    statements, or by other deleters of `content_entities`.
  - Narrowing it would change behavior.
- **Correlate the inner query on `band.repo_id`**: the planner re-derives
  `ce.repo_id = $1` from the outer-join clause, which gives the same Nested Loop
  plan.
- **Stale set inside one statement (`COALESCE(id IN (side EXCEPT live), FALSE)`)**:
  correct and stable, but it reads each side table twice on every call.
  - Without the COALESCE fence, the `IN` is pulled up into a Nested Loop that
    rescans the set operation per band row.
- **One `fp UNION ALL band EXCEPT live` read**: `UNION ALL` hides the column
  statistics, so fresh statistics sort every band row as its own group.
  - That cost 369 ms / 2,032 ms against 275 / 1,701 ms before.
- **Forcing a generic plan, or a targeted `ANALYZE`**: `plan_cache_mode` is
  session state, and a generic plan is still estimate-driven. An `ANALYZE` of
  `content_entities` per repository costs seconds at corpus scale and races
  other writers.

## Fix

Each side table is reaped with a join-free set-difference read, then deletes
run only for the ids it returned:

```sql
SELECT fp.entity_id FROM code_function_fingerprint fp WHERE fp.repo_id = $1
EXCEPT
SELECT ce.entity_id FROM content_entities ce WHERE ce.repo_id = $1
-- and the same for code_fingerprint_band
```

- **Fingerprint rows** are deleted with the existing
  `repo_id = $1 AND entity_id = ANY($2)` statement. It probes the primary key.
- **Band rows** are deleted with
  `COALESCE(band.entity_id IN (SELECT unnest($2::text[])), FALSE)`: one scan of
  the repository's bands, probing a hash of the id chunk.
  - The existing `entity_id = ANY($2)` band delete is not reused. Under a
    generic plan it becomes one repository-range descent of the band primary
    key per array element: 18,854 ms for one 500-id chunk at 5×, against 120 ms
    custom.
- **Chunks** hold 5,000 sorted ids.

Why this holds under any statistics:

- **No join to plan**: there is no join, so there is no nested loop to fall
  into. Each input is read once, into either a HashSetOp or a sorted SetOp
  that spills.
- **Bounded memory**: the HashSetOp holds only the distinct side-table ids
  (fingerprinted functions, not all entities): 512 kB at 1× and 4 MB at 5×,
  measured in the no-spill stale case.
- **Delete set**: the delete's hashed SubPlan holds one chunk of at most 5,000
  ids.
- **Same rows**: a row is deleted if and only if its entity has no
  `content_entities` row in the repository, which is the old predicate.
- **Concurrency**: the read and the delete are separate statements. They rely
  on the invariant the entity reap already documents: one Write per
  repository at a time, from the `claimProjectorWorkQuery` scope guard.

## Before and after

Performance Evidence: median of five interleaved rounds (ms), the whole reap (both tables), shim above.

Fresh statistics, 213-repository corpus:

| repo | churn | plan mode | before | after |
| --- | --- | --- | --- | --- |
| 1× | none | custom | 47.4 | 35.7 |
| 1× | none | generic | 54.8 | 41.2 |
| 1× | 499 stale | custom | 53.0 | 57.4 |
| 1× | 499 stale | generic | 56.1 | 58.2 |
| 5× | none | custom | 585.2 | 530.8 |
| 5× | none | generic | 311.6 | 201.6 |
| 5× | 2,497 stale | custom | 587.8 | 1,362.5 |
| 5× | 2,497 stale | generic | 318.2 | 295.7 |

Stale statistics (repository unseen by the last `ANALYZE`):

| repo | churn | before (single run) | after, custom | after, generic |
| --- | --- | --- | --- | --- |
| 1× | none | fp 15,963 + band > 600,000 (timeout) | 92.6 | 31.6 |
| 1× | 499 stale | fp 19,106 + band not run | 135.9 | 51.8 |
| 5× | none | fp > 300,000 (timeout) | 699.4 | 186.6 |
| 5× | 2,497 stale | fp > 300,000 (timeout) | 1,334.9 | 280.3 |

Old generic plans were not affected (13–94 ms for the fingerprint reap).
Custom plans are what the issue measured. At rows=1 their cost is always below
the generic plan's, so pgx keeps them.

The regression is the 5× churn custom row: 588 ms before, 1,363 ms after.

- When stale rows exist, the band table is read twice: once by the stale-id
  read and once by the delete. There is no index keyed by entity for a single
  pass. In this shim the 5× repository is 7.6% of the band table, so both
  custom plans sequentially scan the whole table.
- At the issue's repository size, the same case costs 4 ms more (53.0 → 57.4
  ms). The generic plans are unchanged or faster.
- A `code_fingerprint_band (repo_id, entity_id)` index would make the delete
  a probe. It is not added here: it adds write amplification on a 32-rows-per
  -function table and needs its own measurement.

## Tests

- `TestContentWriterFingerprintReapScansOnceUnderStaleStatisticsLive` builds
  the stale state on the real bootstrap definitions (10 repositories analyzed,
  target unseen, ten fingerprinted entities removed) and asserts:
  - the planner's one-row estimate, as the precondition;
  - the old anti-join still rescans, as the canary;
  - every node of every statement the reap issues has Actual Loops ≤ 1, with
    the deletes included (four statements).
- `TestContentWriterFingerprintReapMatchesAntiJoinLive` compares the rows
  deleted with the old predicate, kept verbatim as the oracle, with and without
  `ANALYZE`. It covers:
  - nothing stale;
  - churned entities;
  - tombstoned entities;
  - bands whose fingerprint row is already gone;
  - fingerprints without bands;
  - an entity row moved to another repository;
  - every entity gone;
  - an empty repository.

  Other repositories' rows must be untouched, and the changed signal and row
  counts must match.
- `TestContentWriterWriteReapsChurnedFingerprintSideRowsLive` runs two real
  `Write` generations, one with a moved function and one with a removed
  function.
- Unit tests pin the statements, sorted 5,000-id chunks, counts, the changed
  signal, and the empty case (two reads, no delete).

## Observability

Observability Evidence: the existing `reap_stale_fingerprints` stage log (`content writer stage completed`) now also carries `stale_fingerprint_entities`, `stale_band_entities`, `fingerprint_rows_deleted`, and `band_rows_deleted`.

An operator can tell a slow reap that deleted nothing, which was the cliff's
signature, from one doing real churn work. No metric, span, or runtime knob
changed.
