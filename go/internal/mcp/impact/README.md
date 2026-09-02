# MCP impact-analysis route selection

## Purpose

This package owns family membership and pure internal-request selection for
the nine MCP impact-analysis tools: deployment-chain tracing, deployment
config influence, blast radius, change surface (summary and investigation),
contract impact, resource-to-code tracing, dependency-path explanation, and
exposure-path tracing.

## Ownership boundary

This package owns impact-analysis family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp`
keeps tool registration order, global route fanout, the private `impactRoute`
adapter in `dispatch_impact.go`, HTTP dispatch, authorization, timeouts,
response budgets, envelopes, summaries, and telemetry. `internal/mcp/ecosystem`
keeps eight of the nine advertised definitions; `trace_exposure_path` is
registered at the root in `tools_reachability.go`. `internal/query` owns the
bounded reads behind the nine `/api/v0/impact/` paths, including each route's
own depth and limit handling.

## Exported surface

- `Route` selects the internal request for an impact-analysis tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handlers retain the shared
API request duration and error metrics (`request_metrics.go` in
`internal/query`) and their own spans, such as
`SpanQueryChangeSurfaceInvestigation` on the change-surface investigation
path.

## Gotchas / invariants

- The import path ends in `impact`, while the declared package is
  `impacttools`. The root imports it with an explicit alias.
- Every request is a `POST` with a JSON body and no query string. Eight
  builders select their keys explicitly and send every key even when the
  caller omitted it, so a handler sees an explicit empty filter rather than a
  missing field.
- `explain_dependency_path` forwards the caller's decoded argument map itself
  as the body — no selection, no defaults, no coercion — and the returned
  body aliases that map. This pass-through predates the extraction; changing
  it changes the wire body for every caller.
- Dispatcher-side defaults are part of the wire contract and are pinned by
  the tests: `limit` 25 (deployment config, contract impact, change-surface
  investigation) or 50 (blast radius, change surface, resource-to-code);
  `max_depth` 4 (change-surface investigation), 8 (resource-to-code), 5
  (exposure path); `direct_only` true and `max_depth` 0 (deployment chain).
- `trace_deployment_chain` deliberately forwards `max_depth` 0 when the
  caller omits it, so the handler resolves its own operator-safe default
  (`boundedTraceEnrichmentLimit(0)` = 25). Forwarding 8 instead once widened
  the resolved search limit to 80 for callers who changed nothing. The
  handler clamps `max_depth` into [0, 1000] rather than rejecting, so no
  selected value can turn into a 400.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"25"` — falls back to the default.
  Out-of-range values are forwarded as-is; each handler owns its own bound.
- `changed_paths` follows `routecontract.Arguments.StringSlice`: a `[]string`
  widens to `[]any`, and an absent or wrong-typed value travels as JSON
  `null`, never as an empty array.
- The eight selecting builders return a fresh body map per call;
  `explain_dependency_path` is the documented exception above.
- `trace_deployment_chain` and `trace_resource_to_code` are language-parity
  read-surface labels; the #5335 consumer-existence gate resolves them
  against the live tool registry, and this package's switch is now where
  their names are claimed.

No-Observability-Change: this extraction moves only pure impact-analysis
route selection. The root adapter still feeds the same global fanout,
dispatch, authorization, budgets, envelopes, summaries, and transport
telemetry, and the same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [MCP ecosystem registration](../ecosystem/README.md)
- [Read-surface consumer existence gate](../../../../docs/public/reference/read-surface-consumer-existence-gate.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
