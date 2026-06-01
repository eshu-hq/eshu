# AGENTS.md - internal/collector/grafana guidance

## Read first

1. `go/internal/collector/grafana/README.md`
2. `docs/public/reference/observability-evidence.md`
3. `docs/public/guides/collector-authoring.md`
4. `go/internal/facts/observability.go`
5. `go/internal/telemetry/contract_grafana.go`

## Invariants this package enforces

- Live Grafana collection is metadata-only. Do not emit dashboard JSON, panel
  definitions, datasource URLs, query models, query strings, contacts,
  notification destinations, credentials, screenshots, or private URLs.
- Facts are source evidence only. Do not import reducer, query, projector, or
  graph storage packages from this directory.
- Stable fact keys include source class, source instance, scope, provider UID or
  resource identity, and generation.
- Disabled targets must be skipped before client construction.
- Metric labels must stay bounded to provider, status class, fact kind, and
  reason.

## Common changes and how to scope them

- Add another Grafana resource family by extending the normalizer, adding an
  envelope test, and updating `README.md` telemetry or redaction notes when the
  retention boundary changes.
- Add retries only with bounded retry counts, rate-limit classification, tests,
  and `eshu_dp_grafana_retries_total` coverage.
- Add runtime or Helm wiring in the runtime issue, not in this package-only
  source slice.

## Failure modes and how to debug

- `rate_limited` responses should increment
  `eshu_dp_grafana_rate_limited_total` and emit bounded partial coverage
  rather than leaking provider response bodies.
- `permission_hidden` and `unsupported` endpoint responses should emit
  `observability.coverage_warning` facts instead of leaking provider response
  bodies.
- Alert rules outside `TargetConfig.StaleAfter` should retain bounded identity
  only, emit stale observed-rule state, and add a stale coverage warning.
- A source result with raw datasource URLs, query models, contact points, or
  notification URLs is a redaction bug, not a reducer concern.

## Anti-patterns specific to this package

- Persisting Grafana dashboard JSON or alert query models.
- Treating live provider resources as declared source control evidence.
- Adding graph or query imports.
- Putting instance IDs, titles, URLs, or token environment names in metric
  labels.

## What NOT to change without an ADR

- The observed observability fact contract in
  `docs/public/reference/observability-evidence.md`.
- The source-fact-only boundary that leaves declared/applied/observed
  comparison to reducers and query surfaces.
