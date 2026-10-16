# Content-version fence for the config-artifact read model (#8155, PR3b-1b)

Refs #8155, #7126. This note records the proof for migrations 186 and 187:
the commit-time content-version fence and the version-0 seed of
`config_artifact_content_versions`. The design is
[Derive repository config artifacts when content is written](../design/7126-ingest-time-config-artifacts.md)
and its protocol companion, section 2. Rulings: arb-8155-s3, arb-8155-f1
(the commit-time redesign after review finding F1) and arb-8155-c1 (the arm 3
bar). PR3b-1b adds no reader, no derive and no read-path change, so it
removes no read time.

## What ships

| File | Content |
|---|---|
| `186_content_files_version_fence.sql` | The UNLOGGED `config_artifact_fence_pending (bumped_xid, repo_id)` table. Four statement-level triggers on `content_files` (INSERT and UPDATE with `NEW TABLE`, DELETE with `OLD TABLE`, TRUNCATE) stage touched repositories. One deferred constraint trigger, `config_artifact_fence_pending_commit`, runs the ordered upsert with the `(bumped_xid, bumped_at)` guard at COMMIT and clears the stage |
| `187_config_artifact_content_versions_seed.sql` | One version-0 row per repository with content, `bumped_xid` `'0'::xid8`, in `repo_id` order, `ON CONFLICT DO NOTHING` |

Decisions beyond the arb-8155-s3 text:

1. **Two files, not one transaction** (coordinator ruling, 2026-10-16). The
   #7961 lock-shape gate (`scripts/lib/migration_lock_shape.py`, no allowlist)
   refuses `CREATE TRIGGER` and `INSERT ... SELECT` in one file. No reader or
   derive exists before PR3b-2, which must deploy after this PR, so the order
   is safe: a write between the files creates its row at version 1 and the
   seed keeps it. Every repository ends with one row. The remaining
   content-write stall is the `CREATE TRIGGER` catalog lock (186 in its final
   statement order: 0.0025 s on 1,000,000 rows); the seed holds ACCESS SHARE
   and blocks no write.
2. **Guard on `(bumped_xid, bumped_at)`.** A logical restore restarts the
   transaction id counter while stored `bumped_xid` values survive, so an
   id-only guard can skip a bump (the PR3b-1a review forward note).
   `TestVersionFenceSurvivesATransactionIDRestartLive` was RED on the id-only
   guard and is GREEN on the pair. No real transaction id is 0, so the seed's
   `'0'::xid8` never blocks the first bump.
3. **Commit-time bump** (arb-8155-f1, finding F1). The first shape bumped
   inside each content statement and held the version row to commit. A
   retention chunk cycle deletes `content_files` twice in one transaction
   (`generationRetentionChunkPruneFilesQuery`, then
   `pruneContentFilesForGenerationsQuery`), so a Write that locked a doomed
   row and then waited on that version row closed a cycle:
   `TestVersionFenceChunkCycleRacingAWriteDoesNotDeadlockLive` deadlocked in
   20 of 20 rounds on the RED commit ("RED for the retention chunk-cycle
   fence deadlock") and passes 20 of 20 now. Version rows are
   taken only inside COMMIT, after every content lock, in `repo_id` order. A
   Write waits only on content rows, and on a version row only inside the
   other side's COMMIT, bounded by that COMMIT (upsert plus WAL flush):
   observed at most 16 ms disjoint and 21 ms overlap, 7x under the
   contended-batch bar (arm 5 below). This replaces ruling 3e of arb-8155-s3
   ("waits on the version row until the retention commit").
4. **F2-F4.** The seed inserts in `repo_id` order (F2). The 186 header states
   that `session_replication_role = replica` skips the triggers as it skips
   every ordinary trigger (F3) and that an UPDATE moving `repo_id` bumps only
   the new repository (F4); hermetic guards keep both out of Eshu's Go code.

## Performance Evidence

Performance Evidence: private PostgreSQL 18.6 in Docker, default server
configuration, retention `SET LOCAL work_mem = '64MB'`. Every run below was
gated: load1 under 9 for 20 s before the run, voided if the 1 s in-run sampler
reached 9. 1 discarded round, 5 measured, interleaved with the first mover
rotating. Arm 3 runs the real `PruneSupersededGenerations` over 100,000 doomed
1 KiB rows in 50 repositories (GIN indexes dropped, as in the accepted shim).
BD is the measured SQL: migration 186's four functions and pending-table DDL
are byte-identical to it; 186 differs only in DDL order and in creating the
constraint trigger when missing (review finding N1), which runs once at
migration time, not per write.

Arm 3 at 1 KiB, means (the arb-8155-c1 statistic), delta from A in seconds and
percent:

| Session (UTC, load1 start/end/max) | Arm | `prune_content_files` | `ScopeLockHold` | Wall | Temp bytes per prune |
|---|---|---|---|---|---|
| S1 15:00-15:03 (5.67/5.12/5.81) | A | 1.1821 s | 2.7454 s | 3.5159 s | 0 |
| | BD | +0.1149 s (+9.72%) | +0.1086 s (+3.96%) | +0.0891 s (+2.53%) | 181,100,000 |
| S2 15:21-15:26 (3.84/4.99/6.73) | A | 1.2026 s | 2.7598 s | 3.5304 s | 0 |
| | B2 (immediate fence) | +0.1693 s (+14.07%) | +0.2392 s (+8.67%) | +0.2403 s (+6.81%) | 181,100,000 |
| | BD | +0.1674 s (+13.92%) | +0.1715 s (+6.21%) | +0.1712 s (+4.85%) | 181,100,000 |
| S3 15:41-15:46 (4.35/5.40/8.06) | A | 1.2115 s | 2.7572 s | 3.5257 s | 0 |
| | BD | +0.1118 s (+9.23%) | +0.1331 s (+4.83%) | +0.1341 s (+3.80%) | 181,100,000 |
| | BDW256 (diagnostic, `work_mem` 256MB) | +0.0619 s (+5.11%) | +0.0760 s (+2.76%) | +0.0750 s (+2.13%) | 0 |
| | BDW (diagnostic, `work_mem` 2GB) | +0.0490 s (+4.04%) | +0.0667 s (+2.42%) | +0.0689 s (+1.95%) | 0 |

The medians read +10.2%, +12.5% and +15.4% for BD `prune_content_files`.
By session means the +10%/+5% bar of arb-8155-s3 was missed in one of three
sessions (S2); by session medians it was missed in all three on
`prune_content_files` and in two on `ScopeLockHold`; replaced by arb-8155-c1:
added `ScopeLockHold` at most +0.25 s per 100,000 doomed rows.
BD adds +0.11 to +0.17 s, so it meets the budget. B2 reads the same or worse,
so the commit-time move costs nothing; the cost is the `OLD TABLE` capture
and its 181.1 MB spill past the 64MB `work_mem` (the diagnostics remove the
spill and recover about 0.05-0.07 s). The diagnostics are not candidates: the
spill fix needs a retention change and is follow-up issue #8268. The earlier single-session reading (+8.26%, `ScopeLockHold`
+4.04%) is one reading, not the headline.

Production estimate: at most the 100,000-row figure (+0.11 to +0.17 s of
`ScopeLockHold`) per retention cycle per reducer replica, hourly by default,
and usually far less, since most cycles prune fewer content rows.

### 8 KiB class (arm 3, A vs the shipped fence, 100,000 doomed rows)

Retention prune, 8 KiB TOAST class: +0.12 s mean added ScopeLockHold per
100,000-row cycle (25 interleaved rounds, three sessions; budget +0.25 s per
arb-8155-c1). Tail: 1 of 12 paired fence rounds was +1.0 s or more over the
control (control 0 of 12); +1.1 to +1.9 s events were seen in 4 of 20 earlier
fence rounds and 0 of 17 control rounds (counted as rounds more than +1.0 s
over the arm's own steady state, excluding session 1 round 5, where both arms
spiked by about +1.9 s (control 16.54 s); that exclusion is the definition in
arb-8155-c3 1.4); cause not attributed, per-backend first use ruled out. The
8 KiB class passes c1 1.4a by the arb-8155-c3 binding rule (pooled S2 =
+0.1202 s over 25 rounds, at most +0.25 s).

`prune_temp_bytes` = 0 in both arms in all 8 KiB rounds (the `OLD TABLE`
keeps TOAST pointers; the 181 MB spill is the 1 KiB class, #8268).
Production pool: retention prunes on the reducer's shared pool
(`go/cmd/reducer/main.go`; `ConnMaxLifetime` 30 min and `ConnMaxIdleTime`
10 min by default in `go/internal/runtime/data_stores.go`), polled hourly
(`go/internal/reducer/maintenance/retention/runner.go`), so no backend that
ran one pass survives to the next.

Sessions 1 and 2 (session 1 at 17:04 UTC, load1 max 3.05; session 2, load1
max 1.47; `ScopeLockHold`, round 0 discarded by the pre-registered rule):

| Session | Round | A | Fence (B2) | Fence minus A |
|---|---|---|---|---|
| 1 | r0 (discarded) | 14.2072 s | 15.8663 s | +1.6591 s |
| 1 | r1 | 14.5892 s | 14.6666 s | +0.0774 s |
| 1 | r2 | 14.6009 s | 14.5101 s | -0.0907 s |
| 1 | r3 | 14.5540 s | 16.4158 s | +1.8618 s |
| 1 | r4 | 14.6977 s | 14.6091 s | -0.0886 s |
| 1 | r5 | 16.5447 s | 17.2230 s | +0.6783 s |
| 2 | r0 (discarded) | 14.2400 s | 15.9093 s | +1.6693 s |
| 2 | r1 | 14.6867 s | 14.5848 s | -0.1019 s |
| 2 | r2 | 14.6885 s | 14.5359 s | -0.1526 s |
| 2 | r3 | 14.5296 s | 14.5810 s | +0.0514 s |
| 2 | r4 | 14.5605 s | 14.6266 s | +0.0661 s |
| 2 | r5 | 14.5367 s | 14.5406 s | +0.0040 s |
| 2 | r6 | 15.0859 s | 14.5647 s | -0.5211 s |
| 2 | r7 | 15.1367 s | 14.7227 s | -0.4140 s |
| 2 | r8 | 14.5818 s | 14.5471 s | -0.0348 s |

Historical figure: the pooled mean over these 13 measured rounds was
+0.1027 s from the six-decimal raw lines (arb-8155-c2's +0.1025 s used
rounded deltas). Session means, not binding: session 1 +0.4876 s, session 2
-0.1379 s. The fence arm's first prune of a session costs about +1.7 s more
ScopeLockHold than the control in the two earlier 8 KiB sessions (round 0,
discarded); the attribution run (r0 to r6, one pinned backend, new backends,
the fence created once) ruled out a per-backend and a per-database first-use
cost; the cause of the fence-arm tail is not attributed.

Attribution run (arb-8155-c2 2.3; 18:18:49-18:27:54 UTC, load1 0.97/1.88,
max 4.69; one prune backend per row, seeding on another connection):

| Round | Arm | pid | `prune_content_files` | `ScopeLockHold` | Wall | stage_old calls / total / self ms | commit calls / total / self ms |
|---|---|---|---|---|---|---|---|
| r0 | B2 | 4772 | 12.9901 | 14.2315 | 14.7182 | 1 / 8.8 / 8.8 | 50 / 2.2 / 2.2 |
| r1 | B2 | 4772 | 13.2056 | 14.3720 | 14.7345 | 1 / 8.6 / 8.6 | 50 / 0.8 / 0.8 |
| r2 | B2 | 4791 | 13.0227 | 14.5220 | 15.2120 | 1 / 8.8 / 8.8 | 50 / 1.0 / 1.0 |
| r3 | B2 | 4791 | 13.9791 | 15.6917 | 16.0528 | 1 / 8.6 / 8.6 | 50 / 0.9 / 0.9 |
| r4 | B2 | 4811 | 13.3128 | 14.9038 | 15.2993 | 1 / 8.6 / 8.6 | 50 / 0.7 / 0.7 |
| r5 | A | 4822 | 13.1471 | 14.6097 | 15.2887 | 0 / 0.0 / 0.0 | 0 / 0.0 / 0.0 |
| r6 | A | 4822 | 13.0916 | 14.2390 | 14.5953 | 0 / 0.0 / 0.0 | 0 / 0.0 / 0.0 |

Verdict lines as printed:

```text
VERDICT 2.4(a) part 1: r2 - mean(r1,r3) = -0.5098 s, bar <= +0.25 s: true
VERDICT 2.4(a) part 2: (r5 - r6) - (r2 - r3) = +1.5403 s, bar within +/-0.25 s: false
VERDICT 2.4: (b) -> the 8 KiB class FAILS the per-cycle budget as production pays it; STOP and return to the arbiter with this table
SIGNATURE (slow = more than +0.25 s ScopeLockHold over the baseline; baseline mean(r1,r3)=15.0319 s)
SIGNATURE per-backend fence cost (r0 and r2 slow, r1/r3 fast, r5 not slow over r6): false
SIGNATURE per-database one-time cost (only r0 slow): false
SIGNATURE harness artifact (second prune of a session, r1 slow vs r3): false
SIGNATURE any first fence fire on the backend (r4 fast with r2 slow): false
```

arb-8155-c2 2.4(a) part 2 compared single rounds at a +/-0.25 s bar below
the round noise already observed and was retired by arb-8155-c3; the table
rules out a per-backend or per-database first-use cost (r0/r2/r4 at
14.23/14.52/14.90 s against a 14.74 s mean of the five B2 rounds; fence
function self time 8.6-8.8 ms and 0.7-2.2 ms).

Deciding session (arb-8155-c3; 18:37:54-19:08:57 UTC, load1 1.50/1.80, max
3.05; 12 interleaved pairs, order AB, BA, ..., no discard; one pinned prune
backend, pid 4880, unchanged in all 24 rounds). Every round pruned 100,000
rows and 50 generations, bumped 50 (fence) or 0 (control), wrote 0 temp
bytes, and showed checkpointer deltas 0/0/0 (timed, requested, buffers
written), vacuum and autovacuum deltas 0/0, and `n_dead_tup` 0 to 100,000.
Fence function self time: stage_old 8.6-8.8 ms, commit 0.8-1.2 ms.
`ScopeLockHold` per pair:

| Pair | Order | A | Fence (B2) | d = fence minus A |
|---|---|---|---|---|
| 0 | AB | 14.6602 s | 16.7703 s | +2.1101 s |
| 1 | BA | 16.0854 s | 15.3584 s | -0.7270 s |
| 2 | AB | 15.6706 s | 15.1747 s | -0.4958 s |
| 3 | BA | 15.6860 s | 15.4194 s | -0.2666 s |
| 4 | AB | 14.5443 s | 14.5635 s | +0.0192 s |
| 5 | BA | 14.5210 s | 14.5847 s | +0.0637 s |
| 6 | AB | 14.8671 s | 14.7574 s | -0.1097 s |
| 7 | BA | 14.5336 s | 15.1464 s | +0.6128 s |
| 8 | AB | 14.5297 s | 14.6427 s | +0.1130 s |
| 9 | BA | 14.6641 s | 14.5517 s | -0.1124 s |
| 10 | AB | 14.5644 s | 14.9892 s | +0.4247 s |
| 11 | BA | 14.6474 s | 14.6839 s | +0.0365 s |

S1 (mean of the 12 d) = +0.1390 s. S2 (pooled over all 25 valid measured 8 KiB
rounds) = +0.1202 s. kB (d > +1.0 s) = 1, kA (d < -1.0 s) = 0, max d +2.1101 s
(pair 0), min d -0.7270 s (pair 1). The one spike row is pair 0: fence
16.7703 s against control 14.6602 s `ScopeLockHold`, every diagnostic zero, no
cause named. Pair 0 ran AB, so the spike is the second prune of the session.

```text
VERDICT arb-8155-c3 2.5: S2 = +0.1202 s <= +0.25 s: the 8 KiB class PASSES c1 1.4a
```

1 KiB round 0, from the existing session logs (no rerun). Fence minus control
in the same round 0: no fence arm shows a +1.7 s event; the largest fence-arm
round-0 delta is +0.71 s hold (`gated-arm3abd.log.2`: B2 3.190 vs A 2.482),
and the one +1.3 s round-0 outlier is the control (`gated-arm3.log.1`: A 4.023
vs BD 2.722). Fence round 0 against that arm's own measured rounds: at most
+0.07 s above the arm's range (session 3 BDW256 2.943 s against 2.792-2.882 s).
Those sessions logged no backend pids.

Arms 1, 2 and 4 (A vs BD, 14:58:01-14:59:39 UTC, load1 7.54/6.33/8.53), the
S3 statistic (medians with ranges; bar +5%): full Write of 5,000 files
1.3444 s vs 1.3610 s (+1.24%); delta Write 2.204 ms vs 2.199 ms (-0.23%);
one 50,000-row INSERT 6.2555 s vs 6.1923 s (-1.01%). All PASS. Means are
+5.02%, +22.66% and +1.20%, pulled up by round 3, where BD read high on all
three arms at once.

Seed timing on the migration 169 fixture shape (1,000,000 rows over 3,000
repositories, all 10 `content_files` indexes), against the 1 s bar: 186
0.0030 s and 187 0.1150 s at 16:11 UTC (load1 3.32 to 3.20), measured BEFORE
the N1 statement reorder; re-timed on the final statement order with
`TestVersionFenceSeedTimingLive` at 18:30 UTC (load1 1.19 to 1.29): 186
0.0025 s, 187 0.1154 s.

### Arm 5 and the arm 1 confirmation (commit-time fence)

Gated, 20 measured rounds per run after 1 discard, n_writes=40 per mode
(2 Writes of 5,000 files per round), sampler tick 5 ms. Load1 maxima: disjoint
1.92 (18:09:18-18:10:42 UTC), overlap 1.74 (18:11:03-18:12:27), arm 1 1.39
(18:12:48-18:13:05). Per-Write medians [ranges]:

| Run | A | B-last | B-first |
|---|---|---|---|
| disjoint | 1.1226 s [1.0989-1.1490] | 1.1248 s [1.0987-1.1600], +0.20% | 1.1233 s [1.0981-1.1559], +0.06% |
| overlap | 1.1398 s [1.0894-1.3342] | 1.1400 s [1.0952-1.3134], +0.02% | 1.1418 s [1.0954-1.3181], +0.18% |

Wait classes, max episode / median round total (seconds); every class not
listed reads 0 / 0 in every mode:

| Run | Mode | `version_row` | `extend` | `xid_unattributed` |
|---|---|---|---|---|
| disjoint | A | 0 / 0 | 0 / 0 | 0 / 0 |
| disjoint | B-last | 0.0156 / 0.0050 | 0.0050 / 0 | 0.0050 / 0 |
| disjoint | B-first | 0.0104 / 0.0050 | 0 / 0 | 0 / 0 |
| overlap | A | 0.0210 / 0.0156 | 0 / 0 | 0 / 0 |
| overlap | B-last | 0.0209 / 0.0156 | 0 / 0 | 0.0050 / 0 |
| overlap | B-first | 0.0210 / 0.0157 | 0 / 0 | 0.0050 / 0 |

`content_row` and `advisory` read 0 / 0 everywhere. Contended-batch bar
(B-last per-Write median / 10): 0.1125 s disjoint, 0.1140 s overlap.
Deadlocks: 0 of 20 in both runs. Per Write, every mode meets A + 10%; B-last
`version_row` meets the contended-batch bar in both runs (arb-8155-c2 4.1).

Two clauses of evidence section 7 were written for the immediate-bump design
and are restated by arb-8155-c2. "B-first maximum at least B-last maximum,
else void" is retired: under the commit-time design both modes take version
rows inside COMMIT, so their maxima agree within one sampler tick (disjoint
0.0104 s against 0.0156 s, one tick apart; overlap 0.0210 s against 0.0209 s);
a larger gap in either direction returns to the arbiter. "`version_row` in A
must be 0" holds for the disjoint run; in the overlap run A reads 0.0210 s
because a second inserter of the same (repo_id, relative_path) waits on the
first's transaction and the classifier cannot split that content-row wait from
a version-row wait, so `version_row` is an upper bound there and the bar is B
minus A within one tick: B-last 0.0209 - 0.0210 = -0.0001 s, B-first 0.0000 s.

Arm 1 confirmation (one Write of 5,000 files, 5 measured rounds after 1
discard):

| Round | A | B-last |
|---|---|---|
| 1 | 1.0965 s | 1.1050 s |
| 2 | 1.0968 s | 1.0995 s |
| 3 | 1.1075 s | 1.1001 s |
| 4 | 1.1115 s | 1.1118 s |
| 5 | 1.1028 s | 1.0917 s |
| median | 1.1028 s | 1.1001 s (-0.24%) |

B-last `version_row` 0 in every arm 1 round.

## No-Regression Evidence

No-Regression Evidence: the live tests in
`go/internal/storage/postgres/config/artifacts/fence_*_live_test.go` run the
production `ContentWriter.Write` and `GenerationRetentionStore` against the
shipped migrations (`go test ./internal/storage/postgres/config/artifacts/`,
exit 0):

| Proof | Result |
|---|---|
| F1 chunk cycle raced with a Write, 20 rounds | RED on the RED commit (20 deadlocks), now 0 deadlocks, exactly 3 bumps per round |
| F1 SQL two-statement transaction vs an autocommit DELETE, 10 rounds | RED on the RED commit, now 0 deadlocks, 2 bumps per round |
| Arm 6 churn | full 10, delta 2, reap 4, bulk 1, prune 1 per repository; a seeded per-row fence fails the counts |
| Arm 7 cases 1a and 1b, 20 rounds each, a held middle version row forcing overlap | 0 deadlocks, exact bumps; a seeded row-level fence deadlocks |
| 3a: plan of the commit upsert (auto_explain) | `Insert` over `Sort` on `p.repo_id` |
| Case 1c: two real prunes sharing 6 repositories, 3 rounds | 0 deadlocks, each version row bumped twice |
| Cases 2 and 3: Write during an open prune parked in its COMMIT | waits on the version row's tuple lock inside that COMMIT (disjoint keys) or on the prune's transaction (shared doomed key), then lands; version +2 |
| R-1, two-scope and prune interleavings | a derive read before the last commit classifies `stale_version` through `artifacts.Classify` |
| Commit contract | bump visible only after COMMIT; rollback, rolled-back savepoint, `SET CONSTRAINTS ALL IMMEDIATE` plus more writes, and COPY bump once or not at all; pending table empty after every transaction |
| Firing order (`pg_trigger`) | four content triggers last per event, after `content_files_secret_lines_*` and the RI triggers; the fifth is a row-level deferrable, initially deferred constraint trigger, `tgenabled 'O'` |
| Seed | existing content, a write between 186 and 187, re-apply, the race with an in-flight write (commit and rollback), and F2: a writer holding a low row while the seed runs, 3 rounds over 200 repositories, 0 deadlocks |

## Observability Evidence

No-Observability-Change: PR3b-1b adds no metric, span or log. The added
retention work shows in `eshu_dp_generation_retention_phase_duration_seconds`
(phases `prune_content_files` and `commit`),
`eshu_dp_generation_retention_scope_lock_hold_seconds` and
`eshu_dp_generation_retention_duration_seconds`; the spill shows in
`pg_stat_database.temp_bytes` and, when enabled, `log_temp_files`. A Write
waits on content rows, and on a version row only inside another
transaction's COMMIT (at most 16 ms disjoint and 21 ms overlap in arm 5). The
migrations log `bootstrap.postgres.migration.applying`
and `bootstrap.postgres.migration.recorded` with `duration_ms`. A deadlock
would surface as SQLSTATE 40P01 in the projector's retry and in
`eshu_dp_generation_retention_failures_total`.

## Known limits

- A re-run of 186 checks the commit trigger by name only (review R3-a), so
  it leaves an existing `config_artifact_fence_pending_commit` that is
  disabled (`'D'`) or replica-only (`'R'`) as it is. The PR3b-2 startup check
  counts `'D'` and `'R'` as missing (ADR 3.1 item 7), so the read model
  reports `not_installed` and no stale row is trusted. The 186 SQL and its
  checksum stay as measured.

## NOT_CHECKED

- Cause of the fence-arm tail (8 KiB).
- QA p95 of `eshu_dp_generation_retention_scope_lock_hold_seconds`.
- Spill on network storage (these runs had a warm page cache).
- That 181.1 MB is the transition tuplestore alone (inferred from the
  diagnostics, not measured in isolation).

## Commands

```bash
docker run -d --name zz8155fence-pg -p 55451:5432 -e POSTGRES_PASSWORD=pw postgres:18.6
export ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='postgres://postgres:pw@127.0.0.1:55451/postgres?sslmode=disable'
export ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1
cd go && go test ./internal/storage/postgres/config/artifacts/ -count=1 -run VersionFence -v
ESHU_CONFIG_ARTIFACT_SEED_TIMING_ROWS=1000000 \
  go test ./internal/storage/postgres/config/artifacts/ -count=1 -run TestVersionFenceSeedTimingLive -v
```
