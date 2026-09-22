# AGENTS.md — storage/postgres/coordination guidance

## Read first

1. `go/internal/storage/postgres/coordination/doc.go` -- why the loops live
   here and why the lock itself stays in root.
2. `go/internal/storage/postgres/coordination/README.md` -- exported surface
   and the proof that covers it.
3. `go/internal/storage/postgres/schema_bootstrap_lock.go` -- the caller: the
   session locker, the coordination bounds and their defaults, and the
   migration ledger loop that wraps each statement in `RetryOnLockTimeout`.

## Invariants

- Only SQLSTATE 55P03 is retried. A statement that failed for any other
  reason may have side effects; it must surface on the first attempt.
- Every loop is bounded by its policy and by the caller's context. Never add
  an unbounded wait, and never sleep without the injected `Sleeper`.
- Log events are operator contracts (`bootstrap.postgres.ownership.waiting`,
  `.acquired`, `bootstrap.postgres.migration.lock_wait`, `.lock_recovered`);
  keep their names and attributes stable and document changes in
  `docs/public/observability/telemetry-coverage.md`.
- No import of the postgres root package: the dependency is root ->
  coordination only, so `SQLDB.withSchemaBootstrapLock` keeps satisfying the
  root's package-private locker contract.

## Verification

```bash
cd go && go test ./internal/storage/postgres/coordination -count=1
cd go && go test ./internal/storage/postgres -run 'TestBootstrap' -count=1
# live, against an EMPTY disposable database; the pre-existing recovery test
# in the same package leaves its own table behind and fails on a reused one:
cd go && ESHU_POSTGRES_RECOVERY_TEST_DSN=... go test -tags integration ./internal/storage/postgres -run 'TestBootstrapWaitsForOwnership|TestBootstrapRetriesStatementLockTimeout' -count=1 -p 1
```
