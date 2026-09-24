# Postgres graph node owner ledger

## Purpose

This package owns the #5007 cross-scope same-uid node ownership ledger and
its #5062 lock-only companion path: the Postgres-atomic mechanism that
decides which ingestion scope's contribution wins when two scopes project
the same canonical graph node uid, and the shared per-uid advisory-lock
keyspace non-owner-ledger writers serialize against.

## Ownership boundary

This package owns the `graph_node_owner` table (migration
`056_graph_node_owner.sql`) and the `graph_node_owner_backfill_state` table
(migration `074_graph_node_owner_backfill_state.sql`), their DDL, and every
read/write against them: `GraphNodeOwnerStore` (`store.go`) and
`GraphNodeOwnerBackfillStore` (`backfill.go`).

`GraphNodeOwnerStore` methods operate against a caller-supplied transaction
(a `db.ExecQueryer` that MUST be a live transaction) because the advisory
locks they acquire have to stay held across the caller's subsequent graph
write — the owner-ledger decision and the graph write together form the
per-uid critical section. The caller (`go/internal/graphowner.Gate` and
`go/internal/graphowner.LockOnlyGate`) owns commit/rollback and therefore
lock release. This package does not call the graph itself.

Migrations `070`-`073` and `086` (four concurrent partial indexes over the
winning row's resource type/provider/region/account, plus the runtime-digest
read path) live in the `storage/postgres/migrations` package. They back the
CloudResource identity page read by `PostgresCloudResourceListStore` in
`internal/query`, which queries `graph_node_owner` directly.

## Exported surface

- `GraphNodeOwnerStore` with `NewGraphNodeOwnerStore()`; `EnsureSchema`,
  `ResolveOwnedUIDs`, `LockUIDs`, `ReleaseOwnedUIDs`.
- `GraphNodeOwnerEntry` — one uid's ledger contribution (`UID`,
  `SourceOrderKey`, `WinningRow`).
- `GraphNodeOwnerBackfillStore` with `NewGraphNodeOwnerBackfillStore(database)`;
  `IsCloudResourceBackfillComplete`, `SeedExistingGraphNodeOwners`,
  `MarkCloudResourceBackfillComplete`. `GraphNodeOwnerBackfillDB` is the
  `db.ExecQueryer` + `db.Beginner` surface it needs.
- `GraphNodeOwnerBackfillMinimumOrderKeyPrefix` — the sentinel prefix every
  backfilled entry's `SourceOrderKey` must carry.
- `GraphNodeOwnerSchemaSQL`, `GraphNodeOwnerUpsertSuffix`,
  `GraphNodeOwnerAcquireLocksSQL`, `GraphNodeOwnerAdvisoryKey`,
  `DedupeOwnerEntries` — internal implementation exported ONLY for the root
  `#6693` SPLIT test (`postgres` package's
  `graph_node_owner_store_test.go`), which still compares
  `GraphNodeOwnerAdvisoryKey` against root-private
  `packageRegistryIdentityAdvisoryLockKey` and cannot move here until that
  lock-namespace family gets its own leaf. No other caller should depend on
  them.

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `Executor`/`ExecQueryer`/
  `Beginner`/`Transaction` contracts.
- `internal/storage/postgres/migrations` for `BootstrapDefinitions` (the
  embedded DDL source of truth for `graph_node_owner`).
- Test-only: `internal/storage/postgres/fake` (the shared `ExecQueryer` test
  double) and, for `backfill_test.go`/`backfill_live_test.go`, the parent
  `internal/storage/postgres` package (`MigrationSQL`, `SQLDB`) — those two
  test files are `package ownerstore_test` for that reason.

## Telemetry

None. This package executes bounded SQL through the caller-supplied
transaction/database handle and returns structured results
(`contendedLost`, `owned`) for the caller to log or count; it holds no
worker, queue, lease, or retry loop of its own.

No-Observability-Change: this extraction moves the store and its DDL
byte-identically; no metric, span, log field, worker, queue, lease, retry,
or durable write shape changed.

## Gotchas / invariants

- `ResolveOwnedUIDs` and `LockUIDs` MUST use the identical advisory-lock
  acquisition statement (`GraphNodeOwnerAcquireLocksSQL`) and key derivation
  (`GraphNodeOwnerAdvisoryKey`) — `LockUIDs` exists so #5062 lock-only
  callers serialize against a concurrent `ResolveOwnedUIDs` critical section
  on the same uid; a different lock key would provide zero coordination.
- The lock-acquisition statement materializes the sorted, deduplicated key
  set BEFORE calling `pg_advisory_xact_lock`, so two concurrent batches with
  overlapping uids always lock in the same order and cannot deadlock.
- `DedupeOwnerEntries` collapses duplicate uids within one batch to the max
  `SourceOrderKey` before locking/upserting, so lock and upsert argument
  order stay deterministic.
- The upsert (`ON CONFLICT (uid) DO UPDATE ... WHERE
  excluded.source_order_key > graph_node_owner.source_order_key`) only ever
  keeps the strictly-greater order key; never weaken this to an unconditional
  overwrite.
- `SeedExistingGraphNodeOwners` rejects any entry whose `SourceOrderKey`
  does not carry `GraphNodeOwnerBackfillMinimumOrderKeyPrefix` — accepting a
  normal reducer key there would let a backfill race overwrite a newer graph
  write without writing that winner back to the graph.
- Do not import the parent `postgres` package: that is an import cycle.
  `GraphNodeOwnerSchemaSQL`'s DDL comes from `internal/storage/postgres/migrations`
  instead, the same embedded source of truth root's own `BootstrapDefinitions`
  wraps.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/graph/owner/... -race -count=1
go vet ./internal/storage/postgres/graph/owner/...
```

Run `scripts/verify-package-docs.sh` from the repository root.

No-Regression Evidence: `go test ./internal/storage/postgres/graph/owner/... -race -count=1`
covers the store and backfill-store contracts; `go test ./internal/storage/postgres/... -run TestGraphNodeOwner -count=1`
covers the root SPLIT test still asserting on this package's exported
lockstep symbols.
