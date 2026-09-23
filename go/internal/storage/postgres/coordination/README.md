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
polling until release, and ownership give-up after the wait. The live
regressions `TestBootstrapWaitsForOwnershipHeldLongerThanLockTimeoutLive`
and `TestBootstrapConcurrentIndexBuildWaitsOutLockHolderLive` in the root
package (`-tags integration`, `ESHU_POSTGRES_RECOVERY_TEST_DSN`) drive the
real migrator against a held advisory lock and a held table lock. The
second no longer exercises this package's retry loop for that statement
shape: `concurrentIndexBuildLockTimeout` (#7004) exempts a bare CREATE/DROP
INDEX CONCURRENTLY statement from lock_timeout entirely, so it waits out a
conflicting lock instead of hitting 55P03 and retrying.
