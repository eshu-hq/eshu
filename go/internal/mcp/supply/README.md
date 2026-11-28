# Supply MCP namespace

Groups MCP supply-chain route-selection packages. The leaves decide whether
the parent owns a tool and map decoded arguments to a dependency-neutral
internal request without executing it. Registration, dispatch, and telemetry
stay in root `internal/mcp`; graph reads stay in `internal/query`.

### Move record (#6627)

Rename-only `supplychainevidence` to `supply/chain/evidence` and
`supplychainimpact` to `supply/chain/impact` moves. Every exported Go
symbol is identical; only import paths change.

## Related docs

- [MCP architecture](../README.md)
