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
- Every `NextClaimed` exit reports exactly one `ResultRecord` when the
  recorder implements `ResultRecorder` (#8908). Add a new exit path only with
  its row in `TestNextClaimedReportsOneResultForEveryFinalState`. Never add a
  scope, instance, work item, generation, or URL field to `ResultRecord`, and
  never label `StatusTelemetry` metrics with anything beyond `collector_kind`,
  `state`, `status_class`, and `failure_class`.
- A retryable result's delay comes only from `status.retry_after_seconds`,
  clamped to `MaxRetryAfterDelay`; keep the clamp in seconds before the
  multiply so no value overflows `time.Duration`.
- `ResultRecord.RetryAfter`, the retry-after gauge, and the
  `extension_status.retry_after_seconds` log key are the extension's capped
  request, recorded before `ClaimedService` decides. Never document them as the
  applied or honored delay: `ClaimedService` applies `max(PollInterval,
  request)`, and none once the attempt budget is spent.
