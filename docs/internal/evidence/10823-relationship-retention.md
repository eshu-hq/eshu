# #10823 relationship retention sweep: performance and observability evidence

The reducer's generation-retention runner now takes a read-only census of
orphaned relationship generations on every due pass and, only when
`ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ENABLED` is set, deletes their
evidence, candidate and generation rows in small committed batches. This note
is the tracked record the performance-evidence gate requires. It holds the
measurements behind the batch size, the row cap, the census timeout and the WAL
guidance, the lock probes, and the mutation runs.

Performance Evidence: every figure below comes from one throwaway PostgreSQL
18.6 server on a shared development laptop (host load average 19 to 30 for the
first runs, 26 to 56 for the re-runs), the real migrated schema, synthetic rows.
A 50-generation batch (about 2,650 rows) commits in 24 to 43 ms at p50 when a
generation's rows sit together or interleave by id, and in 48 to 104 ms when they
are scattered at random. Its p95 ranged from 28 to 312 ms between runs, and two
runs per layout cannot separate layout from host load. The census is one pass
over each table's generation ids: 0.2 to 0.3 s warm (0.5 to 1.2 s on a first
pass) on 3.6 million evidence rows with no orphans, 2.4 to 8.5 s with 2 million
orphan rows among 5.6 million. The re-runs on the busier host were slower: a
no-orphan census pass took up to 17 times as long (823 ms against 49 ms), so read
every timing as a best case. WAL per deleted row was steadier: 281 to 305 B in 11
of the 12 passes whose rows sat together or interleaved by id (one pass wrote
475 B, and I could not say why), and 0.07 to 4.3 KB for rows scattered at random,
depending on the table size and on whether a checkpoint came first. Nothing was
measured on a production corpus or against a replica.

Observability Evidence: four instruments (`eshu_dp_relationship_retention_orphan_generations`,
`..._rows_swept_total`, `..._generations_skipped_total`, `..._capped_total`), two
phases on `eshu_dp_generation_retention_phase_duration_seconds`, the failure
reason `relationship_orphan_sweep`, the span `generation_retention.relationship_sweep`
and four log lines. The reducer retention tests read each metric back from a
manual reader, including the zero gauge and the failure path; 9 telemetry
mutants turn them red (table below). Operator reading guide:
`docs/public/reference/telemetry/generation-retention-relationships.md`.

## Setup

- PostgreSQL 18.6 (Homebrew), throwaway cluster, `wal_level=replica`,
  `full_page_writes=on`, `wal_compression=off`, `shared_buffers=128MB`,
  `checkpoint_timeout=5min`, `max_wal_size=1GB`, `fsync=off`,
  `synchronous_commit=off`. The last two lower commit latency, not WAL volume.
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

The first S1 and S5 runs below came from an uncommitted scratch copy of that
probe. It had the same seed statements, EXPLAINs and sweep passes; the commit
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
  the together layout the passes took 209 to 531 ms against 162 to 235 ms. A busy
  host eats into the margin. Past roughly 40 million evidence rows with the slow
  plan (a linear extrapolation from 5.6 million) it would fail the pass, and
  nothing would be swept; there is no knob for it.
- The sql.go comment quoted the design's 0.7 to 1.0 s on 3.3 million evidence
  rows. This run measured 0.2 to 0.3 s warm and 0.5 to 1.2 s on a first pass on 3.6 million rows,
  so the design figure sits between the cold and warm runs. The comment now
  cites these runs.

## Delete batches

A batch of 50 orphan generations: 40 evidence, 12 candidate and 1 generation row
each, about 2,650 rows. `EXPLAIN (ANALYZE, BUFFERS)` of each statement on S1 /
S5 (together) / S5 (random), with the 50 ids bound:

| Statement | S1 | S5 together | S5 random | Plan |
|---|---|---|---|---|
| delete evidence (2,000 rows) | 13.3 ms | 35.6 ms | 51.5 ms | Index Scan `relationship_evidence_generation_idx`, `generation_id = ANY`, hash anti join of `scope_generations`, index probes of `ingestion_scopes` and `relationship_generations` per row |
| delete candidates (600 rows) | 8.9 ms | 8.5 ms | 9.8 ms | the same on `relationship_candidates_generation_idx` |
| delete generations (50 rows) | 0.43 ms | 0.29 ms | 0.28 ms | Index Scan `relationship_generations_pkey` |
| classify (50 ids) | 1.07 ms | 0.97 ms | 1.5 ms | primary key probes |

At S5, 20 of the 35 ms of the evidence delete is the hash build of the 120,000
`scope_generations` rows. The planner takes that plan only while it is cheaper
than about 2,000 index probes, so it does not grow without bound.

Committed batch durations over the 38 batches of a 100,700-row pass:

| Run | p50 | p95 | max |
|---|---|---|---|
| S1 together | 25.6 / 25.8 ms | 30.8 / 28.5 ms | 32.4 / 32.0 ms |
| S5 together | 31.0 / 27.4 ms | 39.3 / 33.7 ms | 47.1 / 35.0 ms |
| S1 by id, first attempt (no `ORDER BY`) | 23.5 / 24.5 ms | 37.0 / 30.4 ms | 48.1 / 33.1 ms |
| S1 scattered by id (`ORDER BY i, k`) | 41.1 / 28.4 ms | 133.0 / 110.4 ms | 250.2 / 300.5 ms |
| S5 random | 72.2 / 48.2 ms | 97.3 / 59.5 ms | 98.5 / 60.0 ms |

(Each cell is the first pass, then the second.) The first by-id attempt had no
`ORDER BY`, so the planner reordered the cross join and the rows landed as in the
together layout; its WAL (281 and 305 B per row) says so. The `ORDER BY i, k` run
has the same per-row WAL (281 and 287 B) but a p95 of 133 and 110 ms against 37
and 30 ms. A whole 100,700-row pass took 1.1 to 1.2 s at S1 (1.8 and 2.3 s in the
`ORDER BY i, k` run), 2.7 to 3.0 s at S5 (1.7 to 1.8 s of it the census), and 9.8
to 10.6 s in the S5 random layout (7.9 s of it the census).

Re-runs of the committed probe at S1, one run per layout, at host load averages
of 26 to 56 (first pass, then second):

| Layout | p50 | p95 | max | Whole pass | WAL per row | Full page images |
|---|---|---|---|---|---|---|
| together | 39.9 / 42.6 ms | 240.1 / 206.0 ms | 437.4 / 415.8 ms | 3.1 / 2.8 s | 281 / 287 B | 2,686 / 2,685 |
| scattered by id | 36.0 / 37.3 ms | 88.6 / 104.1 ms | 100.8 / 113.7 ms | 1.9 / 2.0 s | 281 / 287 B | 2,045 / 2,899 |
| random | 68.3 / 103.9 ms | 278.9 / 312.1 ms | 408.1 / 322.5 ms | 5.3 / 5.2 s | 1,220 / 71 B | 13,925 / 454 |

The same together corpus measured a p50 of 25.6 ms and a p95 of 30.8 ms in the
first run and 39.9 ms and 240.1 ms in the re-run, 1.6 and 7.8 times as long. The
host was busier during the re-run, but the runs differ in more than load, so the
batch times cannot separate a layout effect from load or from the run: do not
tune on a p95 taken on this laptop. The WAL per row is the steadier signal:
together and by id match from run to run (one 475 B pass aside), and random does
not.

A hub batch of 50 generations of 1,000 rows (50,000 rows, 4.5% of the S1 table,
0.9% of S5) kept a Bitmap Heap Scan on the generation index in every run: 32 to
113 ms at S1 (five runs), 291 ms at S5 random. No flip to a sequential scan was
seen.
The #10820 purge note measured a flip at 1.43% of a 1.4 million row table for
its layout; if a batch does flip, it costs a table scan and still deletes that
share of the table.

### Plan pin

`TestRelationshipRetentionStatementsAreIndexServedLive` seeds 400,000 evidence
rows (8,000 live generations plus 100 orphans), runs `VACUUM (ANALYZE)` and
requires each delete to read its table through its generation index or primary
key with no Seq Scan, for the custom plan and for the generic plan
(`plan_cache_mode = force_generic_plan`) a pooled driver serves after five runs.
Three mutants that stop each delete using its index (`generation_id || ''`) turn
it red with `X is read with a Seq Scan, want a read through ...`.

## WAL

100,700 rows (38 batches, 1,900 generations) per pass, measured as the
`pg_current_wal_insert_lsn` difference around the pass, with `pg_stat_wal` full
page images. Pass 1 follows a `CHECKPOINT`, so every first touch of a page pays
a full page image; pass 2 follows directly.

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

The second pass of the first S1 together run wrote 47.9 MB (475 B per row, 4,588
full page images against 1,900 in its first pass). A checkpoint landing during
the pass would explain the extra images, but that run did not record one, and the
re-run of the same layout measured 281 and 287 B.

The cost depends on how many distinct pages the swept rows touch since the last
checkpoint, not on the row count. Rows that sit together share pages (about 0.3 KB
per row). Rows scattered at random over a large table pay close to a page each:
1.7 KB per row on the second S5 pass and 4.3 KB on the first, right after a
checkpoint. At S1 the random layout cost 1.2 KB per row on the first pass and 0.07
KB on the second, whose full page images fell from 13,925 to 454: most pages it
touched had been imaged by the first pass. The design's planning figure of 1.7 KB
per row is the S5 second random pass. By-id order did not scatter the S1 rows
across pages (the same WAL per row as together), consistent with the swept ids
being adjacent in each interleaved stripe.

The design text also gave a 50-generation batch as about 1,700 rows, 1.43 MB of
WAL and 1.7 KB per row. 1.43 MB over 1,700 rows is 0.84 KB per row, so those
figures cannot describe one batch. The row cap and the WAL guidance now rest on
the per-row figures above. The runbook keeps 1.7 KB as the planning figure and
gives 0.3 KB (rows together) and 4.3 KB (right after a checkpoint) as the ends.
`wal_compression` was off here; it shrinks full page images. Replica lag was not
measured: the cluster has no replica.

## Lock probes (live PostgreSQL 18)

| Probe | Result |
|---|---|
| A generation stops being an orphan after the census: a scope generation row appears, its relationship generation turns active, or a scope pointer names it | Skipped as `ineligible`, nothing swept, every row intact. Each of the three predicates dropped from classify turns this red. |
| The production activation arrives while a batch holds the generation row (hook after the evidence delete) | It queues behind the row; the batch commits whole; the activation then re-creates the row, empty. Without `FOR UPDATE` in classify it never waits (red). |
| Activation race, 40 rounds started together | The activation updated the existing row 40 times and re-created it 0 times. |
| Activation race, 60 rounds staggered 0 to 11 ms | 9 updates, 51 re-creations after the sweep. Raw observation: every outcome was consistent (children intact on an update, none on a re-creation, never half). |
| Claim-fenced activation of an orphan id | A work item for it cannot exist (foreign key 23503), so the fenced activation returns `ErrExecutionClaimRejected` and touches nothing; the sweep then removes the rows. A claimed intent resolves with `ResolveClaimed`; the unfenced path runs only for an intent with no claim (`ClaimedAt` nil), and is the only caller of the re-creating activation. |
| Real #10820 purge parked mid-flight (holds evidence row locks, waits on a candidate row) | The batch waited 500 ms, rolled back as `lock_timeout` (returned after 513 to 514 ms), the disjoint generation in the next batch was swept, the purge finished. |
| Purge arrives while a batch holds the generation row (cycle) | The batch ended on its own `lock_timeout` before the deadlock detector; the purge completed; neither side saw 40P01. With `lock_timeout` at 5 s the purge is the deadlock victim (`retirement purge delete relationship_generations: deadlock detected`). With it at 0 the sweep waits without bound (20 s context expired). |
| Claimed work item on an old superseded generation, real prune | Prune and sweep both keep it (the scope generation row exists). After the work item succeeds the prune deletes the scope generation row and only then does the sweep remove the relationship rows; a newer retained generation is untouched. |

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

Each mutant edits one statement or line, runs the focused tests with `-p 1`, and
restores the file. All are red. Names are the mutant, then the first failing
test.

| Surface | Mutants (all red) |
|---|---|
| Classify and delete predicates (storage leaf) | drop `scope_generations`, `active` or pointer predicate from every statement (3); from classify alone (3, now caught by a live test, not only by the SQL text pin); evidence delete without pointer; evidence delete without `active`; candidates delete without `scope_generations`; generations delete without status; classify without `FOR UPDATE`; no `SKIP LOCKED`; deletes take the batch, not the claimed ids; batch split across transactions; batch bound removed |
| Locks | `lock_timeout` 0; `lock_timeout` 5 s; generation row deleted first |
| Plans | the three deletes made unable to use their index |
| Served contract | the read model's inner join to `relationship_generations` turned into an outer join (`query/evidence_read_model.go`, restored) |
| Cadence (reducer retention) | accelerate after a failure; after a lock timeout; after a clean pass; never shorten the idle wait; shorten replaces the wait; idle wait unwrapped; skipped-retry wait unwrapped; due check on the poll interval only; deadline not a quarter of the poll interval |
| Rollout | request always deletes; request drops the row cap; census skipped when deletes are off; config default on; config never reads the flag; adapter drops or forces `Deletes`; adapter drops the row cap; adapter drops the deadline; wiring skips the sweeper |
| Telemetry | gauge not recorded on failure; gauge skips zero; failure reason renamed; failure class dropped; census phase not recorded; sweep phase mislabelled; rows, skips and cap counters not recorded |

Two of these were missing at first and now have tests:
`adapter drops the deadline` survived until
`TestRelationshipRetentionSweeperHandsTheDeadlineToTheLeaf`, and the
skipped-retry wrap survived until
`TestRelationshipBacklogBoundsButNeverLengthensASkippedRetry`.

## Not checked

- No quiet-host run: every timing is from a shared laptop at host load averages
  of 19 to 56, and a no-orphan census pass took up to 17 times as long in a
  re-run as in the first run. WAL per row was steadier (see the WAL section).
- No production-shaped corpus: the rows are synthetic, ids are short, and the
  cluster is a single laptop-class node under load with `fsync=off`.
- No replica, so no replica lag, and no `wal_level=logical` or compression run.
- A drain in a shared environment has not run; the default stays off until the
  operator gate in the telemetry reference page is recorded.
- Hub generations scattered over a large table were not measured for a plan
  flip; the purge note measured one at 1.43%.
- The #10841 derive-lock budget commit was not present; the probes do not assert
  the purge's own timing.
