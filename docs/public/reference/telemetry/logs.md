# Telemetry Logs

Logs are the highest-context signal in Eshu.

Use them when you need:

- exact error text
- a breadcrumb trail for one repository, run, or work item
- structured event details that do not belong in metric labels
- a direct bridge from human-readable context into traces

## Canonical Log Shape

Eshu writes one JSON document per log line.

Important top-level fields:

- `timestamp`
- `severity_text`
- `message`
- `event_name`
- `service_name`
- `component`
- `transport`
- `runtime_role`
- `trace_id`
- `span_id`
- `request_id`
- `correlation_id`
- `exception_type`
- `exception_message`
- `exception_stacktrace`
- `extra_keys`

## Why Logs Matter

Metrics tell you that something is wrong.
Traces tell you where the time went.
Logs tell you what actually happened in human terms.

That makes logs the best signal for:

- debugging retries and fallback behavior
- understanding why work items were dead-lettered
- understanding why a repository failed
- explaining queue churn and fallback behavior
- reconstructing operator actions during incidents

## Important Event Families

### API and MCP

- `http.request.completed`
- `mcp.request.received`
- `mcp.tool.completed`

Why it matters:

- gives request context and completion details that metrics intentionally omit

### Git Collector

- `index.discovery.completed`
- `index.parse.started`
- `index.parse.progress`
- `index.parse.slow_file`
- `index.repository.started`
- `index.repository.commit_wait.completed`
- `index.repository.commit.started`
- `index.repository.completed`
- `facts.snapshot.emitted`
- `facts.inline_projection.leased`
- `facts.inline_projection.lease_missed`
- `facts.inline_projection.failed`
- `facts.inline_projection.completed`

Why it matters:

- provides the breadcrumb trail for repo-level indexing behavior
- shows where the Git collector handed work to the facts-first pipeline

### Graph persistence

- `graph.batch.commit.started`
- `graph.batch.commit.chunk_retry`
- `graph.batch.commit.file_failed`
- `graph.batch.entity.flush`
- `graph.batch.chunk.started`
- `graph.batch.chunk.completed`

Why it matters:

- best source for fallback and chunk-level persistence behavior

### Content persistence

- `content.dual_write.failed`
- `content.dual_write_batch.failed`

Why it matters:

- explains silent content-store misses that metrics alone cannot describe

### Resolution Engine

- `resolution.work_item.projected`
- `resolution.work_item.completed`
- `resolution.work_item.failed`
- `resolution.work_item.dead_lettered`
- `resolution.decision.recorded`
- `resolution.stage.failed`

Why it matters:

- explains whether the failure happened during fact load, relationship projection, workload/platform materialization, or final work-item completion
- gives on-call responders the exact `work_item_id`, `source_run_id`, and error class needed to replay or investigate
- records explainable confidence decisions so operators can inspect why a relationship, workload, or platform edge was produced

### Index status and refinalize

- `http.request.completed`
- `index.finalization.started`
- `index.finalization.partial_start`
- `index.finalization.stage.completed`
- `index.finalization.completed`
- `index.finalization.deferred`
- `admin.refinalize.started`
- `admin.refinalize.stage`
- `admin.refinalize.completed`
- `admin.refinalize.failed`
- `admin.refinalize.coverage_repair.started`
- `admin.refinalize.coverage_repair.completed`
- `admin.refinalize.coverage_repair.failed`

Why it matters:

- `GET /api/v0/status/index` and `GET /api/v0/index-status` are read paths, so
  their breadcrumbs usually come from the HTTP request log plus the underlying
  finalization events
- graph-safe admin refinalize should emit the admin.refinalize family only
- unexpected `index.finalization.*` activity without a matching admin repair
  request should be treated as a runtime investigation signal, not a fallback
  bridge path

### Admin and operator workflow

- `admin.facts.replayed`
- `admin.facts.work_items.listed`
- `admin.facts.decisions.listed`
- `admin.facts.dead_lettered`
- `admin.facts.backfill.requested`
- `admin.facts.replay_events.listed`

Why it matters:

- records deliberate replay actions against dead-lettered fact work items so incident timelines stay auditable
- provides a direct breadcrumb when retry pressure drops because an operator manually replayed failed work
- makes queue inspection, dead-letter overrides, and backfill requests visible in the same incident timeline as runtime failures

## What Belongs In `extra_keys`

Good fields for `extra_keys`:

- `run_id`
- `repo_id`
- `repo_name`
- `repo_path`
- `phase`
- `status`
- `duration_seconds`
- `rows`
- `batch_type`
- `backend`
- `file_path`
- `worker_id`
- `work_item_id`
- `source_run_id`
- `attempt_count`
- `error_class`
- `replayed_count`

These are high-value because they are useful for incident filtering but too high-cardinality for metric labels.

## How To Leverage Logs

### During an incident

1. Start from the alerting metric.
2. Filter logs by `service_name`, `component`, and the likely time window.
3. Narrow with `run_id`, `repo_path`, or `work_item_id`.
4. Jump into the matching trace with `trace_id`.

### During performance tuning

Use logs to validate what the metrics imply:

- slow parse files
- commit chunk retries
- per-worker behavior
- fallback paths

### During autoscaling work

Use logs to confirm whether worker saturation is real or whether the queue/database path is the true bottleneck.

## Go Data Plane Logs

The Go data plane emits structured JSON logs via `log/slog` with a custom
`TraceHandler` that injects OTEL trace context into every log line. All Go
services (`ingester`, `reducer`, `bootstrap-index`) share this logging
infrastructure from the `go/internal/telemetry` package.

### Log Format

Every Go data plane log line is a single JSON document written to stderr:

```json
{
  "timestamp": "2026-04-13T18:30:00.000Z",
  "severity_text": "INFO",
  "message": "collector snapshot completed",
  "service_name": "eshu-ingester",
  "service_namespace": "eshu",
  "component": "collector-git",
  "runtime_role": "ingester",
  "trace_id": "abc123def456...",
  "span_id": "789abc...",
  "scope_id": "git-repository-scope:...",
  "generation_id": "...",
  "source_system": "git",
  "pipeline_phase": "emission",
  "collector_kind": "git",
  "fact_count": 42
}
```

Base attributes (`service_name`, `service_namespace`) are set once at logger
creation and appear on every line. Trace correlation (`trace_id`, `span_id`)
is injected automatically by `TraceHandler` from the active OTEL span context.

### Structured Log Keys

These keys come from the frozen telemetry contract (`contract.go`) and appear
consistently across all Go data plane services:

| Key | Type | Description |
| --- | --- | --- |
| `scope_id` | string | Ingestion scope identifier (e.g. `git-repository-scope:<repo_id>`) |
| `scope_kind` | string | Scope type (e.g. `repository`) |
| `source_system` | string | Origin system (e.g. `git`) |
| `generation_id` | string | Scope generation identifier for this collect cycle |
| `collector_kind` | string | Collector type (e.g. `git`) |
| `domain` | string | Reducer or projection domain (e.g. `workload`, `platform`) |
| `partition_key` | string | Shared projection partition key |
| `request_id` | string | Request correlation identifier |
| `failure_class` | string | Error classification for retryable vs terminal failures |
| `refresh_skipped` | bool | Whether a freshness check skipped re-collection |
| `pipeline_phase` | string | Pipeline phase identifier (see below) |

### Pipeline Phase Values

The `pipeline_phase` key lets operators filter logs by pipeline stage. Every
structured log line in the Go data plane carries exactly one of these values:

| Phase | Value | Where | What it covers |
| --- | --- | --- | --- |
| Discovery | `discovery` | Collector | Repository selection and scope assignment |
| Parsing | `parsing` | Collector | File parse, snapshot, content extraction |
| Emission | `emission` | Collector | Fact envelope creation and durable commit |
| Projection | `projection` | Projector | Fact-to-graph/content projection |
| Reduction | `reduction` | Reducer | Reducer intent execution |
| Shared | `shared` | Reducer | Shared projection partition processing |

### Trace Correlation

The custom `TraceHandler` wraps the standard `slog.JSONHandler` and
automatically injects `trace_id` and `span_id` from the active OTEL span
context. This means every structured log line inside a traced code path
carries the trace identifiers needed to jump from logs into Jaeger or your
trace backend.

When no span is active (e.g. during startup), `trace_id` and `span_id` are
omitted rather than emitted as zero values.

### Helper Functions

The telemetry package provides attribute helpers for consistent log shaping:

- `ScopeAttrs(scopeID, generationID, sourceSystem)` — returns scope-level
  attributes for collector and projector logs
- `DomainAttrs(domain, partitionKey)` — returns domain-level attributes for
  reducer and shared projection logs
- `PhaseAttr(phase)` — returns a `pipeline_phase` attribute
- `FailureClassAttr(class)` — returns a `failure_class` attribute for error
  classification

### Go Data Plane Log Events

#### Collector

- Scope assignment completed — `pipeline_phase=discovery`, includes
  `collector_kind` and repository count
- Snapshot completed — `pipeline_phase=emission`, includes `repo_path`,
  `file_count`, `fact_count`
- Commit succeeded/failed — `pipeline_phase=emission`, includes `scope_id`,
  `generation_id`, `fact_count`

#### Projector

- Projection succeeded/failed — `pipeline_phase=projection`, includes
  `scope_id`, `generation_id`, graph/content/intent counts

#### Reducer

- Execution succeeded/failed — `pipeline_phase=reduction`, includes `domain`,
  `partition_key`, `failure_class` on errors
- Shared projection cycle — `pipeline_phase=shared`, includes `domain`,
  `partition_key`

### Go Data Plane Log Recipes

#### Trace a single repository through the pipeline

```
# Filter by scope_id across all services
jq 'select(.scope_id == "git-repository-scope:<repo_id>")' < logs.jsonl
```

#### Find all errors in one pipeline phase

```
jq 'select(.pipeline_phase == "projection" and .severity_text == "ERROR")' < logs.jsonl
```

#### Jump from a log line to its trace

1. Find the log line with the error or event of interest.
2. Copy the `trace_id` value.
3. Search for that trace ID in Jaeger or your trace backend.
4. The matching trace shows the full span tree for that operation.

#### Correlate database latency with pipeline failures

1. Filter logs by `pipeline_phase` and `failure_class`.
2. Use the `trace_id` to find the matching trace.
3. Inspect child `postgres.exec`/`postgres.query` or `neo4j.execute` spans
   for database-level latency or errors.

## Relationship To Existing Logging Standard

This page is the operator-facing guide.

For the full logging envelope and platform rules, see the existing [Logging Standard](../logging.md).
