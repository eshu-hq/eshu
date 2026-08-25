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

No-Regression Evidence: `internal/mcp/ask`, `internal/mcp/cloud`,
`internal/mcp/documentation`, `internal/mcp/playbooks`, and
`internal/mcp/visualization` import `toolcontract` and own their registration
definitions. Their characterization tests pin the
serialized names,
descriptions, input schemas, and local order at SHA-256
`51ee1b7788fce89e28d89aabe738b8e497f21bc9e92cb1cbc2d99bd3a3d8eb02`
for documentation and
`460ff89408273b10319f5656568df06241b10b137c3d77b3f8c8eba8c709e9d6`
for cloud. The visualization definition hash is
`9dc648490f77df7c635e5548c7bc1c1a32bb5748ba230e09e78a284509209c9e`.
The Ask definition hash is
`01422a0903e582b18e10f8e64d784413b0cd1c571880a812117e9d1eab811ff2`.
The query-playbook definitions hash is
`ec0199c133c68ffcf2d425e7db2e0faa308102599792952fc6016d590bb15a90`.
The root assembler still contains 162 tools with ordered-name SHA-256
`8256c2bf64a304185a32bfb1924a6ffd8b3439e9d7d82078ba223382360aa45b`.
`TestReadOnlyToolsRegistrationOrderContract` retains that global order guard,
and all seven extracted constructors remain at their previous assembly
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
