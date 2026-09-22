# Telemetry Contract — Observability Sources

## Purpose

`observability` holds the source-collector span name declarations for
Eshu's four hosted-observability collectors: Grafana, Loki, Prometheus/Mimir,
and Tempo. Each collector gets an `Observe` span (workflow claim through
source fact envelope production) and a `Fetch` span (one bounded REST
metadata call). These used to live directly in root package `telemetry` as
`contract_grafana.go`, `contract_loki.go`, `contract_prometheus_mimir.go`,
and `contract_tempo.go` (issue #6777).

## Ownership boundary

Declarations only — span name constants, one file per source. No
registration order, no `func init()`, no `go/internal/*` imports (including
root package `telemetry`, which would cycle).

## Exported surface

- `SpanGrafanaObserve`, `SpanGrafanaFetch`
- `SpanLokiObserve`, `SpanLokiFetch`
- `SpanPrometheusMimirObserve`, `SpanPrometheusMimirFetch`
- `SpanTempoObserve`, `SpanTempoFetch`

## Dependencies

None (no imports beyond the standard library is not even required — these
are plain string constants).

## Telemetry

This package is telemetry declarations — see Purpose.

## Gotchas / invariants

- Root `go/internal/telemetry` re-exports every span here as a compat alias
  (`compat_observability.go`). Registration into the ordered `spanNames`
  slice happens in root `registration_steps.go`'s `registerVulnerabilityIntelligence`
  step, not here.
- `TestSpanNames` in root `contract_test.go` pins the exact frozen splice
  order.

## Related docs

- `go/internal/telemetry/contract/README.md`
- `go/internal/telemetry/README.md`
