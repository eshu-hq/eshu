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
| PR3b-1b | the `content_files` version-fence triggers and the version-0 seed (next migration) |
| PR3b-2 | the derive in `ContentWriter.Write`, the walker derive, telemetry, env variables |
| PR3c | the stored reader, the read mode, the `/admin/status` field |

Until PR3b-1b ships, nothing writes these tables in production.

## Tables (migration 170)

| Table | Key | Holds |
|---|---|---|
| `config_artifact_content_versions` | `repo_id` | `version`, `bumped_xid` (xid8), `bumped_at`. Bumped once per content transaction by the PR3b-1b trigger |
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
  labels with the same meaning, plus `retry` (a failed derive after a
  success), which the reader never uses. The walker leaves every state row
  from a newer extractor to the newer replicas. A row with a successful
  derive cannot be replaced by an older derive (protocol section 3, step 3).
  A failure-only row could be (step 3's first, Proposed, clause); leaving it
  is a design choice the record leaves open, and readers serve it live
  meanwhile.
- **Claims are not leases.** `ClaimPage` spreads work; the derive's advisory
  lock and advance-only write carry correctness.

## Known limits for the trigger (PR3b-1b)

The ruled bump guard `WHERE v.bumped_xid IS DISTINCT FROM
EXCLUDED.bumped_xid` assumes an xid8 value never repeats for a database. A
logical restore (dump into a fresh cluster) or a logical-replication cutover
restarts the transaction id counter while the stored `bumped_xid` values
survive, so a later transaction could receive a stored value and skip that
repository's bump. PR3b-1b must compare `(bumped_xid, bumped_at)` or otherwise
survive an xid restart. The columns already allow it, so no `ALTER` is
needed. Source: the PR3b-1a review verdict r1 of 2026-10-11, forward note
(`7126-pr3b1a-review-verdict-r1-20261011.md`, local kit note, not in the
repository); an inference from PostgreSQL xid semantics, not a measurement.

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
0.80-0.87 ms over five runs after one discarded run, 59 buffers. The review of
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
