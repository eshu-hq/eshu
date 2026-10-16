# AGENTS.md — internal/storage/postgres/config/artifacts

Scoped instructions for the config-artifact read model (#7126). The root
`AGENTS.md` and `internal/storage/postgres/AGENTS.md` still apply.

## Read first

1. `docs/internal/design/7126-ingest-time-config-artifacts.md` and its
   `-protocol.md` companion (tables, fence, derive protocol, walker).
2. `../../migrations/170_config_artifact_read_model.sql` — the schema. Never
   edit it once shipped; widen through a new migration.
3. `../../infra/inventory/` — the infra read model this one follows.

## What must stay true

- `row_digest` is defined in the schema only. Do not compute it in production
  Go. If the digest expression must change, ship a new migration and a
  re-derive, and update `goRowDigest` in `schema_live_test.go`.
- Keep the UTF8 start-up check fail-closed: an unreadable or non-UTF8
  `server_encoding` keeps the read model off.
- Never delete or reset a `config_artifact_content_versions` row except in
  one statement that also deletes the repository's
  `config_artifact_repo_state` row. The guard test fails the build otherwise.
  It reads one Go literal expression at a time, so keep each statement in one
  literal or one `+` chain of literals with no non-literal operand. It cannot
  see SQL built from non-literal values (including plpgsql
  `EXECUTE format(...)` with the table name as an argument), and it cannot
  prove that the state `DELETE` and the version `DELETE` of a retirement
  statement name the same `repo_id`; check both by hand in review. It reports
  every row-constructor `SET (version, ...)`, so write a bump as
  `version = version + 1`.
  The repository retirement design (#7766) must use that shared disposition.
- `failed` means "no successful derive yet" in both `Classify` and
  `ListDirty`. `ListDirty` labels in `Classify`'s precedence; its one own
  label is `retry` (a failed derive after a success whose rows still match
  the current content version). Do not reuse one label for two populations;
  `TestListDirtyAgreesWithClassifyLive` holds the two in step.
- `Classify` is the reader's trust rule. The PR3c SQL reader must return the
  same reason for every state; test it against `Classify`, not a copy.
- The walk claim is not a lease. Do not make correctness depend on a single
  walker owning a repository.
- Migrations 182 and 183 are the fence. Every `content_files` writer must
  bump; never add a session setting or `WHEN` clause that skips the fence
  triggers (an F2 amendment by ruling is required). Keep the trigger names
  sorting after `content_files_secret_lines_*` and the bump `ORDER BY
  repo_id`; `TestVersionFenceTriggersFireLastLive` and the arm 7 tests fail
  otherwise. Keep the guard on `(bumped_xid, bumped_at)`.

## Tests

- `schema_test.go`, `port_test.go`, `encoding_test.go`,
  `versions_guard_test.go` run without a database.
- `*_live_test.go` need `ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN` (an
  administrative `postgres` database DSN) and
  `ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1`; each test creates and drops
  its own database. They are enrolled in the `live-postgres-readiness` runner
  (`scripts/lib/live_postgres_readiness_inventory*.py`,
  `specs/live-tests.d/`).

## Gates that will fire on your change

- `verify-live-tests-ledger.sh` — a new `*_live_test.go` needs a ledger row.
- The migration golden digest and checksum manifest in `../../migrations`.
- `migration-immutability` lock-shape check on any new migration.
