# Cross-Service Trace Correlation

Eshu runtimes emit OpenTelemetry traces through an OTEL Collector to a backend
such as Jaeger. In Kubernetes, the collector is part of the observability stack.
On a laptop, add `docker-compose.telemetry.yml` to the local Compose command
when you want the local collector and Jaeger.

Use this page when one user-visible operation crosses service boundaries.

## Why Trace Trees Stay Separate

Most Eshu service coordination is asynchronous through Postgres facts, queues,
shared projection intents, and status rows. Trace context is not automatically
propagated through those durable queues like it would be through direct HTTP or
gRPC request headers.

Each runtime can start its own trace root when it collects, claims, reduces, or
serves a request. The trace trees are separate, but they share correlation keys.

## Service Names

Current Go entrypoints pass these names to `telemetry.NewBootstrap(...)`.
All use `service.namespace = eshu`.

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
| Terraform-State Collector | `collector-terraform-state` |
| Workflow Coordinator | `workflow-coordinator` |

## Correlation Keys

Use the key emitted by the failing path. Keys can appear in spans, logs,
metrics, admin payloads, or status rows.

| Key | Useful for |
| --- | --- |
| `scope_id` | Following one repository or source scope across collection, projection, reducer work, and reads. |
| `generation_id` | Connecting one collection generation to downstream projection and reduction. |
| `source_run_id` | Grouping repositories or scopes from one collector run where emitted. |
| `work_item_id` | Following one queued fact or reducer work item from enqueue to completion or failure. |
| `request_id` | Connecting API/MCP logs for one request where emitted. |
| `pipeline_phase` | Filtering logs by `discovery`, `parsing`, `emission`, `projection`, `reduction`, `shared`, `query`, or `serve`. |
| `domain` | Filtering reducer work by domain, such as `workload` or `platform`. |
| `partition_key` | Narrowing reducer or shared-projection work to one conflict partition. |

## Recipes

### Follow a repository from ingestion to graph

1. Get the repository `scope_id` from ingester logs, status output, or the
   `collector.observe` span attributes.
2. Search traces and logs for that `scope_id`.
3. Filter by `service.name` to separate ingester, reducer, and API/MCP reads.
4. Compare these trees:
   - ingester: `collector.observe`, `fact.emit`, `projector.run`,
     `canonical.write`
   - reducer: `reducer.run`, `canonical.write`
   - read path: query spans with child `postgres.query`, `neo4j.query`, or
     `neo4j.query.single`

### Follow one queue item

1. Get `work_item_id` from `/admin/status`, queue inspection, or the failure
   log.
2. Search logs for that work item. If the path does not log it directly, fall
   back to `scope_id`, `generation_id`, `domain`, and `partition_key`.
3. Open each matching `trace_id` in Jaeger.
4. Compare enqueue, claim, fact load, execution, graph write, retry, and ack
   spans.

### Follow an API or MCP request into storage

1. Start from the request log or trace.
2. Capture `request_id` and any scope or entity identifier in the request.
3. Search logs for the request or entity context.
4. Open the query trace and inspect `postgres.query`, `neo4j.query`,
   `neo4j.query.single`, or `neo4j.execute` spans.

### Diagnose shared projection

1. Filter logs by `pipeline_phase=shared` and the target `domain`.
2. Add `partition_key` when you know the conflict key.
3. Open the matching `canonical.write` trace.
4. Compare intent loading, lease management, and graph write spans.

## Correlation Shape

```text
API or MCP request
  service.name: eshu-api or mcp-server
  trace_id: A, request_id: R1
  -> query span
     -> postgres.query / neo4j.query / neo4j.query.single
        scope_id: S1

scope_id S1 connects to:

Ingester
  service.name: ingester
  trace_id: B, generation_id: G1
  -> collector.observe
  -> fact.emit
  -> projector.run
  -> canonical.write

Reducer
  service.name: reducer
  trace_id: C, domain: workload, partition_key: P1
  -> reducer.run
  -> canonical.write
```

## Grafana And Loki

Use logs for context and traces for timing. Example LogQL filters:

```logql
{service_name=~"eshu-api|mcp-server|ingester|reducer"}
  | json
  | scope_id="git-repository-scope:<id>"
```

```logql
{service_name=~"ingester|reducer"}
  | json
  | pipeline_phase="projection"
```

```logql
{service_name="reducer"}
  | json
  | pipeline_phase="shared"
  | domain="workload"
  | severity_text="ERROR"
```

For logs-to-traces links, configure a Loki derived field named `trace_id` that
extracts the JSON field and opens the matching Jaeger trace.

## Dashboard Patterns

- Cross-service latency: graph p95 collection, projection, reducer run, and
  API/MCP request latency where exposed.
- Queue health: graph queue depth, oldest queue age, runtime outstanding work,
  shared projection cycles, and stale intent counts.
- Scope-level progression: start from a `scope_id`, then inspect logs, traces,
  and queue/status rows for the same scope.

## Pitfalls

- A `trace_id` identifies one service trace tree. It does not follow async work
  across queues.
- Correlation keys are not guaranteed on every dependency span. Check parent
  spans and logs when a child `postgres.*` or `neo4j.*` span lacks context.
- Always filter by `service.name` when searching by `scope_id`.
- Logs carry failure class, retry count, domain, and partition context that
  traces may not include.

## When Correlation Fails

1. Check `/healthz`, `/readyz`, and `/admin/status`.
2. Confirm `OTEL_EXPORTER_OTLP_ENDPOINT` and collector health.
3. Check that the query uses the current `service.name` and `pipeline_phase`.
4. Check queue depth, oldest age, retry counts, and dead-letter state.
5. Use logs carrying `failure_class`, `scope_id`, `generation_id`, `domain`,
   or `partition_key` before restarting services.
