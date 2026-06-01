# AGENTS.md - internal/collector/prometheusmimir guidance

## Read first

1. `go/internal/collector/prometheusmimir/README.md`
2. `docs/public/reference/observability-evidence.md`
3. `docs/public/guides/collector-authoring.md`
4. `go/internal/facts/observability.go`
5. `go/internal/telemetry/contract_prometheus_mimir.go`

## Invariants this package enforces

- Live Prometheus/Mimir collection is metadata-only. Do not emit metric samples,
  exemplars, profile data, raw PromQL, scrape target URLs, target label values,
  discovered label values, annotations, tenant IDs, tenant secrets, or alert
  payload bodies.
- Facts are source evidence only. Do not import reducer, query, projector, or
  graph storage packages from this directory.
- Stable fact keys include source class, source instance, scope, provider UID or
  resource identity, and generation.
- Disabled targets must be skipped before client construction.
- Metric labels must stay bounded to provider, status class, fact kind, and
  reason.

## Common changes and how to scope them

- Add another Prometheus-compatible endpoint only after proving the endpoint
  returns bounded metadata and adding redaction tests for forbidden fields.
- Add retries only with bounded retry counts, rate-limit classification, tests,
  and `eshu_dp_prometheus_mimir_retries_total` coverage.
- Add runtime or Helm wiring in the runtime issue, not in this package-only
  source slice.

## Failure modes and how to debug

- `rate_limited` responses should increment
  `eshu_dp_prometheus_mimir_rate_limited_total` and emit bounded partial
  coverage rather than leaking provider response bodies.
- `permission_hidden` and `unsupported` endpoint responses should emit
  `observability.coverage_warning` facts instead of leaking provider response
  bodies.
- Rules or targets outside `TargetConfig.StaleAfter` should retain bounded
  identity only, emit stale observed state, and add a stale coverage warning.
- A source result with raw target URLs, label values, PromQL, annotations, or
  tenant IDs is a redaction bug, not a reducer concern.

## Anti-patterns specific to this package

- Persisting metric samples, PromQL query bodies, target addresses, or tenant
  IDs.
- Treating live provider resources as declared source control evidence.
- Adding graph or query imports.
- Putting instance IDs, URLs, tenant IDs, label values, or token environment
  names in metric labels.

## What NOT to change without an ADR

- The observed observability fact contract in
  `docs/public/reference/observability-evidence.md`.
- The source-fact-only boundary that leaves declared/applied/observed
  comparison to reducers and query surfaces.
