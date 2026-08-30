# MCP CI/CD run-correlation route selection

## Purpose

This package owns family membership and pure internal-request selection for the
three MCP CI/CD run-correlation tools: the bounded correlation listing plus the
two run-correlation aggregate reads (count and inventory).

## Ownership boundary

This package owns CI/CD run-correlation family membership and the pure mapping
from decoded arguments to a dependency-neutral internal request. `internal/mcp`
keeps tool registration and its client-visible order, global route fanout, the
private adapter, HTTP dispatch, authorization, timeouts, response budgets,
envelopes, summaries, and telemetry. `internal/query` owns the bounded reads
these paths reach, including correlation filtering, repository-access scoping,
and aggregate grouping.

## Exported surface

- `Route` selects the internal request for a CI/CD run-correlation tool without
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

- The import path ends in `cicd`, while the declared package is `cicdtools`.
  The root uses an explicit import alias.
- `limit` defaults to 50 on the listing route and to 100 on the inventory route;
  `offset` defaults to 0 and exists only on the inventory route. These are the
  dispatcher's historical defaults, not handler defaults, and the handlers still
  enforce their own bounds.
- An absent or non-string `group_by` falls back to `outcome` on the inventory
  route. An explicitly empty string takes the same fallback.
- The listing route resolves `provider_run_id` from the argument of that name,
  falling back to `run_id` when it is absent, empty, or not a string. `run_id`
  is still forwarded under its own key, so a caller that sends only `run_id`
  gets both keys populated. Neither aggregate route carries either key.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`, and
  `float64` are accepted, a `float64` truncates toward zero, and every other
  type falls back to the default.
- `Route` returns a fresh query map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure CI/CD run-correlation
route selection. The root adapter still feeds the same global fanout, dispatch,
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
