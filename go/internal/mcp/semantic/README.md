# MCP semantic registration

## Purpose

This package owns three MCP tool definitions for semantic evidence and semantic
context search reads.

## Ownership boundary

This package owns registration data only. `internal/mcp` retains the tools'
global positions, route resolution, HTTP dispatch, authorization, query
execution, response envelopes, transport, and telemetry. The parent also keeps
the split routing shape: the two semantic-evidence routes stay in
`dispatch_semantic_evidence.go`, while semantic search stays in
`dispatch_semantic_search.go`.

The HTTP query handlers remain responsible for selector validation, tenant
scope, bounds, truth metadata, and response shaping.

## Exported surface

- `EvidenceTools` returns `list_semantic_documentation_observations` and
  `list_semantic_code_hints`, in that order.
- `SearchTools` returns `search_semantic_context`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by both constructors.

## Telemetry

None. Registration only constructs in-memory data. The parent MCP package keeps
transport and dispatch signals, while the HTTP handlers retain
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `semantic`, while the declared package is
  `semantictools`. The root uses an explicit import alias.
- Both constructors return fresh definitions. A caller may modify one result
  without changing a later result.
- The combined definitions hash is
  `4f58551bed9b8e61e7595b12b68f05f2a140ad9c53b11e95f60a3f7b8999021d`.
- The root registry keeps the evidence pair followed by semantic search, after
  investigation packets and before documentation finding aggregates. The
  complete registry remains 162 tools in the same order.
- Semantic evidence and semantic search deliberately keep separate root
  routers.
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
