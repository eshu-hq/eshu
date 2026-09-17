# Entity MCP namespace

Groups MCP entity-resolution route selection. The `resolution` leaf
selects the three entity tools (`resolve_entity`, `get_entity_context`,
`get_entity_content`), mapping decoded arguments to a dependency-neutral
internal request without executing it. `get_entity_content` stays
registered with the content family's five tools in `tools_content.go`
even though its routing lives here.

## Ownership boundary

The leaf owns family membership and pure request selection. Root
`internal/mcp` owns tool registration and its order, global route fanout,
the private adapter, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. Query execution stays in
`internal/query`. This single-child parent supplies a stable namespace for
related siblings.

### Move record (#6627)

Rename-only `entityresolution` to `entity/resolution` move. Every exported
Go symbol is identical; only import paths change.

No-Observability-Change: no stage added and no metric, span, or log name
changed; this namespace declares no function, holds no state, and performs
no I/O of its own.

## Related docs

- [MCP architecture](../README.md)
