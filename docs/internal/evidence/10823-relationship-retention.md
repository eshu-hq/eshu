# #10823 relationship retention sweep: performance and observability evidence

The reducer's generation-retention runner now takes a read-only census of
orphaned relationship generations on every due pass and, only when
`ESHU_GENERATION_RETENTION_RELATIONSHIP_SWEEP_ENABLED` is set, deletes their
evidence, candidate and generation rows in small committed batches. This note
is the tracked record the performance-evidence gate requires. It holds the
measurements behind the batch size, the row cap, the census timeout and the WAL
guidance, the lock probes, and the mutation runs.

Performance Evidence: every figure below comes from one throwaway PostgreSQL
18.6 server on a loaded local development machine (host load average 19 to 30),
the real migrated schema, synthetic rows. A 50-generation batch (about 2,650
rows) commits in 24 to 72 ms at p50. The census is one pass over each table's
generation ids: 0.2 to 0.5 s on 3.6 million evidence rows with no orphans, 2.4
to 8.5 s with 2 million orphan rows among 5.6 million. WAL per deleted row is
0.3 KB when a generation's rows sit together and 1.7 KB to 4.3 KB when they are
scattered. Nothing was measured on a production corpus or against a replica.

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
  scattered by id (interleaved, swept in id order) and random (`ORDER BY
  random()` on insert, so the swept rows sit on random pages).

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
- The 60 s census timeout has about seven times the headroom of the slowest run.
  Past roughly 40 million evidence rows with the slow plan it would fail the
  pass, and nothing would be swept; there is no knob for it.
- The sql.go comment quoted the design's 0.7 to 1.0 s on 3.3 million evidence
  rows. This run measured 0.2 to 0.5 s warm and 1.2 s cold on 3.6 million rows,
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
| S1 scattered by id | 41.1 / 28.4 ms | 133.0 / 110.4 ms | 250.2 / 300.5 ms |
| S5 random | 72.2 / 48.2 ms | 97.3 / 59.5 ms | 98.5 / 60.0 ms |

(Each cell is the first pass, then the second.) A whole 100,700-row pass took
1.2 s at S1, 2.7 to 3.0 s at S5 (1.7 s of it the census), and 9.8 to 10.6 s in
the S5 random layout (7.9 s of it the census).

A hub batch of 50 generations of 1,000 rows (50,000 rows, 4.5% of the S1 table,
0.9% of S5) kept a Bitmap Heap Scan on the generation index: 39 ms (S1 together),
32 ms (S1 scattered), 291 ms (S5 random). No flip to a sequential scan was seen.
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
| S1 together | 1 | 28.3 MB | 281 B | 1,900 |
| S5 together | 1 / 2 | 28.3 / 28.9 MB | 281 / 287 B | 2,461 / 3,233 |
| S1 scattered by id | 1 / 2 | 28.3 / 28.9 MB | 281 / 287 B | 2,613 / 1,407 |
| S5 random | 1 / 2 | 436.0 / 170.9 MB | 4,329 / 1,697 B | 46,312 / 18,612 |

An S1 together run whose second pass ran after a checkpoint the server started
itself measured 47.9 MB (475 B per row). The cost depends on how many distinct
pages the swept rows touch, not on the row count: rows that sit together share
pages (about 0.3 KB per row), rows scattered over a large table pay about a page
each (1.7 KB per row, 4.3 KB right after a checkpoint). The design's planning
figure of 1.7 KB per row is the second random pass. The sweep id order did not
scatter the S1 rows because the swept ids were adjacent in the heap.

The design text also gave a 50-generation batch as about 1,700 rows, 1.43 MB of
WAL and 1.7 KB per row. 1.43 MB over 1,700 rows is 0.84 KB per row, so those
figures cannot describe one batch. The row cap and the WAL guidance now rest on
the per-row figures above. The runbook keeps 1.7 KB as the planning figure and
states the 0.3 to 4.3 KB range. `wal_compression` was off here; it shrinks full
page images. Replica lag was not measured: the cluster has no replica.

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

## Mutation runs

Each mutant edits one statement or line, runs the focused tests with `-p 1`, and
restores the file. All are red. Names are the mutant, then the first failing
test.

| Surface | Mutants (all red) |
|---|---|
| Classify and delete predicates (storage leaf) | drop `scope_generations`, `active` or pointer predicate from every statement (3); from classify alone (3, now caught by a live test, not only by the SQL text pin); evidence delete without pointer; evidence delete without `active`; candidates delete without `scope_generations`; generations delete without status; classify without `FOR UPDATE`; no `SKIP LOCKED`; deletes take the batch, not the claimed ids; batch split across transactions; batch bound removed |
| Locks | `lock_timeout` 0; `lock_timeout` 5 s; generation row deleted first |
| Plans | the three deletes made unable to use their index |
| Cadence (reducer retention) | accelerate after a failure; after a lock timeout; after a clean pass; never shorten the idle wait; shorten replaces the wait; idle wait unwrapped; skipped-retry wait unwrapped; due check on the poll interval only; deadline not a quarter of the poll interval |
| Rollout | request always deletes; request drops the row cap; census skipped when deletes are off; config default on; config never reads the flag; adapter drops or forces `Deletes`; adapter drops the row cap; adapter drops the deadline; wiring skips the sweeper |
| Telemetry | gauge not recorded on failure; gauge skips zero; failure reason renamed; failure class dropped; census phase not recorded; sweep phase mislabelled; rows, skips and cap counters not recorded |

Two of these were missing at first and now have tests:
`adapter drops the deadline` survived until
`TestRelationshipRetentionSweeperHandsTheDeadlineToTheLeaf`, and the
skipped-retry wrap survived until
`TestRelationshipBacklogBoundsButNeverLengthensASkippedRetry`.

## Not checked

- No production-shaped corpus: the rows are synthetic, ids are short, and the
  cluster is a single laptop-class node under load with `fsync=off`.
- No replica, so no replica lag, and no `wal_level=logical` or compression run.
- A drain in a shared environment has not run; the default stays off until the
  operator gate in the telemetry reference page is recorded.
- Hub generations scattered over a large table were not measured for a plan
  flip; the purge note measured one at 1.43%.
- The #10841 derive-lock budget commit was not present; the probes do not assert
  the purge's own timing.
