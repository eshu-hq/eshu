# MCP relationship registrations and route selection

## Purpose

This package owns the MCP definitions and dependency-neutral route selection
for code-relationship stories, code-relationship analysis, and bounded
relationship-edge listing.

## Ownership boundary

This package owns registration data and the pure family selectors that decide
whether a tool belongs to the relationship family and convert decoded arguments
into `routecontract.Request` values. `internal/mcp` retains global fanout order,
root route adapters, HTTP dispatch, authorization, transport, timeouts, response
budgets, response envelopes, and telemetry. `internal/query` owns relationship
validation, graph reads, bounds, and response shaping.

## Exported surface

- `CodeTools` returns `get_code_relationship_story` followed by
  `analyze_code_relationships`.
- `AnalyzeCodeRelationshipsSchema` returns the canonical analysis schema
  without requiring callers to inspect the ordered tool registry.
- `Tool` returns the `list_relationship_edges` definition.
- `CodeRoute` selects code-story and code-analysis requests.
- `EdgeRoute` selects bounded relationship-edge requests.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by both constructors.
- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  selected-request values used by both route selectors.
- `internal/sourcetool` owns the canonical closed vocabulary advertised by the
  optional `source_tool` field.

## Telemetry

None. Registration constructs in-memory data and route selection constructs an
in-memory request value. The parent MCP package keeps dispatch and transport
signals, while the relationship HTTP handlers retain
`eshu_dp_api_request_duration_seconds` and `eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `relationships`, while the declared package is
  `relationshiptools`. The root uses an explicit import alias.
- All constructors return fresh definitions or schemas. Mutating one result,
  including a nested schema slice or map, must not change a sibling definition
  or a later result.
- `CodeTools` returns exactly two definitions in story-then-analysis order.
- The root registry keeps those definitions at zero-based positions 8 and 9
  within the 33-tool codebase group and the 162-tool global registry.
- The root registry keeps `list_relationship_edges` at position 161 of 162,
  after `ask` and before `list_repository_files`.
- Keep the `source_tool` enum aligned with `sourcetool.Canonical`.
- Keep global fanout order, request execution, graph reads, query execution,
  authorization, transport, timeout, budget, envelope, and telemetry behavior
  out of this package.

No-Observability-Change: these selectors preserve the existing selected method,
path, body, and query values. Root dispatch, authorization, timeouts, response
budgets, envelopes, transport, and telemetry remain unchanged, as do query
validation, graph reads, and response shaping.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
