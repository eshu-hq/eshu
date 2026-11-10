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
- `host_derived_target` is host-owned: never let static config set it, and never
  put a clone URL in a scope id, error, log, or metric. Keep owner keys bound to
  host and owner, since the minted token goes to that remote.
- `SourceCommitSHA` on a derived generation comes only from the target this host
  delivered on the claim, never from the component's result or from static config.
  The coordinator's commit-skew fence (#10055) trusts it as the evidence commit.
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
- `Config.TokenProvider` mints once per opted-in claim (targets entry with
  `auth_mode: host_minted` for the claimed scope, or a claim carrying a
  host-resolved derived target) and only the minted token
  reaches `Request.Config` under `HostMintedGitHubTokenKey`; static config
  must not define that key (`NewSource` rejects it). A static claim without
  the opt-in never consults the provider. Provider errors stay
  bounded: only `github/app` faults keep their chain, everything else maps to
  a terminal `auth_denied` denial, and no token, key, or credential may appear
  in any error, record, or telemetry value.
- `ResultRecord.RetryAfter`, the retry-after gauge, and the
  `extension_status.retry_after_seconds` log key are the extension's capped
  request, recorded before `ClaimedService` decides. Never document them as the
  applied or honored delay: `ClaimedService` applies `max(PollInterval,
  request)`, and none once the attempt budget is spent.
- `Config.ClaimOutcomeRecorder` (#9420) gets exactly one `RecordClaimOutcome`
  per `NextClaimed` exit, called before `recordResult` so the `ResultRecord`
  reflects a `source_state_record` failure. Only completed outcomes are
  stashed, only `ObserveClaimedGenerationCommitted` and
  `ObserveClaimedGenerationRefused` consume the stash, and every exit clears the
  item's entry first. Never write success state before the commit hook, never
  write it on a refusal (#10037), and never let a failure-row write error change
  a claim's class or terminality.
- The stderr relay (#9823) is an allowlist of three integer keys
  (`inflated_bytes`, `budget_bytes`, `compressed_bytes`) parsed in
  `satellite_stderr.go`; the full contract and evidence are in
  `STDERR_RELAY.md`. Never relay a satellite string, a raw line, or a new
  key without a named, bounded, secret-free reason and a hostile-stderr test.
  Figures reach `ResultRecord` only for a validated SDK result whose FINAL
  assembled state is not complete or unchanged, never a host
  verdict, and ride on the existing result log line; do not add a log line.
