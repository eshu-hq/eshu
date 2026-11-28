# Container MCP namespace

Groups MCP container-image route selection. The `image` leaf selects the
four container-image tools (`list_container_image_identities`,
`list_container_image_tag_history`, `count_container_image_identities`,
`get_container_image_identity_inventory`), mapping decoded arguments to a
dependency-neutral internal request without executing it.

## Ownership boundary

The leaf owns family membership and pure request selection. Root
`internal/mcp` owns tool registration and its order, global route fanout,
the private adapter, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. Query execution stays in
`internal/query`. This single-child parent supplies a stable namespace for
related siblings.

### Move record (#6627)

Rename-only `containerimage` to `container/image` move. Every exported Go
symbol is identical; only import paths change.

## Related docs

- [MCP architecture](../README.md)
