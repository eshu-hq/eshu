# Content-version fence for the config-artifact read model (#8155, PR3b-1b)

Refs #8155, #7126. This note records the proof for migrations 184 and 185:
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
| `184_content_files_version_fence.sql` | The UNLOGGED `config_artifact_fence_pending (bumped_xid, repo_id)` table. Four statement-level triggers on `content_files` (INSERT and UPDATE with `NEW TABLE`, DELETE with `OLD TABLE`, TRUNCATE) stage touched repositories. One deferred constraint trigger, `config_artifact_fence_pending_commit`, runs the ordered upsert with the `(bumped_xid, bumped_at)` guard at COMMIT and clears the stage |
| `185_config_artifact_content_versions_seed.sql` | One version-0 row per repository with content, `bumped_xid` `'0'::xid8`, in `repo_id` order, `ON CONFLICT DO NOTHING` |

Decisions beyond the arb-8155-s3 text:

1. **Two files, not one transaction** (coordinator ruling, 2026-10-16). The
   #7961 lock-shape gate (`scripts/lib/migration_lock_shape.py`, no allowlist)
   refuses `CREATE TRIGGER` and `INSERT ... SELECT` in one file. No reader or
   derive exists before PR3b-2, which must deploy after this PR, so the order
   is safe: a write between the files creates its row at version 1 and the
   seed keeps it. Every repository ends with one row. The remaining
   content-write stall is the `CREATE TRIGGER` catalog lock (184: 0.0030 s on
   1,000,000 rows); the seed holds ACCESS SHARE and blocks no write.
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
   other side's COMMIT. This replaces ruling 3e of arb-8155-s3 ("waits on the
   version row until the retention commit").
4. **F2-F4.** The seed inserts in `repo_id` order (F2). The 184 header states
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
BD is the migration 184 body, byte-identical to the measured SQL.

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
The +10%/+5% bar of arb-8155-s3 was missed in two of three sessions; replaced
by arb-8155-c1: added `ScopeLockHold` at most +0.25 s per 100,000 doomed rows.
BD adds +0.11 to +0.17 s, so it meets the budget. B2 reads the same or worse,
so the commit-time move costs nothing; the cost is the `OLD TABLE` capture
and its 181.1 MB spill past the 64MB `work_mem` (the diagnostics remove the
spill and recover about 0.05-0.07 s). The diagnostics are not candidates: the
spill fix needs a retention change and is follow-up issue #8268. The earlier single-session reading (+8.26%, `ScopeLockHold`
+4.04%) is one reading, not the headline.

Production estimate: at most the 100,000-row figure (+0.11 to +0.17 s of
`ScopeLockHold`) per retention cycle per reducer replica, hourly by default,
and usually far less, since most cycles prune fewer content rows.

Arms 1, 2 and 4 (A vs BD, 14:58:01-14:59:39 UTC, load1 7.54/6.33/8.53), the
S3 statistic (medians with ranges; bar +5%): full Write of 5,000 files
1.3444 s vs 1.3610 s (+1.24%); delta Write 2.204 ms vs 2.199 ms (-0.23%);
one 50,000-row INSERT 6.2555 s vs 6.1923 s (-1.01%). All PASS. Means are
+5.02%, +22.66% and +1.20%, pulled up by round 3, where BD read high on all
three arms at once.

Seed timing on the migration 169 fixture shape (1,000,000 rows over 3,000
repositories, all 10 `content_files` indexes, 16:11 UTC, load1 3.32 to 3.20):
184 0.0030 s, 185 0.1150 s, against the 1 s bar.

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
| Seed | existing content, a write between 184 and 185, re-apply, the race with an in-flight write (commit and rollback), and F2: a writer holding a low row while the seed runs, 3 rounds over 200 repositories, 0 deadlocks |

## Observability Evidence

No-Observability-Change: PR3b-1b adds no metric, span or log. The added
retention work shows in `eshu_dp_generation_retention_phase_duration_seconds`
(phases `prune_content_files` and `commit`),
`eshu_dp_generation_retention_scope_lock_hold_seconds` and
`eshu_dp_generation_retention_duration_seconds`; the spill shows in
`pg_stat_database.temp_bytes` and, when enabled, `log_temp_files`. A Write
waits on content rows, and on a version row only inside another
transaction's COMMIT. The migrations log `bootstrap.postgres.migration.applying`
and `bootstrap.postgres.migration.recorded` with `duration_ms`. A deadlock
would surface as SQLSTATE 40P01 in the projector's retry and in
`eshu_dp_generation_retention_failures_total`.

## NOT_CHECKED

- The 8 KiB class at 100,000 rows (arb-8155-c1 1.4a) and the attributed arm 5
  with the arm 1 confirmation (1.4b), on the commit-time shape.
- The spill cost on production storage (these runs had a warm page cache).
- The QA p95 of `eshu_dp_generation_retention_scope_lock_hold_seconds`.
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
