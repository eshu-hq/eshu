# Telemetry

## Purpose

`telemetry` owns the frozen Go data-plane observability contract: metric
instruments, dimension keys, span names, structured log keys, provider setup,
Prometheus export, and trace-aware `slog` logging.

## Ownership boundary

This package owns names and helper APIs. Runtime packages decide when to emit a
signal and reuse this package's constants and attributes when they do it.

Keep metric, span, and log catalogs in `contract.go`, companion contract files,
the registries, and the public telemetry reference. This README is for
maintainers, not a second inventory.

## Exported surface

Use `doc.go` and `go doc ./internal/telemetry` for the godoc-rendered contract.
The stable anchors are provider setup, `Instruments`, observable gauge
registration, frozen registries, attribute helpers, and trace-aware logging.

## Dependencies

`telemetry` depends on the Go standard library, Prometheus, and OpenTelemetry.
It must remain a leaf package and must not import Eshu runtime, storage, facts,
reducer, collector, or query packages.

## Telemetry

This package is the telemetry contract implementation. Metrics registered here
use the `eshu_dp_` prefix. `NewProviders` always creates the Prometheus handler
used by `/metrics`; OTLP export is enabled only when
`OTEL_EXPORTER_OTLP_ENDPOINT` is set.

## Gotchas / invariants

- Add metric dimensions, span names, and log keys to the registry before callers
  use them.
- Use `Attr*`, `ScopeAttrs`, `DomainAttrs`, `PhaseAttr`, and
  `FailureClassAttr` instead of inline label or log-key strings.
- Keep metric labels bounded. Repository names, paths, fact IDs, delivery IDs,
  source paths, and attribute keys belong in spans or logs.
- Observable gauges are registered once per process after their observers exist.
- The Prometheus exporter uses a dedicated registry and exposes only
  `service.name` and `service.namespace` as resource constant labels.

## Related docs

- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/telemetry/metrics.md`
- `docs/public/reference/telemetry/traces.md`
- `docs/public/reference/telemetry/logs.md`
