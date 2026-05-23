# Telemetry Agent Rules

These rules are mandatory for changes under `go/internal/telemetry`.

## Read First

1. `go/internal/telemetry/README.md`
2. `go/internal/telemetry/contract.go`
3. `go/internal/telemetry/instruments.go`
4. `go/internal/telemetry/logging.go`
5. `go/internal/telemetry/provider.go`
6. `docs/public/reference/telemetry/index.md`
7. The focused telemetry reference page for the signal being changed.

## Invariants

- This package MUST remain a leaf. Do not import Eshu runtime, facts, storage,
  reducer, collector, query, or MCP packages.
- Every metric registered here MUST use the `eshu_dp_` prefix.
- Metric names, span names, log keys, and dimension keys are frozen contracts.
  Add new constants to the registry before callers use them.
- Callers MUST use `Attr*`, `ScopeAttrs`, `DomainAttrs`, `PhaseAttr`, and
  `FailureClassAttr`; do not inline label or log-key strings.
- Metric labels MUST stay bounded. Repository names, paths, fact IDs, work IDs,
  and raw error text belong in spans, logs, or status payloads.
- Observable gauges MUST be registered once per process after observers exist.
- `NewProviders` owns the dedicated Prometheus registry and the
  `service.name` / `service.namespace` resource labels.

## Change Rules

- New metric: add the `Instruments` field, register it in `NewInstruments`,
  add any dimension key and helper, update the focused public telemetry page,
  and run `go test ./internal/telemetry -count=1`.
- New span: add the `Span*` constant, add it to `spanNames`, call it with
  `tracer.Start(ctx, telemetry.SpanXxx)`, update the trace reference, and run
  telemetry tests.
- New log key: add it to `contract.go`, add it to `logKeys`, prefer a helper
  when multiple packages need it, and update the log reference.
- New pipeline phase: add the phase constant in `logging.go`, use `PhaseAttr`,
  and update logs/correlation docs when async traceability changes.

## Failure Checks

- Missing gauge on `/metrics`: confirm observable gauge registration ran and
  observer errors are logged.
- Missing `trace_id`: confirm the log call receives a context with an active
  valid span.
- Missing OTLP export: confirm `OTEL_EXPORTER_OTLP_ENDPOINT` is set.
- Duplicate instrument registration: confirm each binary calls
  `NewInstruments` once per meter and passes `*Instruments` down.

## Forbidden Without Architecture-Owner Approval

- Renaming the `eshu_dp_` metric namespace.
- Removing or renaming entries returned by `MetricDimensionKeys()`,
  `SpanNames()`, or `LogKeys()`.
- Narrowing wide-range histogram buckets without same-shape evidence.
- Removing `WithResourceAsConstantLabels` for `service.name` and
  `service.namespace`.
- Using the default Prometheus registry.
