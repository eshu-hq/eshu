# Telemetry Logs

Logs are Eshu's highest-context runtime signal. Use them when metrics identify
that something changed and traces show where time went, but you still need the
exact scope, work item, failure class, retry, or operator action.

The code contract lives in:

- `go/internal/telemetry/logging.go`
- `go/internal/telemetry/contract.go`
- `go/internal/telemetry/registry.go`

For the platform-wide logging envelope, see
[Logging Standard](../logging.md).

## When To Use Logs

Use logs for:

- exact error text
- repository, scope, generation, domain, and partition context
- terminal failure and retry classification
- high-cardinality identifiers that do not belong in metric labels
- the `trace_id` and `span_id` needed to pivot into a trace backend

Do not use logs as the first alerting signal. Alert from metrics, explain
latency with traces, then use logs for the details that metrics deliberately
omit.

## Current Go Log Shape

Go runtimes create JSON `slog` loggers through
`telemetry.NewLogger` or `telemetry.NewLoggerWithWriter`.

Every log line from those loggers carries:

| Field | Meaning |
| --- | --- |
| `timestamp` | UTC RFC3339 timestamp normalized from the built-in `slog` time key. |
| `severity_text` | Normalized `slog` level. |
| `message` | Human-readable log message. |
| `service_name` | Runtime service name from telemetry bootstrap. |
| `service_namespace` | Stable namespace, normally `eshu`. |
| `component` | Component name passed by the runtime. |
| `runtime_role` | Runtime role passed by the runtime. |

When the log call receives a context with an active OpenTelemetry span,
`TraceHandler` also adds:

| Field | Meaning |
| --- | --- |
| `trace_id` | Trace identifier for the active span. |
| `span_id` | Active span identifier. |
| `severity_number` | OpenTelemetry severity number derived from the `slog` level. |

Startup logs, fallback logs, and any log emitted outside an active span can omit
`trace_id`, `span_id`, and `severity_number`. That is expected.

## `event_name` Is Optional

`event_name` appears only when a call site attaches
`telemetry.EventAttr(...)` or writes the key directly during early fallback
startup. Do not assume every log line has an event name.

Current explicit event names in the Go runtimes include:

| Event name | Typical source |
| --- | --- |
| `runtime.startup.failed` | Runtime bootstrap failures. |
| `runtime.shutdown.failed` | Telemetry shutdown failures. |
| `runtime.server.listening` | API, MCP, or pprof listener startup. |
| `runtime.server.failed` | API listener failure. |
| `runtime.server.stopped` | API shutdown completion. |
| `runtime.postgres.connected` | Runtime Postgres connection success. |
| `runtime.neo4j.connected` | Runtime Neo4j connection success. |
| `bootstrap.schema.started` | Data-plane schema bootstrap started. |
| `bootstrap.postgres.applied` | Postgres schema applied. |
| `bootstrap.graph.applied` | Graph schema applied. |
| `bootstrap.graph.skipped` | Graph schema bootstrap skipped. |
| `bootstrap.graph.adopted` | Existing graph schema adopted. |
| `bootstrap.graph.adoption_incomplete` | Existing graph schema adoption was incomplete. |
| `documentation.extraction.completed` | Documentation extraction completed. |
| `documentation.drift.completed` | Documentation drift analysis completed. |

Older examples such as `http.request.completed`, `mcp.request.received`,
`resolution.work_item.completed`, and `graph.batch.commit.started` are not
current universal Go event families. If a doc needs one of those names, verify
the current call site first.

## Structured Keys

The frozen structured log-key registry is exposed by
`telemetry.LogKeys()`. Current registered keys are:

| Key | Use |
| --- | --- |
| `scope_id` | Scope identifier for ingestion, projection, or query work. |
| `scope_kind` | Scope type, such as repository. |
| `source_system` | Origin system, such as Git or Terraform state. |
| `generation_id` | Generation identifier for a collect or projection cycle. |
| `collector_kind` | Collector family, such as Git, Confluence, AWS, OCI, package registry, or Terraform state. |
| `domain` | Reducer, projection, or materialization domain. |
| `partition_key` | Domain-specific partition key for shared work. |
| `request_id` | Request correlation identifier. |
| `failure_class` | Closed failure classification used for triage and retry decisions. |
| `refresh_skipped` | Whether a freshness check skipped collection. |
| `pipeline_phase` | End-to-end pipeline phase filter. |
| `acceptance.scope_id` | Shared-acceptance scope identifier. |
| `acceptance.unit_id` | Shared-acceptance deployable-unit identifier. |
| `acceptance.source_run_id` | Source run tied to an acceptance decision. |
| `acceptance.generation_id` | Generation tied to an acceptance decision. |
| `acceptance.stale_count` | Number of stale acceptance rows observed. |
| `depth` | Terraform drift prior-config walk depth. |
| `prior_config_addresses` | Prior Terraform config addresses found inside the depth window. |
| `state_only_addresses` | Terraform state addresses absent from current config. |
| `addresses_promoted_to_removed_from_config` | State-only addresses promoted to removed-from-config evidence. |
| `multi_element.prefix` | Repeated Terraform nested-block prefix that was truncated. |
| `multi_element.count` | Number of repeated block elements observed by the state flattener. |
| `multi_element.source` | Truncation source, such as parser walk or state flattening. |
| `resource_type` | Terraform resource type for composite-capture diagnostics. |
| `attribute_key` | High-cardinality Terraform attribute key kept out of metric labels. |
| `path` | Source-prefixed Terraform parser path for a composite-capture skip. |
| `error` | Diagnostic error class or message for the logged failure. |

High-cardinality values such as file paths, repository paths, package names,
state locators, image digests, delivery IDs, and raw cloud resource identifiers
belong in logs or traces, not metric labels.

## Pipeline Phases

`pipeline_phase` is the stable filter for end-to-end debugging across the Go
data plane.

| Phase value | Covers |
| --- | --- |
| `discovery` | Repository selection and scope assignment. |
| `parsing` | File parse, snapshot, and content extraction. |
| `emission` | Fact envelope creation and durable commit. |
| `projection` | Fact-to-graph, content, or intent projection. |
| `reduction` | Reducer intent execution. |
| `shared` | Shared projection partition processing. |
| `query` | Read-path query operations. |
| `serve` | API or MCP request handling. |

Use `pipeline_phase` before searching by message text. Messages are human
readable and useful, but phase values are the durable operational contract.

## Common Triage Paths

### One repository or scope looks stale

1. Start from `/admin/status` or queue metrics to identify the affected runtime.
2. Filter logs by `scope_id` or `generation_id`.
3. Check `pipeline_phase` to see whether the run stopped in discovery,
   parsing, emission, projection, reduction, or shared work.
4. Pivot to `trace_id` when the log line carries one.

### A reducer or projection domain is failing

1. Filter by `pipeline_phase=reduction`, `pipeline_phase=projection`, or
   `pipeline_phase=shared`.
2. Narrow by `domain` and `partition_key`.
3. Read `failure_class` before retrying work.
4. Use the matching trace to identify whether the failure is storage, graph,
   query shape, contention, or input data.

### Terraform drift evidence is incomplete

1. Start from the drift metric or failed drift query.
2. Filter logs by `resource_type`, `attribute_key`, `path`, or `error`.
3. Check `depth`, `prior_config_addresses`, `state_only_addresses`, and
   `addresses_promoted_to_removed_from_config`.
4. If composite capture skipped a nested field, inspect `multi_element.*`
   fields before changing parser or schema-bundle behavior.

### Runtime startup failed

1. Search for `event_name=runtime.startup.failed`.
2. Check `service_name`, `component`, and `runtime_role`.
3. Read the attached `error` value.
4. Confirm dependency readiness in metrics, `/readyz`, and `/admin/status`
   before restarting repeatedly.

## Example

```json
{
  "timestamp": "2026-04-16T15:08:17.112345Z",
  "severity_text": "INFO",
  "message": "bootstrap projection succeeded",
  "service_name": "bootstrap-index",
  "service_namespace": "eshu",
  "component": "bootstrap-index",
  "runtime_role": "bootstrap-index",
  "trace_id": "5b2c4f1f0f0b54f8b7c1fb85ac20fd68",
  "span_id": "f1a4e1f0c3139f0a",
  "severity_number": 9,
  "pipeline_phase": "projection",
  "scope_id": "repository:payments",
  "worker_id": 3,
  "fact_count": 1234,
  "duration_seconds": 2.5
}
```

## Change Rules

When changing log behavior:

1. Add new frozen keys in `go/internal/telemetry/contract.go`.
2. Register keys in `go/internal/telemetry/registry.go`.
3. Add helper functions in `go/internal/telemetry/logging.go` only when repeated
   call sites need them.
4. Update this page and [Cross-Service Correlation](cross-service-correlation.md)
   when the key affects async traceability.
5. Run `go test ./internal/telemetry -count=1`.

Do not add new high-cardinality metric labels to avoid writing a log. Logs and
trace attributes are the right place for unbounded operational detail.
