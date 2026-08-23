# MCP tool contract

`toolcontract` owns the dependency-neutral data shape used to register an MCP
tool. Both `internal/mcp` and its domain registration packages may depend on
this package without creating a parent-child import cycle.

## Ownership

This package owns only `ToolDefinition`: the tool name, description, and input
schema sent by `tools/list`.

`internal/mcp` continues to own:

- the ordered `ReadOnlyTools` assembly;
- route resolution and HTTP dispatch;
- response envelopes, summaries, and size/deadline guards;
- SSE and stdio transports; and
- transport authorization and telemetry.

Domain registration packages own their tool definitions after they are moved.
They must not add routing, storage access, or query logic here.

## Compatibility

`mcp.ToolDefinition` is a type alias for `toolcontract.ToolDefinition`, so
existing callers keep the same source and JSON contract. Moving a registration
family must preserve its position in `mcp.ReadOnlyTools` and must not change the
registered tool set or schemas.

## Verification

Run:

```bash
cd go
go test ./internal/mcp/... -count=1
```
