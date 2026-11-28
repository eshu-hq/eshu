# Postgres IaC reachability store

## Purpose

This package owns durable IaC (infrastructure as code) reachability rows:
per active repository generation, one row per Terraform/Helm/etc. artifact
recording whether the platform graph found a modeled reference to it, and
the operator-facing cleanup finding (`in_use`, `candidate_dead_iac`,
`ambiguous_dynamic_reference`) that follows from that reachability class.

## Ownership boundary

This package owns the `iac_reachability_rows` table, its DDL, batched
upsert, and the cleanup-finding reads (`ListCleanupFindings`,
`ListLatestCleanupFindings`, `CountLatestCleanupFindings`,
`HasLatestRows`) the query and MCP surfaces read through
(`internal/query/iac`). It is distinct from code reachability
(`reachabilitystore.CodeReachabilityStore`), a separate call-graph store
family.

The parent `postgres` package keeps `MaterializeIaCReachability`, the
reducer-side computation that turns content-file evidence into
`IaCReachabilityRow` values and calls `Upsert` here: it is a method on
`IngestionStore`, which has not moved out of root yet (`#6693`).

## Exported surface

- `IaCReachabilityStore` with `NewIaCReachabilityStore(database db.ExecQueryer)` for mutations and `NewIaCReachabilityReader(reader db.Queryer)` for cleanup reads.
- `Upsert`, `ListCleanupFindings`, `ListLatestCleanupFindings`,
  `CountLatestCleanupFindings`, `HasLatestRows`.
- `IaCReachabilitySchemaSQL()` returns the DDL.
- `IaCReachabilityRow`, and the `IaCReachability`/`IaCFinding` enums
  (`IaCReachabilityUsed`, `IaCReachabilityUnused`,
  `IaCReachabilityAmbiguous`; `IaCFindingInUse`, `IaCFindingCandidateDead`,
  `IaCFindingAmbiguousDynamic`).

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows`
  contracts.

## Telemetry

None. This package executes bounded SQL through the injected database
handle; the reducer materializer that calls `Upsert`
(`iac_reachability_materializer.go`, parent `postgres` package) records the
materialization duration and row-count metrics.

## Gotchas / invariants

- `Upsert` writes in bounded batches of 500 rows; `ON CONFLICT` updates
  every mutable column so a re-materialization of the same artifact
  overwrites its prior row rather than duplicating it.
- `ListCleanupFindings`/`ListLatestCleanupFindings` always exclude
  `used` rows; a used artifact is never a cleanup candidate even when
  `includeAmbiguous` is set.
- `ListLatestCleanupFindings`/`CountLatestCleanupFindings`/`HasLatestRows`
  join `scope_generations` and filter `status = 'active'`, so a
  superseded generation's rows never appear in the "latest" surface.
- Do not import the parent `postgres` package: that is an import cycle.
  Root's `iac_reachability_materializer.go` imports this package instead.
