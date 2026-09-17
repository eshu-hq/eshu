# Package MCP namespace

Groups MCP package-registry route selection. The `registry` leaf selects
the four package-registry tools (`list_package_registry_packages`,
`count_package_registry_packages`,
`get_package_registry_package_inventory`,
`list_package_registry_versions`), mapping decoded arguments to a
dependency-neutral internal request without executing it.

## Ownership boundary

The leaf owns family membership and pure request selection. Root
`internal/mcp` owns tool registration and its order, global route fanout,
the private adapter, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. Query execution stays in
`internal/query`. This single-child parent supplies a stable namespace for
related siblings.

### Move record (#6627)

Rename-only `packageregistry` to `package/registry` move. Every exported
Go symbol is identical; only import paths change.

No-Observability-Change: no stage added and no metric, span, or log name
changed; this namespace declares no function, holds no state, and performs
no I/O of its own.

## Related docs

- [MCP architecture](../README.md)
