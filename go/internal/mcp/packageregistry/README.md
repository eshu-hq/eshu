# MCP package-registry route selection

## Purpose

This package owns family membership and pure internal-request selection for the
six MCP package-registry tools: package, version, dependency, and correlation
listings plus the two package aggregate reads.

## Ownership boundary

This package owns package-registry family membership and the pure mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp` keeps
tool registration and its client-visible order, global route fanout, the private
adapter, HTTP dispatch, authorization, timeouts, response budgets, envelopes,
summaries, and telemetry. `internal/query` owns the bounded reads these paths
reach.

## Exported surface

- `Route` selects the internal request for a package-registry tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handlers retain the shared
API request duration and error metrics.

## Gotchas / invariants

- The import path ends in `packageregistry`, while the declared package is
  `packageregistrytools`. The root uses an explicit import alias.
- `limit` defaults to 50 on the four listing routes and to 100 on the inventory
  route; `offset` defaults to 0. These are the dispatcher's historical defaults,
  not handler defaults, and the handlers still enforce their own bounds.
- An absent or non-string `group_by` falls back to `ecosystem` on the inventory
  route. An explicitly empty string takes the same fallback.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`, and
  `float64` are accepted, a `float64` truncates toward zero, and every other
  type falls back to the default.
- `Route` returns a fresh query map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure package-registry route
selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
