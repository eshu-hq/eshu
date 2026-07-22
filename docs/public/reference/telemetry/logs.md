# Telemetry Logs

Logs are Eshu's highest-context runtime signal. Use them after metrics identify
the changed service or phase and traces show where time went.

The code contract lives in `go/internal/telemetry/logging.go`,
`go/internal/telemetry/contract.go`, and `go/internal/telemetry/registry.go`.
For the platform envelope, see [Logging Standard](../logging.md).

## Log Shape

Go runtimes create JSON `slog` loggers through `telemetry.NewLogger` or
`telemetry.NewLoggerWithWriter`.

Every line from those loggers carries:

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
`TraceHandler` also adds `trace_id`, `span_id`, and `severity_number`.
Startup logs and logs emitted outside an active span can omit those fields.

## Event Names

`event_name` appears only when a call site attaches
`telemetry.EventAttr(...)` or writes the key directly during early fallback
startup. It is not a required field on every log line.

Current event-name call sites include runtime startup, shutdown, listener,
Postgres, Neo4j, data-plane schema bootstrap, documentation extraction and
drift completion, and repository or service query stage timing. Verify current
names with `EventAttr(...)` call sites before documenting a new event family.

Older examples such as `resolution.work_item.completed` and
`graph.batch.commit.started` are not universal Go event families in the current
code.

`query.graph_read.warning` is emitted only for slow, deadline, or unavailable
graph-read outcomes. It carries `pipeline_phase="query"`, a bounded
`failure_class`, and `duration_seconds`; it deliberately omits Cypher text,
graph addresses, and raw driver errors.

See [Graph-read safety](graph-read-safety.md) for the shared deadline and
operator triage contract.

## Structured Keys

`telemetry.LogKeys()` exposes the frozen registry. Start with these groups:

| Key group | Use |
| --- | --- |
| `scope_id`, `scope_kind`, `source_system`, `generation_id`, `collector_kind` | Locate source scope and collection generation. |
| `domain`, `partition_key`, `failure_class`, `refresh_skipped`, `pipeline_phase` | Triage reducer, projection, shared-work, retry, and skip behavior. |
| `request_id` plus emitted `trace_id` and `span_id` | Correlate request logs with traces. |
| `acceptance.*` | Debug shared-acceptance decisions. |
| `resource.fingerprint`, `resource.identity_kind`, `resource.type` | Correlate cloud or infrastructure resources without exposing raw ARNs, Terraform addresses, or secret-shaped names. |
| `depth`, `prior_config_addresses`, `state_only_addresses`, `addresses_promoted_to_removed_from_config`, `multi_element.*`, `resource_type`, `attribute_key`, `path`, `error` | Debug Terraform-state drift and composite-capture behavior. |
| `semantic_extraction.status`, `semantic_extraction.source_class`, `semantic_extraction.provider_kind`, `semantic_extraction.provider_profile_class`, `semantic_extraction.budget_state`, `semantic_extraction.budget_reason` | Debug semantic extraction queue, provider, and budget lifecycle without logging prompts, provider responses, credentials, source IDs, or provider profile IDs. |

High-cardinality values such as file paths, repository paths, package names,
state locators, image digests, delivery IDs, and raw cloud resource identifiers
must not become metric labels. For cloud/runtime resource logs, use
`resource.fingerprint`, `resource.identity_kind`, and `resource.type` instead
of the raw identifier.

## Pipeline Phases

`pipeline_phase` is the stable filter for end-to-end debugging:

| Value | Covers |
| --- | --- |
| `discovery` | Repository selection and scope assignment. |
| `parsing` | File parse, snapshot, and content extraction. |
| `emission` | Fact envelope creation and durable commit. |
| `projection` | Fact-to-graph, content, or intent projection. |
| `reduction` | Reducer intent execution. |
| `shared` | Shared projection partition processing. |
| `query` | Read-path query operations. |
| `serve` | API or MCP request handling. |

Use `pipeline_phase` before searching by message text. Messages can change;
phase values are the durable operational contract.

## Triage Order

1. Start from `/admin/status` or queue metrics to identify the affected runtime.
2. Filter logs by `scope_id`, `generation_id`, `domain`, `partition_key`, or
   `request_id`.
3. Check `pipeline_phase` and `failure_class`.
4. Pivot to `trace_id` when the log line carries one.
5. Use exact errors and high-cardinality identifiers from logs to decide
   whether the failure is source data, storage, graph, retry, contention, or
   caller shape.

## Change Rules

When changing log behavior:

1. Add new frozen keys in `go/internal/telemetry/contract.go`.
2. Register keys in `go/internal/telemetry/registry.go`.
3. Add helper functions in `go/internal/telemetry/logging.go` only when
   repeated call sites need them.
4. Update this page and [Cross-Service Correlation](cross-service-correlation.md)
   when the key affects async traceability.
5. Run `go test ./internal/telemetry -count=1`.

Do not add high-cardinality metric labels to avoid writing a log. Logs and
trace attributes are the right place for unbounded operational detail.
