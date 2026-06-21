# askwiring — Agent Instructions

This package is the single source of truth for Ask Eshu engine wiring.

## Ownership

- Owned by the answer-experience surface (capability:answer-experience).
- Changes here affect both `cmd/api` and `cmd/mcp-server` simultaneously.

## Rules

- MUST keep `BuildAskHandler` default-off: any construction failure returns
  a nil-Asker handler, never panics.
- MUST NOT import `cmd/api` or `cmd/mcp-server` (package main; unreachable).
- MUST NOT import `ask/engine` from `query`; the `engineAsker` adapter in
  this package exists to break that cycle.
- MUST keep `SetPosture` always non-nil (no-op when engine not built).
- MUST keep the `EnvAskEnabled`, `EnvAskNarrationEnabled`,
  `EnvAskMaxIterations`, and `EnvAskMaxToolCallsPerTurn` constants in lockstep
  with operator docs (`docs/public/reference/`).
- MUST keep `ResolveEngineOptions` safe-by-default: an unset, empty,
  non-numeric, zero, or negative override keeps the engine default; values
  above the documented ceiling are clamped, never accepted raw. Do not loosen
  the safety bound silently.
- MUST apply `golang-engineering` skill when editing Go in this package.
- MUST apply `eshu-mcp-call-rigor` when changing the MCPRunner wiring.
