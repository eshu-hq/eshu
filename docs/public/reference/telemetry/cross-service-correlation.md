# Cross-Service Correlation

Use this page when one user-visible operation crosses runtime boundaries.
Eshu does not rely on one trace tree for the whole pipeline because most work
moves asynchronously through Postgres facts, queues, shared projection intents,
and status rows. Each service can start its own trace root; the shared
correlation keys connect those traces to logs, metrics, and `/admin/status`.

## Service Names

Go entrypoints call `telemetry.NewBootstrap(...)` with these current
`service.name` values. All use `service.namespace=eshu`.

| Runtime | `service.name` |
| --- | --- |
| API | `eshu-api` |
| MCP Server | `mcp-server` |
| Ingester | `ingester` |
| Projector | `projector` |
| Reducer | `reducer` |
| Bootstrap Index | `bootstrap-index` |
| Bootstrap Data Plane | `eshu-bootstrap-data-plane` |
| Webhook Listener | `webhook-listener` |
| Git Collector | `collector-git` |
| AWS Cloud Collector | `collector-aws-cloud` |
| Confluence Collector | `collector-confluence` |
| OCI Registry Collector | `collector-oci-registry` |
| Package Registry Collector | `collector-package-registry` |
| SBOM Attestation Collector | `collector-sbom-attestation` |
| Terraform-State Collector | `collector-terraform-state` |
| Workflow Coordinator | `workflow-coordinator` |

## Correlation Keys

Use the key emitted by the failing path. These keys can appear in spans, logs,
status rows, admin payloads, or metric labels when the value is bounded.

| Key | Use |
| --- | --- |
| `scope_id` | Follow one repository or external source scope. |
| `generation_id` | Connect one collection generation to downstream projection and reduction. |
| `source_run_id` | Group scopes from one collector run where the source emits it. |
| `work_item_id` | Follow one queued item when status or failure logs expose it. |
| `request_id` | Connect API or MCP request logs with trace context where emitted. |
| `pipeline_phase` | Filter logs by `discovery`, `parsing`, `emission`, `projection`, `reduction`, `shared`, `query`, or `serve`. |
| `domain` | Filter reducer work by materialization domain. |
| `partition_key` | Narrow reducer or shared-projection work to one conflict partition. |

The frozen log-key registry is `telemetry.LogKeys()` in
`go/internal/telemetry/registry.go`. Runtime and reducer observability surfaces
publish that registry for maintainer checks, but not every log line carries
every key.

## How To Correlate

### Repository Or Source Scope

1. Start from `/admin/status`, a queue metric, or an ingester/collector log and
   capture `scope_id` plus `generation_id` when present.
2. Search logs by those keys and filter by `service_name`.
3. Open the matching `trace_id` values in the trace backend.
4. Compare collection spans (`collector.observe`, `collector.stream`,
   `fact.emit`), projection spans (`projector.run`, `canonical.write`), reducer
   spans (`reducer.run`, `canonical.write`), and read-path spans.

### Queue Or Reducer Work

1. Capture `domain`, `partition_key`, and any visible work item identifier from
   `/admin/status` or a failure log.
2. Filter logs by `pipeline_phase=reduction` or `pipeline_phase=shared`.
3. Use `failure_class` before retrying or replaying work.
4. Open the trace for the same log event and compare claim, fact load, handler,
   graph write, retry, and ack timing.

### API Or MCP Read

1. Start from the request log or query trace.
2. Capture `request_id`, the requested scope/entity, and the trace IDs.
3. Inspect query-handler spans and child storage spans:
   `postgres.query`, `neo4j.query`, `neo4j.query.single`, or `neo4j.execute`.
4. If the read looks stale, confirm queue and generation state in
   `/admin/status` before blaming the read handler.

## Pitfalls

- A `trace_id` identifies one trace tree. It does not automatically follow
  async queue work across runtimes.
- Child storage spans may omit the high-level scope. Check the parent span and
  logs for `scope_id`, `generation_id`, `domain`, or `partition_key`.
- Always filter by `service_name` when searching by a shared key.
- Logs carry high-cardinality details such as paths, safe resource
  fingerprints, delivery IDs, and exact errors. Do not move those values into
  metric labels, and do not log raw cloud resource identifiers when a
  fingerprint can correlate the event.
