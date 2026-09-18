# Telemetry operational helpers

## Attribute and log helpers

Attribute helpers — typed constructors for every metric dimension key, for
example `AttrDomain`, `AttrScopeID`, `AttrWritePhase`; use these rather than
`attribute.String` literals when recording metrics.
`SafeResourceLogIdentity` and `SafeResourceLogAttrs` turn raw cloud or
infrastructure identifiers into deterministic fingerprints plus bounded
identity-kind and resource-type fields. Use them when logs need to correlate a
resource without exposing ARNs, Terraform addresses, or secret-shaped names.
Scanner-worker labels use `AttrAnalyzer`, `AttrTargetKind`, and
`AttrLimitKind`; values must come from bounded analyzer, target, and limit
enums, not paths, package names, registry URLs, or raw locators.
Webhook listener labels use `AttrProvider`, `AttrEventKind`, `AttrDecision`,
`AttrStatus`, `AttrOutcome`, and `AttrReason` so provider intake stays on the
same bounded vocabulary as the rest of the data plane.

`ScopeAttrs`, `DomainAttrs`, `AcceptanceAttrs` — return `[]slog.Attr` slices
for the common scope, domain, and acceptance-context log fields.

`PhaseAttr`, `FailureClassAttr`, `AcceptanceStaleCountAttr`, `EventAttr` —
single-key `slog.Attr` constructors for the most frequently repeated log fields.

### Logging

`NewLogger` and `NewLoggerWithWriter` — construct a JSON `slog.Logger` backed
by `TraceHandler`. The handler injects `trace_id`, `span_id`, and
`severity_number` from the active OTEL span context on every record. Base
attributes `service_name`, `service_namespace`, `component`, and `runtime_role`
are attached at logger creation.

### Refresh counter

`RecordSkippedRefresh`, `SkippedRefreshCount` — process-local atomic counter for
incremental-refresh skip tracking. Not a metric; used only for status-surface
reporting.

## Dependencies

No internal Eshu package imports; external dependencies are OTEL and Prometheus.

This is a leaf package. Introducing any `go/internal/*` import here creates a
circular dependency and must not happen.

## Telemetry

This package defines the telemetry contract. It emits nothing itself at
runtime; all emission happens in the packages that consume `Instruments`.

## Gotchas / invariants

- `NewInstruments` registers every counter and histogram but does not wire
  observable gauges. Call `RegisterObservableGauges` after the queue and worker
  implementations are ready, otherwise `eshu_dp_queue_depth`,
  `eshu_dp_queue_oldest_age_seconds`, and `eshu_dp_worker_pool_active` will not
  appear on `/metrics`.
- Observable gauges are registered exactly once per process. Calling
  `RegisterObservableGauges` more than once for the same meter produces
  duplicate-instrument errors from the OTEL SDK.
- The Prometheus exporter uses its own `prometheus.NewRegistry()` (not the
  default registry), so it is isolated from any third-party code that
  registers on the default registry.
- The Prometheus exporter is constructed with
  `otelprom.WithResourceAsConstantLabels` keyed to allow `service.name`
  and `service.namespace` only. Dropping or narrowing that filter breaks
  every dashboard that filters by `service_name` or `service_namespace`.
  The regression gate lives in `provider_resource_labels_test.go` (test
  TestPrometheusExposesServiceLabelsOnMetrics). The default exporter
  behavior leaves those attributes on `target_info` alone, so a Grafana
  template variable scoped to a data-plane selector like
  `label_values(eshu_dp_facts_emitted_total, service_name)` returns
  empty even though `label_values(target_info, service_name)` works.
- `TraceHandler` injects trace IDs only when a valid span is active; log lines
  outside any span deliberately omit trace fields.
- `RecordGOMEMLIMIT` no-ops when `meter` is nil.
- High-cardinality values — repository paths, fact IDs, work-item IDs — belong
  in spans or log fields, never labels.
- Metric names are frozen once registered; prefer adding a new name over
  renaming.

## Related docs

See `docs/public/reference/telemetry/index.md`, `docs/public/architecture.md`,
and `docs/public/deployment/service-runtimes.md`.
