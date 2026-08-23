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

No-Regression Evidence: the boundary extraction is type-only. A disposable
`internal/mcp/doctools` package imported `toolcontract`, owned the four
documentation registrations, and returned them through the existing root
assembler. Before and after the scratch move, `ReadOnlyTools` contained 162
tools with the same ordered-name SHA-256
`8256c2bf64a304185a32bfb1924a6ffd8b3439e9d7d82078ba223382360aa45b`.
The scratch branch then passed `go test ./internal/mcp/... -count=1`,
`go build ./...`, and `go vet ./...`. The family move was discarded; only the
neutral contract and root alias land here.

No-Observability-Change: this package has no runtime behavior, storage access,
network calls, goroutines, logs, spans, or metrics. Tool assembly, routing,
dispatch, authorization, and telemetry remain owned by `internal/mcp`.

Run:

```bash
cd go
go test ./internal/mcp/... -count=1
```
