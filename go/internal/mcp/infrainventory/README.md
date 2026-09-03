# MCP infrastructure-inventory route selection

## Purpose

This package owns family membership and pure internal-request selection for
four MCP infrastructure tools: the graph-backed resource-count summary
(`count_infra_resources`), the paginated grouped-inventory read
(`get_infra_resource_inventory`), the single-resource investigation packet
(`investigate_resource`), and the infrastructure-relationship analyzer
(`analyze_infra_relationships`).

## Ownership boundary

This package owns infrastructure-inventory family membership and the mapping
from decoded arguments to a dependency-neutral internal request.
`internal/mcp` keeps tool registration order (`count_infra_resources` and
`get_infra_resource_inventory` live in `tools_infra_resource_aggregates.go`;
`investigate_resource` and `analyze_infra_relationships` live in
`ecosystem/tools.go`), global route fanout, the private `infraInventoryRoute`
adapter in `dispatch_infra_resource_aggregates.go`, HTTP dispatch,
authorization, timeouts, response budgets, envelopes, and telemetry.
`internal/query` owns the bounded reads behind each
`/api/v0/infra/resources/...`, `/api/v0/impact/resource-investigation`, and
`/api/v0/infra/relationships` path.

The sibling `find_infra_resources` tool is not part of this family even
though it shares the `/api/v0/infra/resources` namespace with
`count_infra_resources` and `get_infra_resource_inventory`. It builds a
different request shape and is owned by the `infrasearch` package, reached
through the `infraResourceSearchRoute` adapter — searching resources and
counting/investigating them are different families that happen to share a
namespace.

## Exported surface

- `Route` selects the internal request for an infrastructure-inventory tool
  without executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP
package keeps transport and dispatch signals, while the HTTP handlers retain
the shared API request duration and error metrics (`request_metrics.go` in
`internal/query`).

No-Regression Evidence: this is a pure route-selection move with no new
graph, storage, queue, or HTTP query work. `count_infra_resources`,
`get_infra_resource_inventory`, `investigate_resource`, and
`analyze_infra_relationships` keep the exact method, path, and body/query
keys (including numeric defaults) the pre-extraction root switch arms sent,
diffed against `git show origin/main:go/internal/mcp/dispatch.go`.
`go test ./internal/mcp/infrainventory ./internal/mcp/... ./cmd/mcp-server -count=1`
covers per-tool request selection, `resolveRoute` delegation for all four
tools, and the route-serves-data registry.

No-Observability-Change: this extraction moves only pure infrastructure-
inventory route selection. The root adapter still feeds the same global
fanout, dispatch, authorization, budgets, envelopes, and transport telemetry,
and the same query handlers execute the requests.

## Gotchas / invariants

- The import path ends in `infrainventory`, while the declared package is
  `infrainventorytools`. The root imports it with an explicit alias.
- `count_infra_resources` and `get_infra_resource_inventory` are `GET`
  requests with query parameters and no body; `investigate_resource` and
  `analyze_infra_relationships` are `POST` requests with a JSON body and no
  query string. All four are built fresh per call.
- `get_infra_resource_inventory` defaults `group_by` to `"provider"` and
  `limit`/`offset` to `100`/`0` when the caller omits them, matching the
  pre-extraction root switch arm this route selection replaces.
- `investigate_resource` defaults `max_depth` to `4` and `limit` to `25`;
  `analyze_infra_relationships` forwards `target` as `entity_id` and
  `query_type` as `relationship_type` with no defaults, matching the
  pre-extraction root switch arms this route selection replaces.
- String fields travel even when empty (an explicit blank filter), matching
  the root switch arms these route selections replaced.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"100"` — falls back to the default.
- Family membership is an explicit name switch, never a prefix match.
- None of the four tools validates its arguments before building a request,
  unlike `servicecontext`'s `get_service_context`/`get_service_story`; `Route`
  reports only `(Request, bool)`, never an error.
- The **route-serves-data registry** (`route_serves_data_registry*.go` in the
  parent package) cites `internal/query` handler files by path (for example
  `go/internal/query/infra_resource_aggregates_handler.go`,
  `go/internal/query/impact_resource_investigation.go`, and
  `go/internal/query/infra_relationship_filter.go`), never
  `go/internal/mcp/dispatch.go`,
  `go/internal/mcp/dispatch_infra_resource_aggregates.go`, or this package,
  so this extraction does not require repointing any registry entry —
  confirmed by searching the registry files for `internal/mcp/` path
  references before this move (none found) and by running
  `TestRouteServesDataRegistry` after it.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
