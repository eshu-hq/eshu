# storage/postgres/coordination

Bounded wait and retry loops for the Postgres schema migrator (#6956).

## What lives here

- `WaitForOwnership` polls a `Locker` until the schema advisory lock is
  taken, logs the current holder (`bootstrap.postgres.ownership.waiting`)
  while it waits, logs `bootstrap.postgres.ownership.acquired` once it gets
  the lock after waiting, and fails with the holder named once
  `OwnershipPolicy.Wait` is spent.
- `RetryOnLockTimeout` re-runs one migration statement after
  `lock_timeout` (SQLSTATE 55P03, `IsLockNotAvailable`) with doubling backoff
  from `LockRetryPolicy.InitialBackoff` to `MaxBackoff`, logging
  `bootstrap.postgres.migration.lock_wait` per retry and
  `bootstrap.postgres.migration.lock_recovered` on success, and fails once
  the next backoff would exceed `Budget`. Any other error returns on the
  first attempt untouched.
- `SleepContext` is the production `Sleeper`; tests inject a recorder and a
  fixed clock, so every bound is proven without a database.
- `ConcurrentIndexBuildPlan` (production's only caller of
  `IsSoleConcurrentIndexStatement`) classifies a bare `CREATE`/`DROP INDEX
  CONCURRENTLY` statement once and exempts it from
  `lock_timeout` entirely (0, disabled) instead of retrying it through
  `RetryOnLockTimeout` (#7004): a build canceled by `lock_timeout` restarts
  its table scan from zero on the next attempt, so on a database with
  steadily overlapping transactions it never converges before the retry
  budget runs out. `LockTimeoutSetting` renders the `set_config('lock_timeout',
  ...)` value (`"0"` for disabled, never `time.Duration`'s `"0s"`).
  `RunWithConcurrentIndexBuildLogging` wraps the exempted statement's
  execution with its own `bootstrap.postgres.migration.concurrent_index_build.
  starting`/`.finished` log pair, since it has no `lock_timeout` left to
  retry after and so never gets `lock_wait`/`lock_recovered`.

## What deliberately stays in the postgres root

The advisory lock (`SQLDB.withSchemaBootstrapLock`, the session `Locker`),
the migration ledger, and `applyTrackedDefinitions` remain in
`go/internal/storage/postgres`: the root asserts a package-private locker
contract on `SQLDB`, and moving the lock implementation out would silently
degrade bootstrap to unlocked DDL (see `docs/internal/design/storage-collector-tree.md`).
This package imports only the standard library, pgx's `pgconn` error type,
and `internal/telemetry`, so the dependency runs root -> coordination only.

## Proof

`go test ./internal/storage/postgres/coordination` covers retry until the
lock clears, no retry on other errors, budget exhaustion (error keeps its
55P03 classification), context cancellation during backoff, ownership
polling until release, ownership give-up after the wait, and the #7004
statement classifier (`TestIsSoleConcurrentIndexStatement`,
`TestConcurrentIndexBuildPlan`, `TestLockTimeoutSetting`). The live
regressions `TestBootstrapWaitsForOwnershipHeldLongerThanLockTimeoutLive`
and `TestBootstrapConcurrentIndexBuildWaitsOutLockHolderLive` in the root
package (`-tags integration`, `ESHU_POSTGRES_RECOVERY_TEST_DSN`) drive the
real migrator against a held advisory lock and a held table lock. The
second no longer exercises this package's retry loop for that statement
shape: `ConcurrentIndexBuildPlan` (#7004) exempts a bare CREATE/DROP
INDEX CONCURRENTLY statement from lock_timeout entirely, so it waits out a
conflicting lock instead of hitting 55P03 and retrying.
