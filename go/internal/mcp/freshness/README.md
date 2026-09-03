# MCP freshness registration

## Purpose

This package owns four MCP tool definitions for generation, repository, and
service freshness reads.

## Ownership boundary

This package owns registration data only. `internal/mcp` retains the tools'
global positions, route resolution, HTTP dispatch, authorization, response
envelopes, and transport telemetry. The parent keeps the split routing shape:
`get_repository_freshness` stays in `dispatch_repositories.go`, while
`get_generation_lifecycle`, `get_changed_since`, and
`get_service_changed_since` stay in `dispatch_freshness.go`.

The HTTP query handlers remain responsible for selector validation, generation
lookup, delta classification, bounds, and response shaping.

## Exported surface

- `Tools` returns `get_generation_lifecycle`, `get_changed_since`,
  `get_repository_freshness`, and `get_service_changed_since` in that order.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by `Tools`.

## Telemetry

None. Registration only constructs in-memory data. The parent MCP package keeps
transport and dispatch signals, while the HTTP handlers retain
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `freshness`, while the declared package is
  `freshnesstools`. The root uses an explicit import alias.
- `Tools` returns fresh definitions. A caller may modify one result without
  changing a later result.
- The definition hash is
  `47899f086bbaa8ac252f4502e442cc44cefc59b4faedfc4baefde75431143bed`.
- The root registry keeps all four definitions after visualization and before
  context tools. The complete registry remains 162 tools in the same order.
- Repository freshness does not use `dispatch_freshness.go`; its route remains
  part of the repository dispatcher.
- Keep query execution, route mapping, authorization, and telemetry out of this
  package.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, query execution, response shaping, or telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
