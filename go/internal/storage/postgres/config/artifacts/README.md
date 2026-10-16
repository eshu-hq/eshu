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
| PR3b-1b | migration 184: the four `content_files_version_fence_*` triggers; migration 185: the version-0 seed |
| PR3b-2 | the derive in `ContentWriter.Write`, the walker derive, telemetry, env variables |
| PR3c | the stored reader, the read mode, the `/admin/status` field |

From PR3b-1b on, every `content_files` write bumps
`config_artifact_content_versions`; nothing reads or derives from it until
PR3b-2.

## Tables (migration 170)

| Table | Key | Holds |
|---|---|---|
| `config_artifact_content_versions` | `repo_id` | `version`, `bumped_xid` (xid8), `bumped_at`. Bumped once per content transaction by the migration 184 triggers; seeded at version 0 by migration 185 |
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
- `Reader`, `Repository`, `Row`, `State`, `Reason`, `Classify` — the
  read-model port and the trust condition
- `ClaimPage`, `LoadCursor` — the walker's cursor claim over the versions
  table
- `ListDirty`, `DirtyRequest`, `Dirty` — the walker's dirty join
- `Trigger`, `Outcome`, `Status*`, `Reason*` — labels and status values

## Invariants

- **UTF8 only.** `config_artifact_utf8` is IMMUTABLE only because a
  database's `server_encoding` never changes. A process that finds another
  encoding, or cannot read it, keeps the read model `not_installed`: no
  derive, live reads only.
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

## The version fence (PR3b-1b)

Migration 184 is a commit-time fence (ruling arb-8155-f1). Four
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

Migration 185 seeds one version-0 row (`bumped_xid` `'0'::xid8`) per
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

No-Observability-Change: no runtime path calls this package in PR3b-1a, so it
emits no metric, span, or log in production. When PR3b-2 and PR3c call
`CheckEncoding`, it logs `event_name=postgres.server_encoding` at INFO or
ERROR, and every statement here runs through the caller's instrumented
Postgres wrapper (`eshu_dp_postgres_query_duration_seconds`).

## Verification

```bash
cd go && go test ./internal/storage/postgres/config/artifacts -count=1
# live proofs (disposable database on a PostgreSQL 18 server):
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='postgres://USER@HOST:PORT/postgres?sslmode=disable' \
ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/storage/postgres/config/artifacts -count=1 -run Live
```
