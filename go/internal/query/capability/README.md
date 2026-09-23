# capability

Serves the capability catalog: which capabilities exist, and the truth ceiling
each one reaches at a given profile.

## What it owns

- `handler.go` — `Handler`, mounted at GET `/api/v0/capabilities` by root's
  `APIRouter`, and behind the `get_capability_catalog` MCP tool. Supports a
  compact and a full projection, selected by `?view=`.
- `lookup.go` — the live registry view plus `CatalogKey`, the capability id
  for the catalog read itself.
- `handler_list_test.go` — drives the route through a bare `ServeMux`, without
  root's `APIRouter`. The router-level tests stay in package `query`.

## What it does not own

Capability **registration**. The `register` function, the per-capability rows,
and the support matrix live in `go/internal/query/contract`. This package
reads the resulting registry through
`querycontract.CompatibilityCapabilityMatrix`; importing `query/contract`
directly would create a cycle, because those rows depend on the shared
contract types this package also uses.

Root's `capability_keys.go` likewise stays in `package query`: those five ids
are named by root's own routes, not by this handler.

## Dependencies and telemetry

Depends only on `query/querycontract` for HTTP, profile, and truth-envelope
primitives, and on `query/queryauth` for permission checks. It adds no span,
metric, or log of its own; its reads are served from an in-memory registry
with no backend call, so there is nothing to time.
