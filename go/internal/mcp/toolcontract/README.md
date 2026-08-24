# MCP tool contract

## Purpose

`toolcontract` owns the data shape used to register an MCP tool. Registration
families can import this package without importing their parent `internal/mcp`
package.

## Ownership boundary

This package owns only `ToolDefinition`: the tool name, description, and input
schema sent by `tools/list`.

`internal/mcp` continues to own ordered tool assembly, route resolution, HTTP
dispatch, response envelopes, transports, authorization, and telemetry. Domain
registration packages own their definitions after they move; routing, storage
access, and query logic do not belong here.

## Exported surface

- `ToolDefinition` describes one MCP tool registration.

See `doc.go` for the godoc-rendered contract.

## Dependencies

This package has no internal package dependencies. It deliberately sits below
the MCP root and future domain registration packages in the import graph.

## Telemetry

This package emits no metrics, spans, or logs. MCP transport and dispatch
telemetry remains in `internal/mcp`.

## Gotchas / invariants

`mcp.ToolDefinition` must remain a type alias for
`toolcontract.ToolDefinition`, and the JSON field names are part of the MCP
wire contract. Tool membership and order remain owned by `mcp.ReadOnlyTools`.
A family move must preserve the registered set, order, and schemas.

No-Regression Evidence: `internal/mcp/documentation` imports `toolcontract` and
owns the six documentation registrations. Its characterization test pins the
serialized names, descriptions, input schemas, and local order at SHA-256
`51ee1b7788fce89e28d89aabe738b8e497f21bc9e92cb1cbc2d99bd3a3d8eb02`.
The root assembler still contains 162 tools with ordered-name SHA-256
`8256c2bf64a304185a32bfb1924a6ffd8b3439e9d7d82078ba223382360aa45b`.
`TestReadOnlyToolsRegistrationOrderContract` retains that global order guard,
and the two documentation constructors remain at their previous assembly
positions.

No-Observability-Change: tool assembly, routing, dispatch, authorization, and
telemetry remain owned by `internal/mcp`.

## Verification

Run:

```bash
cd go
go test ./internal/mcp/... -count=1
go vet ./internal/mcp/...
```

## Related docs

- [MCP package](../README.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
