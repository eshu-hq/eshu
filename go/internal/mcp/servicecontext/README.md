# MCP service-context route selection

## Purpose

This package owns family membership and pure internal-request selection for
the four MCP service-context tools: `get_service_context`,
`get_service_story`, `get_service_intelligence_report`, and
`investigate_service`.

## Ownership boundary

This package owns service-context family membership and the mapping from
decoded arguments to a dependency-neutral internal request, including the
selector-validation errors `get_service_context` and `get_service_story`
return before dispatch. `internal/mcp` keeps tool registration order (all
four tool definitions live in the sibling `service` package's `ContextTools`
and `IntelligenceTools` constructors), global route fanout, the private
`serviceContextRoute` adapter in `dispatch_service_selector.go`, HTTP
dispatch, authorization, timeouts, response budgets, envelopes, and
telemetry. `internal/query` owns the bounded reads behind
`/api/v0/services/{service_name}/context`,
`/api/v0/services/{service_name}/story`, and
`/api/v0/investigations/services/{service_name}`;
`internal/serviceintelhttp` owns the composed read behind
`/api/v0/services/{service_name}/intelligence-report`.

`list_service_catalog_correlations` is not part of this family even though it
lives in the same `service` registration package: its routing stays in
`dispatch_repositories.go` and `dispatch_service_catalog.go`, and it shares no
selector logic with the four tools here. `get_workload_context` and
`get_workload_story` are also not part of this family: they map `workload_id`
directly to `/api/v0/workloads/{id}/context` and `/story` with no selector
normalization, qualified-identifier stripping, or repository fallback, so
they share no helper with this package and stay in the root switch in
`dispatch.go`.

## Exported surface

- `Route` selects the internal request for a service-context tool without
  executing it, reports `handled=false` for every other tool, and reports
  `handled=true` with a non-nil error for `get_service_context` and
  `get_service_story` when the caller supplied no usable selector.

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

- The import path ends in `servicecontext`, while the declared package is
  `servicecontexttools`. The root imports it with an explicit alias.
- Every request is a `GET` with a query map and no body.
- `Route` returns `(routecontract.Request, bool, error)`, not the two-value
  `(routecontract.Request, bool)` shape the deadcode/codequality/
  entityresolution/codeintel/iacmanagement selectors use: `get_service_context`
  and `get_service_story` validate their selector before dispatch, exactly as
  the pre-extraction root switch did, so a caller must check the error even
  when `handled` is `true`. `relationships.EdgeRoute` is the other family that
  uses this three-value shape, for the same reason (its own selector
  validation).
- `normalizeQualifiedIdentifier` strips a `<type>:` prefix (for example
  `workload:payments-api` -> `payments-api`) before a selector becomes an
  HTTP path segment. `canonicalWorkloadIdentifier` returns the selector
  unchanged only when it carries the canonical `workload:` prefix, so
  `get_service_story`, `get_service_intelligence_report`, and
  `investigate_service` can forward it as the `service_id` query parameter
  without also forwarding a non-workload-qualified value (for example a
  `service:` catalog id).
- `get_service_context` requires `workload_id`; a `service_name` argument is
  rejected before dispatch to avoid a ServeMux redirect on a mismatched
  selector shape, matching the pre-extraction root switch.
- `get_service_story` and `get_service_intelligence_report` accept
  `workload_id` or `service_name`. `investigate_service` reads only
  `service_name` -- its registered schema exposes no `workload_id`, and the
  route ignores one if a caller supplies it, so advertising both here would
  send callers down a selector that is silently dropped. These forward a
  repository selector (`repo`, `repository_id`, or `repo_id`, checked in that
  order) as the `repo` query parameter when the caller starts from
  repository-scoped context.
- `investigate_service` does not validate its selector: an absent
  `service_name` reaches the query handler as an empty path segment, matching
  the pre-extraction root switch arm this replaces.
- The **route-serves-data registry** (`route_serves_data_registry*.go` in the
  parent package) does not cite any of these four tools or their `internal/query`
  / `internal/serviceintelhttp` handlers, confirmed by searching the registry
  files for these tool names and handler paths before this move (none found),
  so this extraction does not require repointing any registry entry.

No-Observability-Change: this extraction moves only pure service-context
route selection. The root adapter still feeds the same global fanout,
dispatch, authorization, budgets, envelopes, and transport telemetry, and the
same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [MCP service registration](../service/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
