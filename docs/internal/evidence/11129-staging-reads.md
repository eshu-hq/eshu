# #11129 staging reads: evidence

Issue #11129. Rulings: [11129 staging-reads rulings](../arbiter/11129-staging-reads-rulings-2026-11-21.md).

The winners keyed tick read the whole staging queue on every tick, so a staged
burst of N marks cost about N/50,000 ticks times a scan of N rows. This note
records the theory proofs that came before any production code (T1-T5), the
before figures, and the work proof on the built binary.

All theory runs used disposable PostgreSQL 18.6 (`work_mem` 4MB,
`shared_buffers` 128MB, 2 parallel workers per gather) on a shared 16-core
host, with a 12.16M-row staging queue over 121,600 keys loaded by 8 concurrent
writers in 1,000-row statements. That table is 2,714 MB of heap, about 30
percent wider per row than the #11103 P2 dataset (1,988 MiB), so the figures
lean pessimistic. The writer-path runs (T5-A) used a private PostgreSQL 18.6
container with the same settings.

## Before: the shipped statements

Five staging statements per keyed tick, plus the gauge sample before the lease
claim on every tick of every instance:

| Statement | Plan at 12.16M rows | Time |
| --- | --- | --- |
| collapse stats `count(*), max(seq)` | Parallel Seq Scan (Index Only Scan once vacuumed) | 25.6 s cold in P2, 472 ms vacuumed |
| residual `count(*), min(captured_at)` | Parallel Seq Scan, 347k buffers | 686 ms warm |
| gauge sample (same staging aggregate) | Parallel Seq Scan | as the residual |
| collapse fact DELETE (no seq range) | Hash Join over a Seq Scan of 9.51M rows, 16 batches, 39,542 temp blocks (309 MB) | 7.37 s |
| collapse fact SELECT at `work_mem` 4MB | external merge sort, 9,896 kB on disk | 182 ms |

The DELETE plan did not need stale statistics: right after a fresh `ANALYZE` the
planner still estimated 10 unnested seqs per staged group (457,530 rows) and
chose the Seq Scan. That statement was not named in the issue; it is the likely
source of the unattributed P2 p95 (8.3 s) and of its 46.8 s first tick
(NOT_CHECKED against P2's own plans).

The regression pin `TestSupplyChainImpactWinnersStagingStatementsAvoidSeqScanLive`
failed on the shipped SQL for the right reason: a Seq Scan on the staging table
in the stats read, both collapse arms (two per arm under pre-burst stats), the
residual read, the gauge sample, and the rebuild stamp, under fresh and
pre-burst stats, custom and generic plans.

## Why P2 ended with 121,000 keys in the dirty log

P2 staged its marks copy-round by copy-round (`generate_series(1,100) c,
generate_series(0,121599) k`, c outer), so every key is re-marked once per
121,600 seqs, about every 2.43 ticks of 50,000 rows. A key the drain removes
comes back at its next mark. Only keys drained after their final mark stay
gone, which is roughly the last three ticks at 200 identifiers each: 121,600 -
600 = 121,000. The drain rate was 200 keys per tick throughout; the depth is the
re-mark pattern, not a stalled drain.

## Theory proofs

T1, replacement reads (EXPLAIN ANALYZE, BUFFERS): every read is an Index Only
Scan and none is a Seq Scan. The bound read walks at most 50,001 seq-index
entries: 14.6 ms and 140 buffers once vacuumed, 37.1 ms with 50,001 heap fetches
and 1,571 pages on a fresh unvacuumed load. The residual read (bounded count,
`min(seq)`, `max(seq)`, `min(captured_at)` in one snapshot) took 7.6 ms warm and
14.2 ms unvacuumed. The minimum takes the `captured_at` index endpoint, 4
buffers. Custom and generic plans are identical. `CREATE INDEX CONCURRENTLY` on
the 12.16M rows took 3.9 s (246 MB; the seq index is 260 MB).

T2, scoped `work_mem`: at 32MB, set with `set_config(..., true)`, the fact-arm
SELECT is Index Scan, Sort (quicksort 11,693 kB), GroupAggregate, in the same
key order, with no temp buffers. `current_setting('work_mem')` reads 4MB again
after the restore and after a rollback. 32MB is 2.7 times the measured sort.

T3, pure staging insert cost of the `captured_at` index, 8 writers, three
alternating pairs: without the index 62.4 / 58.6 / 59.7 s (194.9 / 207.7 /
204.0 tps), with it 72.8 / 74.0 / 74.6 s (167.1 / 164.5 / 163.0 tps), so +22 to
25 percent. This is the component cost of a second right-edge btree. The ruling
moved the bar to the real writer transaction (T5-A).

T4, unfixed shape under a held REPEATABLE READ snapshot after 50 collapse ticks
(2.5M deleted rows): the bound read 581 ms, the combined residual 1,724 ms
(three dead-head walks of about 580 ms), the rebuild-stamp minimum 583 ms. The
tick times of that loop include the unfixed DELETE and do not measure the
design.

T5-B, ranged arms (`seq >= lo AND seq <= bound` on both SELECTs and both
DELETEs), generic plan, 12.16M rows: the fact SELECT is an Index Scan on that
range; the fact DELETE is a one-batch Hash Join (2,856 kB) over an Index Scan of
the same range, 61 to 73 ms, no temp, no Seq Scan; the scope arm 6 to 17 ms. The
arm statement spent 1.0 to 1.6 s in total, almost all of it in the merge INSERT
into the dirty log (50,000 conflict updates), not in staging.

T5-C, the ranged design under a held snapshot, 50 ticks, 2.55M dead entries:
three left-edge walks inside the lock, each under 1 s: the bound read 538 ms,
the residual bounded walk 495 ms, the residual minimum about 525 ms. The arms
read only `[lo, bound]`: the fact SELECT 1,576 buffers, the fact DELETE 73 ms,
the scope arm 17 ms, with no dead-head walk. After the snapshot ends, killed
index entries still cost leaf-page reads until vacuum: one residual read took
3.47 s reading 6,982 leaf pages on the busy host, and 50 ms after a manual
`VACUUM` (69.7 s for 12.16M rows and two indexes). Autovacuum did not process
that table in 25 minutes on the shared server (NOT_CHECKED why).

T5-D, plan stability: with a fresh `ANALYZE`, and with stats taken on an empty
table followed by the 12.16M load and no `ANALYZE`, every staging statement
keeps an index plan under custom and generic plans. Under the empty-table stats
the arms plan a Bitmap Index Scan on the seq index and a Merge Join, 0.73 to
0.84 s, no temp. No statement needed restructuring.

T5-A, the real writer transaction, with and without the index. The original bar
(ruling 2) was writer throughput within 5 percent at the real writer count,
with the relative overhead at the highest count at most twice the 1-writer
overhead; ruling 3 restated it on resolvable measures before round 2, and
ruling 4 judged round 2. Each pass is
the production writer shape: the conflict-domain advisory lock (keyed per
writer's own scope and generation, so writers never share a key), a
`BatchInsertVersionedFacts` upsert of 714 findings, `captureWinnersDirtyFactsSQL`
staging 714 marks, commit. Each cell ran in a fresh template clone after a
`CHECKPOINT`, on a private container with the same settings. CPU is the
container's cgroup usage per pass, WAL bytes the exact insert-LSN difference
per pass.

Round 1 (a timed quiet window, alternating pairs): at 8 writers, 6 pairs, CPU
per pass 71.93 to 73.28 ms (+1.88 percent), WAL +2.08 percent, throughput
22.23 to 22.31 passes/s; at 1 writer, 2 pairs, CPU +1.81 percent, throughput
-0.95 percent; at 16 writers, 5 pairs, CPU +2.10 percent, WAL +3.13 percent,
throughput -2.2 percent. The throughput pair spread at 8 writers (-6.5 to +13.8
percent) could not resolve a 5 percent bar, and the round's ratio bar named no
measure, so the arbiter restated the bar before a second round (ruling 3):

- R1 at 8 writers: throughput delta at or above -5 percent on the pooled mean
  of both rounds' pairs, CPU per pass at most +5 percent, WAL bytes per pass at
  most +5 percent, and the share of active writer samples waiting on LWLock or
  Lock rising by at most 2 points.
- R2, 16 writers over 1 writer: non-FPI WAL record bytes added per mark at most
  2x, CPU overhead at most 2x, the same 2-point wait rule at 16 writers;
  full-page-image bytes reported beside it.

Round 2 (T5-A2, 28 cells: 4 pairs at 1 writer, 4 at 8, 6 at 16, index first on
even pairs, plus `pg_waldump --stats=record` per cell and a 10 ms
`pg_stat_activity` wait sampler of the writer backends):

| Measure | 1 writer | 8 writers | 16 writers |
| --- | --- | --- | --- |
| Btree WAL records added per pass | +715.8 | +716.4 | +718.0 |
| non-FPI record bytes added per mark | +64.5 B | +63.9 B | +65.5 B |
| WAL bytes per pass | +1.80% | +1.58% | +3.89% |
| CPU per pass | -5.25% (bimodal, see below) | -0.47% | +1.59% |
| throughput | -0.99% | -2.13% (pooled with round 1: -0.55%) | -3.14% |
| LWLock+Lock share of active samples | +0.00 points | -0.29 points | +1.23 points |
| FPI bytes added per mark | -1.4 | +2.1 (SE 25) | +92 (SE 61) |

The Btree count shows about 714 more records per pass with the index (one leaf
insert per staged row plus splits; at 1 writer INSERT_LEAF +106,808 and SPLIT_R
+292 is exactly 714 x 150), so the harness stages what it reports, and the
record bytes per mark (about 65 B) are flat from 1 to 16 writers and match the
pure-insert figure. Round 1's "+40 B/mark" was read from `pg_stat_wal` before
the writer backends flushed it; round 2 reads the exact LSN difference.

Verdict (ruling 4, the source of the pass): T5-A passes and migration 330
ships.

- R1 at 8 writers passes all four checks: throughput -0.55 percent pooled over
  10 pairs (95 percent interval about -5.9 to +4.8), CPU -0.47 percent
  (-2.76 to +1.82), WAL bytes +1.58 percent (-0.30 to +3.45), lock-wait share
  -0.29 points.
- The wait-share precondition tripped: the no-index share spread 6.44 points
  across the 8-writer cells (71.7 to 78.2 percent), drifting over blocks of
  cells and tracking throughput (correlation -0.84). Pairing does not cancel
  that drift (pair deltas +3.69, -2.32, -1.89, -0.64). The precondition's
  purpose is met by splitting the same samples by wait event: lock classes
  other than LWLock/WALWrite moved -1.12 points (interval -3.52 to +1.28),
  where an index contention point would show; LWLock/WALWrite alone moved
  +0.83 points (interval -1.20 to +2.85, the commit flush queue); at 16 writers
  WALWrite moved +0.14 points.
- R2 passes on record bytes (65.47 / 64.55 = 1.01x) and waits (+1.23 points at
  16 writers, interval +0.76 to +1.71, all six pairs positive, in WALInsert
  +0.73 and BufferContent +0.49, no new event in the top eight).
- The R2 CPU ratio is withdrawn, neither pass nor fail. CPU per pass is the
  container's cgroup counter, which also charges background work: 1-writer
  cells 10, 11, 12 and 15 contain Heap/INPLACE, Gin/CREATE_PTREE,
  Standby/INVALIDATIONS records and extra commits (64.96, 64.86, 59.87, 61.17
  ms per pass) and cells 8, 9, 13 and 14 do not (53.64 to 54.03 ms), so the
  1-writer denominator moves by up to 12 percent with no index change. The
  replacement bound was chosen after the data: added CPU per pass at 16
  writers as a percent of no-index CPU per pass, 95 percent upper bound at
  most +5 percent. Result: +1.59 percent (upper bound +2.72 percent), +1.27 ms
  per pass, 1.8 microseconds per mark.
- Full-page images: by sign 4 of 6 pairs at 16 writers are positive, under the
  5-of-6 trigger. Added FPI bytes per mark: 1 writer -1.4 B, 8 writers +2.1 B
  (SE 25), 16 writers +92 B (SE 61). The added Btree FPI bytes (mean about 51
  MB per cell, up to 141 MB) exceed the whole new index (about 14 MB), so they
  are images of other indexes' pages. Unexplained.

The costs, stated plainly: throughput -0.99 percent at 1 writer (interval -1.66
to -0.32), -0.55 percent pooled at 8 writers, -3.13 percent at 16 writers
(interval -4.78 to -1.49; -2.66 pooled with round 1); about +65 B of WAL records
and 714 Btree inserts per pass of 714 marks; at 16 writers CPU +1.59 percent and
lock wait +1.23 points. Host load during 8-writer cells 4 to 6 rose to 9.5 to
11.4; every cell is kept.

Data and sources, committed beside this note in
[11129-staging-reads/](11129-staging-reads/): `t5a2-rows.tsv` (one row per
cell, with the top wait events), `t5a2-advisory-keys.txt` (one distinct key per
writer in every cell), `t5a2-waldump-btree.txt` (the Btree and Total rows of
`pg_waldump --stats=record` per cell), `t5a2-analysis.txt` and
`t5a2-analyze.py.txt` (the fixed rule as code and its output),
`t5a2-harness.go.txt` (the throwaway harness source, never compiled into the
repository), and `t5a-round1-rows.txt` (round 1 cells and the pure-insert
pairs).

The limiter, from the no-index wait profile, on this host and container (not a
production claim): one writer is about 89 percent on CPU; at 8 writers
LWLock/WALWrite is 67 to 72 percent of active samples; at 16 writers
LWLock/WALWrite (about 44 percent) and LWLock/WALInsert (about 35 percent)
dominate. A pass writes about 2.8 MB of WAL, nearly all of it the findings
upsert, so the writers queue on the WAL flush and insert locks at commit with
or without the index. In round 2, 8 writers ran 1.05 times the 1-writer rate
(17.72 over 16.92 passes/s).

Component cost, recorded and not a bar: a pure staging insert pays +22 to 25
percent wall time at 8 writers (T3) and +66 B of WAL per staged row (324 to 390
B, deterministic) at 1 writer.

## Source answers the ruling asked for

- No writer sets `seq`. All seven staging INSERTs (keyed_sql.go, audit.go,
  writer_retract.go twice, migrations 310 and 311 twice each) name their
  columns without it, and nothing updates or copies into the table. The
  sequence hands each row its own seq, which the DELETE join and the span upper
  bound rely on.
- The migration runner supports `CREATE INDEX CONCURRENTLY` when it is the only
  statement of its file: it runs outside a transaction on a dedicated
  bootstrap connection, and an invalid concurrent index is dropped by name
  before the definition reruns (migrations 099, 105, 319 and 327 do the same).
- A row that was INSERTed before the tick clock and commits after the tick's
  residual read is covered by the stamp for one tick while still queued; the
  next tick applies it. That is the accepted #10706 over-claim
  (`TestSupplyChainImpactWinnersResweepKeyedHorizonDrainedLateCommitLive`).
  The change keeps the residual read where it was, so the window is unchanged;
  the keyed.go comment that said otherwise is corrected.
- The staging statements sit outside the #11117 winners contract hash; the
  rebuild text, which is inside it, is unchanged.

## Work proof: the P2 rerun on the built code

Performance Evidence: the #11103 P2 burst rerun on this branch's code, private
PostgreSQL 18.6 container (same settings), `ResweepWinnersKeyed` ticks driven
back to back, the production gauge sample (`DirtyLogStats`) before every tick.
12,160,000 marks over 121,600 keys were staged copy-round by copy-round (2m07s,
one bulk statement), then the container was restarted so the first tick read
cold buffers, and 4 writers staged 50 re-marks every 500 ms each during the
first 200 ticks (113,850 marks). Each staging statement was timed by its SQL
text inside the tick transaction, the dirty-log reads separately.

| Phase, per tick | Before (#11103 P2) | After (n = 246 ticks) |
| --- | --- | --- |
| collapse plus staging reads, in the lock | not separated; whole tick p50 3.21 s, p95 8.35 s, max 46.8 s | first (cold) 0.69 s, p50 0.70 s, p95 1.57 s, max 4.28 s |
| dirty-log reads (remaining count and minimum), in the lock | not separated | first 6 ms, p50 50 ms, p95 97 ms, max 0.99 s |
| gauge sample, before the lease | a whole-queue scan | p50 73 ms, p95 131 ms, max 0.43 s (staging part p50 13 ms; dirty-log part p50 26 ms) |
| whole tick | p50 3.21 s, p95 8.35 s, max 46.8 s, 16.6 min total | p50 0.91 s, p95 2.03 s, max 4.95 s, 4.6 min total |
| ticks to empty staging | 244 for 12.16M marks | 246 for 12.27M marks (12.16M plus 113,850 written during the drain) |
| temp files | 5,976 kB on the first collapse sort | 0 files, 0 bytes for the whole drain |

Every bar holds: collapse plus staging reads at most 5 s per tick including the
first cold tick (max 4.28 s), at most 250 ticks, no temp file, with writers
inserting during the drain. The whole-tick figures are comparable with P2 only
loosely: the before run shared the host and its database, the after run ran in
a private container during a quiet window. The dirty-log reads are #11136's
subject and stay outside this change. The run's output and harness source are
`11129-staging-reads/p2-rerun-output.txt` and `p2-harness.go.txt`.

Concurrency proof: `TestSupplyChainImpactWinnersStagingLateSeqSurvivesRangedCollapseLive`
commits a mark below the tick's `lo` after the bound read, a mark with a lower
in-range seq after the fold's snapshot, and a mark above the bound mid-tick.
All three stay staged, the residual read counts them exactly, the stamped
watermark is below each one's `captured_at`, the next collapse folds them, and
marks equal log versions plus the staging residual. The collapse arms keep the
shared `staged` CTE, so a row is deleted only when it was merged.

Observability Evidence: the tick-phase histogram already splits `collapse` and
`stamp`; this change adds `remaining_upper_bound` to the tick span and commit
log line and the `eshu_dp_supply_chain_impact_winners_staging_depth_upper_bound`
gauge, and the telemetry reference names the held-snapshot check
(`pg_stat_activity.backend_xmin`).

## NOT_CHECKED

- What ran in the background of 1-writer T5-A2 cells 10, 11, 12 and 15
  (autovacuum presumed).
- Why full-page-image bytes rise at 16 writers in 4 of 6 pairs (checkpoint
  timing is a hypothesis).
- What raised host load to 11.4 inside the quiet window.
- Byte equality of the harness lock key with the production writer's key: the
  harness builds `kind \x1f scope \x1f generation` the way the landing
  interleave test does; the production writer side was read
  (`writer_retract.go`), not compared byte for byte.
- Whether the WAL flush limiter appears on production storage.
- Why autovacuum did not process the staging table in 25 minutes on the shared
  theory server.
