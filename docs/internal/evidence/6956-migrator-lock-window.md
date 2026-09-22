# #6956 schema migrator: wait for the owner, retry a lost lock race

## The two failure modes, reproduced first

Both were surfaced by the separate review of #6959 (migration 119) and both
reproduce on a disposable PostgreSQL 18 database with the pre-fix migrator
(`go/internal/storage/postgres/schema_bootstrap_wait_live_test.go`, run
before the change; `-tags integration`, `ESHU_POSTGRES_RECOVERY_TEST_DSN`):

| mode | staging | pre-fix result |
| --- | --- | --- |
| another bootstrapper owns the schema advisory lock `(5318, 0)` | a second session holds `pg_advisory_lock` for 15 s (3x the 5 s statement lock timeout) | `acquire schema bootstrap ownership: timeout: context deadline exceeded` after 5.005 s; nothing applied |
| a migration statement loses a lock race | a second session holds `LOCK TABLE ... IN SHARE UPDATE EXCLUSIVE MODE` for 15 s (what an anti-wraparound autovacuum holds) while the migrator runs `CREATE INDEX` on that table | `apply index: ERROR: canceling statement due to lock timeout (SQLSTATE 55P03)` after 5.011 s; no receipt recorded, bootstrap failed |

Only `db-migrate` (`cmd/bootstrap-data-plane`) and `bootstrap-index` run the
migrator, so the first mode is a retried Job or bootstrap-index starting
next to `db-migrate`; the second is any deployment whose `fact_records` is
large enough for autovacuum to hold `ShareUpdateExclusiveLock` when a
migration wants a conflicting lock (migration 118's index build and 119's
`CREATE STATISTICS` both do).

## Conflict domains, scopes, ordering

- Contested resources: the session advisory lock `(5318, 0)` between
  bootstrappers; table-level locks on the migrated relation between the
  migrator's DDL and any other session (autovacuum, a long read).
- Transaction scope: each migration statement runs autocommit on the
  session that owns the advisory lock, followed by its ledger `INSERT` on
  the same session. Retry scope: one statement. A statement that failed with
  55P03 never acquired its lock, so it applied nothing; re-running it is
  safe. `CREATE INDEX CONCURRENTLY` can leave an invalid index when it is
  cancelled mid-build, and the existing invalid-index cleanup in
  `execContextWithLockTimeout` drops it before every attempt, so a retry
  rebuilds from clean.
- Idempotency key: `(path, variant)` in `eshu_schema_migrations`; the
  receipt is written only after the statement succeeds, so a retried
  statement records exactly once (asserted by the live test).
- Ordering and deadlock: the waiter holds nothing while it polls the
  advisory lock (`pg_try_advisory_lock`, one try per second, no blocking
  wait), so it cannot participate in a lock cycle; the retrying migrator
  holds the advisory lock but waits on nothing between attempts (it sleeps),
  and the session it waits for (autovacuum, another client) never waits on
  the advisory lock. No cycle is possible. Polling has no queue order (the
  previous blocking `pg_advisory_lock` queued waiters FIFO): when the lock
  frees, every waiter races for it and a waiter can lose repeatedly. Each
  is bounded only by its own ownership wait, which is acceptable for the
  two bootstrappers that exist (a Job and bootstrap-index) and is stated in
  the public doc rather than hidden.
- Starvation bounds: ownership wait 4 m (`ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT`),
  statement retry budget 4 m of backoff (`ESHU_SCHEMA_LOCK_RETRY_BUDGET`,
  backoff 5 s doubling to 15 s), both also cut by the caller's context. The
  defaults sum to 8 m so a run stays inside the chart's schema bootstrap Job
  deadline (`schemaBootstrap.activeDeadlineSeconds: 600`); a bound the Job
  cannot reach would never print its holder diagnostic, and a unit test pins
  the sum under 600 s. The 5 s statement `lock_timeout` is unchanged, so a
  single attempt still cannot pin a table lock queue for long.
- Retry boundary: only SQLSTATE 55P03 is retried. Any other failure returns
  on the first attempt with the original error.

## Change

`go/internal/storage/postgres/coordination` (new leaf, root -> leaf only)
holds `WaitForOwnership` and `RetryOnLockTimeout` as pure loops over an
injected `Locker`, `Sleeper`, and clock. The root keeps the session locker
(`connAdvisoryLocker`, `pg_try_advisory_lock` plus a `pg_locks` join for the
holder description), the bounds and their defaults, and the ledger loop,
which now wraps each statement in `RetryOnLockTimeout`. `BootstrapOptions`
gains `OwnershipWait` and `LockRetryBudget`, and `BootstrapOptionsFromEnv`
reads them from the two environment variables (unset keeps the package
default; non-durations and non-positive values, including an explicit
`0s`, are refused by name). `bootstrap-data-plane` and `bootstrap-index`
both use it; the local supervisor applies its schema through
`ApplyDefinitions` without the ownership lock and is unaffected. The holder
query filters `pg_locks` on the current database: advisory locks are per
database, so the same key held elsewhere on the instance neither blocks nor
is named.

## Proof

- Hermetic (`go test ./internal/storage/postgres/coordination`): retry
  until the lock clears with backoff 1 s, 2 s, 4 s and a `lock_recovered`
  event naming `attempts=4`; no retry on a non-55P03 error; budget
  exhaustion after exactly the attempts the budget allows, error naming the
  budget and the path and still classifying as 55P03; context cancellation
  during backoff returns `context.Canceled` after one attempt; ownership
  polling logs the holder and proceeds on the third try; ownership give-up
  names the wait and the holder.
- `TestBootstrapOptionsFromEnv`: both knobs, all five cases each (unset,
  set, garbage, zero, negative), plus both set at once;
  `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline` pins the
  defaults' sum under 600 s. `cmd/bootstrap-data-plane` and
  `cmd/bootstrap-index` each prove the hop from the environment to the
  migrator on a fake executor: a refused knob stops before any statement,
  an accepted one executes every bootstrap definition.
- Live (same tests as the RED table, after the change; each run uses
  unique object and receipt names and cleans up, and asserts on the
  captured log that the wait or the retry actually happened, so a rerun
  cannot pass vacuously): the waiter finishes after the 15 s owner
  released, its waiting log names that owner's backend pid and not the pid
  of a same-key holder in another database of the instance, and the table
  exists; the retried `CREATE INDEX CONCURRENTLY` (migration 118's shape)
  succeeds behind the 15 s table lock with `lock_wait` and `lock_recovered`
  events, the index is valid, and the ledger holds exactly one receipt.
  The pre-existing `TestBootstrapRetryAfterRecordedIndexRecoveryFailsLive`
  and the six schema-ledger live tests still pass. Timings are in the
  review packet's task logs.

No-Regression Evidence: the happy path executes the same statements on the same session as before with one `pg_try_advisory_lock` round trip replacing the blocking `pg_advisory_lock` (both return immediately when the lock is free); no migration statement, ledger query, or lock timeout changed. Measured on the same disposable database: `TestBootstrapRetryAfterRecordedIndexRecoveryFailsLive` (bootstrap of a two-definition layout, twice, plus the recovery path) takes 0.03-0.05 s on origin/main 4a04039dbb (three runs) and 0.03 s on this branch. The new behavior only runs while another session is in the way, where the alternative was a failed bootstrap.

Observability Evidence: `bootstrap.postgres.ownership.waiting` (holder from `pg_locks` joined to `pg_stat_activity`, current database only: pid, application_name when the client set one, state, connected_for; waited_ms, wait_ms) on the first failed try and then every 15 s while blocked, `bootstrap.postgres.ownership.acquired` (waited_ms, polls) once the wait ends, `bootstrap.postgres.migration.lock_wait` (path, attempt, backoff_ms, slept_ms, budget_ms, error) per retry, `bootstrap.postgres.migration.lock_recovered` (path, attempts, slept_ms) on success; the terminal errors name the holder or the spent budget. The events are documented in `docs/public/deployment/service-runtimes-bootstrap.md` and `docs/public/reference/environment-runtime-storage.md`; `scripts/verify-telemetry-coverage.sh` reports no new untracked stage for the leaf (it is a helper of the existing schema bootstrap, not a pipeline stage), and the grandfathered coverage table cannot grow.
