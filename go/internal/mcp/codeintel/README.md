# MCP code-intelligence route selection

## Purpose

This package owns family membership and pure internal-request selection for
the eight MCP code-intelligence tools: entity-name search, symbol search,
structural inventory, call graph metrics, route-caller tracing, code-topic
investigation, the language-specific AST query, and the function call-chain
finder.

## Ownership boundary

This package owns code-intelligence family membership and the mapping from
decoded arguments to a dependency-neutral internal request. `internal/mcp`
keeps tool registration order (`find_code`, `find_symbol`,
`execute_language_query`, and `find_function_call_chain` live in
`tools_codebase.go`; `investigate_code_topic` in `tools_code_topic.go`;
`inspect_call_graph_metrics` in `tools_call_graph_metrics.go`;
`inspect_code_inventory` in `tools_structural_inventory.go`; and
`trace_route_callers` in `tools_route_to_caller.go`), global route fanout,
the private `codeIntelRoute` adapter in `dispatch.go`, HTTP dispatch,
authorization, timeouts, response budgets, envelopes, summaries, and
telemetry. `internal/query` owns the bounded reads behind each
`/api/v0/code/...` path.

`search_entity_content` and `search_file_content` are not part of this
family even though both are code-search tools. Both build their request body
from the root `contentSearchBody` helper and stay together in `dispatch.go`'s
switch; moving one into this package would orphan that shared helper from the
pair that owns it. See the sibling entry in `dispatch.go`'s
`entityResolutionRoute` doc comment for the same boundary from the other
side.

## Exported surface

- `Route` selects the internal request for a code-intelligence tool without
  executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP
package keeps transport and dispatch signals, while the HTTP handlers retain
the shared API request duration and error metrics (`request_metrics.go` in
`internal/query`).

## Gotchas / invariants

- The import path ends in `codeintel`, while the declared package is
  `codeinteltools`. The root imports it with an explicit alias.
- Every request is a `POST` with a JSON body and no query string, built
  fresh per call. Each of the eight tools has its own body shape; none is
  shared across tools.
- String fields travel even when empty (an explicit blank filter), matching
  the root switch arms these route selections replaced.
- `limit` and `offset` defaults differ per tool and mirror the value the
  root switch previously sent: `find_code` limit 10; `find_symbol`,
  `inspect_code_inventory`, `inspect_call_graph_metrics`, and
  `investigate_code_topic` limit 25 / offset 0; `trace_route_callers`
  max_depth 2 / limit 25; `execute_language_query` limit 50;
  `find_function_call_chain` max_depth 5. Preserve these exactly — a changed
  default is a client-visible page-size or depth change.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"25"` — falls back to the default.
- `find_symbol`'s `entity_types` travels through
  `routecontract.Arguments.StringSlice`, the same helper the root
  `stringSlice` used: absent or malformed input serializes as `null`, a
  present list serializes as `[]any`.
- Family membership is an explicit name switch, never a prefix match.
- The **route-serves-data registry** (`route_serves_data_registry*.go` in the
  parent package) cites `internal/query` handler files by path, not files in
  this package or `dispatch.go`, so this extraction does not require
  repointing any registry entry — confirmed by searching the registry files
  for `internal/mcp/` path references before this move (none found) and by
  running `TestRouteServesDataRegistry` after it.

No-Observability-Change: this extraction moves only pure code-intelligence
route selection. The root adapter still feeds the same global fanout,
dispatch, authorization, budgets, envelopes, summaries, and transport
telemetry, and the same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
