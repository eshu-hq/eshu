# MCP replatforming-planning route selection

## Purpose

This package owns family membership and pure internal-request selection for
two MCP replatforming tools: the bounded, truth-labeled migration plan
compose (`compose_replatforming_plan`) and the account/environment/service
readiness rollup (`get_replatforming_rollups`).

## Ownership boundary

This package owns replatforming-planning family membership and the mapping
from decoded arguments to a dependency-neutral internal request.
`internal/mcp` keeps tool registration order (both tools live in
`tools_iac.go`), global route fanout, the private `replatformingRoute`
adapter in `dispatch_iac.go`, HTTP dispatch, authorization, timeouts,
response budgets, envelopes, and telemetry. `internal/query` owns the bounded
reads and scope validation behind each `/api/v0/replatforming/...` path,
including the scope_kind check on the plan route.

The sibling `list_aws_runtime_drift_findings` tool is not part of this
family even though it sits next to these two in the pre-extraction root
switch and shares the "not part of the IaC-management family" grouping. It
builds a narrower body against a different path
(`/api/v0/aws/runtime-drift/findings`) and stays in the parent's
`dispatch_iac.go` — planning/summarizing replatforming work and listing raw
AWS runtime-drift findings are different families that happen to have lived
in the same pre-extraction file.

## Exported surface

- `Route` selects the internal request for a replatforming-planning tool
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
graph, storage, queue, or HTTP query work. `compose_replatforming_plan` and
`get_replatforming_rollups` keep the exact method, path, and body keys
(including numeric defaults) the pre-extraction root switch arms sent,
diffed against `git show origin/main:go/internal/mcp/dispatch_iac.go`.
`go test ./internal/mcp/replatforming ./internal/mcp/... ./cmd/mcp-server -count=1`
covers per-tool request selection, `resolveRoute` delegation for both tools,
and the route-serves-data registry.

No-Observability-Change: this extraction moves only pure replatforming-plan
route selection. The root adapter still feeds the same global fanout,
dispatch, authorization, budgets, envelopes, and transport telemetry, and the
same query handlers execute the requests.

## Gotchas / invariants

- The import path ends in `replatforming`, while the declared package is
  `replatformingtools`. The root imports it with an explicit alias.
- Both requests are `POST` with a JSON body and no query string, built fresh
  per call.
- `compose_replatforming_plan` forwards the full scope-selector set
  (`scope_kind`, `scope_id`, `account_id`, `region`, `service_name`,
  `workload_id`, `repo_id`, `environment`, `arn`, `resource_id`) because a
  plan can anchor on a single resource. The `internal/query` handler behind
  `/api/v0/replatforming/plans` rejects a missing or unsupported
  `scope_kind`, but that check runs there, not in `Route`.
- `get_replatforming_rollups` forwards only `scope_id`, `account_id`, and
  `region`: the rollup summarizes across a scope, not one resource, so it
  deliberately never forwards `arn` even when the caller sends one.
- `limit` defaults to 100 for both tools, the same value
  `internal/query/iac.go`'s `iacManagementDefaultLimit` substitutes for a
  nonpositive limit before clamping anything above `iacManagementMaxLimit`
  (500) down to 500, so the dispatcher's default is indistinguishable from an
  omitted limit at the handler. `offset` defaults to 0 and the handler clamps
  a negative offset up to 0 rather than rejecting it, unlike the dead-code
  investigation route's hard reject.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are honoured, a `float64` truncates toward zero, and every
  other type — including a stringified `"25"` — falls back to the default.
- Family membership is an explicit name switch, never a prefix match.
- Neither tool validates its arguments before building a request, unlike
  `servicecontext`'s `get_service_context`/`get_service_story`; `Route`
  reports only `(Request, bool)`, never an error.
- The **route-serves-data registry** (`route_serves_data_registry*.go` in the
  parent package) does not cite `go/internal/mcp/dispatch.go`,
  `go/internal/mcp/dispatch_iac.go`, or this package by path — its closed map
  keys on `GET` read-surface routes from `specs/fact-kind-registry.v1.yaml`,
  and neither replatforming route appears there — so this extraction does
  not require repointing any registry entry, confirmed by searching the
  registry files for `internal/mcp/` path references before this move (none
  found) and by running `TestRouteServesDataRegistry` after it.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
