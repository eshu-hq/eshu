# MCP visualization registration and route selection

## Purpose

This package owns the MCP tool definition and pure internal-request selection
for deriving a bounded visualization packet from an answer the caller has
already received.

## Ownership boundary

This package owns registration data, visualization family membership, and the
pure mapping from decoded arguments to a dependency-neutral internal request.
`internal/mcp` keeps the tool's global position, global route fanout, private
adapter, HTTP dispatch, authorization, response envelopes, summaries, and
telemetry. `internal/query` owns packet derivation and validation.

## Exported surface

- `Tools` returns the `derive_visualization_packet` definition.
- `Route` selects the visualization request without executing it.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by `Tools`.
- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument and
  internal-request shapes used by `Route`.

## Telemetry

None. Registration and route selection only construct in-memory values. The
parent MCP package keeps transport and dispatch signals, while the HTTP handler
retains the shared API request duration and error metrics.

## Gotchas / invariants

- The import path ends in `visualization`, while the declared package is
  `visualizationtools`. The root uses an explicit import alias.
- `Tools` returns a fresh definition. A caller may inspect or modify one result
  without changing a later result.
- The root registry keeps this definition between the work-item and freshness
  families in the client-visible 162-tool order.
- `Route` maps `view` as a string and passes `source_response` and
  `source_truth` through unchanged. It returns `handled=false` for unrelated
  tools.
- This tool reshapes a caller-supplied response. Keep global fanout, HTTP
  execution, query, storage, authorization, and telemetry out of this package.

No-Observability-Change: this extraction moves only pure visualization route
selection. The root adapter still feeds the same global fanout, dispatch,
authorization, summaries, and transport telemetry, and the same query handler
executes the request.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [MCP tool contract](../toolcontract/README.md)
- [Visualization packets](../../../../docs/public/reference/visualization-packets.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
