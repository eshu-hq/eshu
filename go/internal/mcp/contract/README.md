# Contract MCP namespace

Groups the two MCP shared contracts, kept as separate packages because
request selection and tool registration are distinct responsibilities:

- `route` owns decoded argument access (`Arguments`) and the selected
  internal-request value (`Request`). Family packages depend on it; it
  depends on nothing inside `internal/mcp`.
- `tool` owns the tool registration shape (`ToolDefinition`). Family
  registration packages depend on it; it depends on nothing inside
  `internal/mcp`.

## Ownership boundary

A moved child must not acquire an import of its orchestrating root, and
these shared contracts must not acquire imports of their consumers. Both
leaves stay dependency-free inside `internal/mcp`. All consumers moved
with the contracts in the same renames; no old-path forwarding package
was left behind.

### Move record (#6627)

Rename-only `routecontract` to `contract/route` and `toolcontract` to
`contract/tool` moves. Every exported Go symbol is identical
(`Arguments`, `Request`, `ToolDefinition` keep their names, methods, and
shapes); only import paths change.

## Related docs

- [MCP architecture](../README.md)
