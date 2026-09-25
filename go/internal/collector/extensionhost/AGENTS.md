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
- Grant decisions are reported at the owning site (`NewSource` for activation,
  `Source.validateGrantCoverage` for emission), once per distinct core kind per
  result. Observation must never change the allow/deny outcome, and no observer
  or telemetry value may carry credentials, grant scope, config, or payloads.
  Keep the recheck hot path allocation-free beyond the observer call.
