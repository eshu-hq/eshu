# Observability Standards

Cross-cutting naming conventions, attribute vocabulary, and cardinality rules
for logs, metrics, spans, and traces in the Eshu Go data plane.

## Structured log keys

All `slog` attribute keys must come from a canonical source:

| Source | Package | Purpose |
|--------|---------|---------|
| `go/internal/telemetry/contract.go` | `telemetry` | Frozen platform keys (scope, domain, phase, failure class) |
| `go/pkg/log/log.go` | `log` | Ergonomic constructors for common attributes |

**Never** use a raw string literal as a `slog` key.  Always call a constructor:

```go
// Correct
logger.ErrorContext(ctx, "msg", log.Err(err), log.CollectorKind(k))

// Wrong — ad-hoc key, may drift
logger.ErrorContext(ctx, "msg", slog.String("error", err.Error()), slog.String("collector_kind", k))
```

### Key naming conventions

- Lowercase snake_case (`collector_kind`, `pipeline_phase`).
- Period-delimited groups for nested context (`acceptance.scope_id`).
- High-cardinality values (paths, IDs, names) go in logs and span attributes,
  never in metric labels.

## Metric dimension cardinality

Metric labels must stay bounded.  Every dimension key is registered in
`go/internal/telemetry/contract.go` and verified by the cardinality audit
test (`go/internal/telemetry/cardinality_audit_test.go`).

### Hard-banned dimension keys

These keys must never appear as OTEL metric label values:

| Key | Reason |
|-----|--------|
| `repo_id` | Unbounded — one per repository |
| `commit_sha` | Unbounded — one per commit |
| `envelope_id` | Unbounded — one per fact envelope |
| `intent_id` | Unbounded — one per reducer intent |
| `worker_id` | Unbounded — one per worker instance |
| `repository_id` | Unbounded — one per repository |
| `workload_id` | Unbounded — one per deployable unit |
| `repo_path` | Unbounded — file system path |
| `cluster_id` | Unbounded per cluster; use for logs only |

### Risk-tracked dimension keys

These keys are currently in the approved metric dimension registry but carry
high-cardinality risk.  They are grandfathered and tracked via follow-up issues
(#3942, #3943).  New metric registrations should prefer bounded alternatives.

| Key | Risk | Follow-up |
|-----|------|-----------|
| `scope_id` | Bounded per collector run but unbounded globally | #3942 |
| `generation_id` | One per scope generation — unbounded | #3943 |

### Approved dimension keys

All approved metric dimension keys are listed in the `metricDimensionKeys`
registry in `go/internal/telemetry/registry.go`, maintained in lockstep with
the constants in `go/internal/telemetry/contract.go`.  The cardinality audit
test (`go/internal/telemetry/cardinality_audit_test.go`, issue #3818) asserts
that no hard-banned key appears in the registry or in any metric registration
site, and warns on risk-tracked keys.

### When to add a dimension

1. The value set is closed and small (≤20 distinct values).
2. The dimension helps operators answer a specific question at 3 AM.
3. The key is registered in `contract.go` AND the `metricDimensionKeys` slice
   AND documented in `docs/public/observability/telemetry-coverage.md`.

If any of these conditions is not met, use a span attribute or a log field instead.

## Tracer names

Every binary must use `telemetry.DefaultSignalName` (`"eshu/go/data-plane"`)
as its tracer name.  No hardcoded tracer name strings are allowed.

```go
// Correct
tracer := providers.TracerProvider.Tracer(telemetry.DefaultSignalName)

// Wrong
tracer := otel.Tracer("eshu-api")
```

This ensures trace correlation across binaries in Tempo/Honeycomb.

## Span names

Span names must use the frozen constants from `go/internal/telemetry/contract.go`:

```go
ctx, span := tracer.Start(ctx, telemetry.SpanProjectorRun)
```

Never inline span name strings.

## Log-phase labels

Every log line should carry a `pipeline_phase` attribute:

```go
logger.InfoContext(ctx, "msg", log.PipelinePhase("parse"))
```

The canonical phases are defined in `go/internal/telemetry/logging.go`:

| Constant | Value | When |
|----------|-------|------|
| `PhaseDiscovery` | `discovery` | Repo selection and scope assignment |
| `PhaseParsing` | `parsing` | File parse, snapshot, content extraction |
| `PhaseEmission` | `emission` | Fact envelope creation and commit |
| `PhaseProjection` | `projection` | Fact-to-graph/content projection |
| `PhaseReduction` | `reduction` | Reducer intent execution |
| `PhaseShared` | `shared` | Shared projection partition processing |
| `PhaseQuery` | `query` | Read-path query operations |
| `PhaseServe` | `serve` | API/MCP request handling |

## Log-level guidelines

| Level | When |
|-------|------|
| `Debug` | Fine-grained state during development only |
| `Info` | Every state transition visible to operators |
| `Warn` | Recoverable anomaly the operator should investigate |
| `Error` | Failure the current operation cannot recover from |

Never log at `Error` for a transient condition the system retries successfully.

## Trace context in logs

The `TraceHandler` in `go/internal/telemetry/logging.go` automatically
injects `trace_id` and `span_id` on every log line when a valid span is in
context.  No manual `trace_id` injection is needed at call sites.

## Migration path

1. Replace raw `slog.String("key", val)` with `go/pkg/log` constructors.
2. Replace hardcoded tracer names with `telemetry.DefaultSignalName`.
3. The cardinality audit test gates new metric dimensions in CI.
