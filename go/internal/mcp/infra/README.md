# Infrastructure MCP namespace

Groups MCP infrastructure route-selection packages. The leaves decide
whether the parent owns a tool and map decoded arguments to a
dependency-neutral internal request without executing it.

- `inventory` selects the infrastructure-inventory tools
  (`count_infra_resources`, `get_infra_resource_inventory`).
- `search` selects the infrastructure-search tool (`find_infra_resources`).

## Ownership boundary

Leaf packages own family membership and pure request selection. Root
`internal/mcp` owns tool registration and its order, global route fanout,
the private adapters, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. Query execution stays in
`internal/query`.

### Move record (#6627)

Rename-only `infrainventory` to `infra/inventory` and `infrasearch` to
`infra/search` moves. Every exported Go symbol is identical; only import
paths change.

No-Observability-Change: no stage added and no metric, span, or log name
changed; this namespace declares no function, holds no state, and performs
no I/O of its own.

## Related docs

- [MCP architecture](../README.md)
