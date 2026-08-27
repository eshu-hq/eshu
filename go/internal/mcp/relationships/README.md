# MCP relationship registrations

## Purpose

This package owns the MCP definitions for code-relationship stories, code-
relationship analysis, and bounded relationship-edge listing.

## Ownership boundary

This package owns registration data only. `internal/mcp` retains global
positions, route resolution, HTTP dispatch, authorization, response envelopes,
and transport telemetry. `internal/query` owns relationship validation, graph
reads, bounds, and response shaping.

## Exported surface

- `CodeTools` returns `get_code_relationship_story` followed by
  `analyze_code_relationships`.
- `Tool` returns the `list_relationship_edges` definition.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by both constructors.
- `internal/sourcetool` owns the canonical closed vocabulary advertised by the
  optional `source_tool` field.

## Telemetry

None. Registration only constructs in-memory data. The parent MCP package keeps
transport and dispatch signals, while the relationship HTTP handlers retain
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `relationships`, while the declared package is
  `relationshiptools`. The root uses an explicit import alias.
- Both constructors return fresh definitions. Mutating one result, including a
  nested schema slice or map, must not change a sibling definition or a later
  result.
- `CodeTools` returns exactly two definitions in story-then-analysis order.
- The root registry keeps those definitions at zero-based positions 8 and 9
  within the 33-tool codebase group and the 162-tool global registry.
- The root registry keeps `list_relationship_edges` at position 161 of 162,
  after `ask` and before `list_repository_files`.
- Keep the `source_tool` enum aligned with `sourcetool.Canonical`.
- Keep routing, graph reads, query execution, authorization, and telemetry out
  of this package.

No-Observability-Change: these extraction moves do not change routing,
dispatch, authorization, graph reads, query execution, response shaping, or
telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
