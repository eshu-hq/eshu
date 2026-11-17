# #10823 relationship retention sweep: performance and observability evidence

The reducer's generation-retention runner now takes a read-only census of
orphaned relationship generations on every due pass and, only when
`ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ENABLED` is set, deletes their
evidence, candidate and generation rows in small committed batches. This note
is the tracked record the performance-evidence gate requires. It holds the
measurements behind the batch size, the row cap, the census timeout and the WAL
guidance, the lock probes, and the mutation runs.

Performance Evidence: every figure below comes from one throwaway PostgreSQL
18.6 server on a shared development laptop, the real migrated schema, synthetic
rows. The server ran with `fsync=off` and `synchronous_commit=off`: they remove
the commit's wait for a WAL flush, so a production commit (and, with synchronous
replication, the replica's acknowledgement) adds to every batch time here, and
they do not change how much WAL a batch writes. Timings follow the host. On a
calm host (load average about 20) a 50-generation batch (about 2,650 rows whose
generations sit together) commits in 52 to 57 ms at p50 and 109 to 127 ms at p95,
and its five statements take 51 ms in `EXPLAIN (ANALYZE)`. A window at load
averages of 55 to 85 ran the same statements 27 to 63 times slower (the hub batch's
evidence delete 3.4 s against 127 ms, the median batch of a pass 3.6 s against
57 ms), so read a timing from this laptop as a best case, never as a budget. One
hub generation of 120,000 evidence and 2,000 candidate rows drains in 13 batches
of at most 20,050 rows in 3.1 s on the calm host, the slowest batch 481 ms. Before
the row limit it ran as one batch that held all 124,598 row locks until its
commit, about 2 s (1.6 to 2.6 s across the three variants of one probe run, and
60.6 s in an earlier run that I attribute to a host stall and could not
reproduce). WAL per deleted row is 336 to 347 B when rows sit together
(the row lock each delete now writes adds about 55 B to the 281 to 305 B of the
first design), 1,276 B on the first pass over rows scattered at random right after
a checkpoint and 126 B on the second (S1), and 367 B for the hub. The census is
one pass over each table's generation ids: 0.2 to 0.3 s warm (0.5 to 1.2 s on a
first pass) on 3.6 million evidence rows with no orphans, 2.4 to 8.5 s with 2
million orphan rows among 5.6 million. Nothing was measured on a production
corpus or against a replica.

Observability Evidence: four instruments (`eshu_dp_relationship_retention_orphan_generations`,
`..._rows_swept_total`, `..._generations_skipped_total`, `..._capped_total`), two
phases on `eshu_dp_generation_retention_phase_duration_seconds`, the failure
reason `relationship_orphan_sweep`, the span `generation_retention.relationship_sweep`
and four log lines (the committed-batch line also says how many generations the
row limit cut, `batch_continued`, so a large generation reads as batches that
finish none and continue one). The reducer retention tests read each metric back from a
manual reader, including the zero gauge and the failure path; 9 telemetry
mutants turn them red (listed under Mutation runs, run at an earlier head).
Operator reading guide:
`docs/public/reference/telemetry/generation-retention-relationships.md`.

## Setup

- PostgreSQL 18.6 (Homebrew), throwaway cluster, `wal_level=replica`,
  `full_page_writes=on`, `wal_compression=off`, `shared_buffers=128MB`,
  `checkpoint_timeout=5min`, `max_wal_size=1GB`, `fsync=off`,
  `synchronous_commit=off`. The last two remove the commit's wait for a WAL flush
  (a production commit pays it) and leave WAL volume alone. The final runs also
  set `track_io_timing` and `track_wal_io_timing`, which feed the I/O wait the
  probe prints.
- Hosts, as the one-minute load average at the start and the end of each run: the
  final together-layout run 20 then 19 ("calm"), the final random-layout run 24
  then 50, and an earlier together-layout run 85 then 55 ("overloaded"). Swap was
  98 to 99% used in the calm and random windows, where I sampled it; I did not
  sample it in the overloaded one. The tables name which run is which.
- The real bootstrap schema (`ApplyBootstrap`), `VACUUM (ANALYZE)` after every
  seed. Evidence rows are about 480 bytes with a small JSONB detail.
- Corpus S1: 12,000 scopes, 24,000 live generations (one pointed at, one
  superseded, each with a scope generation row), 720,000 evidence rows,
  240,000 candidates. Orphans on top: 10,000 generations of 40 evidence and 12
  candidate rows (1.12 million evidence rows in all).
- Corpus S5: five times that: 3.6 million live evidence rows, then 50,000 orphan
  generations (2.0 million rows) for 5.6 million in all. The evidence table is
  1.7 GB with its indexes.
- Orphan layouts: together (a generation's rows are contiguous in the heap),
  scattered by id (`ORDER BY i, k` on insert: row 1 of every generation, then
  row 2, and so on) and random (`ORDER BY random()`, so the swept rows sit on
  random pages).

Every census, delete and WAL figure below comes from
`TestRelationshipRetentionScaleProbe` (`scale_probe_test.go`), a manual probe
that skips unless a scale is set and is not a CI test. Scale 1 is S1 and scale 5
is S5:

```text
ESHU_RELATIONSHIP_RETENTION_PROBE=1 ESHU_RELATIONSHIP_RETENTION_PROBE_LAYOUT=random \
  ESHU_POSTGRES_DSN=... ESHU_POSTGRES_DISPOSABLE=1 \
  go test -p 1 -count=1 -v -timeout 40m -run TestRelationshipRetentionScaleProbe \
  ./internal/storage/postgres/retention/relationships/
```

The final S1 runs and the hub drain (the three runs of the probe, one per layout
and load window) came from the committed probe at head `530d3e1fd7`, which is before
the rebase onto main at `f9cfd99c34`. The SQL constants of `sql.go` hash to
`d67530eb2d13bcaf` there and at the final head (the first 16 hex digits of one
sha256 over the raw-string texts of the eleven statements in `sql.go`, taken by a
scratch script that is not in the tree, so a comment-only edit leaves it alone),
and `batch.go`, `sweep.go` and `census.go` are byte-identical, so the measured
statements are the shipped ones. The first S1 and S5 runs below came from an uncommitted scratch copy of
that probe. It had the same seed statements, EXPLAINs and sweep passes; the commit
changed the names, split it into helpers, and set the EXPLAIN statement timeout
to the production census bound (60 s, where the scratch copy used 600 s). The
committed probe was then re-run at S1 for each layout (the re-run tables below).

## Census

`EXPLAIN (ANALYZE, BUFFERS)` of the three census statements, then the whole
census-only pass (one read-only transaction). The first pass is cold.

| State | Evidence | Candidates | Generations | Census-only pass |
|---|---|---|---|---|
| S1, no orphans | 35.7 ms | 22.5 ms | 6.7 ms | 49 to 55 ms |
| S1, orphan-abundant, together | 206.7 ms | 50.8 ms | 54.8 ms | 162 to 235 ms |
| S5, no orphans | 365.8 ms | 368.6 ms | 39.0 ms | 1.23 s cold, then 262 to 265 ms |
| S5, orphan-abundant, together | 2,062 ms | 214.8 ms | 86.4 ms | 2.4 to 3.1 s |
| S5, orphan-abundant, random | 8,870 ms | 246.4 ms | 67.3 ms | 7.9 to 8.5 s |

A second S5 steady-state run measured 166, 447 and 50 ms, and 203 to 519 ms for
the pass. The orphan-abundant passes are saturated: the gauge reads 5,000 per
table.

- With no orphans the evidence plan is a Parallel Index Only Scan of
  `relationship_evidence_generation_idx` (Heap Fetches 0) under a hash anti join
  of `scope_generations`, then index probes of `ingestion_scopes` and
  `relationship_generations` that never execute.
- With 2 million orphan rows the work grows with the orphan rows, not with the
  5,000 list limit: the DISTINCT is a Sort and Unique over every orphan row
  (`external merge`, 33 MB of temp). In the random layout the planner read the
  evidence table with a Parallel Seq Scan (168,000 buffers, 39,600 dirtied)
  instead of the index, and the statement took 8.9 s. I did not find out why the
  planner preferred it there; the census statements are therefore not plan-pinned.
- The 60 s census timeout has about seven times the headroom of the slowest first
  run (8.5 s, S5 random). The S1 re-runs (committed probe, host load average 26
  to 56) were slower than the first S1 run on the same live rows: no-orphan
  census-only passes took 97 to 823 ms against 49 to 55 ms (2 to 17 times), and
  the no-orphan evidence statement 57 to 509 ms against 35.7 ms. With orphans in
  the together layout the passes took 209 to 531 ms against 162 to 235 ms. Whether
  load can push the 8.5 s case past the 60 s timeout was not measured. Past
  roughly 40 million evidence rows with the slow plan (a linear extrapolation from
  5.6 million) it would fail the pass, and nothing would be swept; there is no
  knob for it.
- The sql.go comment quoted the design's 0.7 to 1.0 s on 3.3 million evidence
  rows. This run measured 0.2 to 0.3 s warm and 0.5 to 1.2 s on a first pass on 3.6 million rows,
  so the design figure sits between the cold and warm runs. The comment now
  cites these runs.
- The census statements did not change with the row limit. The final S1 run on the
  calm host measured the census-only pass at 141 to 167 ms with no orphans
  (`EXPLAIN` 80.0, 32.7 and 8.0 ms) and 121 to 128 ms with orphans (59.4, 54.3 and
  48.7 ms), inside the earlier ranges.

## Delete batches

A batch of 50 orphan generations: 40 evidence, 12 candidate and 1 generation row
each, about 2,650 rows. `EXPLAIN (ANALYZE, BUFFERS)` of each statement as shipped
(rolled back, 50 ids bound, `LIMIT 10000` on the two picking deletes), S1,
together layout, calm host:

| Statement | Execution | Plan |
|---|---|---|
| classify (50 ids) | 1.2 ms | primary key probes; generation rows locked `FOR UPDATE SKIP LOCKED` |
| delete evidence (2,000 rows) | 37.9 ms | `Limit` over `LockRows` over an Index Scan of `relationship_evidence_generation_idx`, a hash anti join of `scope_generations` and index probes of `ingestion_scopes` and `relationship_generations` per row; then one primary key probe per picked row (about 8 us each) to delete it |
| delete candidates (600 rows) | 9.8 ms | the same on `relationship_candidates_generation_idx` |
| delete generations (50 rows) | 1.2 ms | Index Scan `relationship_generations_pkey`, both child guards index-served |
| settle (50 ids) | 0.8 ms | one index probe per id and table |

The evidence delete takes about three times what the first design's did (38 ms
against 13.3 ms at S1, measured in different host windows): it now locks every row it deletes and finds each again by
primary key. The first design's S5 EXPLAINs (35.6 ms together and 51.5 ms random
for the evidence delete) were not repeated on the shipped statements.

Committed batch durations over the 38 batches of a 100,700-row pass. Pass 1
follows a `CHECKPOINT`, pass 2 does not:

| Run | Load, start to end | Pass | p50 | p95 | max | Whole pass | WAL per row | Full page images |
|---|---|---|---|---|---|---|---|---|
| together, calm | 20 to 19 | 1 | 51.6 ms | 126.7 ms | 300.0 ms | 2.7 s | 336 B | 2,117 |
| | | 2 | 56.6 ms | 108.9 ms | 166.0 ms | 2.7 s | 342 B | 3,041 |
| random | 24 to 50 | 1 | 435.4 ms | 1,060 ms | 1,393 ms | 16.7 s | 1,276 B | 14,192 |
| | | 2 | 175.6 ms | 6,408 ms | 8,135 ms | 39.8 s | 126 B | 177 |
| together, overloaded | 85 to 55 | 1 | 94.5 ms | 226.3 ms | 331.0 ms | 4.6 s | 336 B | 2,185 |
| | | 2 | 3,575 ms | 15,646 ms | 16,831 ms | 170.8 s | 347 B | 3,460 |

The overloaded run is in the table because it shows how far a timing from this
laptop can move: it started with the one-minute load average at 85 and ended at 55,
and the same hub evidence delete took 3,403 ms in `EXPLAIN` against 127 ms on the
calm host (the settle read 3,897 ms against 8 ms). I read that as the host, not a
statement, because the same statements ran at their usual speed in the calm run and
in the A/B below, but I could not prove it from the run itself. The second pass of
the random run is the same kind of unexplained: the load average rose from 24 to
50 across the run, and 13 statements of that pass took over a second (8 evidence
deletes, 4 candidate deletes and a classify, the longest 6.7 s), where its first
pass had none. Layout and load moved together there, so I could not separate them.

The first design measured, at load average 19 to 30 and 26 to 56 (50 generations,
no row locks): p50 24 to 43 ms when rows sit together or interleave by id and 48
to 104 ms at random, p95 28 to 312 ms between runs. Two runs per layout could not
separate layout from host load then either: do not tune on a p95 taken on this
laptop.

A hub batch of 50 generations of 1,000 rows (50,000 rows, 4.5% of the S1 table,
0.9% of S5) kept a Bitmap Heap Scan on the generation index in every first-design
run: 32 to 113 ms at S1 (five runs), 291 ms at S5 random. No flip to a sequential
scan was seen. The #10820 purge note measured a flip at 1.43% of a 1.4 million row
table for its layout; if a batch does flip, it costs a table scan and still
deletes at most `BatchRows` rows per picking statement.

### Plan pin

`TestRelationshipRetentionStatementsAreIndexServedLive` seeds 400,000 evidence
rows (8,000 live generations plus 100 orphans), runs `VACUUM (ANALYZE)` and
requires the evidence, candidate and generation deletes and the settle read to
reach their tables through a generation index or a primary key with no Seq Scan,
for the custom plan and for the generic plan (`plan_cache_mode =
force_generic_plan`, with the row limit prepared as a `bigint` parameter) a pooled
driver serves after five runs. The three mutants that stop a delete using its index
(`generation_id || ''`) turn it red with `X is read with a Seq Scan, want a read
through ...`.

## Hub generation and the row limit

The arbiter's rule for #10823 was to bound the batch in rows if a hub generation of
at least 100,000 evidence rows took more than 2 s or wrote more than the 170 MB
pass figure. `probeHubSeed` adds one generation of 120,000 evidence and 2,000
candidate rows ahead of the 49 ordinary orphans (124,598 rows in the first census
slice) and `probeHubDrain` drains that slice:

| Design | Run | Batches | Time | Slowest batch | WAL |
|---|---|---|---|---|---|
| no `LIMIT` | committed | 1 | 1,992 ms | 1,992 ms | 44.7 MB (358 B per row, about 255,000 records, 5,898 full page images) |
| no `LIMIT` | rolled back, warm / fresh connection | 1 | 2.6 s / 1.6 s | | |
| no `LIMIT` | an earlier probe, committed | 1 | 60.6 s | 60.6 s | not reproduced; I attribute it to a host stall and cannot prove it |
| `LIMIT 10000` | calm host | 13 | 3.1 s | 481 ms | 45.7 MB (367 B per row, 275,541 records, 3,948 full page images) |
| `LIMIT 10000` | random layout run, load 24 to 50 | 13 | 9.8 s | 1.29 s | 57.0 MB (457 B per row, 6,083 full page images) |
| `LIMIT 10000` | overloaded host, load 85 to 55 | 13 | 122.6 s | 25.6 s | 45.7 MB (367 B per row) |

The unbounded batch sat at the 2 s line (1.6 to 2.6 s across the three variants of
one probe run) with one earlier run at 60.6 s, and a #10820 purge of that repository waits for the whole batch, so
the batch is bounded in rows: each picking delete takes at most 10,000 rows, a
batch deletes at most 20,050 rows, and a generation the limit cut is run again by
the same pass. The bound costs about half again the total time on the calm host
(3.1 s against 2.0 s: 13 batches, each with its own try locks, classification and
settle) and cuts the longest row-lock hold from about 2 s to 0.5 s. A purge or an
activation can still wait for one batch, so the wait is bounded in rows, not in
seconds: 481 ms at worst on the calm host, 25.6 s at worst on the overloaded one.
The purge's derive-lock budget (#10853) bounds its advisory-lock waits, not its
row waits.

Is the limit itself slower per row? A back-to-back A/B on a minimal replica of the
retention tables (same columns and indexes, 6,000 live generations, a 30,000-row hub,
two rounds each, rolled back, the variants run alternately so each pair saw the same
host; load not recorded) says no: the shipped statement deleted 10,000 evidence rows in 134 to 153 ms
(13 to 15 us per row) and the same statement without the limit deleted 31,960 in
372 to 388 ms (12 us per row). Joining the picked rows to the table by `ctid`
instead of the primary key halves the evidence delete (67 ms for 10,000 rows) and
was not adopted: it changes what the delete is keyed on, and a 150 ms statement is
not worth reopening the proof for.

## WAL

100,700 rows (38 batches, 1,900 generations) per pass, measured as the
`pg_current_wal_insert_lsn` difference around the pass, with `pg_stat_wal` records
and full page images. Pass 1 follows a `CHECKPOINT`, so every first touch of a page
pays a full page image; pass 2 follows directly. Final statements (each delete
locks its rows `FOR UPDATE`, which writes one lock record per row):

| Layout | Pass | WAL | Per row | Records | Full page images |
|---|---|---|---|---|---|
| S1 together, calm | 1 / 2 | 33.9 / 34.5 MB | 336 / 342 B | 160,593 / 220,513 | 2,117 / 3,041 |
| S1 together, overloaded | 1 / 2 | 33.9 / 34.9 MB | 336 / 347 B | 165,944 / 246,800 | 2,185 / 3,460 |
| S1 random | 1 / 2 | 128.5 / 12.7 MB | 1,276 / 126 B | 236,032 / 209,592 | 14,192 / 177 |
| hub slice (124,598 rows), calm | 1 | 45.7 MB | 367 B | 275,541 | 3,948 |
| hub slice, random run | 1 | 57.0 MB | 457 B | | 6,083 |

First design (no row lock record), the runs the S5 planning figures come from:

| Layout | Pass | WAL | Per row | Full page images |
|---|---|---|---|---|
| S1 together | 1 / 2 | 28.3 / 47.9 MB | 281 / 475 B | 1,900 / 4,588 |
| S5 together | 1 / 2 | 28.3 / 28.9 MB | 281 / 287 B | 2,461 / 3,233 |
| S1 by id, first attempt (no `ORDER BY`) | 1 / 2 | 28.3 / 30.7 MB | 281 / 305 B | 2,468 / 2,693 |
| S1 scattered by id (`ORDER BY i, k`) | 1 / 2 | 28.3 / 28.9 MB | 281 / 287 B | 2,613 / 1,407 |
| S5 random | 1 / 2 | 436.0 / 170.9 MB | 4,329 / 1,697 B | 46,312 / 18,612 |
| S1 together, probe re-run | 1 / 2 | 28.3 / 28.9 MB | 281 / 287 B | 2,686 / 2,685 |
| S1 scattered by id, probe re-run | 1 / 2 | 28.3 / 28.9 MB | 281 / 287 B | 2,045 / 2,899 |
| S1 random, probe re-run | 1 / 2 | 122.9 / 7.2 MB | 1,220 / 71 B | 13,925 / 454 |

The row lock adds 55 to 56 B per row at S1 in both layouts (together 281 to 336
and 287 to 342; random 1,220 to 1,276 and 71 to 126), and nothing else changed:
the full page images are the same order. The S5 figures were not repeated. The
runbook plans with 1.7 KB per row, the S5 random second pass of the first design;
with the lock record that is about 1.75 KB (3% more), and the 4.3 KB first pass
right after a checkpoint is about 4.4 KB. Both are derived, not measured.

The second pass of the first S1 together run wrote 47.9 MB (475 B per row, 4,588
full page images against 1,900 in its first pass). A checkpoint landing during the
pass would explain the extra images, but that run did not record one, and the
re-runs of the same layout measured 281 and 287 B (336 and 342 B with the lock).

The cost depends on how many distinct pages the swept rows touch since the last
checkpoint, not on the row count. Rows that sit together share pages (about 0.3 KB
per row). Rows scattered at random over a large table pay close to a page each:
1.7 KB per row on the second S5 pass and 4.3 KB on the first, right after a
checkpoint. At S1 the random layout cost 1.3 KB per row on the first pass and 0.13
KB on the second, whose full page images fell from 14,192 to 177: most pages it
touched had been imaged by the first pass. By-id order did not scatter the S1 rows
across pages (the same WAL per row as together), consistent with the swept ids
being adjacent in each interleaved stripe.

The design text also gave a 50-generation batch as about 1,700 rows, 1.43 MB of
WAL and 1.7 KB per row. 1.43 MB over 1,700 rows is 0.84 KB per row, so those
figures cannot describe one batch. The row cap and the WAL guidance rest on the
per-row figures above. `wal_compression` was off here; it shrinks full page images.
Replica lag was not measured: the cluster has no replica.

## Lock probes (live PostgreSQL 18)

The batch never waits for a row lock: every row lock is `FOR UPDATE ... SKIP
LOCKED`, and the generation row is deleted only when no evidence and no candidate
row of it is left. The first design waited on the purge's rows under a 500 ms
`lock_timeout` and a reviewer reproduced a purge `40P01` against it: the batch
locked the generation row first, the purge locked it last, and a lock timeout only
ends the waiter that carries it, so the purge's own 1 s deadlock check could fire
first (700 and 800 ms after the purge queued in the reviewer's delay table). The
probes below run against the wait-free batch, on the branch rebased onto main at
`f9cfd99c34`, which carries the purge's request-level derive-lock budget (#10853),
so the purge-interplay tests drive the purge as it is on main. The whole leaf
package passes there (61 tests, plus the opt-in scale probe, which skips). Tests
named in the table are in the leaf package.

| Probe | Result |
|---|---|
| A generation stops being an orphan after the census: a scope generation row appears, its relationship generation turns active, or a scope pointer names it | Skipped as `ineligible`, nothing swept, every row intact. Each of the three predicates dropped from classify turns this red. |
| The production activation arrives while a batch holds the generation row (hook after the evidence delete) | It queues behind the row; the batch commits whole; the activation then re-creates the row, empty. Without `FOR UPDATE` in classify it never waits (red). |
| An activation commits after classify took its snapshot and before it locks the row (`TestActivationCommittedBetweenSnapshotAndRowLockIsNotClaimedLive`; the shipped statement staged by one inserted advisory-lock wait) | The generation comes back unclaimed; a whole pass counts it `lock_busy`, never swept (`TestPassOverAnActivationRaceNeverCountsTheGenerationSweptLive`). Without `rg.status <> 'active'` in the locked CTE it is claimed (red). PostgreSQL keeps the row lock on the newest version even when the re-check drops the row, so a lock probe cannot show this; the claimed flag is the contract. |
| Activation race, rounds started together and staggered | See the activation-race runs in the final live run below: every outcome is consistent (children intact when the activation updated the row, none when it re-created it, never half). |
| Claim-fenced activation of an orphan id | A work item for it cannot exist (foreign key 23503), so the fenced activation returns `ErrExecutionClaimRejected` and touches nothing; the sweep then removes the rows. A claimed intent resolves with `ResolveClaimed`; the unfenced path runs only for an intent with no claim (`ClaimedAt` nil), and is the only caller of the re-creating activation. |
| A holder owns every candidate row of an orphan generation while the real #10820 purge is parked mid-flight (it deleted the evidence rows and waits on the candidates) (`TestSweepYieldsToAPurgeInFlightLive`) | The batch skips that generation as `lock_busy` without waiting (`lock_timeout` 0), leaves every row, and sweeps the disjoint generation in the next batch; the purge completes once the holder lets go. |
| Purge arrives while the batch holds the generation row (`TestPurgeArrivingWhileTheBatchHoldsTheGenerationRowLive`: the batch resumes 0, 300, 700, 800 and 1200 ms after the purge queued behind it) | At every delay the purge completes and the repository is retired, neither side sees `40P01`, and the batch counts the generation `lock_busy` (`lock_timeout` 0, nothing committed). On the first design the same table gave a purge `40P01` at 700 and 800 ms and a batch `lock_timeout` at the others. |
| The other arrival order: the batch has deleted the evidence rows when the purge starts (`TestPurgeArrivingWhileTheBatchHoldsChildRowsLive`) | The purge waits on the batch's rows, the batch carries on and commits the whole generation, the purge wakes to nothing left and completes. Neither side is a deadlock victim and nothing is skipped. |
| The purge rolls back (`TestSweepSweepsAGenerationAfterTheHoldingPurgeRollsBackLive`) | The first pass leaves every row and counts `lock_busy`; after the purge is cancelled the rows are restored, and the next pass sweeps the generation. |
| Rows of a generation with no generation row held by another session (`TestRowsHeldOnAGenerationWithoutAGenerationRowAreNotCountedSweptLive`) | The batch deletes the other kind of rows, skips the held rows, counts the generation `lock_busy`, never swept, and the next pass finishes it. Dropping the evidence or the candidate branch of the settle read turns it red. |
| A generation larger than the batch row limit, with a limit of 1,000 (`TestGenerationLargerThanTheRowLimitIsDrainedInBoundedBatchesLive`, two cases so each picking statement is the one that stops a batch) | Evidence larger: batches of 2,000, 1,203 and 512 rows. Candidates larger: 2,000, 1,510, 1,000 and 505. No batch deletes more than 2,050 rows, both generations are swept once, nothing is counted `lock_busy` (a cut generation is run again, not skipped). A limit written as `LIMIT $2 * 100000` on either statement, a limit argument that ignores the test knob, a cut that is never set, set only for one statement, dropped from the result or counted as busy each turn it red (mutants below). |
| Rows of a large generation held by another session: 100 of 2,500 evidence rows, limit 1,000 (`TestLargeGenerationWithSomeRowsHeldEndsBusyOnceLive`) | The limit counts rows taken, never the held rows it skips: batches of 1,000, 1,000 and 400, the generation counts `lock_busy` once, at the end, and keeps its generation row and the 100 held rows; the next pass sweeps it. |
| ACCESS EXCLUSIVE on `relationship_evidence_facts` held by another session when the batch's evidence delete starts (`TestRelationLockHeldByAnotherSessionEndsTheBatchOnTheLockTimeoutLive`) | The one wait the batch cannot skip: it returns in about 500 ms with every generation of the batch counted `lock_timeout` and every row intact; the next pass sweeps both. `lock_timeout` 0 (the sweep waits for the holder) and 5 s (it waits that long) turn it red. |
| Claimed work item on an old superseded generation, real prune | Prune and sweep both keep it (the scope generation row exists). After the work item succeeds the prune deletes the scope generation row and only then does the sweep remove the relationship rows; a newer retained generation is untouched. |

The operator gate adds a check that no other path deadlocks during a real drain: the
`pg_stat_database.deadlocks` delta over the drain window must be 0.

## Served contract

`TestSweptResolvedIDAnswersTheExistingNotFoundLive` runs the real evidence
drilldown handler over the real `ContentReader` read model. Three `resolved_id`
values (one of a retained generation, two of a generation the sweep removes)
answer 200 with the row before the sweep. After it, the retained id still
answers 200 and both swept ids answer exactly what an id that never existed
answers: 404 `{"detail":"relationship evidence not found","error":"Not Found"}`.
The swept generation's `resolved_relationships` rows are still there (two), so
the 404 comes from the read model's inner join to `relationship_generations`.

## Mutation runs

Each mutant edits one statement or line of production code and runs the focused
tests with `-p 1` (the live ones on PostgreSQL 18.6, mostly with `-failfast`,
which is why a failing list can be short). A scratch runner, not in the tree,
expects the focused run to fail, restores the file and checks that `git status
--short <file>` prints nothing. The 57 definitions were regenerated against the
rebased tree (each `old` text occurs once in its file) and run at head
`bdc6d22fd1`, before the comment-only commit that follows it. The statement texts
hash the same at both, and the last survival check ran on the tree with that
commit's comment edits. 51 are red on a failing assertion and 6 are survival
checks that stay green (below). Two needed a second run to count.
`b-cut-counts-busy` first failed to build (`declared and not used`), was rewritten
as `if cut && false` and came back red. `pl-cand-no-index` first came back red
only because the plan test's five-minute fixture deadline ran out on a loaded host
(its `VACUUM` timed out); the second run failed on the candidates delete's plan.

| Surface | Red mutants |
|---|---|
| Rule A predicates (12) | evidence delete without the `scope_generations`, pointer or `active` predicate (3); candidates delete without each (3); classify without each (3, caught by live tests as well as the SQL text pin); generation row delete without status, `scope_generations` or pointer (3, killed by the SQL text pin alone, see below) |
| Row locks (7) | evidence delete without `SKIP LOCKED`; candidates delete without `SKIP LOCKED`; classify without `SKIP LOCKED`; classify without `FOR UPDATE`; status test removed from the locked CTE; `lock_timeout` 0; `lock_timeout` 5 s |
| Generation row guards (3) | no evidence and candidate guard; no evidence guard; no candidate guard |
| Settle read (3) | without the evidence, the candidate or the generation branch (the generation branch is killed by its SQL text pin alone, see below) |
| Plans (3) | each of the three deletes made unable to use its index |
| Row limit (3) | evidence delete with `LIMIT $2 * 100000`; candidates delete with the same; the evidence one again with the SQL text pin not selected, so the live test alone kills it |
| Batch (12) | deletes take the batch, not the claimed ids; generation row deleted first; settle read skipped; swept ignores what is left; busy ignores what is left; an empty batch commits; the limit argument ignored; a cut never set, set for evidence only, set for candidates only; continued generations dropped; a cut counted as busy |
| Pass (4) | an uncommitted batch counted; continued generations not re-queued; the budget never checked while continuing; the log line drops `batch_continued` |
| Defaults and cadence (4) | default batch size unbounded (red in the leaf suite, and again in the reducer suite as a second mutant); default batch size plus one; the capped flag not reset after a clean pass |

Survival checks, green on purpose or by finding (6):

- `cad-capped-flag-not-reset-old-suite` and `batch-default-unbounded-old-suite` run
  their package without the one test added for that gap
  (`TestRelationshipSweepCadenceFollowsOnlyTheLatestPass`,
  `TestDefaultBatchSizeBoundsEveryBatch`) and stay green, so each of those tests is
  the only kill for its mutant in its package. The reducer suite also kills the
  second one (`batch-default-unbounded-reducer-suite`, red).
- `p-gen-status-live-only`, `p-gen-sg-live-only` and `p-gen-ptr-live-only` run the
  live guard test alone and stay green. `gen-delete-and-settle-live-only` removes
  all three predicates and the settle read's generation branch together and runs
  every live test of the package (22 test functions, 48 results, none failing); it
  stays green too. So the generation row delete's own status, `scope_generations`
  and pointer predicates, and the settle read's generation branch, are held by their
  SQL text pins alone (`TestDeleteStatementsReassertEveryPredicate`,
  `TestSettleStatementReadsEveryTable`): no live test reaches them. Behind classify
  the status predicate is redundant, because a writer that changes the status needs
  the row lock the batch holds from classify on. The other two matter only when a
  writer commits a `scope_generations` row or a scope pointer between
  `delete_candidates` and `delete_generations`, which the batch's row lock does not
  stop. A test that drives that interleaving through the fault hook would close the
  gap; it is not in this slice.

Earlier misses that now have tests: `adapter drops the deadline` survived until
`TestRelationshipRetentionSweeperHandsTheDeadlineToTheLeaf`, the skipped-retry wrap
until `TestRelationshipBacklogBoundsButNeverLengthensASkippedRetry`, the unbounded
default batch size until `TestDefaultBatchSizeBoundsEveryBatch`, and a capped flag
that is never reset until `TestRelationshipSweepCadenceFollowsOnlyTheLatestPass`.

The served-contract, cadence, rollout and telemetry mutants of the earlier runs
(29: the read model's inner join turned outer; accelerate after a failure, after a
lock timeout or after a clean pass; never shorten the idle wait; shorten replaces
the wait; idle wait and skipped-retry wait unwrapped; due check on the poll
interval only; deadline not a quarter of the poll interval; request always deletes
or drops the row cap; census skipped when deletes are off; config default on or
never read; adapter drops or forces `Deletes`, drops the row cap or the deadline;
wiring skips the sweeper; gauge not recorded on failure or skipping zero; failure
reason renamed or class dropped; census phase not recorded; sweep phase
mislabelled; the rows, skips and cap counters not recorded) were red when they ran
at an earlier head. They were not re-run (see Not checked).

## Not checked

- No dedicated-host run: every timing is from a shared laptop. The calm window
  (load average about 20) gave the best-case figures, the same statements ran 27 to
  63 times slower at load averages of 55 to 85, and in the random-layout run I could
  not separate layout from load. Read a timing as a best case.
- No production-shaped corpus: the rows are synthetic, ids are short, and the
  cluster is a single laptop-class node. It ran with `fsync=off` and
  `synchronous_commit=off`, so a production commit adds its WAL flush (and a
  synchronous replica's acknowledgement) to every batch time here.
- No replica, so no replica lag, and no `wal_level=logical` or compression run.
- S5 (3.6 million evidence rows) was not re-run on the shipped statements. Its WAL
  figures (1.7 and 4.3 KB per row) predate the row lock record each delete now
  writes. The 55 B per row measured at S1 would make them about 3% larger, which is
  derived, not measured at S5; its EXPLAINs and batch times are the first design's.
- A drain in a shared environment has not run; the default stays off until the
  operator gate in the telemetry reference page is recorded, including the
  `pg_stat_database.deadlocks` delta of 0.
- Hub generations scattered over a large table were not measured for a plan flip;
  the purge note measured one at 1.43%.
- The purge's request-level derive-lock budget is merged (#10853, `f9cfd99c34`) and
  the interplay tests ran against it. They do not assert the purge's own timing. The
  budget bounds the purge's advisory-lock waits, not its row waits, so a purge that
  waits for a batch's rows is bounded by that batch alone.
- No live test drives a `scope_generations` row or a scope pointer arriving between
  `delete_candidates` and `delete_generations`. The generation row delete's own
  status, `scope_generations` and pointer predicates and the settle read's
  generation branch are held by SQL text pins alone (mutation runs above).
- The cadence, rollout, telemetry and served-contract mutants (29) ran at an
  earlier head and were not re-run at this one. The production files they edit are
  unchanged since `fff6b876a7` apart from comments and one environment registry
  description string (`git diff fff6b876a7 HEAD` on those files); the tests that
  kill them are unchanged or only gained cases (two reducer test files and one
  line in the telemetry contract test), and they pass at this head.
