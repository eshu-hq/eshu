# #7004 concurrent-index-build lock_timeout exemption evidence

## Theory shim (prove-the-theory-first)

Local shim, `postgres:16`, one other session holding an open transaction
(`BEGIN; SELECT pg_sleep(n)`) for 20 s while a second session runs
`set lock_timeout='2s'; create index concurrently t_v_idx on t(v);`:

```
ERROR:  canceling statement due to lock timeout
Time: 2172.253 ms
t_v_idx | indisvalid = f
```

An idle-in-transaction holder (`BEGIN; SELECT 1;` with no further statement,
no `pg_sleep`) does **not** reproduce the cancellation: a concurrent build
started behind it with `lock_timeout='1s'` completes immediately. Only an
actively running statement on the other session holds back the snapshot
horizon `CREATE INDEX CONCURRENTLY`'s internal wait blocks on. This matches
production: ops-qa's blockers are ordinary in-flight application queries,
not idle sessions.

## Baseline (before fix, origin/main 874012542e)

`go test ./internal/storage/postgres -run TestConcurrentIndexBuildOutlivesOlderTransactionLive`
against `postgres:16` (local Docker, `postgres:16` image, no extensions),
table `schema_cic_older_tx_proof_<ts>` with a single `id INTEGER NOT NULL`
column, zero rows, through the real bootstrap apply path
(`ApplyDefinitionsWithLockTimeout`) with `lock_timeout=1s` while another
session runs `SELECT pg_sleep(3)` (opened ~200ms before the build starts):

```
schema_lock_timeout_integration_test.go:264: CREATE INDEX CONCURRENTLY behind a 3s open transaction (lock_timeout 1s) failed after 1.011s:
apply proof_index: ERROR: canceling statement due to lock timeout (SQLSTATE 55P03), want it to wait out the transaction instead of canceling
--- FAIL: TestConcurrentIndexBuildOutlivesOlderTransactionLive (3.04s)
```

Failure lands at ~1.01 s, matching the configured `lock_timeout`; the
blocking session is still running (3 s hold) when it fails.

Second baseline shape, `-tags integration`, `TestBootstrapConcurrentIndexBuildWaitsOutLockHolderLive`
(pre-fix name `TestBootstrapRetriesStatementLockTimeoutLive`): a table held
in `SHARE UPDATE EXCLUSIVE MODE` by another session for `3 * defaultSchemaLockTimeout`
(15 s) while `applyBootstrapDefinitionsWith` runs a `CREATE INDEX CONCURRENTLY
IF NOT EXISTS` migration statement (the production shape, migration 118) with
the default 5 s `lock_timeout` and the default 3 m `ESHU_SCHEMA_LOCK_RETRY_BUDGET`.
Before the fix this statement retried with doubling backoff
(`bootstrap.postgres.migration.lock_wait` / `.lock_recovered`) every time it
hit `lock_timeout`, and each retry restarts the index build's table scan from
zero — on ops-qa's `fact_work_items` (113) and `fact_records` (118) targets,
with client transactions older than 5 s observed on 3 of 3 samples 20 s
apart (longest 84.2 s, 2026-09-23 12:04Z), that retry loop cannot converge
before the 3 m budget runs out.

## After (this fix, HEAD)

Same `TestConcurrentIndexBuildOutlivesOlderTransactionLive` test, same
`postgres:16` container, same blocker shape, run against the fixed code:

```
--- PASS: TestConcurrentIndexBuildOutlivesOlderTransactionLive (3.04s)
```

The build now waits the full 3 s instead of canceling at 1 s, then succeeds;
`proofIndexValidity` confirms the resulting index is `indisvalid = true`.

`TestBootstrapConcurrentIndexBuildWaitsOutLockHolderLive` (`-tags
integration`, `ESHU_POSTGRES_RECOVERY_TEST_DSN` against the same
`postgres:16` container):

```
--- PASS: TestBootstrapConcurrentIndexBuildWaitsOutLockHolderLive (15.16s)
```

Waits the full 15 s `SHARE UPDATE EXCLUSIVE` hold (past 3x the old
`lock_timeout`) and succeeds; the migration receipt is recorded exactly once
(`eshu_schema_migrations` row count == 1) and the built index is valid. No
`bootstrap.postgres.migration.lock_wait` is logged (asserted absent); instead
`bootstrap.postgres.migration.concurrent_index_build.starting` and
`.finished` (with `duration_ms` ≈ 15000) are logged, giving an operator the
same visibility the old retry logging gave, for a statement that no longer
retries.

## No-Regression Evidence

No-Regression Evidence: classifying a bare `CREATE`/`DROP INDEX CONCURRENTLY` statement removes it
from `lock_timeout`/`RetryOnLockTimeout` entirely instead of changing that
loop's behavior; every other statement shape is untouched (same
`lock_timeout`, same retry policy, proven by
`TestConcurrentIndexBuildLockTimeout`'s non-CIC case returning the caller's
unmodified bound, and by the unchanged
`TestBootstrapWaitsForOwnershipHeldLongerThanLockTimeoutLive` /
coordination package tests, all still green). No SQL query shape, index
definition, or hot-path Cypher changed. `go test
./internal/storage/postgres/... ./cmd/bootstrap-data-plane/... -count=1`
passes with no behavior change outside the CIC/DIC statement path.

## Observability Evidence

Observability Evidence: new structured log events on the production bootstrap path
(`schemaConnectionExecutor.execContextWithLockTimeout` via
`coordination.RunWithConcurrentIndexBuildLogging`):
`bootstrap.postgres.migration.concurrent_index_build.starting` (no fields
beyond the shared logger context) and `.finished` (`duration_ms`, `failed`
bool), captured verbatim in the `TestBootstrapConcurrentIndexBuildWaitsOutLockHolderLive`
log buffer above. These sit next to the existing
`bootstrap.postgres.migration.lock_wait`/`.lock_recovered` pair and the
per-statement `bootstrap.postgres.migration.applying`/`.recorded` events
that already wrap every migration statement (including CIC ones), so an
operator watching the bootstrap Job for a long-running statement has a named
signal instead of silence. Documented in
docs/public/deployment/service-runtimes-bootstrap.md and
go/internal/storage/postgres/coordination/{README.md,AGENTS.md}.

## Safety

`ShareUpdateExclusiveLock` (PG16 Table 13.2) does not conflict with
`RowExclusiveLock`, the mode ordinary `INSERT`/`UPDATE`/`DELETE` take, so
disabling `lock_timeout` for this statement shape never blocks application
writers -- only other sessions requesting `ShareUpdateExclusiveLock` or
stronger (another concurrent index build, `VACUUM`, most DDL) can queue
behind it, and `dropInvalidConcurrentIndexes`'s own `DROP INDEX CONCURRENTLY
IF EXISTS` cleanup runs under the same disabled timeout for the same reason.

This is **not** bounded by the schema bootstrap Job's `activeDeadlineSeconds`:
that Kubernetes deadline kills the bootstrap client pod only. Both
`eshu-bootstrap-data-plane` and `bootstrap-index` run with
`context.Background()` and no signal handling, so the Postgres backend keeps
running the statement to completion or error regardless of the client's
death, still holding the session schema advisory lock. A build that
completes leaves a **valid** index and releases that lock; the next
bootstrap run then waits on the same advisory lock (`WaitForOwnership`) and
can itself need retrying or investigating if the orphan ran unusually long.
A build that is canceled, terminated, or errors leaves an **invalid** index,
which the next run's `dropInvalidConcurrentIndexes` drops before rebuilding
it -- proven by the pre-existing `TestSQLDBRebuildsInvalidConcurrentIndexLive`,
rerun green against this change.
