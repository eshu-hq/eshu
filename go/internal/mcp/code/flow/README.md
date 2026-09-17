# MCP code-flow route selection

## Purpose

This package owns family membership and pure internal-request selection for
the four MCP code-flow tools: bounded taint-path evidence, reaching-definition
summaries, control-flow-graph summaries, and program-dependence summaries for
one repository.

## Ownership boundary

This package owns code-flow family membership and the mapping from decoded
arguments to a dependency-neutral internal request. `internal/mcp` keeps tool
registration order (the four definitions live at the root in
`tools_code_flow.go`), global route fanout, the private `codeFlowRoute`
adapter in `dispatch_code_flow.go`, HTTP dispatch, authorization, timeouts,
response budgets, envelopes, summaries, and telemetry. `internal/query` owns
the bounded reads behind the four `/api/v0/code/flow/` paths, including the
limit clamp and the line floor.

## Exported surface

- `Route` selects the internal request for a code-flow tool without executing
  it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP package
keeps transport and dispatch signals, while the HTTP handlers retain the shared
API request duration and error metrics (`request_metrics.go` in
`internal/query`); the code-flow handlers register no span of their own.

## Gotchas / invariants

- The import path ends in `codeflow`, while the declared package is
  `codeflowtools`. The root imports it with an explicit alias.
- Every request is a `POST` with a JSON body and no query string. All four
  tools share one six-key body built fresh per call; the four string keys are
  sent even when empty, so a handler sees an explicit blank filter rather than
  a missing field.
- `repo_id` is the only field that can reject: the handler 400s a blank one
  (`repo_id is required` after trimming). `language`, `symbol`, and
  `file_path` are narrowing filters whose loss silently widens the page.
- `limit` defaults to 25 here, the same value the handler substitutes for any
  limit at or below zero before clamping anything above 100 down to 100, so
  the dispatcher's default is indistinguishable from an omitted limit at the
  handler and no limit value can 400. The advertised schema's 1..100 range
  describes the handler's clamp, not a dispatcher-side check; out-of-range
  values are forwarded as-is.
- `line` deliberately defaults to 0, which is not a filter value: the handler
  floors negatives to 0 and treats 0 as "no line filter", and its
  symbol-ambiguity signal fires only when `line` is 0, so forwarding a
  positive default would silently suppress ambiguity reporting for callers
  who set no line.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"25"` — falls back to the default.
- Family membership is an explicit name-to-path map, never a prefix match,
  even though the four names share the `dispatch_` spelling.

No-Observability-Change: this extraction moves only pure code-flow route
selection. The root adapter still feeds the same global fanout, dispatch,
authorization, budgets, envelopes, summaries, and transport telemetry, and the
same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)
- [MCP tool contract matrix](../../../../docs/public/reference/mcp-tool-contract-matrix.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
