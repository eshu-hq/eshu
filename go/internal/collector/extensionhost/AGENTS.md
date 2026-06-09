# Collector Extension Host Agent Rules

This package is the core-owned intake adapter for public collector SDK
extensions. Follow the root `AGENTS.md`, `docs/internal/agent-guide.md`, and
`go/internal/collector/AGENTS.md`.

- Keep extensions outside Eshu internals. Do not pass Postgres, graph, reducer,
  API, MCP, or workflow-control handles through `Request`.
- Use the public SDK validator before mapping facts into internal envelopes.
- Preserve `collector.ClaimedService` as the only workflow claim mutation owner.
- Treat returned claim, scope, generation, and fencing mismatches as terminal
  identity failures.
- Keep status and error fields bounded. Do not include provider response bodies,
  credentials, local file paths, or high-cardinality source values in errors or
  status records.
- Add failing tests first for every behavior change.
