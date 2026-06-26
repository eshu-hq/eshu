# pkg/log

## Purpose

Canonical `slog.Attr` constructors for the Eshu data plane.  Every function
returns an attribute with a stable key name.  Callers use these instead of
raw `slog.String("key", val)` so log lines carry predictable keys across
binaries and packages.

## Ownership boundary

This package owns the ergonomic log-attribute surface.  It does not create
loggers, own handlers, or enforce level policies — those live in
`go/internal/telemetry/logging.go`.  Keys that overlap the frozen telemetry
contract reference `go/internal/telemetry` constants directly.

## Exported surface

See `doc.go` for the godoc contract.

### Telemetry-backed constructors (key from `go/internal/telemetry`)

| Function | Wire key |
|----------|----------|
| `ScopeID(string)` | `scope_id` |
| `ScopeKind(string)` | `scope_kind` |
| `CollectorKind(string)` | `collector_kind` |
| `Domain(string)` | `domain` |
| `GenerationID(string)` | `generation_id` |
| `FailureClass(string)` | `failure_class` |
| `PipelinePhase(string)` | `pipeline_phase` |
| `RequestID(string)` | `request_id` |
| `SourceSystem(string)` | `source_system` |
| `PartitionKey(string)` | `partition_key` |

### Package-owned constructors (key defined here)

| Function | Wire key |
|----------|----------|
| `Err(error)` / `ErrStr(string)` | `error` |
| `TenantID(string)` | `tenant_id` |
| `RepoPath(string)` | `repo_path` |
| `Queue(string)` | `queue` |
| `IntentID(string)` | `intent_id` |
| `WorkerID(string)` | `worker_id` |
| `Component(string)` | `component` |
| `RuntimeRole(string)` | `runtime_role` |
| `RepositoryID(string)` | `repository_id` |
| `WorkloadID(string)` | `workload_id` |
| `ClusterID(string)` | `cluster_id` |
| `ElapsedSeconds(float64)` | `elapsed_seconds` |
| `BatchSize(int)` | `batch_size` |
| `SkipReason(string)` | `skip_reason` |
| `Language(string)` | `language` |
| `Provider(string)` | `provider` |
| `Operation(string)` | `operation` |
| `Status(string)` | `status` |
| `EventKind(string)` | `event_kind` |
| `EventName(string)` | `event_name` |

### Context-aware `With*` helpers

| Function | Wire key |
|----------|----------|
| `WithTenant(ctx, string)` | `tenant_id` |
| `WithCollectorKind(ctx, string)` | `collector_kind` |
| `WithScopeID(ctx, string)` | `scope_id` |
| `WithDomain(ctx, string)` | `domain` |
| `WithGenerationID(ctx, string)` | `generation_id` |
| `WithFailureClass(ctx, string)` | `failure_class` |
| `WithPipelinePhase(ctx, string)` | `pipeline_phase` |

## Dependencies

- `go/internal/telemetry` — frozen log key constants.
- `log/slog` — standard library.

## Telemetry

This package emits no metrics or spans.  It manufactures attributes consumed
by callers that emit logs.

## Gotchas / invariants

- **High-cardinality keys stay in logs, never in metric labels.**
  `RepoPath`, `IntentID`, `WorkerID`, `RepositoryID`, `WorkloadID` are
  suitable for structured log fields and span attributes but must not appear
  in OTEL metric label sets.
- Keys that match metric dimension wire values (`KeyProvider`, `KeyOperation`,
  `KeyStatus`, `KeyEventKind`) do so by intent so operators can correlate log
  lines and metric labels on the same field.
- The `With*` helpers accept `context.Context` as a forward-compatibility
  slot.  Today they do not read from the context; the parameter exists so
  future trace-log correlation can enrich attributes without changing every
  call site.
- When adding a new key, check the telemetry contract first.  If a matching
  constant exists in `go/internal/telemetry/contract.go`, reference it rather
  than duplicating the string literal.

## Related docs

- `docs/internal/design/observability-standards.md` — cross-cutting logging and
  telemetry conventions.
- `go/internal/telemetry/README.md` — full metric, span, and log key inventory.
