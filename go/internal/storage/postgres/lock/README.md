# Postgres lock store

## Purpose

This package serializes concurrent Postgres writers with transaction-level
advisory locks (`pg_advisory_xact_lock` / `pg_advisory_xact_lock_shared`
over `hashtext` partition keys) so overlapping commits cannot interleave.
It is the `lock/` leaf of the storage/postgres split (#6693).

## Ownership boundary

This package owns the `PackageRegistryIdentityLocker` and
`PlatformGraphLocker` wrapper types, the deferred-maintenance partition-lock
helpers, and their key/SQL constants. `cmd/ingester` builds the package
locker to serialize registry writes per commit; `cmd/reducer` builds the
platform locker around materialization; `cmd/projector` and
`cmd/bootstrap-index` build the package locker around projection and index
writes. Root `internal/storage/postgres` (ingestion, backfill, acceptance
gates) calls the deferred-maintenance helpers directly.

## Exported surface

- `PackageRegistryIdentityLocker{DB}` with
  `WithPackageRegistryIdentityLocks`, constructed as a struct literal.
- `PlatformGraphLocker{DB}` with `WithPlatformLocks`, constructed as a
  struct literal.
- `PackageRegistryIdentityAdvisoryLockKey` key function.
- `AcquireDeferredMaintenanceRepoSharedLock`,
  `AcquireDeferredMaintenanceRepoExclusiveLocks`, `SortedUniqueRepoKeys`,
  `DeferredMaintenanceRepoLockKey`, `DeferredMaintenanceRepoLockKeyFromID`,
  `DeferredMaintenanceLockNamespace`,
  `DeferredMaintenancePartitionedSharedLockSQL`,
  `DeferredMaintenancePartitionedExclusiveLockSQL`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `Beginner`/`Transaction`
  contracts.
- `internal/storage/postgres/scope` for repository scope resolution in the
  deferred-maintenance helpers.

## Telemetry

None of its own. Lock acquisition runs inside the caller's transaction, so
tracing and metrics are the caller's responsibility, not this package's.

No-Observability-Change: this extraction moves only the locker types, the
deferred-maintenance lock helpers, and their key/SQL constants. Lock
predicates, ordering, and key derivation are byte-identical apart from the
package clause and the necessary export of the deferred helpers (root
ingestion callers stay in root until the ingestion steps), and no metric,
span, or log name changes.

No-Regression Evidence: focused `internal/storage/postgres/...` tests
(including this package) run green on the moved tree with `-race`,
including the staying deferred concurrency and split-lock proofs against
the exported helpers; the lock SQL text is unchanged so no plan or
lifecycle proof is re-owed.

## Gotchas / invariants

- Lock ordering is always ascending by ID inside both `With*` wrappers: two
  transactions locking overlapping sets can never deadlock. Do not sort
  descending or iterate map order.
- The deferred-maintenance exclusive path takes one partition lock per
  repository (`SortedUniqueRepoKeys` dedupes first); the shared path takes
  a single namespace-wide shared lock.
- The helpers were exported (uppercase) because root ingestion, backfill,
  and acceptance-gate callers stay in root until the ingestion steps; the
  bodies are byte-identical to the unexported originals.
- Do not import the parent `postgres` package: that is an import cycle.
  Every caller above imports this package directly instead.

## Related docs

- [Postgres storage](../../README.md)
- [storage/postgres target tree](../../../../../../docs/internal/design/6693-postgres-target-tree.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -race -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
