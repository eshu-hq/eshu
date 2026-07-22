# AGENTS.md — internal/telemetry guidance for LLM assistants

## Read first

1. `go/internal/telemetry/README.md` — full metric, span, and log inventory
2. `go/internal/telemetry/contract.go` — frozen span names, log keys, metric
   dimension keys, and the `Bootstrap` type
3. `go/internal/telemetry/instruments.go` — all `Instruments` fields and their
   registered metric names; the `Attr*` helper functions
4. `go/internal/telemetry/logging.go` — `TraceHandler`, phase constants,
   `ScopeAttrs`, `DomainAttrs`, and `PhaseAttr`
5. `go/internal/telemetry/provider.go` — `NewProviders`, `Providers`, OTLP and
   Prometheus wiring
6. `docs/public/reference/telemetry/index.md` — operator-facing tuning and
   signal-selection guidance
7. `docs/public/observability/telemetry-coverage.md` — telemetry coverage
   contract. Every stage in the data plane maps to a row; new stages fail
   X2 without a corresponding entry. Cite this doc when adding a new stage.

## Invariants this package enforces

- **Leaf contract** — no `go/internal/*` imports are permitted here. This
  package is a sink, not a hub. If you need to import any ESHU-internal package,
  the dependency belongs in the caller, not here.
- **`eshu_dp_` prefix** — every metric name registered in `instruments.go` must
  start with `eshu_dp_`. Names without this prefix will conflict with the Python
  `eshu_` namespace.
- **Frozen log keys** — log key constants in `contract.go` (for example
  `LogKeyScopeID`, `LogKeyFailureClass`) are frozen. Reuse an existing key
  before adding a new one. New keys require updating `contract.go`, the
  telemetry reference doc, and the cross-service correlation guide.
- **Frozen span names** — `Span*` constants in `contract.go` are frozen. Add
  new names to the `spanNames` slice in `contract.go` before using them in
  callers. Query-handler spans such as `SpanQueryEvidenceCitationPacket` must
  stay stable because API and MCP observability depends on the span name.
- **No high-cardinality metric labels** — file paths, fact IDs, repository
  names, and work-item IDs must not appear in metric attribute values. They
  belong in span attributes or log fields. Dashboards and alert rules depend on
  bounded label cardinality.

- **Graph-read outcomes stay closed and sanitized** — `neo4j.query` read spans
  use the constants in `contract_graph_read.go`; the duration histogram uses
  only `operation="read"` and the same closed outcome vocabulary. Slow,
  deadline, and unavailable warnings must not include Cypher text, graph
  addresses, or raw driver errors.

## How to add a new metric

1. Add the field to `Instruments` in `instruments.go`. For example, a new
   counter would be `FactsEmitted metric.Int64Counter` (using the existing
   `metric.Int64Counter` type for counters or `metric.Float64Histogram` for
   histograms).
2. Register the instrument inside `NewInstruments` using the meter, with a name
   starting with `eshu_dp_`, a description, and explicit bucket boundaries if the
   default OTEL buckets are not appropriate for the measurement range.
3. If the metric needs a new dimension key, add the constant to the
   `MetricDimensionScopeID`-style group in `contract.go` and add it to
   `metricDimensionKeys` so `MetricDimensionKeys()` stays current. Add a
   matching `AttrScopeID`-style helper function in `instruments.go`.
4. Run `go test ./internal/telemetry -count=1` to verify registration succeeds.
5. Update `docs/public/reference/telemetry/index.md` (metrics table) and this
   package's `README.md` in the same PR.

## How to add a new span

1. Add a `Span*` constant to the `spanNames` constant block in `contract.go`.
2. Add the constant to the `spanNames` slice so `SpanNames()` returns it.
3. In the calling package, use `tracer.Start(ctx, telemetry.SpanXxx)` — never
   inline the string literal.
4. Update `docs/public/reference/telemetry/index.md` (span table).

## How to add a new log key

1. Add a constant in the `LogKeyScopeID`-style group in `contract.go`:

   ```go
   LogKeyNewField = "new_field"
   ```

2. Add it to the `logKeys` slice so `LogKeys()` returns it.
3. If the key is commonly used together with other keys, add a helper function
   (like `ScopeAttrs` or `DomainAttrs`) in `logging.go`.
4. Reuse the key across all packages rather than creating package-local string
   literals.

## How to add a new pipeline phase

1. Add a constant in `logging.go` alongside the existing `PhaseDiscovery`-style
   block:

   ```go
   PhaseNewStage = "new_stage"
   ```

2. Use `PhaseAttr` with the new constant value at every log site for the new
   phase.
3. Update `docs/public/reference/telemetry/index.md` (structured log keys table).

## Observable gauge wiring

Observable gauges are registered separately from counters and histograms because
they need live data sources. The correct call order at startup is:

```
providers, _ := telemetry.NewProviders(ctx, bootstrap)
inst, _      := telemetry.NewInstruments(meter)
// ... wire queue and worker implementations ...
telemetry.RegisterObservableGauges(inst, meter, queueObs, workerObs)
telemetry.RegisterAcceptanceObservableGauges(inst, meter, acceptanceObs)
telemetry.RecordGOMEMLIMIT(meter, limitBytes)
```

Calling `RegisterObservableGauges` more than once for the same meter produces a
duplicate-instrument error from the OTEL SDK.

## Common failure modes and how to debug

- **Metric missing from `/metrics`** — if it is a gauge, `RegisterObservableGauges`
  was probably not called, or the observer returned an error. Add an error log
  in the observer implementation. For counters/histograms, check whether
  `NewInstruments` returned an error that was silently swallowed.

- **`trace_id` absent from log lines** — `TraceHandler` only injects trace
  context when a valid span is active in the passed context. Ensure `ctx` flows
  from a span-bearing call site. Log lines outside any span deliberately omit
  trace fields.

- **OTLP export not working** — OTEL_EXPORTER_OTLP_ENDPOINT must be non-empty
  for OTLP gRPC exporters to be created. `NewProviders` skips OTLP wiring when
  the env var is empty; the Prometheus exporter always runs.

- **Duplicate instrument registration panic** — each `metric.Meter` instance
  can register a given instrument name only once. If two packages call
  `NewInstruments` with the same meter, the second call will fail. Each binary
  should call `NewInstruments` once and pass `*Instruments` down.

## Anti-patterns specific to this package

- **Adding internal Eshu imports** — this package must stay a leaf. Any
  dependency on `internal/facts`, `internal/reducer`, `internal/storage`, etc.
  creates a circular import that blocks compilation.

- **Inlining metric name strings in callers** — always reference the
  `Instruments` field and the `Attr*` helpers. Never write
  `meter.Float64Histogram("eshu_dp_projector_run_duration_seconds", ...)` outside
  this package.

- **Adding repository or file path values to metric attributes** — these are
  high-cardinality and will produce unbounded label sets in Prometheus. Use
  span attributes or `slog` log fields for path-level context.

- **Using the default Prometheus registry** — `NewProviders` creates a dedicated
  `prometheus.Registry`. Registering instruments on `prometheus.DefaultRegisterer`
  bypasses the bridge and those metrics will not appear on the Eshu `/metrics`
  endpoint.

- **Dropping `WithResourceAsConstantLabels` from the Prometheus exporter**
  — `provider.go` keys the allow-keys filter to `service.name` and
  `service.namespace` so both reach Prometheus as labels on every
  `eshu_dp_*` metric. Removing or narrowing that filter turns the
  `$service` and `$namespace` Grafana template variables back into empty
  dropdowns and silently empties every panel that filters by them. The
  regression gate lives in `provider_resource_labels_test.go` (test
  TestPrometheusExposesServiceLabelsOnMetrics).

## What NOT to change without discussion

- The `eshu_dp_` prefix — a rename requires coordinating with all dashboards,
  alerts, and the Python namespace.
- `MetricDimensionKeys()`, `SpanNames()`, `LogKeys()` — these return the frozen
  contract set; tests assert on their contents.
- Bucket boundaries on wide-range histograms such as
  `eshu_dp_reducer_queue_wait_seconds` (0.001–21600 s) and
  `eshu_dp_generation_fact_count` (10–300000) — they were chosen to capture the
  full observed distribution; narrowing them silently truncates operator data.

## Evidence

### 2026-06-26 — Remove scope_id from metric dimension registry (#3942)

No-Regression Evidence: `go test ./internal/telemetry ./internal/projector
./cmd/bootstrap-index ./internal/collector ./internal/reducer ./internal/app
./internal/storage/cypher ./cmd/reducer ./cmd/ingester -count=1` → all PASS.
The change replaces `scope_id` (unbounded per-scope identifier) with
`scope_kind` (bounded closed set of ~18 values) as a metric dimension label on
all affected instruments. Call sites in projector, bootstrap-index, and
collector use `AttrScopeKind(string(scope.ScopeKind))`; reducer cross-repo
resolution metrics drop the dimension entirely (scope_kind would be a constant
`"repository"`, so it carries no diagnostic value). Metric emission volume,
instrument count, and code paths are byte-identical aside from the label key
change — no worker, queue, lease, batch, Cypher, or concurrency path touched.
Span-attribute uses of `AttrScopeID` (collector observe, canonical write/retract
spans) are unchanged.

No-Observability-Change: the change adds no route, graph query shape, queue
domain, worker, lease, runtime knob, metric instrument, or new metric label key
(`scope_kind` was already registered). Operators slice the same histograms and
counters with `scope_kind` instead of `scope_id`; structured logs still carry
`scope_id` via `LogKeyScopeID`.
