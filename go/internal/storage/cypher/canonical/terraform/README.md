# Canonical Terraform State

## Purpose

This package builds Terraform state graph rows and statement-free write parts,
including MATCHES_STATE resolution and Terraform resource attribute promotion.

## Ownership boundary

The leaf owns query templates, row shaping, resolver contracts, and ordering of
Terraform state parts. The parent `cypher` package owns executable `Statement`
wrapping, the writer API, and canonical phase dispatch.

## Exported surface

- `BuildTerraformStateStatements` — build the ordered parts for one materialization
- `Part` — statement-free Cypher plus parameters and execution flags
- `TerraformStateOwnershipResolver` and `TerraformStateConfigMatchResolver` — resolver ports

See `doc.go` for the package contract.

## Dependencies

- `internal/projector/canonical` — materialization and resolver data contracts
- `internal/storage/cypher/infra/search` — backend-neutral search-key query clause

## Telemetry

None. The parent writer executes parts through its existing instrumentation.

## Gotchas / invariants

- Preserve the order of migration, attribute removal, stale-content removal,
  upsert, resource retract, module/output upserts, then relationship retract
  before relationship upsert.
- MATCHES_STATE relationship retractions set `Drain` so the root executor can
  run them as standalone autocommit statements.
- Resolver ambiguity fails closed; transient ownership failures preserve edges.

## Related docs

- `docs/public/reference/cypher-performance.md`
- `go/internal/storage/cypher/README.md`
