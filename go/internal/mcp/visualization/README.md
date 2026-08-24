# MCP visualization registration

## Purpose

This package owns the MCP tool definition for deriving a bounded visualization
packet from an answer the caller has already received.

## Ownership boundary

This package owns registration data only. `internal/mcp` still owns the tool's
global position, route resolution, HTTP dispatch, authorization, response
envelopes, and telemetry. `internal/query` owns packet derivation and validation.

## Exported surface

- `Tools` returns the `derive_visualization_packet` definition.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by `Tools`.

## Telemetry

None. Registration only constructs in-memory data. The parent MCP package keeps
transport and dispatch signals, while the HTTP handler retains the shared API
request duration and error metrics.

## Gotchas / invariants

- The import path ends in `visualization`, while the declared package is
  `visualizationtools`. The root uses an explicit import alias.
- `Tools` returns a fresh definition. A caller may inspect or modify one result
  without changing a later result.
- The root registry keeps this definition between the work-item and freshness
  families in the client-visible 162-tool order.
- This tool reshapes a caller-supplied response. Do not add query, storage,
  authorization, or route logic here.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, query execution, or telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [Visualization packets](../../../../docs/public/reference/visualization-packets.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
