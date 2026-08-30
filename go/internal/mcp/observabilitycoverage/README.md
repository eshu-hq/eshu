# MCP observability-coverage route selection

## Purpose

This package owns family membership and pure internal-request selection for the
MCP observability-coverage tool: the bounded, cursor-paged listing of which
monitored cloud resources and services carry alarm, dashboard, log, or trace
coverage, and which coverage gaps remain.

## Ownership boundary

This package owns observability-coverage family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp` keeps
tool registration and its client-visible order, global route fanout, the private
adapter, HTTP dispatch, authorization, timeouts, response budgets, envelopes,
summaries, and telemetry. `internal/query` owns the bounded read this path
reaches, including scope anchoring, the 1-200 limit bound, and cursor paging.

## Exported surface

- `Route` selects the internal request for an observability-coverage tool
  without executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handler retains the shared
API request duration and error metrics.

## Gotchas / invariants

- The import path ends in `observabilitycoverage`, while the declared package is
  `observabilitycoveragetools`. The root uses an explicit import alias.
- The query carries exactly twelve keys: `after_correlation_id`,
  `coverage_signal`, `coverage_status`, `limit`, `observability_object_ref`,
  `outcome`, `provider`, `resource_class`, `scope_id`, `source_class`,
  `target_service_ref`, and `target_uid`. That is the widest key set the
  repository router selects, and the handler reads each by name with no
  catch-all. Dropping one is not uniformly silent: `limit` is required and a
  scope anchor is required, so losing either returns 400; losing
  `coverage_status`, `source_class`, `resource_class`, or `outcome` widens the
  caller's page to rows they filtered out, so the answer reports coverage the
  query never asked about; and losing `after_correlation_id` breaks keyset
  paging.
- Every key is sent even when the caller omitted it, so the handler sees an
  explicitly empty filter rather than no filter key at all.
- `limit` defaults to 50. That is the dispatcher's historical default, not a
  handler default, and the handler still enforces its own 1-200 bound.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`, and
  `float64` are accepted, a `float64` truncates toward zero, and every other
  type falls back to the default.
- The listing has no `offset`, no `group_by`, and no aggregate sibling; paging
  is cursor-only through `after_correlation_id`.
- `Route` returns a fresh query map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure observability-coverage
route selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handler executes the request.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
