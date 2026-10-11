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
  for every Go statement and migration.
- **Trust.** `Classify` trusts stored rows only when the state's
  `content_version` equals the current version, its `extractor_version`
  equals the reader's, and `row_count` is not NULL. A later failed derive of
  the same content keeps the rows trusted; the dirty join still lists it for
  the walker.
- **Claims are not leases.** `ClaimPage` spreads work; the derive's advisory
  lock and advance-only write carry correctness.

## Telemetry

No runtime path calls this package yet, so it emits nothing in production.
`CheckEncoding` logs `event_name=postgres.server_encoding` at INFO for UTF8
and at ERROR otherwise (with `config_artifacts_status=not_installed` and the
`reason`), for the binaries that will call it in PR3b-2 and PR3c. Every
statement runs through the caller's `db.Queryer`, which in the runtimes is the
instrumented Postgres wrapper.

## Verification

```bash
cd go && go test ./internal/storage/postgres/config/artifacts -count=1
# live proofs (disposable database on a PostgreSQL 18 server):
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='postgres://USER@HOST:PORT/postgres?sslmode=disable' \
ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/storage/postgres/config/artifacts -count=1 -run Live
```
