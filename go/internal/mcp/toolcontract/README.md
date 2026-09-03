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
`internal/mcp/documentation`, `internal/mcp/ecosystem`, `internal/mcp/freshness`,
`internal/mcp/investigation`, `internal/mcp/playbooks`,
`internal/mcp/relationships`, `internal/mcp/semantic`,
`internal/mcp/service`, and
`internal/mcp/visualization` import
`toolcontract` and own their registration definitions. Their characterization
tests pin the
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
The relationship-edge definition hash is
`d3c56a788ae3818221a05c3ccb28a7a7a278c27ffdb8aa3722bcfe785e657ca3`.
The freshness definitions hash is
`47899f086bbaa8ac252f4502e442cc44cefc59b4faedfc4baefde75431143bed`.
The investigation workflow and packet definitions are 4,824 serialized bytes
with SHA-256
`393e7901eda034e7a18a8a043895e2cde337dc0b103f994126bcc7ae972b8a82`.
The semantic evidence and search definitions hash is
`4f58551bed9b8e61e7595b12b68f05f2a140ad9c53b11e95f60a3f7b8999021d`.
The ecosystem definitions are 20,585 serialized bytes with SHA-256
`8dcb60e87971b24d53f1be68ccbc7657faa03a1378f34d92990833db0ab0284f`.
The service catalog, context, investigation, and intelligence-report
definitions are 5,219 serialized bytes with SHA-256
`49c243812a07ca8e5a32112878b1d030af123899b57d30de23bebbcb6b8954e5`.
The root assembler still contains 162 tools with ordered-name SHA-256
`8256c2bf64a304185a32bfb1924a6ffd8b3439e9d7d82078ba223382360aa45b`.
`TestReadOnlyToolsRegistrationOrderContract` retains that global order guard,
and all seventeen extracted constructors remain at their previous assembly
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
