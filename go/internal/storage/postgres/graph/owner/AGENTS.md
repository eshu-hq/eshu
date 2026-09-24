# AGENTS.md — Postgres graph node owner ledger guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `store.go` for `GraphNodeOwnerStore` (the #5007 ownership resolver and the
   #5062 `LockUIDs` lock-only path).
4. `backfill.go` for `GraphNodeOwnerBackfillStore` (the one-time upgrade
   seed).
5. `docs/internal/design/5007-cross-scope-node-ownership.md` for the design
   background.

## Invariants

- `LockUIDs` MUST issue the SAME `GraphNodeOwnerAcquireLocksSQL` statement,
  keyed by the SAME `GraphNodeOwnerAdvisoryKey` derivation, that
  `ResolveOwnedUIDs`'s `acquireLocks` path uses. They must never drift —
  `acquireLocks` delegates to `LockUIDs` specifically to guarantee this.
- Keep the upsert's `WHERE excluded.source_order_key >
  graph_node_owner.source_order_key` guard: it is the atomic max resolution
  the whole ledger depends on. Never relax it to an unconditional overwrite.
- Keep the lock-acquisition SQL's `ORDER BY k` INSIDE the `DISTINCT k`
  subquery, before `pg_advisory_xact_lock` is applied — moving it after would
  reintroduce a deadlock between concurrent overlapping batches.
- `SeedExistingGraphNodeOwners` MUST reject any entry whose `SourceOrderKey`
  does not start with `GraphNodeOwnerBackfillMinimumOrderKeyPrefix`. A normal
  reducer key here could let an upgrade backfill silently overwrite a newer
  real contribution.
- Keep the package clause as `package ownerstore`; callers import the
  `storage/postgres/graph/owner` path without an alias.
- Never import the parent `postgres` package from here — DDL comes from
  `internal/storage/postgres/migrations` instead.
- `GraphNodeOwnerSchemaSQL`, `GraphNodeOwnerUpsertSuffix`,
  `GraphNodeOwnerAcquireLocksSQL`, `GraphNodeOwnerAdvisoryKey`, and
  `DedupeOwnerEntries` are exported ONLY so the root
  `graph_node_owner_store_test.go` SPLIT test can keep asserting on them
  (it also needs root-private `packageRegistryIdentityAdvisoryLockKey`, so it
  cannot fully move here yet). Do not add a new non-test caller for them
  without first re-checking whether that root test can finally move.

## Common changes

- Change the ledger DDL only through the `056_graph_node_owner.sql` /
  `074_graph_node_owner_backfill_state.sql` migrations, with
  `TestGraphNodeOwnerSchemaSQLMatchesMigration` /
  `TestGraphNodeOwnerBackfillStateMigrationDeclaresDurableMarker` updated in
  lockstep. Never edit an already-shipped migration file's SQL text.
- A change to the advisory-lock key derivation or the acquisition statement
  needs `concurrency-deadlock-rigor` proof (contention, lock ordering) before
  landing — this is the mechanism that prevents cross-scope owner-ledger
  deadlocks.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- `LockUIDs` acquiring a different key than `ResolveOwnedUIDs` would let a
  #5062 lock-only writer's graph write race a concurrent owner-ledger
  critical section on the same uid with zero coordination — the exact bug
  #5062 exists to prevent.
- Weakening the upsert's strictly-greater guard would let an
  out-of-order/stale contribution win the ledger and desynchronize it from
  the graph.
- Accepting a non-minimum order key in the backfill path could let the
  one-time upgrade seed silently displace a real reducer-written owner.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/graph/owner/... -race -count=1
go vet ./internal/storage/postgres/graph/owner/...
go test ./internal/storage/postgres/... -run TestGraphNodeOwner -count=1
```

Run `scripts/verify-package-docs.sh` from the repository root.
