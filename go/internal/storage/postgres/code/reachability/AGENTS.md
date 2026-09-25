# AGENTS.md — Postgres code-reachability store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `store.go`, `loader.go`, and `helpers.go` for the store, pending-input
   loader, and row helpers.
4. `store_test.go`, `store_sql_shape_test.go`, and
   `store_route_liveness_live_test.go` for the store contract, SQL-shape
   assertions, and the #5494 route-liveness live proof.
5. `internal/reducer/AGENTS.md` for how `CodeReachabilityProjectionRunner`
   drives this store's `InputLoader`/`RowWriter` methods end to end.

## Invariants

- Keep the DDL, batched upsert/replace, and watermark-stamping predicates
  byte-identical when moving or refactoring code; predicate or lifecycle
  changes need their own issue with EXPLAIN ANALYZE and contention proof per
  `eshu-postgres-rigor`.
- Never edit the migration files under `../../migrations`; their checksums
  are load-bearing.
- `ReplaceRepositoryRows` must always stamp the watermark, even for an empty
  replacement; dropping that breaks the #5376 upgrade-backfill anti-loop
  proof.
- Bump `CodeReachabilityVerdictSchemaEpoch` whenever verdict semantics
  change so every projected repo re-projects exactly once; see the constant's
  doc comment in `store.go` for the epoch history.
- Keep the package clause as `package reachabilitystore`; callers import the
  `storage/postgres/code/reachability` path without an alias.
- Never import the parent `postgres` package from non-test code here.
- `code_reachability_upgrade_backfill_live_test.go` stays in the parent
  `postgres` package: it defines `testSuffix`, which unrelated root live
  tests also use. Do not move it here without first extracting `testSuffix`
  to something every caller can share; until then, this package's own
  `store_route_liveness_live_test.go` keeps its own copies of the DSN-open,
  suffix, and cleanup helpers it needs.

## Common changes

- Change a schema constant only with its `SchemaSQL`-shape test and a new
  migration file updated together, keeping the Go constant and migration SQL
  byte-identical.
- Change loader predicates (roots, Ruby classes, Rails route facts) only
  with the corresponding query-shape test updated and, for a live-DB
  behavior change, the route-liveness live proof re-run against
  `ESHU_POSTGRES_DSN`.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Skipping the watermark stamp on an empty replacement re-schedules a
  no-Ruby or zero-verdict repo forever (the #5376 P1 defect this store's
  anti-loop tests guard against).
- A verdict-semantics change without bumping
  `CodeReachabilityVerdictSchemaEpoch` leaves already-indexed repos on a
  stale verdict without re-projecting.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/code/reachability/... -count=1
go vet ./internal/storage/postgres/code/reachability/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
