# Admission MCP namespace

Groups MCP admission-decisions route selection. The `decisions` leaf owns
the one admission-decisions listing selector
(`list_admission_decisions`), mapping decoded arguments to a
dependency-neutral internal request without executing it.

## Ownership boundary

The leaf owns family membership and pure request selection. Root
`internal/mcp` owns tool registration and its order, global route fanout,
the private adapter, HTTP dispatch, authorization, timeouts, response
budgets, envelopes, summaries, and telemetry. The reducer owns the
correlation admission decisions the path lists. This single-child parent
supplies a stable namespace for related siblings.

### Move record (#6627)

Rename-only `admissiondecisions` to `admission/decisions` move. Every
exported Go symbol is identical; only import paths change.

## Related docs

- [MCP architecture](../README.md)
