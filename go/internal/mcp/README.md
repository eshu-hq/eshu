# internal/mcp

## Purpose

`internal/mcp` owns Eshu's Model Context Protocol server and read-only tool
surface. It translates JSON-RPC tool calls into the same internal HTTP handler
chain used by the public API, then shapes the response back into MCP content.

## Ownership boundary

This package owns MCP protocol handling, stdio and HTTP/SSE transport,
`ReadOnlyTools`, tool schemas, route dispatch, canonical envelope detection,
and MCP result shaping.

It does not own query planning, graph reads, content-store reads, pagination
semantics, result truth, or route-level authorization. Those stay in
`internal/query`; this package forwards requests to that handler.

## Exported surface

Use `go doc ./internal/mcp` for the godoc contract. The stable public surface is
small:

- `Server`, `NewServer`, `Server.Run`, and `Server.RunHTTP` for stdio and
  HTTP/SSE serving.
- `ToolDefinition` and `ReadOnlyTools` for the advertised read-only tool
  registry.

Tool family counts and exact arguments belong in `tools_*.go`, `resolveRoute`,
dispatch tests, and the public MCP reference. Keep this README at the
package-boundary level.

## Dependencies

- `internal/query` supplies the mounted HTTP handler, `ResponseEnvelope`, and
  `EnvelopeMIMEType`.
- `internal/buildinfo` supplies the version string in the initialize response.

No storage driver, fact store, graph backend, or telemetry instrument should be
imported here.

## Telemetry

The package does not declare metrics or spans. Query handlers emit the
operation-level telemetry after dispatch enters `internal/query`.

Structured logs in `server.go` cover server start, SSE session start/close, SSE
buffer overflow, and debug-level tool dispatch method/path details.

## Gotchas / invariants

- Every tool from `ReadOnlyTools` must have a matching route case or helper;
  `TestEveryRegisteredToolHasDispatchRoute` enforces this.
- Dispatch always asks for `application/eshu.envelope+json`; only responses
  containing `data`, `truth`, and `error` are treated as canonical envelopes.
- Authorization from the outer MCP HTTP request is forwarded to the internal
  query handler.
- MCP results for canonical envelopes contain a text block and a resource block
  using `query.EnvelopeMIMEType`.
- Tool schema or name changes are client-facing contract changes. Update MCP
  docs, HTTP docs for shared routes, and handler/dispatch tests together.
- SSE sessions use a bounded response channel and a keepalive ticker. Overflow
  is logged and dropped instead of blocking the server.

## Focused tests

```bash
go test ./internal/mcp -count=1
go doc ./internal/mcp
```

## Related docs

- `docs/public/reference/mcp-reference.md`
- `docs/public/reference/mcp-tool-contract-matrix.md`
- `docs/public/guides/mcp-guide.md`
- `docs/public/reference/http-api.md`
- `go/cmd/mcp-server/README.md`
