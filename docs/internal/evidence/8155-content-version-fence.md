# Content-version fence for the config-artifact read model (#8155, PR3b-1b)

Refs #8155, #7126. This note records the proof for migrations 182 and 183:
the four `content_files_version_fence_*` triggers and the version-0 seed of
`config_artifact_content_versions`. The design is
[Derive repository config artifacts when content is written](../design/7126-ingest-time-config-artifacts.md)
and its protocol companion, section 2. PR3b-1b adds no reader, no derive and
no read-path change, so it removes no read time.

## What ships

| File | Content |
|---|---|
| `182_content_files_version_fence.sql` | Three plpgsql functions and four triggers. INSERT and UPDATE are statement-level with a `NEW TABLE`, DELETE is statement-level with an `OLD TABLE`, TRUNCATE bumps every version row. Each bumps a touched repository once per transaction, in `repo_id` order |
| `183_config_artifact_content_versions_seed.sql` | `INSERT ... SELECT DISTINCT repo_id FROM content_files ... ON CONFLICT DO NOTHING`, version 0, `bumped_xid` `'0'::xid8` |

Two deviations from the S3 ruling text need review:

1. **Two files, not one transaction.** Ruling section 4 keeps the ADR order
   "triggers, then seed, one transaction". The #7961 lock-shape gate
   (`scripts/lib/migration_lock_shape.py`, no allowlist) refuses `CREATE
   TRIGGER` and `INSERT ... SELECT` in one file. Reproduced on a one-file
   draft: "table-locking DDL (CREATE TRIGGER) and bulk DML (INSERT ...
   SELECT) in one file hold the lock through the DML", exit 1. The split is
   the arbiter's own two-step form. It is correct because no reader or derive
   exists before PR3b-2: a write between the two files creates its row at
   version 1 through the trigger, and the seed keeps it
   (`TestVersionFenceMigrationsOnExistingContentLive`,
   `TestVersionFenceSeedRacesAFencedWriteLive`). The content-write stall is now
   the `CREATE TRIGGER` catalog change only.
2. **Guard on `(bumped_xid, bumped_at)`.** The ruled guard compares
   `bumped_xid` only. The PR3b-1a review left a forward note (package
   README): a logical restore restarts the transaction id counter while stored
   `bumped_xid` values survive, so a later transaction can receive an id a row
   holds and skip its bump. `TestVersionFenceSurvivesATransactionIDRestartLive`
   reproduced it (version 0, want 1) on the xid-only guard and passes on the
   pair. `now()` is fixed within a transaction, so the guard still bumps once
   per transaction. Reviewers: the seed's `bumped_xid` `'0'::xid8` is the
   value the ruling flagged; no real transaction id is 0.

## Performance Evidence

Performance Evidence: S3 arm 3 on the real `PruneSupersededGenerations`
(`prune_content_files` phase, 100,000 rows over 50 repositories, 1 KiB inline
bodies, n=6, load1 2.19-3.34, 2026-10-16): `OLD TABLE` +8.26%, +0.1105 s per
100,000-row cycle, `ScopeLockHold` +4.04%. The amended arm 3 bar (ruling
arb-8155-s3) is at most +10% of `prune_content_files` and at most +5% of
`ScopeLockHold` on both body classes, so the 1 KiB class passes. Write cost
from S3 (appendix section 6): arms 1, 2 and 4 at +0.71%, +1.47% and +1.07%.

Seed timing, whole PR3b-1b, on the migration 169 fixture shape (1,000,000
`content_files` rows over 3,000 repositories, all 10 `content_files` indexes,
PostgreSQL 18.6 in Docker, `TestVersionFenceSeedTimingLive` with
`ESHU_CONFIG_ARTIFACT_SEED_TIMING_ROWS=1000000`, 2026-10-16 13:39-13:41 UTC):

| Migration | Wall | Bar |
|---|---|---|
| 182 triggers | 0.0033 s | at most 1 s |
| 183 seed | 0.1329 s | at most 1 s |

Host load1 was 45.28 at the start and 28.43 at the end (other sessions), so
both figures are upper bounds. The default 30,000-row run measured 0.0035 s
and 0.0199 s. The loose-index-scan and two-step amendments of ruling section
4 are not needed for time; the split above is needed for the gate.

Mechanism probe (not merge evidence, load1 about 11): on 20,000 8 KiB TOASTed
rows, an `OLD TABLE` trigger with the shipped bump body added about 4 ms per
DELETE (68.3-77.2 ms without, 72.4-73.2 ms with) and 0 temp bytes, so the
transition table keeps TOAST pointers and does not detoast. A one-round shim
debug run of arm 3 at 2,000 rows read +220% for 8 KiB; one round at that size
under load is noise-level and is not a result.

Pending for the merge, on a quiet host (load1 under 9), with the harnesses in
the coordinator's kit: the 8 KiB class, arms A and B2, 1 discard plus at least
5 interleaved rounds, with `prune_temp_bytes`; and arm 5 attributed (modes A,
B-last, B-first; 20 rounds disjoint, 20 overlapping) plus the arm 1
confirmation (5 rounds). Both measure the shipped migration 182 functions,
including the `(bumped_xid, bumped_at)` guard. NOT_CHECKED until then.

## No-Regression Evidence

No-Regression Evidence: the live tests in
`go/internal/storage/postgres/config/artifacts/fence_*_live_test.go` run the
production `ContentWriter.Write` and `GenerationRetentionStore` against the
shipped migrations:

| Proof | Result |
|---|---|
| Arm 6 churn (`TestVersionFenceChurnIsOnePerTransactionLive`) | full 10, delta 2, reap 4, bulk 1, prune 1 per repository, exact |
| Seeded RED: per-row unguarded fence | full 5,000, delta 4, reap 1,700, bulk 50,000, prune 700: fails the counts |
| Arm 7 cases 1a and 1b (20 rounds each, a held middle version row forces overlap) | 0 deadlocks, every repository bumped exactly twice per round |
| Seeded RED: row-level fence on the same rounds | deadlocks in 1a and in 1b |
| 3a: DELETE fence plan (auto_explain) | `Insert` over `Unique` over `Sort` on `fence_old_rows.repo_id`, no reordering node between |
| 3b, case 1c: two real prunes, scope sets sharing 6 repositories, 3 rounds | 0 deadlocks, each shared version row bumped twice |
| 3c: concurrent first Writes of an unseeded repository, 10 rounds | one row, no error, version 2 |
| 3d: firing order from `pg_trigger` | fence last per event, after `content_files_secret_lines_*`, and for DELETE after the row-level RI triggers |
| Cases 2 and 3: Write during an open prune | waits on the version row's tuple lock (disjoint keys) or the prune's transaction (shared doomed key), then lands; version +2 |
| R-1, two-scope and prune interleavings | a derive read before the last commit classifies `stale_version` through `artifacts.Classify`; a read after it is trusted |

## Observability Evidence

No-Observability-Change: PR3b-1b adds no metric, span or log (ruling
addition 3e). A Write of a repository whose prune is open waits on the
version row, or on a doomed content row, until the retention commit. That
wait lies inside the existing
`eshu_dp_generation_retention_scope_lock_hold_seconds` window. The prune's
added work shows in `eshu_dp_generation_retention_phase_duration_seconds`
(phase `prune_content_files`) and
`eshu_dp_generation_retention_duration_seconds`. The migrations log
`bootstrap.postgres.migration.applying` and
`bootstrap.postgres.migration.recorded` with `duration_ms`. `CREATE TRIGGER`
queues behind a running retention or content transaction; the migrator's
`lock_timeout` retry covers that wait. A deadlock would surface as SQLSTATE
40P01 in the projector's retry and in
`eshu_dp_generation_retention_failures_total`.

## Commands

```bash
docker run -d --name zz8155fence-pg -p 55451:5432 -e POSTGRES_PASSWORD=pw postgres:18.6
export ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='postgres://postgres:pw@127.0.0.1:55451/postgres?sslmode=disable'
export ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1
cd go && go test ./internal/storage/postgres/config/artifacts/ -count=1 -run VersionFence -v
ESHU_CONFIG_ARTIFACT_SEED_TIMING_ROWS=1000000 \
  go test ./internal/storage/postgres/config/artifacts/ -count=1 -run TestVersionFenceSeedTimingLive -v
```
