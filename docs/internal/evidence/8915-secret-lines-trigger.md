# 8915: Secret-Line Update Trigger On Unchanged Re-Upserts

Diagnosis for #8915, phase 1. No production code changed. The slow mode in the
issue (4 to 13 times slower, 4 of 16 rounds) did **not** reproduce, so its
cause is **NOT PROVEN**. The run did show where the time goes on an unchanged
re-upsert, and it is not the migration-138 trigger.

## Decision, threshold, budget

| Item | Value |
|---|---|
| Decision | Is the slow unchanged re-upsert a plan flip in the trigger, detoast cost, or something else? |
| Threshold | Reproduce a slow round in at least 30 runs and show a node-level difference between a fast and a slow round |
| Time-box | 60 minutes, UTC 02:42 to 03:42 |
| Result | Slow round not reproduced (max/min 1.47x). Trigger share of wall is 1.7%. |

## Setup

| Item | Value |
|---|---|
| Database | `ghcr.io/eshu-hq/ci-postgres-bookworm` at the digest pinned in `live-postgres-readiness.yml` (PostgreSQL 18.6, aarch64) |
| Settings | Container defaults: `shared_buffers` 128MB, `work_mem` 4MB, `max_wal_size` 1GB. `track_functions=all`. `auto_explain` preloaded. |
| Schema | Real migrations (229 applied) through `ApplyBootstrap` |
| Writer | The real `ContentWriter.Write`, one connection, one batch statement of 50 files (batch limit is 500) |
| Corpus | 50 files, each 500,000 bytes, 491 finding lines each (24,550 `content_file_secret_lines` rows). Synthetic: random words plus `password = "..."` lines. |
| Rounds | 1 seed write, then 36 unchanged re-upserts of the same repository |
| Sampling | `pg_stat_activity` every 50 ms during each round. `pg_stat_user_functions` and `pg_stat_database` deltas per round. |
| Host | Shared, 16 cores. Load average was about 4 at the start and rose to 40 to 70 during the later experiments. |

The corpus is not the one from the #8814 review (that scratch area was not
available), so absolute times are not comparable to the issue's table.

## Result 1: distribution over 36 unchanged rounds

Wall time per round (ms, in order): 9451 9127 9766 9567 10189 9820 9084 9056
11105 9252 9233 9165 9206 9712 12371 9265 13317 9926 11484 10264 11533 9653
11295 10017 9151 9345 12481 10177 9924 9779 11170 9400 9091 11303 10048 10898.

| Statistic | Seconds |
|---|---|
| min | 9.056 |
| median | 9.780 |
| max | 13.317 |
| max / min | 1.47 |

No round came near 4 to 13 times the median. The seed (insert) round took
11.7 s.

Per-round trigger time (`content_file_secret_lines_derive_updated`, from
`pg_stat_user_functions`): min 151 ms, median 168 ms, max 808 ms. At the median
that is 1.7% of the round's wall time.

Every unchanged round wrote 257,056,873 temp bytes (the same number each round).

## Result 2: what the slow rounds have in common

Correlation of round wall time with each signal, over the 36 rounds:

| Signal | Pearson r |
|---|---|
| Samples waiting on `LWLock:WALWrite` | 0.83 |
| `blks_read` delta | 0.41 |
| Trigger function time | 0.46 |
| Samples on CPU | 0.20 |

Fastest and slowest rounds side by side:

| Round | Wall ms | WALWrite samples | `blks_read` | Trigger ms |
|---|---|---|---|---|
| 07 (fastest) | 9056 | 3 | 4,559 | 183 |
| 16 (slowest) | 13317 | 56 | 95,287 | 165 |

The slow rounds wait on WAL writes and storage reads. Their trigger time is the
same as the fast rounds. A WAL-triggered checkpoint also ran during the
experiments (`checkpoint complete ... write=78.307 s`).

## Result 3: where the statement time goes

The writer's batch statement was replayed inside a rolled-back transaction, with
parts of the schema dropped inside that transaction. Five repetitions per
variant, seconds per statement. The host load was 40 to 70 during this matrix,
so compare variants by ratio, not absolute value.

| Variant | min | median | max |
|---|---|---|---|
| A: full schema | 6.3 | 7.2 | 10.1 |
| B: secret-lines update trigger dropped | 6.7 | 7.5 | 8.4 |
| C: both secret-lines triggers dropped | 7.0 | 8.2 | 10.2 |
| D: both content GIN trigram indexes dropped | 1.2 | 2.1 | 3.3 |
| E: secret-lines triggers and GIN indexes dropped | 1.0 | 1.3 | 1.9 |
| F: version-fence triggers dropped | 9.1 | 10.1 | 12.0 |
| G: all those triggers and GIN indexes dropped | 0.8 | 1.1 | 1.6 |
| H: `content_files_language_repo_idx` dropped | 6.3 | 6.4 | 9.1 |
| A again, same session as H | 6.8 | 7.0 | 8.4 |

Reading: dropping the secret-lines triggers (B, C) moves nothing outside noise.
Dropping the two GIN trigram indexes on `content` and `content_lower` (D) cuts
the statement from about 7 s to about 2 s. Variant F ran slower than A, which is
noise from the loaded host and not a real cost of removing triggers.

Heap-only-tuple (HOT) updates: `pg_stat_user_tables` showed 500 updates from
these replays and no HOT updates (`n_tup_hot_upd` stayed 613). Dropping the
covering index in variant H did not change the statement time, so this run does
not show why the updates are not HOT. The replays were rolled back, which can
leave dead tuples on the page. NOT PROVEN.

## Result 4: trigger plans (auto_explain, log_analyze, log_buffers)

One replay of the batch statement, 5.96 s in total. Trigger-internal statements:

| Statement | Time | Plan |
|---|---|---|
| `DELETE ... USING new_rows` (update trigger) | 143.8 ms | Hash Anti Join of two Named Tuplestore Scans, estimate 50 rows each, `Batches: 32 (originally 1)`, 8.8MB hash memory. The `content_file_secret_lines_pkey` scan was `never executed`. |
| `changed` CTE and derive insert | 11.7 ms | Hash Anti Join, 0 rows out. The regex filter and `regexp_split_to_table` were `never executed`. |
| Version-fence insert | 4.0 ms | HashAggregate over the transition table, 1 row |
| Insert trigger (empty `new_rows`) | 0.001 ms | |
| Outer `INSERT ... ON CONFLICT` on `content_files` | 5,799 ms | 562,963 shared buffer hits, 6,281 dirtied, 50 conflicting tuples |

An earlier replay in the same session showed the same shape: the same `DELETE`
took 149.5 ms and the `changed` CTE 12.4 ms.

Observations about the plan:

- The planner knows the row count of a transition table (50) but not its real
  width. It assumes about 282 bytes per row. The real rows carry 500,000 bytes of
  detoasted `content`, so the hash starts as one batch and spills to 32.
- The comparison `o.content IS NOT DISTINCT FROM n.content` over 50 detoasted
  500KB values is part of the 143.8 ms node. That bounds the detoast and compare
  cost on this corpus to about 3 ms per file.
- The derive step does not run the regex on an unchanged re-upsert. The
  materialized fence in migration 138 works as designed.

## Verdict on each theory

| Theory | Verdict | Observation |
|---|---|---|
| Plan flip over transition tables | NOT PROVEN | The row estimate is right and the width estimate is wrong, but across 36 rounds trigger time stayed between 151 and 808 ms. A flip to a 78 s plan did not occur. Per-round plans were not captured, only two replays. |
| Detoast cost in `IS NOT DISTINCT FROM` | Ruled out as the dominant cost | The whole `DELETE` node, compare included, is 144 ms for 50 files. |
| Trigger is the common factor | Not supported here | Removing the secret-lines triggers does not change the statement time. The trigger is 1.7% of wall. |
| Time lies in index maintenance and WAL | Supported | Dropping the two GIN trigram indexes removes about 70% of the statement time. Slow rounds track `WALWrite` waits (r = 0.83). |

The issue's slow mode itself was not reproduced, so what made 4 of 16 rounds
slow there is **NOT PROVEN**. The issue's run differed from this one in the
corpus, the host load, and the number of phases per round.

## Not checked

- The original #8814 corpus and harness.
- Per-round `auto_explain` plans across all 36 rounds.
- Concurrent writers of the same repository.
- A batch of 500 files (about 250MB of transition tuples per side).
- Why the replayed updates are not HOT.
- Any fix. None is proposed: the cause of the slow mode is not proven.

## Reproduce

1. Start the digest-pinned PostgreSQL 18.6 image with
   `shared_preload_libraries=auto_explain` and mount the volume at
   `/var/lib/postgresql`.
2. Run `ApplyBootstrap` against a fresh database.
3. Build 50 records of 500,000 bytes with 491 secret-like lines each, write them
   once through `NewContentWriter(...).Write`, then write the identical
   materialization 36 more times.
4. For plans, run the writer's `INSERT ... ON CONFLICT DO UPDATE` shape
   (selecting from the table itself) in a transaction with
   `SET LOCAL auto_explain.log_min_duration = 0`, then `ROLLBACK`.
