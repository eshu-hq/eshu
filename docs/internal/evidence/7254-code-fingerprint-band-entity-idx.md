# code_fingerprint_band Entity Index Evidence (#7254)

`code_fingerprint_band` carries `entity_id` only as the fourth column of its
primary key `(repo_id, band_no, band_hash, entity_id)`. Two statements filter
on `(repo_id, entity_id)` and so cannot seek:

- `deleteFingerprintBandsForEntitiesSQL` (`repo_id = $1 AND entity_id =
  ANY($2)`), run in 500-id chunks on every `Write` for every re-upserted
  fingerprinted entity, and
- `staleFingerprintBandEntityIDsSQL`, the stale-id read of the #7230 reap's
  second pass (`repo_id = $1` set-difference against `content_entities`).

Migration `151_code_fingerprint_band_entity_idx.sql` adds
`code_fingerprint_band_entity_idx ON code_fingerprint_band (repo_id,
entity_id)` as a sole `CREATE INDEX CONCURRENTLY IF NOT EXISTS` statement.

## Fixture (local only, no QA environment)

`postgres:16.15` in a throwaway container, loaded with the migration 111
table and lookup index plus a minimal `content_entities`
(`(repo_id, entity_id)` index, as migration 035).

| Table | Rows | Note |
| --- | --- | --- |
| `repo-5x` bands | 799,370 | 229,040 entities, ~3.49 bands each; the #7254 shim reports 799,200 |
| `repo-5x` `content_entities` | 229,040, then 10% removed (206,136 live) | churn so the reap has stale ids (22,904) |
| 20 filler repos | 3,197,480 | 45,808 entities each; the shim's whole table held 10.5M rows, this one holds ~4.0M |

Sizes: table 355 MB, primary key 430 MB, new index 81 MB. The concurrent build
took 11.5 s on this table. Band hashes are `md5` of a clustered integer, so the
fixture has LSH-like shared bands; it is not the shim's data.

Statements are measured with `EXPLAIN (ANALYZE, BUFFERS)` inside `BEGIN ...
ROLLBACK` on this fixture database. `plan_cache_mode = force_generic_plan` is
the generic arm, `force_custom_plan` the reference. One 500-id chunk is spread
across the repository (`repo-5x:e<(g*457)%229040+1>`).

## Before / after

Warm executions (a warm-up run first), three each, in milliseconds.

| Statement | Plan mode | Before (no index) | After (index) |
| --- | --- | --- | --- |
| upsert delete, one 500-id chunk | generic | 4,647 / 4,413 / 4,695 | 668 (first, cold pages) / 5.0 / 4.4 |
| upsert delete, one 500-id chunk | custom | 127 / 239 / 157 | 9.2 / 4.7 / 4.3 |
| reap stale-id read (`EXCEPT`) | generic | 3,066 / 3,310 / 3,436 | 749 / 911 / 822 (534 in a later single run) |
| reap delete of a 500-id stale chunk | generic | 134 / 137 / 396 | 102 / 88 / 89 (73 later) |

Plan shape, generic upsert delete, before (cold single run 3,409 ms; warm above):

```text
Delete on code_fingerprint_band
  -> Bitmap Heap Scan on code_fingerprint_band  (rows=1745)
       Recheck Cond: (repo_id = $1)
       Filter: (entity_id = ANY ($2))
       Rows Removed by Filter: 797625
       -> Bitmap Index Scan on code_fingerprint_band_lookup_idx  (rows=799370)
```

After (3.7 ms):

```text
Delete on code_fingerprint_band
  -> Index Scan using code_fingerprint_band_entity_idx  (rows=1745)
       Index Cond: ((repo_id = $1) AND (entity_id = ANY ($2)))
```

The stale-id read changes from an index-only scan of the 430 MB primary key to
an index-only scan of the 81 MB entity index; the remaining cost is the sort
and set difference, which the index does not remove.

### Auto mode (acceptance 1)

`plan_cache_mode = auto`, six executions of the delete in one session, EXPLAIN
of the sixth. Without the index the sixth execution is the generic bitmap plan
above (3,229 ms, 797,625 rows filtered). With the index it is
`Index Scan using code_fingerprint_band_entity_idx` (3.8 ms). Auto mode does
select the slow generic plan on this fixture.

### Write amplification, first local check (1x only)

Inserting one 1x repository's bands (45,808 entities, ~159.8k rows) through
`INSERT ... SELECT`, three runs each, rolled back: 1,216 / 1,196 / 1,240 ms
without the index, 1,640 / 1,825 / 1,743 ms with it. That is about +0.5 s
(about 40%) for the statement. The statement also generates the entity rows,
which cost the same in both arms, so the ratio for the band rows alone is
higher than 40%; the absolute delta is the figure to use. This was the
first, 1x-only check through `INSERT ... SELECT`; the 5x, production-writer and
reference-corpus measurements are in the sections below.

## Production hit search (acceptance 2)

Searched on 2026-10-02: the `log-postgres.txt` of every retained #7206
verification run that has one (9 base-arm and 6 candidate-arm runs, 2026-09-26
and 2026-09-27; three further base runs kept no Postgres log, and the three
fixture runs are not production-shaped). The run metadata does not label a
"round 3", so the whole set was searched. The runs ran Postgres 18.6 (`postgres:18-alpine`,
`shared_buffers=4GB`) with `auto_explain.log_min_duration=5s`, `log_analyze=on`, `log_buffers=on`,
`log_nested_statements=on`, so only executions of 5 s or more are in the logs.

`Index Scan using code_fingerprint_band_pkey` on the band DELETE
(`repo_id = $1 AND entity_id = ANY($2::text[])`) appears in 7 runs, once each:
6 of the 9 base-arm runs and 1 of the 6 candidate-arm runs. Every one is
`repository:r_de3355a0`, plans as an `Index Scan` on the primary key with the
`entity_id = ANY` in the `Index Cond`, deletes 0 rows, and reads 12,684 to
12,849 buffers:

| Run | Arm | Duration (ms) | Planner row estimate |
| --- | --- | --- | --- |
| `20260926T175555Z` | base | 5,439.6 | 4,941 |
| `20260926T191149Z` | base | 6,046.3 | 4,967 |
| `20260927T124149Z` | base | 5,855.5 | 1 |
| `20260927T144010Z` | base | 5,833.3 | 5,192 |
| `20260927T162432Z` | base | 5,957.8 | 5,041 |
| `20260927T180556Z` | base | 5,980.5 | 1 |
| `20260927T193419Z` | candidate | 5,754.4 | 4,981 |

So the hazard has fired in production-shaped runs of both arms. Each hit is a
probe that matched nothing and still cost about 5.4 to 6.0 s, which the
entity index is expected to remove (these hits read about 12.7k buffers, against
about 6M in the shim, so the mechanism is the same plan shape at a smaller
scale, not the same cost). The runs below the 5 s threshold are not in the logs,
so the other 8 of the 15 searched runs are "no execution of 5 s or more", not
"the plan did not occur". The execution time is far below the 18,853 ms the issue quotes for
one chunk; that figure came from the #7230 shim, not from these runs.

## Shim at the #7230 sizes (acceptances 3 and 5, drift query)

Measured 2026-10-02 on the remote validation host (run dir basename
`7254-shim-20261002T165534Z`, raw runs kept there) against `origin/main`
`abc6d3c7b`, which contains #7465. PostgreSQL 16.15 (`postgres:16`),
`shared_buffers=1GB`, autovacuum off, default `work_mem` and
`plan_cache_mode=auto`. The data has exactly the #7230 sizes: 200 filler
repositories (2,961,056 entities, 328,204 fingerprint rows, 10,502,528 band
rows) plus a 1x repository (45,808 entities, 4,995 fingerprinted functions,
159,840 band rows) and a 5x repository (229,040 entities, 24,975 functions,
799,200 band rows); 11,461,568 band rows in all. The band sharing is a
generator written for this run (clone families with shared band hashes), since
the #7230 note does not publish its own, so the sizes match and the data does
not. One loaded database was cloned into BEFORE (no entity index) and AFTER
(the migration 151 statement verbatim, 15.8 s to build, 98 MiB), both
re-analyzed, and the arms alternate within every round. A peer's stack was
running on the host (load average 2.0 to 3.3), so absolute milliseconds carry
noise; the arms share it.

### Acceptance 3: write cost through the production writer

`ContentWriter.upsertFingerprintBatches` unchanged (fingerprint batches of 300,
band deletes in 500-id chunks, band inserts of 300 rows), one pinned autocommit
connection, batch concurrency 1, `VACUUM` and `CHECKPOINT` before each run,
medians of 5 interleaved rounds unless noted, milliseconds:

| Repo | Run | Phase | BEFORE | AFTER | Change |
| --- | --- | --- | --- | --- | --- |
| 1x | first ingest | band-row insert | 3,209 | 4,111 | +28% (+5.6 us per band row) |
| 1x | first ingest | whole call | 3,661 | 4,561 | +25% |
| 1x | first ingest | WAL | 114 MB | 133 MB | +17% |
| 5x | first ingest | band-row insert | 17,754 | 22,594 | +27% (+6.1 us per band row) |
| 5x | first ingest | whole call | 19,823 | 24,746 | +25% |
| 5x | first ingest | WAL | 514 MB | 610 MB | +19% |
| 1x | re-upsert | whole call | 45,980 | 5,508 | -88% (delete phase 41,558 to 150) |
| 5x | re-upsert | whole call | 1,364,104 (22.7 min, one run) | 29,378 (5 runs) | -97.8% (delete phase 1,341,433 to 785) |

The fingerprint-row phase does not move (320 to 321 at 1x, 1,481 to 1,548 at
5x). So the index costs about 25% on a first ingest of a repository, all of it
in the band inserts, and in exchange removes the delete cliff on every
re-upsert: without it, from the sixth 500-id chunk the planner takes the
generic primary-key scan and the 5x repository spends 22.4 of its 22.7 minutes
in `deleteFingerprintBandsForEntities`. The BEFORE 5x re-upsert is a single
run, because one run takes about 20 minutes; the AFTER arm has five.
Even where the custom plan survives, the delete chunk goes from 848.2 to 16.0 ms
at 5x, so the index pays back wherever the delete scans; no measured cell shows
a net cost on the steady path. A repository too small for the delete to matter
would only pay the insert cost, and that was not measured.

### Acceptance 5: the 18.9 s chunk and the #7230 churn cell

One 500-id chunk of `deleteFingerprintBandsForEntitiesSQL` spread across the 5x
repository, `EXPLAIN (ANALYZE, BUFFERS)`, 5 interleaved rounds, median exec ms:

| Plan mode | BEFORE | AFTER |
| --- | --- | --- |
| generic | 26,219.6 (25,818 to 26,741) | 15.6 (15.2 to 16.7) |
| custom | 848.2 | 16.0 |
| `auto`, sixth execution | 26,324.1 | 10.8 |

This shim reproduces the issue's plan exactly: a generic plan with an
`Index Scan using code_fingerprint_band_pkey` and
`Index Cond: (repo_id = $1) AND (entity_id = ANY ($2))`, 6.02M shared-buffer
hits for 16,000 deleted rows. It takes 26.2 s here against the issue's
18,853 ms (1.39x; different hardware and data). With the index the plan is
always `Index Scan using code_fingerprint_band_entity_idx` at 2,727 buffer
hits, and `auto` selects it on the sixth execution.

The production `reapStaleFingerprints` (both tables), churn deleting every
tenth function entity (499 stale at 1x, 2,497 at 5x), fresh statistics, 5
interleaved rounds, median ms:

| Repo | Churn | Plan | BEFORE | AFTER |
| --- | --- | --- | --- | --- |
| 1x | none | custom / generic | 76.6 / 72.9 | 49.5 / 44.6 |
| 1x | 499 stale | custom / generic | 165.4 / 160.4 | 86.3 / 82.0 |
| 5x | none | custom / generic | 424.1 / 411.9 | 247.2 / 236.0 |
| 5x | 2,497 stale | custom / generic | 1,775.8 / 923.9 | 397.0 / 432.9 |

BEFORE is the #7230 after-state, and its 5x churn custom cell is 1,775.8 ms
against the 1,362.5 ms recorded in the #7230 note (1.30x). With the index it
is 397.0 ms, below that note's 587.8 ms pre-#7230 figure too (a cross-hardware
comparison, since this host runs 1.2 to 3x that note's laptop values, so only
the ratios carry; not re-measured
here: that SQL is gone from the tree). The 1x churn cell no longer regresses
(165.4 to 86.3 ms; #7230 recorded +4 ms). The stale band delete at 5x drops
1,323 to 136 ms and the stale-id read 371 to 195 ms.

### Drift query

`listCodeDriftedPairsQuery` with the production floor (50) and budget (200),
plain wall time, 5 interleaved rounds, median ms: 5x custom 4,710 to 4,650,
5x generic 9,734 to 9,662, 1x custom 511 to 503, 1x generic 1,020 to 1,009,
identical row counts (9,033 at 1x, 81,235 at 5x). Nothing moves, and no plan in
either arm touches the new index. That holds for this shim's state only: it was
vacuumed, so the planner joined the band primary key with index-only scans (a
merge join), not the migration 111 lookup index that the query's comment names.
A table that autovacuum has not yet processed (for example right after a bulk
load) has an empty visibility map; see the next subsection.

### Drift query with an empty visibility map

Rebuilt on the remote host on PostgreSQL 16.15 and 18.6 (run dir basename
`7254-plan-20261002T205403Z`): the 11.3M-row shape with a 5x repository
(799,200 band rows, 78,094 result rows; the generator is this run's, not real
sketches), cloned into BEFORE and AFTER arms, with autovacuum off, `ANALYZE` only and an
empty visibility map (`relallvisible = 0`, constructed here to model a table
that autovacuum has not processed yet), and again after `VACUUM (ANALYZE)`.

- With the empty visibility map and a custom plan, the AFTER arm's band
  self-join uses `code_fingerprint_band_entity_idx` on both sides (a
  `Parallel Bitmap Heap Scan` over `Bitmap Index Scan on
  code_fingerprint_band_entity_idx`, feeding a `Parallel Hash Join`), on both
  versions. The BEFORE arm does a `Parallel Hash Join` over two
  `Parallel Seq Scan`s. The estimated costs are within 2 to 3% (about 390k and
  397k): the primary-key index-only scans lose because without a visibility map
  every entry needs a heap visit.
- After `VACUUM (ANALYZE)`, or under a forced generic plan, both arms choose the
  same plan and the index is unused. With `enable_indexonlyscan = off` the
  planner chooses the entity index in the vacuumed custom case as well.
- Warm execution time barely moves, and its sign depends on heap layout. 16.15,
  empty visibility map, custom plan: 4,908 to 4,493 ms (-8.5%, different
  plans); vacuumed 4,760 to 4,742; with the rows inserted in random heap order
  7,853 to 8,601 ms (+9.5%). 18.6: 5,092 to 4,793 ms in one pass, and 5,083 to
  5,081 ms vacuumed. A second 18.6 pass landed on a different statistics sample
  where the AFTER arm flipped to a lookup-index merge join (5,771 ms, 624 ms of
  it JIT).
- Plan flips across `ANALYZE` samples happen in both arms (40 samples per arm:
  18.6 1 of 40 without and 0 of 40 with; 16.15 1 of 40 and 3 of 40), so they are
  not caused by migration 151; their cause was not found.

Warm medians, 5x repository, milliseconds (the plan column names the AFTER
arm's self-join; BEFORE is the same unless noted):

| Version | Table state | Plan mode | BEFORE | AFTER | AFTER plan |
| --- | --- | --- | --- | --- | --- |
| 16.15 | no VACUUM | custom | 4,908 (seq-scan hash join) | 4,493 | entity index, bitmap hash join |
| 16.15 | no VACUUM | generic | 11,616 | 11,321 | lookup index, merge join |
| 16.15 | vacuumed | custom | 4,760 | 4,742 | primary key, index-only merge join |
| 16.15 | vacuumed | generic | 10,206 | 10,197 | lookup index, merge join |
| 18.6 | no VACUUM | custom | 5,045 | 5,771 (one sample flipped to a merge join; 5,092 to 4,793 in another pass) | entity index, or merge join on a flip |
| 18.6 | no VACUUM | generic | 11,263 | 11,133 | lookup index, merge join |
| 18.6 | vacuumed | custom | 5,083 | 5,081 | primary key, index-only merge join |
| 18.6 | vacuumed | generic | 9,921 | 9,941 | lookup index, merge join |

So the earlier "no plan uses the index" holds only for a vacuumed table. If
autovacuum has not processed the band table when `code_drifted` runs, the index
does enter the drift query's self-join plan, at about the same warm cost per
execution on a 5x repository. Whether the corpus Postgres is in that state when
the stage runs was not observed (no `relallvisible` or autovacuum reading in the
corpus run), so this is a possible regime, not a measured one in production.
Cause of the reference-corpus `code_drifted` item (3,444 s WITH against
2,201 s WITHOUT) is not established. Candidates: an index-driven plan change
(not checked inside the corpus run), a generic-plan switch (a theory in #7531),
host load, and variance on one heavy repository. The shim does not predict
corpus timing in either arm (the WITHOUT item's 2,201 s is also hundreds of
times this repository's 4.7 s elapsed), so a same-scale shim result neither
implicates nor exonerates the index here. A generic plan is about twice as slow
as a custom one in both arms, which is why #7531 tracks the plan regime.
NOT_CHECKED: cold-cache execution (all runs here are warm; the seq-scan plan reads about 232k
pages from cache against about 20k heap pages for the bitmap plan, so a
disk-bound run could move either way), concurrency, and real band hashes.

### Limits of the shim

Batch concurrency was 1 (production runs up to 4 over a pool), concurrent
writers contending on the new index were not tried, and the reference-corpus
timing is reported separately below. The first 1x measurement attempt wrapped
runs in a rolled-back transaction, which made the BEFORE deletes slower than the
production writer (one statement per autocommit) would see; it was discarded.
A killed attempt left 48,000 BEFORE band rows deleted; they were restored
through the production writer and verified before the later blocks.

## Reference corpus (acceptance 3, whole-bootstrap timing)

Measured 2026-10-02 on the remote validation host (run dir basename
`7254-corpus-20261002T183433Z`), 984 repositories, Neo4j backend, the accepted
full-corpus profile knobs unchanged. One image built from `origin/main`
`575ef287b` (it contains migration 151) ran both arms back to back, one run per
arm. WITH is the stock schema. WITHOUT dropped `code_fingerprint_band_entity_idx`
after `db-migrate` finished and before `bootstrap-index` started (the migration
is recorded as applied; the WITHOUT arm's second `db-migrate` pass logged
`applied 0, skipped 176`, and the index was absent from the database for the
whole arm). Per-repo stage times come from the `content writer stage completed`
lines in the bootstrap log; stage sums add worker time, they are not wall time.

| Measure | WITHOUT | WITH | Change |
| --- | --- | --- | --- |
| `upsert_fingerprints` sum, 951 repos each | 403.9 s | 469.0 s | +65.1 s (+16.1%) |
| `upsert_fingerprints` mean, p99, max per repo | 0.425, 5.56, 26.7 s | 0.493, 7.81, 29.6 s | |
| `reap_stale_fingerprints` sum | 53.1 s | 39.9 s | -13.2 s (-24.8%) |
| bootstrap-index wall | 1,594.4 s | 1,683.3 s | +88.9 s (+5.6%) |
| launch to queue terminal | 3,029.5 s | 4,288.5 s | +1,259 s |
| queue | 17,210 of 17,210 succeeded, 0 failed, 0 dead-letter | same | |

No stage is named for the band upsert: the fingerprint-related stages are
`upsert_fingerprints` and `reap_stale_fingerprints`, and the band inserts sit
inside the first. Per repository the median ratio is 1.006; the ten slowest
repositories sum to 126.8 s without and 124.8 s with, and the other 941 repos
sum to 277.1 s and 344.2 s (+24%), so the added time is in mid-size
repositories.

How far to trust it: this is one pair, with the WITHOUT arm run first right
after the image build, and a peer's Postgres was at 100 to 200% CPU on the host
throughout. Fourteen earlier complete #7206 runs on the same host and corpus (no
entity index) put the `upsert_fingerprints` sum at 285.9 to 353.3 s, about 24%
min to max, on 728 to 804 repositories (so compare means per repo: 0.356 to
0.461 s there against 0.425 and 0.493 here) and the bootstrap wall at 1,194 to
1,315 s, about 10%. The +16.1% stage sum and +5.6% wall are inside those spreads,
so this run supports neither a specific cost nor a saving. Two readings cut
the other way and are not hidden by that: the WITH mean per repo (0.493 s) is
above every earlier run's (0.356 to 0.461 s), and both arms' bootstrap walls
(1,594 and 1,683 s) are above the earlier 1,194 to 1,315 s, which ran on other
commits, with host load not recorded. The reap's -24.8% is within its own spread and is not
a clean saving either. Neo4j and read-path effects were not examined. It does show that the
index does not make the reference-corpus bootstrap fail, stall, or leave the
queue unclean.

One reducer item needs its own mention. The queue tail is longer WITH because a
single `code_drifted` item ran 3,443.8 s against 2,200.8 s WITHOUT; the longest
such item in the earlier runs that logged it was 1,564 to 2,024 s. While it ran,
`pg_stat_activity` showed the band self-join of `listCodeDriftedPairsQuery`,
which joins on `(repo_id, band_no, band_hash)`. On a vacuumed table the shim
shows no plan in either arm touching the new index; with an empty visibility map
and a custom plan the AFTER arm's plan does use it, at about the same warm cost
on a 5x repository (see "Drift query with an empty visibility map"). Cause not
established: candidates are that plan change (not checked inside this run, which
logged no plans), a generic-plan switch (theory, #7531), host load (load average
peaked at 73.8 WITH against 55.7 WITHOUT, and the peers' load differed between
arms), and ordinary variance on one heavy repository (`r_de3355a0`, 241,726
`upsert_fingerprints` rows). A shim of that size cannot settle it: the WITHOUT
arm's own item took 2,201 s. NOT_CHECKED: the plan of that exact
statement on the heavy repository inside the corpus run (that run did not log
plans), an `EXPLAIN` on production data (the QA session this needed had expired), repeat corpus
runs, and a second WITHOUT run to bound the tail.

## Migration build strategy (acceptance 4)

A concurrent build takes `ShareUpdateExclusiveLock`, which does not block
`INSERT`/`UPDATE`/`DELETE`, so writers keep running during the build. The file
holds exactly one statement so the migration coordinator runs it in autocommit
without the bootstrap `lock_timeout` (#7004) and drops an invalid same-name
index before a retry. `IF NOT EXISTS` makes a re-run a no-op (verified on the
fixture). The build reads the table once; 11.5 s for 4.0M rows here.

## No-Regression Evidence

No-Regression Evidence (#7254): the change is one new index and no query text
changes, so no reader gets a different result. Measured on the #7230-sized
shim: the upsert path's delete goes from the generic-plan primary-key scan
(26.2 s per 500-id chunk at 5x) to an index seek (about 11 to 16 ms), the
steady re-upsert of the 5x repository from 22.7 minutes to 29.4 s, and the
reap's churn cell from 1,775.8 to 397.0 ms; on a vacuumed table the drift query
does not move and no plan uses the index, and with an empty visibility map and a
custom plan the index is in its plan with warm time -8.5% to +9.5%. The
hazard also fired in 7 of the 15
retained #7206 production-shaped runs, each time costing 5.4 to 6.0 s for a
DELETE that matched nothing.

The cost is on the write side and is not small: about +25% on a first ingest of
a repository (+5.6 to 6.1 microseconds per band row, all in the band inserts),
+17 to 19% WAL, and about 98 MiB of storage per 11.5M band rows (4.7% of the two
existing band indexes). On the 984-repository reference corpus one pair of runs
shows +16.1% on the `upsert_fingerprints` stage sum and +5.6% on bootstrap wall,
both inside the run-to-run spread of earlier runs, with a clean queue in both
arms, so that run neither confirms nor refutes a corpus-level cost. That is a
deliberate trade against a delete cliff that grows with a repository's band
rows. No lease, claim, queue, or transaction path changes. Not measured:
production batch concurrency, concurrent writers contending on the new index,
and the cause of the one long `code_drifted` item in the WITH corpus arm (see
the reference-corpus section).

## Observability Evidence

No-Observability-Change: no metric, span, log, or status signal is added. The
index is observable through `pg_stat_user_indexes`
(`idx_scan` on `code_fingerprint_band_entity_idx`) and the existing
`upsert_fingerprints` stage timing.
