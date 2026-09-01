# MCP Kubernetes-correlation route selection

## Purpose

This package owns family membership and pure internal-request selection for the
MCP Kubernetes-correlation tool: the bounded listing of the reducer's Kubernetes
workload ownership and drift correlations, which say whether a live workload's
image is owned by a known deployment source, has drifted from it, or has no
source at all.

## Ownership boundary

This package owns Kubernetes-correlation family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp` keeps
tool registration and its client-visible order, global route fanout, the private
adapter, HTTP dispatch, authorization, timeouts, response budgets, envelopes,
summaries, and telemetry. `internal/query` owns the bounded read this path
reaches, including the anchor rule, the required `limit` and its 1..200 bound,
the access-scope short-circuit for a caller with no grant, and the keyset
paging behind `after_correlation_id`.

## Exported surface

- `Route` selects the internal request for a Kubernetes-correlation tool
  without executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handler retains the shared
API request duration and error metrics and its own
`SpanQueryKubernetesCorrelations` span.

## Gotchas / invariants

- The import path ends in `kubernetes`, while the declared package is
  `kubernetestools`. The root uses an explicit import alias.
- The query carries exactly ten keys: `after_correlation_id`, `cluster_id`,
  `drift_kind`, `image_ref`, `limit`, `namespace`, `outcome`, `scope_id`,
  `source_digest`, and `workload_object_id`. The handler reads each by name
  with no catch-all, and a dropped key fails in one of four ways. `limit` is
  required, so losing it returns 400 on every request. The six anchors are
  required as a group, so losing one returns 400 only for the caller whose
  sole anchor it was and silently widens every other caller's page past the
  anchor they named. `outcome` and `drift_kind` are optional equality filters,
  so losing one returns 200 over every outcome or drift kind.
  `after_correlation_id` is the keyset cursor, so losing it returns 200 from
  the first page again, and a caller continuing a truncated page sees rows it
  already has.
- Every key is sent even when the caller omitted it, so the handler sees an
  explicitly empty filter rather than no filter key at all. The handler trims
  each value, so an all-whitespace anchor counts as absent there.
- `limit` defaults to 50. That is the dispatcher's default, not the handler's:
  the handler has none and rejects an absent `limit` outright, so the default
  is what keeps an MCP caller who omitted `limit` from a 400. The handler
  still enforces its own bound; a zero, negative, or over-200 value is
  forwarded as-is and rejected there with `limit must be between 1 and 200`,
  not clamped. The advertised schema names the same 1..200 range.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`, and
  `float64` are accepted, a `float64` truncates toward zero, and every other
  type falls back to the default. A stringified `"25"` therefore becomes a
  50-row page rather than an error.
- The listing pages by `after_correlation_id` only; it has no `offset`, no
  `group_by`, and no count or inventory sibling. The handler reports
  `truncated` and a `next_cursor` carrying the last row's correlation ID when
  more rows exist.
- `Route` returns a fresh query map per call, so a caller may mutate one result
  without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure Kubernetes-correlation
route selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handler executes the request.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [Kubernetes correlation read model](../../../../docs/internal/design/388-kubernetes-correlation-readmodel.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
