# MCP relationship-edge registration

## Purpose

This package owns the MCP definition for listing bounded relationship edges.

## Ownership boundary

This package owns registration data only. `internal/mcp` retains the tool's
global position, route resolution, HTTP dispatch, authorization, response
envelopes, and transport telemetry. `internal/query` owns relationship-edge
validation, graph reads, bounds, and response shaping.

## Exported surface

- `Tool` returns the `list_relationship_edges` definition.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by `Tool`.
- `internal/sourcetool` owns the canonical closed vocabulary advertised by the
  optional `source_tool` field.

## Telemetry

None. Registration only constructs in-memory data. The parent MCP package keeps
transport and dispatch signals, while the relationship-edge HTTP handler
retains `eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `relationships`, while the declared package is
  `relationshiptools`. The root uses an explicit import alias.
- `Tool` returns a fresh definition. Mutating one result must not change a
  later result.
- The root registry keeps `list_relationship_edges` at position 161 of 162,
  after `ask` and before `list_repository_files`.
- Keep the `source_tool` enum aligned with `sourcetool.Canonical`.
- Keep routing, graph reads, query execution, authorization, and telemetry out
  of this package.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, graph reads, query execution, response shaping, or telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
