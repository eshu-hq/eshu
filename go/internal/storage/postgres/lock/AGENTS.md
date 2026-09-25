# AGENTS.md — Postgres lock store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `package_registry_identity.go` and `platform_graph.go` for the locker
   wrappers, `deferred_maintenance.go` for the partition-lock helpers.
4. `package_registry_identity_test.go` / `platform_graph_test.go` /
   `shared_intent_acceptance_writer_test.go` for the ordering and fencing
   contracts.

## Invariants

- Keep the lock predicates, key derivation, and ascending-ID ordering
  byte-identical when moving code; ordering or predicate changes need their
  own issue with a deadlock/contention proof per `eshu-postgres-rigor` and
  `concurrency-deadlock-rigor`.
- Keep `WithPackageRegistryIdentityLocks` / `WithPlatformLocks` sorting
  lock acquisition ascending by ID: two transactions locking overlapping
  sets must never be able to deadlock.
- Keep the package clause as `package lockstore`: callers import the
  `storage/postgres/lock` path without an alias.
- Never import the parent `postgres` package from here: `cmd/ingester`,
  `cmd/reducer`, `cmd/projector`, `cmd/bootstrap-index`, and root
  `internal/storage/postgres` import this package directly, and importing
  back would be a cycle.
- The deferred-maintenance helpers are exported (uppercase) because root
  ingestion, backfill, and acceptance-gate callers stay in root until the
  ingestion steps; do not re-lowercase them until the last root caller has
  moved, and then only in the step that moves it.
- Tests here use `internal/storage/postgres/fake` (`fake.ExecQueryer`,
  `fake.Rows`, `fake.Result`) for the fake database double, not
  package-private `fakeExecQueryer`/`queueFakeRows`/`fakeResult`: those
  stayed unexported test-file code in root and cannot be imported from
  here.
- `shared_intent_acceptance_writer_test.go` is `package lockstore_test`
  (external): it imports root `postgres` for
  `NewSharedIntentAcceptanceWriter` and this package for the exported
  deferred helpers. It keeps its own copy of the `advisoryLockManager` /
  `advisoryLockTx` in-memory advisory-lock simulator: root's
  `deferred_maintenance_lock_fakes_test.go` defines the original for the
  staying deferred concurrency proofs, and Go test-only symbols do not
  cross package boundaries.

## Common changes

- Change a lock predicate, key derivation, or acquisition order only with
  the ordering tests updated plus a deadlock/contention proof (overlapping
  lock sets, concurrent claimants, lease-expiry reap races).

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Sorting lock acquisition in non-ascending order (or map order) lets two
  transactions deadlock on overlapping sets.
- Dropping `FOR UPDATE SKIP LOCKED` lets two concurrent claimants take the
  same row; dropping the fencing-token match lets a stale claimant complete
  a re-claimed row.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/lock/... -race -count=1
go vet ./internal/storage/postgres/lock/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
