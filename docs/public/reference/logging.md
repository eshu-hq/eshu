# Logging Standard

For the operator-facing log guide, also see
[Telemetry Overview](telemetry/index.md).

Eshu uses structured JSON logging on the Go runtimes. That is
intentional: Grafana, Elasticsearch, Datadog, and similar tools can ingest the
same shape without custom parsing.

## Current Go Contract

The logger contract comes from `go/internal/telemetry/logging.go`
and the Go runtime entrypoints.

Guaranteed top-level fields from the Go logger are:

- `timestamp`
- `severity_text`
- `message`
- `service_name`
- `service_namespace`
- `component`
- `runtime_role`

Trace-correlated logs also carry:

- `trace_id`
- `span_id`
- `severity_number`

Common structured dimensions used by the Go data plane include:

- `scope_id`
- `scope_kind`
- `source_system`
- `generation_id`
- `collector_kind`
- `domain`
- `partition_key`
- `failure_class`
- `pipeline_phase`
- `request_id`

`event_name` is **optional** on the Go path. It is present only where a call
site explicitly attaches
`telemetry.EventAttr(...)`.

## Event Names

`event_name` is sparse by design. Current Go code uses it for runtime startup,
runtime server lifecycle, datastore connection, bootstrap schema, documentation
drift/extraction, and query-stage timing events. Examples include:

- `runtime.startup.failed`
- `runtime.shutdown.failed`
- `runtime.server.listening`
- `runtime.server.failed`
- `runtime.server.stopped`
- `runtime.neo4j.connected`
- `runtime.postgres.connected`
- `bootstrap.schema.started`
- `bootstrap.postgres.applied`
- `bootstrap.graph.applied`
- `documentation.drift.completed`
- `documentation.extraction.completed`
- `service_query.stage_started`
- `service_query.stage_completed`
- `repository_query.stage_started`
- `repository_query.stage_completed`
- `supply_chain_query.stage_started`
- `supply_chain_query.stage_completed`
- `supply_chain_query.stage_failed`

`supply_chain_query.stage_failed` is an ERROR event emitted once when the
supply-chain impact-findings route answers HTTP 500 from its own code. It carries
`stage`, `repo_id`, `duration_seconds`, the error text cut to 256 bytes, and two
closed-set labels that never contain error text: `error_site` (`reader_stale`,
`reader_unavailable`, `other`) and `error_cause` (`deadline_exceeded`,
`canceled`, `conn_done`, `eof`, `conn_refused`, `conn_reset`, `net_timeout`,
`sqlstate_<class>`, `unknown`). A `canceled` cause is usually a client that
disconnected.

The `supply_chain_query.stage_completed` event for stage `impact_findings_query`
also carries `reader_borrow_seconds`, `reader_identity_seconds`,
`reader_replay_seconds`, and `business_query_seconds` (#7545), so a slow findings
read names the guarded-reader stage that paid for it. Each is a float in seconds
and the SUM of that stage across every guarded-reader operation inside the
findings read (a stage can run more than once per request, for example the
borrow), not a single latency. The four attributes appear together, are always
on with no sampling, and appear only when the guarded reader recorded a stage
for the read; a read that did not go through it omits them rather than logging
zeros. Other stages never carry them. `business_query_seconds` times the
database call that starts the query, not the row streaming and scanning that
follows it, so the four sums can be well below the stage's `duration_seconds`:
the remainder is row consumption, decoding, and handler-side work, not an
unexplained reader stall.

Do not treat examples such as `http.request.completed`,
`mcp.request.received`, or `index.discovery.completed` as current universal Go
events. Those are test or illustrative values unless a call site explicitly
emits them.

## Phase-Scoped Runtime Logs

Most of the Go write plane uses plain structured `slog` messages plus shared
dimensions, not a separate event namespace per phase.

The important pattern is:

- message stays human-readable
- structured fields carry scope, domain, queue, and phase context
- `pipeline_phase` is the stable phase filter for end-to-end debugging

Current phase values come from `go/internal/telemetry/logging.go`:

- `discovery`
- `parsing`
- `emission`
- `projection`
- `reduction`
- `shared`
- `query`
- `serve`

Examples of current Go runtime messages include:

- `bootstrap collection complete`
- `bootstrap projection succeeded`
- `bootstrap projection failed`
- `bootstrap phase start`
- `bootstrap phase complete`
- `cross-repo relationship resolution started`
- `cross-repo relationship resolution completed`
- `sql relationship materialization started`
- `sql relationship materialization completed`
- `inheritance materialization started`
- `inheritance materialization completed`
- `canonical atomic write completed`
- `canonical sequential write completed`
- `neo4j transient error, retrying`

These are stable enough for operators to search, but they are not all promoted
to `event_name`.

## JSON Logging Rules

- Keep logs in JSON in deployed environments.
- Treat `event_name` as optional metadata, not a required top-level field.
- Put operational dimensions in structured key/value fields, not in free-form
  text.
- Keep trace correlation intact by preserving `trace_id` and `span_id`.
- Use `pipeline_phase` for end-to-end filtering across collector, projector,
  reducer, and query/runtime surfaces.

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

## Operator Guidance

- Start with metrics to detect the failure or slowdown.
- Pivot to traces to see which stage or store consumed time.
- Use logs to extract scope IDs, failure classes, partition keys, and exact
  retry/write context.

That order keeps logs focused on explanation instead of using them as the
first-line alerting signal.
