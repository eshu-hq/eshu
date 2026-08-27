# MCP service registration

## Purpose

This package owns five MCP tool definitions for service catalog correlations,
service context and investigation, and the composed service intelligence
report.

## Ownership boundary

This package owns registration data only. `internal/mcp` retains the tools'
global positions, route resolution, HTTP dispatch, authorization, query
execution, response envelopes, transport, and telemetry.

Routing deliberately remains split in the parent. Service catalog correlation
requests enter through `dispatch_repositories.go` and
`dispatch_service_catalog.go`. Service context, story, investigation, and
intelligence-report requests enter through `dispatch.go`, with shared selector
handling in `dispatch_service_selector.go`. HTTP query handlers continue to own
selector validation, tenant scope, storage and graph reads, result bounds,
truth metadata, and response shaping.

## Exported surface

- `CatalogTools` returns `list_service_catalog_correlations`.
- `ContextTools` returns `get_service_context`, `get_service_story`, and
  `investigate_service`, in that order.
- `IntelligenceTools` returns `get_service_intelligence_report`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by all three constructors.

## Telemetry

None. Registration only constructs in-memory data. The parent MCP package keeps
transport and dispatch signals, while the HTTP handlers retain
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `service`, while the declared package is
  `servicetools`. The root uses an explicit import alias.
- Every constructor returns fresh definitions. A caller may modify one result
  without changing a sibling definition or a later result.
- The five combined serialized definitions are 5,219 bytes with SHA-256
  `49c243812a07ca8e5a32112878b1d030af123899b57d30de23bebbcb6b8954e5`.
- The root keeps the existing assembly positions: catalog is tool 76; the
  context trio is tools 112–114; and service intelligence is tool 115. Their
  neighbors stay unchanged, and the complete registry remains 162 tools in the
  same order.
- Service catalog routing and the service selector routes deliberately remain
  separate in the parent package.
- Keep query execution, route mapping, authorization, transport, and telemetry
  out of this package.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, query execution, response shaping, transport, or telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
