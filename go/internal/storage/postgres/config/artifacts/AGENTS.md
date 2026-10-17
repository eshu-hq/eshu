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
- Keep the fence start-up check fail-closed: an absent, disabled (`'D'`) or
  replica-only (`'R'`) fence trigger, or a failed `pg_trigger` read, keeps the
  read model off. `fenceTriggers` in `fence.go` is the one list of trigger
  names; when a migration renames, adds or drops a fence trigger, change it
  in the same PR (`TestFenceTriggersMatchMigration186` parses migration 186).
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
- The derive (`derive*.go`) follows protocol section 3 in this order: read
  the version (`contentVersionSQL`) before the list, extract with no lock and
  no open transaction, then call `lockRepository` FIRST in the write
  transaction and read the state after it. Moving the version read after the
  list, taking the lock earlier, or reading the state before the lock breaks
  the protocol. Different tests catch each: `derive_order_live_test.go`
  catches the late version read (`TestDeriveReadsVersionBeforeListLive`) and
  the early lock (`TestDeriveHoldsTheLockOnlyInStepThreeLive`), while the
  state-before-lock order is caught only by
  `TestDeriveWritesInOneTransactionUnderTheLock` (hermetic) and
  `derive_race_live_test.go` (`TestDeriveTwoDerivesOfOneRepositorySerializeLive`),
  not by `derive_order_live_test.go` (mutation table in
  `docs/internal/evidence/8155-derive-core.md`). `lockRepository` and
  `repoLockKey` (`lock.go`) are the one definition of the repository lock;
  every writer of a repository's rows or state row, the walker included,
  takes it through them, never a second copy of the key
  (`TestDeriveWriteAndFailureRecordShareOneRepositoryLock`).
- Keep `insertRowsSQL` free of `ON CONFLICT`. A `row_digest` conflict means a
  derive built one (`raw_path`, `relative_path`, `evidence_kind`) triple twice;
  `DO NOTHING` would store one row fewer than the `row_count` the state upsert
  records. The insert must fail with SQLSTATE 23505 and roll back (pinned by
  `TestDeriveDuplicateRowDigestFailsLoudlyLive` and
  `TestInsertRowsSQLDoesNotAbsorbAConflict`). Idempotence of a retried derive
  comes from the `DELETE` in the same transaction
  (`TestDeriveRederiveReplacesRowsWithoutConflictLive`).
- `mayWrite` is the write condition. Change it only with the protocol
  companion; `TestMayWriteConditions` pins every branch, including the
  Proposed clause for a failure-only state row.
- A failed derive changes only `failure_count`, `last_error_class`,
  `last_failure_at` and `last_failure_extractor_version`
  (`recordFailureSQL`). Never let it touch the rows, `content_version`,
  `extractor_version` or `row_count` of an existing state row, and keep it on
  a context detached from the caller so a timed-out derive still records.
  `TestDeriveFailureRecordWaitsOnTheWriteLockLive` pins the extractor half: its
  failing deriver runs an older extractor than the held write, so copying the
  failing version into `extractor_version` fails it. The racing test
  (`TestDeriveFailureRecordRacingNewerWritesLive`) biases the write and failure
  order with sleeps and requires at least 5 rounds of each; it does not force
  either order.
- `storedRows` copies the build verbatim (`TestStoredRowsCopiesTheBuildVerbatim`).
  The extractor-version guard in `content/artifacts` hashes its own copy of
  it, so a change to `storedRows` needs an `ExtractorVersion` bump and the
  same change to `deriveGuardRows` there.
- Step 2 reads through `contentReader`, which must send only the neutral
  package's statements and row mappers (`contentartifacts.ListStatement` and
  its siblings), never a copy: the S4 equality test compares the stored rows
  with the query package's live reader.
- No metric or span here: `DeriveRepository` returns a `Result` and the
  wiring slice counts it. Do not add an env lookup for the timeout here; it
  is a `DeriverConfig` field.
- Migrations 186 and 187 are the fence. Every `content_files` writer must
  bump; never add a session setting or `WHEN` clause that skips the fence
  triggers (an F2 amendment by ruling is required). Keep the bump at COMMIT
  (the deferred `config_artifact_fence_pending_commit` trigger): a bump inside
  a content statement deadlocks the retention chunk cycle (arb-8155-f1). Keep
  the trigger names sorting after `content_files_secret_lines_*`, the commit
  upsert `ORDER BY repo_id`, the seed `ORDER BY repo_id`, and the guard on
  `(bumped_xid, bumped_at)`. Never set `session_replication_role` or change
  `content_files.repo_id` in Go; `fence_guard_test.go` fails otherwise.

## Tests

- `schema_test.go`, `port_test.go`, `encoding_test.go`, `fence_test.go`,
  `fence_migration_test.go`, `versions_guard_test.go`, `derive_reader_test.go`
  and `derive_test.go` (over the scripted database in `derive_fake_test.go`)
  run without a database. `export_test.go` exposes the derive's test hooks to the live
  tests.
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
