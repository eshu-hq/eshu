# AGENTS.md - internal/collector/loki guidance

## Read first

1. `README.md` - package boundary, telemetry, and invariants.
2. `doc.go` - godoc contract for callers.
3. `types.go` - source, target, and fact-normalization contracts.
4. `http_client.go` - bounded Loki API reads and redaction behavior.
5. `envelope.go` - durable observability fact identity and payload shape.
6. `docs/public/reference/observability-evidence.md` - source-class and
   redaction contract.

## Invariants

- Collect metadata only. Do not persist log lines, raw LogQL, private URLs,
  tenant IDs, tenant headers, token values, credentials, or provider response
  bodies.
- Treat live Loki state as observed source evidence only. Reducers and query
  surfaces own declared/applied/observed comparison and user-facing truth.
- Keep metric labels bounded to provider, status class, fact kind, and bounded
  reason values. Instance IDs, label values, tenant IDs, and URLs belong only in
  redacted payload fields or fingerprints.
- High-cardinality label values become coverage warnings. Do not widen the
  allowlist or cardinality limit without tests that prove redaction and
  rejection behavior.
- Series and rule client-truncation at `resource_limit` must emit a
  `WarningTruncated` coverage-warning fact; never drop records silently.
- `series_lookback` is an independent series-window knob. It must NOT inherit
  `stale_after` (a rule-staleness setting that is inert for series) -- keep the
  generous `defaultSeriesLookback` fallback so a deployment that set
  `stale_after` does not silently change its series-fetch window. A `/series`
  time-window exclusion is inherently silent (Loki cannot report what a window
  excluded); document the coverage consequence whenever this default changes.
- HTTP 200 Loki responses with API `status:error` fail closed with a bounded
  error. Do not retain the provider error body.

## Common changes

- Adding another Loki metadata endpoint requires a failing test first, a
  redaction assertion for every source field that could contain user data, and
  a telemetry or docs update when the operator signal changes.
- New emitted fact fields must be metadata-only and covered by envelope tests.
- Runtime command, Helm, workflow coordinator wiring, and live smoke proof do
  not belong in this package unless the runtime closeout issue is in scope.

## What not to change without design review

- Do not call `/loki/api/v1/query` or `/loki/api/v1/query_range`; those
  endpoints can return log lines.
- Do not materialize graph edges or coverage truth from this package.
- Do not store raw label values just because a label name is allowlisted; keep
  value evidence bounded and fingerprinted.
