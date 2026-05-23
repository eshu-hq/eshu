# MCP

## Purpose

`internal/mcp` owns Eshu's Model Context Protocol server and read-only tool
surface. It translates JSON-RPC tool calls into the internal HTTP handler chain
used by the public API, then returns MCP content blocks.

## Ownership Boundary

This package owns MCP protocol handling, stdio and HTTP/SSE transport,
`ReadOnlyTools`, tool schemas, route dispatch, envelope detection, and MCP
result shaping. Query planning, graph reads, content reads, pagination, truth
labels, and route authorization stay in `internal/query`.

## Exported Surface

See `doc.go` and `go doc ./internal/mcp` for the contract. The stable anchors
are server construction/run methods, `ToolDefinition`, and `ReadOnlyTools`.

## Telemetry

The package declares no metrics or spans. Query handlers emit operation-level
telemetry after dispatch enters `internal/query`. Structured logs cover server
start, SSE session lifecycle, SSE buffer overflow, and debug dispatch
method/path details.

## Gotchas / Invariants

- Every registered tool must have a dispatch route; tests enforce this.
- Dispatch requests `application/eshu.envelope+json` and treats only
  `data`/`truth`/`error` responses as canonical envelopes.
- Outer MCP authorization is forwarded to the internal query handler.
- Canonical MCP results include both text and resource blocks.
- Tool schema or name changes are client-facing and need docs plus tests.
- SSE uses a bounded response channel; overflow is logged and dropped.

## Focused Tests

```bash
cd go
go test ./internal/mcp -count=1
go doc ./internal/mcp
```

## Related Docs

- `docs/public/guides/mcp-guide.md`
- `docs/public/reference/mcp-reference.md`
- `docs/public/reference/mcp-tool-contract-matrix.md`
- `docs/public/reference/http-api.md`
