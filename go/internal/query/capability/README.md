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
and the support matrix live in `go/internal/query/contract`. Those rows add
themselves to the registry from their own `init()`s, so `lookup.go` imports
that package blank -- for the linkage, not for any symbol -- and then reads
the assembled registry back through
`querycontract.CompatibilityCapabilityMatrix`. Nothing under `contract/` or
`querycontract/` imports this package, so the edge runs one way.

Root's `capability_keys.go` likewise stays in `package query`: those five ids
are named by root's own routes, not by this handler.

## Dependencies and telemetry

Three imports, all one-way: `query/querycontract` for HTTP, profile,
pagination, and truth-envelope primitives; `capabilitycatalog` for the `Entry`,
`Maturity`, and `Surface` shapes the full view serializes; and a blank
`query/contract` for registration linkage. Verify with
`go list -f '{{range .Imports}}{{.}}\n{{end}}' ./internal/query/capability/`.

It adds no span, metric, or log of its own; its reads are served from an
in-memory registry with no backend call, so there is nothing to time.
