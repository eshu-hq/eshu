# Config Artifact Read Model (storage)

## Purpose

Stores the `config_paths` rows of each repository at content-write time,
without the repository name, so the repository story and context routes read
the rows of all related repositories with one indexed statement (#7126). The
live extractor stays the reference: a missing or stale row costs latency, not
accuracy. See `docs/internal/design/7126-ingest-time-config-artifacts.md` and
its protocol companion.

## Status by PR

| PR | Adds |
|---|---|
| PR3b-1a (this package's first version) | migration 170 tables, the encoding check, the read-model port, the walk claim, the dirty join, the version-row guard |
| PR3b-1b | migration 186: the five fence triggers (four `content_files_version_fence_*` on `content_files` and `config_artifact_fence_pending_commit` on the pending table); migration 187: the version-0 seed |
| PR3b-2a | `CheckFence`, the start-up check of the five fence triggers (no caller yet) |
| PR3b-2b | `Deriver.DeriveRepository`, the derive core of protocol section 3 (no caller yet) |
| PR3b-2 (rest) | the derive call in `ContentWriter.Write`, the walker derive, the start-up wiring of `CheckEncoding` and `CheckFence`, the derive counter, histogram and span, env variables |
| PR3c | the stored reader, the read mode, the `/admin/status` field |

From PR3b-1b on, every `content_files` write bumps
`config_artifact_content_versions`. PR3b-2b adds the derive that reads it;
no runtime calls the derive until the rest of PR3b-2.

## Tables (migration 170)

| Table | Key | Holds |
|---|---|---|
| `config_artifact_content_versions` | `repo_id` | `version`, `bumped_xid` (xid8), `bumped_at`. Bumped once per content transaction by the migration 186 triggers; seeded at version 0 by migration 187 |
| `config_artifact_repo_state` | `repo_id` | the version and extractor version the last successful derive read, `row_count` (NULL until a success), list and candidate counts, failure count and class, and `last_failure_extractor_version` for the walker's skip rule |
| `config_artifact_paths` | `(repo_id, row_digest)` | `raw_path`, `relative_path`, `evidence_kind`, `name_scoped`; `row_digest` is a stored generated SHA-256 |
| `config_artifact_backfill_markers` | `marker_name` | when the first full walk finished |
| `config_artifact_reconcile_cursor` | `walk_name` | where the walker's last page ended |

`row_digest` is `sha256(config_artifact_utf8(length(raw_path) ':' raw_path
length(relative_path) ':' relative_path evidence_kind))`. The length prefix
counts characters (PostgreSQL `length`), so two rows that differ only in
where a separator sits get different digests. No Go code computes it; the
live test recomputes it in Go and in SQL to prove the definition.
`config_artifact_paths` sets `autovacuum_vacuum_scale_factor = 0.02`,
because each derive rewrites a repository's rows.

## Exported surface

- `CheckEncoding`, `Encoding` — the start-up UTF8 check (re-ruling 3)
- `CheckFence`, `Fence`, `FenceTrigger`, `MissingFenceTrigger` — the
  start-up check of the five fence triggers (S3 ruling)
- `Reader`, `Repository`, `Row`, `State`, `Reason`, `Classify` — the
  read-model port and the trust condition
- `ClaimPage`, `LoadCursor` — the walker's cursor claim over the versions
  table
- `ListDirty`, `DirtyRequest`, `Dirty` — the walker's dirty join
- `NewDeriver`, `Deriver`, `DeriverConfig`, `Database`, `Request`, `Result`,
  `DefaultDeriveTimeout` — the derive core (protocol section 3)
- `Trigger`, `Outcome`, `Status*`, `Reason*` — labels and status values

## Invariants

- **UTF8 only.** `config_artifact_utf8` is IMMUTABLE only because a
  database's `server_encoding` never changes. A process that finds another
  encoding, or cannot read it, keeps the read model `not_installed`: no
  derive, live reads only.
- **Fence present.** Stored rows are trusted only while every
  `content_files` write bumps the version. `CheckFence` reads `pg_trigger`
  once (one statement, `fenceTriggersSQL`) and requires the four
  `content_files_version_fence_*` triggers on `content_files` and
  `config_artifact_fence_pending_commit` on `config_artifact_fence_pending`,
  each non-internal with `tgenabled` `'O'` or `'A'`. An absent trigger, a
  same-named trigger on another table, `'D'` (disabled) and `'R'` (replica
  only) count as missing: the result is reason `version_fence_missing`, the
  read model stays `not_installed`, and the caller skips every derive with
  outcome `fence_missing`. A failed catalog read gives reason
  `fence_check_failed` and the same outcome `fence_missing` (coordinator
  ruling 2026-10-17; `TestDeriveMapsEveryStartupCheckResult` pins every
  reason). Content Writes and live reads proceed either way.
  `fenceTriggers` in `fence.go` is the only list of names;
  `TestFenceTriggersMatchMigration186` parses the embedded migration 186 and
  fails when its `CREATE TRIGGER` statements diverge.
- **Version rows.** Delete or reset a version row only in the same
  statement that deletes the repository's state row (the retirement path).
  A version row created again while its state row survives counts up from 1
  and can reach the old `content_version`, so readers would trust stale
  rows. `TestNoStatementDeletesVersionRowsOutsideRetirement` enforces this
  for every Go literal expression (one string literal, or one run of
  literals joined by `+`; a non-literal operand splits the chain) and every
  migration. It reads `DELETE`, `MERGE ... THEN DELETE`, `TRUNCATE`,
  `DROP TABLE`, `ALTER TABLE ... RENAME TO`, and any top-level `SET` of
  `version` (plain or row-constructor) other than `version + 1`, with
  optional schema and quotes on the table name. A SET list is read at its
  own nesting level, past strings, parenthesized groups and `CASE ... END`.
  Known blind spots:
  - SQL assembled from non-literal values, including plpgsql
    `EXECUTE format('DELETE FROM %I', ...)` with the table name passed as an
    argument;
  - a retirement statement whose state `DELETE` names another repository
    than its version `DELETE`: the guard sees that both deletes are in one
    statement, not that they match on `repo_id`. Review that by hand.

  A row-constructor `SET (version, ...) = (...)` is always reported, even as
  a bump; write the bump as a plain `version = version + 1`.
- **Trust.** `Classify` trusts stored rows only when the state's
  `content_version` equals the current version, its `extractor_version`
  equals the reader's, and `row_count` is not NULL. A later failed derive of
  the same content keeps the rows trusted; the dirty join still lists it for
  the walker, as `retry`.
- **Reasons.** `Classify` returns `missing`, `failed` (no successful derive
  yet), `stale_version`, or `stale_extractor`. `ListDirty` returns the same
  labels with Classify's precedence, plus `retry`: a failed derive after a
  success whose rows still match the current content version, which the
  reader trusts (or finds stale only by extractor).
  `TestListDirtyAgreesWithClassifyLive` checks this over the cross product of
  the state inputs. The walk order is a separate priority: never derived,
  failed after a success, older extractor (even when the content also
  moved), missing state, moved content. The walker leaves every state row
  from a newer extractor to the newer replicas. A row with a successful
  derive cannot be replaced by an older derive (protocol section 3, step 3).
  A failure-only row could be (step 3's first, Proposed, clause); leaving it
  is a design choice the record leaves open, and readers serve it live
  meanwhile.
- **Claims are not leases.** `ClaimPage` spreads work; the derive's advisory
  lock and advance-only write carry correctness.

## The derive (PR3b-2b)

`Deriver.DeriveRepository` runs protocol section 3 for one repository. The
docs/internal/evidence/8155-derive-core.md note has the sequence diagram.

1. **Version first.** `contentVersionSQL` reads `v0 = coalesce(version, 0)`
   on the pool, before the list. Content committed later bumps the version
   past `v0` at its COMMIT, so a derive never labels rows with a version
   newer than the content it read.
2. **No lock, no transaction.** `contentartifacts.LoadConfigPaths` lists the
   files, reads the candidates of the whole window (allowance -1) through
   `contentReader`, which sends the neutral package's statements and row
   mappers, and runs the extractors with no name. `storedRows` sets
   `NameScoped` from `contentartifacts.NameScoped` and fails on an unknown
   evidence kind or a named row.
3. **One short transaction.** `lockRepository` (`lock.go`) takes
   `pg_advisory_xact_lock(hashtextextended(repoLockKey(repo_id), 0))` first,
   with `repoLockKey` = `'config_artifact_paths:' || repo_id`, then the state
   row is read on a snapshot taken after the previous holder committed. The
   failure-only update takes the same lock through the same helper, and the
   walker reuses it. `mayWrite` allows a write for no state row
   or one with no success (`row_count` or `content_version` NULL, the
   **Proposed** clause), for `v0 >` stored with an extractor version at least
   the stored one, and for `v0 =` stored with a greater extractor version.
   Otherwise the transaction rolls back: `superseded`. A write deletes the
   repository's rows, inserts the new rows in one `unnest` statement, and
   upserts the state (`content_version = v0`, counts, `failure_count = 0`;
   an empty scope or generation keeps the stored one). The insert has no
   `ON CONFLICT` clause: `row_digest` covers exactly (`raw_path`,
   `relative_path`, `evidence_kind`), and `name_scoped` is a function of
   `evidence_kind`, so a key conflict means the extractors built one triple
   twice. That fails the derive with SQLSTATE 23505 (a rollback and a
   failure-only record) instead of storing one row fewer than `row_count`. A
   retried derive stays idempotent because the same transaction deletes the
   repository's rows first.

Failure semantics: the derive runs under `DeriverConfig.Timeout` (default
`DefaultDeriveTimeout`, 10 s) and recovers a panic. An error, a panic or a
timeout writes a failure-only update (`recordFailureSQL`, under the same
lock, on a context detached from the caller and bounded at 5 s):
`failure_count + 1`, `last_error_class`, `last_failure_at`,
`last_failure_extractor_version`. A new row carries this derive's extractor
version and NULL `row_count`; an existing row keeps its rows,
`content_version`, `extractor_version` and `row_count`. `Result` carries the
outcome and the error for logging; the derive never fails its caller. The
deadline interrupts statements and is checked before step 3; a CPU-bound
extractor is not preempted and ends as `timeout` when it returns. An
unsupported `Encoding` or `Fence` (including `fence_check_failed`) returns
`encoding_unsupported` or `fence_missing` without a statement.

`last_error_class` is `panic`, `timeout`, `canceled` (the caller's context
was canceled), `sqlstate_<SQLSTATE>` for a PostgreSQL error, or `error`.

`DeriverConfig.ExtractorVersion` is required and positive. This package
defines no extractor-version constant: the wiring slice supplies
`contentartifacts.ExtractorVersion`. Its guard
(`TestExtractorOutputIsPinned` in `content/artifacts`, decision record
section 7, "S0 test, version guard") copies `storedRows` and re-implements the
`content_files` statements, so it cannot see a change here.
`TestStoredRowsCopiesTheBuildVerbatim` (`derive_reader_test.go`) pins
`storedRows` as a verbatim copy of the build. A change to `storedRows` or to
what a statement returns needs an `ExtractorVersion` bump and the same change
to the guard's copy (`content/artifacts` README, "What the guard does not
see").

## The version fence (PR3b-1b)

Migration 186 is a commit-time fence (ruling arb-8155-f1). Four
statement-level triggers on `content_files` (INSERT and UPDATE with a `NEW
TABLE`, DELETE with an `OLD TABLE`, TRUNCATE) stage the touched repositories in
the UNLOGGED `config_artifact_fence_pending`. One deferred constraint trigger,
`config_artifact_fence_pending_commit`, bumps them at COMMIT in `repo_id`
order and clears the stage, so a transaction takes version rows only after
its last content lock. A retention chunk cycle deletes `content_files` twice
in one transaction; with a bump inside each statement it deadlocked with a
concurrent Write (`TestVersionFenceChunkCycleRacingAWriteDoesNotDeadlockLive`).
The once-per-transaction guard compares `(bumped_xid, bumped_at)`, not
`bumped_xid` alone, because a logical restore restarts the transaction id
counter while stored values survive
(`TestVersionFenceSurvivesATransactionIDRestartLive`).

Limits, each pinned by a guard test: `session_replication_role = replica`
skips the triggers as it skips every ordinary trigger, and no Eshu binary sets
it; an UPDATE that changes `repo_id` bumps only the new repository, and no Go
statement changes `repo_id` (`fence_guard_test.go`).

Migration 187 seeds one version-0 row (`bumped_xid` `'0'::xid8`) per
repository with content, in `repo_id` order. It is a separate file because the
#7961 lock-shape gate refuses `CREATE TRIGGER` and bulk DML in one file; it
holds ACCESS SHARE on `content_files` only, and `ON CONFLICT DO NOTHING` keeps
a row a fenced write created first. The evidence is in
`docs/internal/evidence/8155-content-version-fence.md`.

A Write waits on content rows, and on a version row only inside another
transaction's COMMIT. The retention cost is the `OLD TABLE` capture: +0.11 to
+0.17 s of `eshu_dp_generation_retention_scope_lock_hold_seconds` per
100,000-row prune, inside the arb-8155-c1 budget of +0.25 s.

## Telemetry

No runtime path calls this package yet, so it emits nothing in production.
`DeriveRepository` logs `event_name=config_artifacts.derive.failed` at ERROR
for every failed derive and returns a `Result` the wiring slice counts on
`eshu_dp_config_artifact_derives_total{trigger,outcome}` and observes on the
derive duration histogram; this package registers no metric or span.
`CheckEncoding` logs `event_name=postgres.server_encoding` at INFO for UTF8
and at ERROR otherwise (with `config_artifacts_status=not_installed` and the
`reason`), for the binaries that will call it in PR3b-2 and PR3c. Every
statement runs through the caller's `db.Queryer`, which in the runtimes is the
instrumented Postgres wrapper.

## Evidence (PR3b-1a)

No-Regression Evidence: baseline is the base branch without this change,
where none of these tables or functions exist. PR3b-1a adds no caller in any runtime binary
and no trigger on `content_files`, so no existing hot path runs new work; the
only runtime change is migration 170 at bootstrap (five `CREATE TABLE IF NOT
EXISTS` and one `CREATE OR REPLACE FUNCTION` on empty tables, recorded at 5 ms
by the tracked migrator on a fresh PostgreSQL 18.6 database). The two new
statements a future walker runs were measured on PostgreSQL 18.6 (Homebrew,
local) with 3,000 version rows and 2,990 state rows: the walk page
(`walkPageSQL`, 500 rows) is an Index Only Scan on
`config_artifact_content_versions_pkey`, 5 buffers; the dirty join
(`dirtySQL`, generic plan via `PREPARE`/`EXECUTE` with
`plan_cache_mode = force_generic_plan`, 21 dirty of 3,000) is a hash join over
two sequential scans of tables that grow with repositories, not files,
59 buffers per execution. In round 2 it ran in 0.80-0.87 ms over five runs
after one discarded run. The round 5 form (labels in Classify's precedence,
plus a separate priority column) has the same plan and buffers; an
interleaved A/B of the round 4 and round 5 statements, 15 runs each with the
first mover alternating, gave medians of 1.217 ms and 1.152 ms on a host at
load average 22. The review of
round 1 measured 62-65 ms at 300,000 repositories (parallel hash join), so the
cost is linear in repositories. Both tables are O(repositories) (815 in the QA
environment on 2026-10-10).

No-Observability-Change: no runtime path calls this package yet, so it
emits no metric, span, or log in production. `CheckEncoding` and `CheckFence`
have no caller (PR3b-2 wires them into the deriving binaries and the API).
When called, `CheckEncoding` logs `event_name=postgres.server_encoding` at
INFO or ERROR, and every statement here runs through the caller's
instrumented Postgres wrapper (`eshu_dp_postgres_query_duration_seconds`).

Observability Evidence: `CheckFence` logs
`event_name=postgres.config_artifact_fence` (the hermetic tests in
`fence_test.go` assert the level, event and every field below):

| Level | When | Fields |
|---|---|---|
| INFO | all five triggers fire | `fence_triggers=5` |
| ERROR | a trigger is absent, disabled or replica-only | `missing_triggers` (each `table.trigger=state`, state `absent`, `D` or `R`), `missing_count`, `config_artifacts_status=not_installed`, `reason=version_fence_missing`, `outcome=fence_missing` |
| ERROR | the `pg_trigger` read failed | `config_artifacts_status=not_installed`, `reason=fence_check_failed`, `error` |

At 3 AM grep for `"event_name":"postgres.config_artifact_fence"` with
`"level":"ERROR"`; `missing_triggers` names the trigger to restore (re-apply
migration 186 for a dropped trigger, `ALTER TABLE ... ENABLE TRIGGER` for
`D` or `R`). `Deriver.DeriveRepository` returns `OutcomeFenceMissing` for
both fence reasons; the `eshu_dp_config_artifact_derives_total` counter that
carries it is registered by a later PR3b-2 slice, so no metric exists yet.

Performance Evidence: the baseline (base branch) runs no fence check; after
this change the check is one catalog statement per process start, and no
binary calls it yet, so no hot path changes. Backend: PostgreSQL 18.6
(`postgres:18.6` in Docker, local). Input shape: a fully bootstrapped
database, 770 `pg_trigger` rows (26 non-internal), five requested pairs, five
rows returned. `EXPLAIN (ANALYZE, BUFFERS)` of `fenceTriggersSQL`: a hash
join of the five pairs over a sequential scan of `pg_trigger`, 18 shared
buffers on the scan, 0.043-0.064 ms execution over four runs on 2026-10-16
(schema at origin/main 26be17f76). The cost
grows with catalog triggers, not with content rows. It is safe because it
reads only `pg_trigger`, takes no lock beyond the catalog's ACCESS SHARE, and
never fails or blocks the caller.

## Evidence (PR3b-2b, the derive core)

Performance Evidence: baseline is the base branch, where no derive exists;
this slice adds no caller, so no runtime hot path runs new work. The cost a
later caller pays is step 3's write transaction, the only part that holds
the repository lock. Backend: `postgres:18.6` in Docker, local, a 16-core
host at load average 10-14, 2026-10-17. Shape: one repository of 20,000 rows
over 5,000 files, beside 800 repositories of 21 rows, the statements of
`derive_sql.go` under `EXPLAIN (ANALYZE, BUFFERS)`, one transaction per run:

| Shape | Lock | State read | Delete | Insert | Upsert | Commit | Transaction |
|---|---|---|---|---|---|---|---|
| 20,000 rows, `VACUUM` before each run (5 runs) | 0.05-2.2 ms | 0.06-0.7 ms | 6.5-7.9 ms (Bitmap Heap Scan on the primary key) | 134.6-213.0 ms (about 201,000 shared buffers hit) | 0.1 ms | 10.1-81.7 ms | 192.6-232.0 ms |
| 20,000 rows, back to back, no vacuum (6 runs) | 0.07 ms | 0.07-0.4 ms | 0.1-9.6 ms | 128.2-868.7 ms | 0.1 ms | 32.3-154.8 ms | 233.2-910.9 ms |
| 21 rows (the R1 average), `VACUUM` before each run (5 runs) | 0.03-0.08 ms | 0.04-0.14 ms | 0.05-0.11 ms | 0.20-0.51 ms | 0.05-0.07 ms | 0.47-0.57 ms | 0.9-1.5 ms |

The insert dominates: each row computes the generated SHA-256 `row_digest`
and inserts into the `(repo_id, row_digest)` key. Without a vacuum the dead
tuples of the previous rewrites slow it; the 868.7 ms run overlapped an
autovacuum of the table (`last_autovacuum` 01:21:51). The lock serializes
only derives of one repository: a 20,000-row repository holds it for about
0.2 s, a 21-row repository for about 1 ms, and no content write waits on it.
Step 2 holds no lock and no transaction (proved by
`TestDeriveHoldsTheLockOnlyInStepThreeLive`). The end-to-end derive time on
a large repository is not measured here; S1 measures it with the caller.

Observability Evidence: a failed derive logs one record at ERROR (the
hermetic `runFailure` tests assert the level, the event and the fields):

| Field | Value |
|---|---|
| `event_name` | `config_artifacts.derive.failed` |
| `repo_id`, `trigger` | the repository and `write`, `backfill` or `repair` |
| `outcome`, `error_class` | `error`, `panic` or `timeout`; `panic`, `timeout`, `canceled`, `sqlstate_<SQLSTATE>` or `error` |
| `error`, `stack` | the failure; the goroutine stack for a panic |
| `content_version`, `extractor_version` | `v0` (-1 before step 1) and this binary's extractor version |
| `duration_seconds`, `failure_recorded` | wall time; whether the failure-only update committed |
| `failure_record_error` | only when the failure-only update itself failed |

At 3 AM grep for `"event_name":"config_artifacts.derive.failed"`; group by
`error_class` to tell a parse bug (`panic`), a slow repository (`timeout`)
and a database error (`sqlstate_*`) apart, and by `repo_id` for the
repository. The state row keeps the same story: `SELECT repo_id,
failure_count, last_error_class, last_failure_at FROM
config_artifact_repo_state WHERE failure_count > 0`. `failure_recorded=false`
means the state row did not count the failure. If the content moved, the
version mismatch still sends readers live and lists the repository for the
walker; if it did not, the stored rows are still exact. The derive
outcome counter and duration histogram are registered by the wiring slice.

## Known limits for the wiring slice

These came out of the independent review of the derive core and belong to the
wiring and walker slices, not to this one.

- A caller can block for `Timeout` plus the 5 s failure-update bound (15 s with
  the default) when the repository lock is contended; `derive_write.go`
  `recordFailure` bounds only its own update, and the caller-facing total is
  not in the `DeriveRepository` godoc yet.
- A canceled caller context records a failure (`failure_count` + 1, class
  `canceled`; `derive_write.go` `classifyFailure`), so repeated restarts
  mid-derive can push `failure_count` toward the walker's skip threshold of 5;
  confirm that is wanted when the walker skip rule lands.
- `classifyFailure` reads the deadline error after the derive returns, so a
  non-context error that lands just before the deadline can be classed
  `timeout` instead of its own class (`derive_write.go`); the rows and the
  state are unaffected.

## Verification

```bash
cd go && go test ./internal/storage/postgres/config/artifacts -count=1
# live proofs (disposable database on a PostgreSQL 18 server):
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='postgres://USER@HOST:PORT/postgres?sslmode=disable' \
ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/storage/postgres/config/artifacts -count=1 -run Live
```
