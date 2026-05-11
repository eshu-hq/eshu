# Telemetry

Eshu exposes four operator views:

- metrics for rates, latency, backlog, concurrency, and capacity
- traces for request and pipeline timing
- structured JSON logs for repository, run, work item, and failure context
- `/admin/status` for live stage, queue state, and failure summaries

## What To Watch

| Symptom | First signal | Next signal |
| --- | --- | --- |
| API or MCP is slow | request metrics | traces and runtime logs |
| Queue backlog is rising | queue depth and oldest-age metrics | reducer `/admin/status` |
| One repo is slow | ingester metrics | discovery report and collector logs |
| Graph writes are slow | reducer and graph-write metrics | graph backend traces/logs |
| Replay or dead-letter behavior looks wrong | recovery metrics | `/admin/status` and recovery logs |

Go data-plane metrics use the `eshu_dp_` prefix. Runtime status gauges use the
`eshu_runtime_` prefix.

Every metric Eshu emits carries `service_name` and `service_namespace`
labels derived from the OTEL resource attributes. Filter dashboards and
alerts on those labels rather than `instance` or `job`. The runtime defaults
to `service_namespace="eshu"` and sets `service_name` per binary
(`collector-git`, `collector-terraform-state`, `eshu-ingester`,
`eshu-reducer`, and so on).

Docker Compose exposes Prometheus-format metrics on the runtime ports listed in
[Health Checks](health-checks.md). Add `docker-compose.telemetry.yml` when you
want a local OpenTelemetry collector and Jaeger at `http://localhost:16686` for
developer or DevOps testing.

## Reference

- [Telemetry Overview](../reference/telemetry/index.md)
- [Metrics](../reference/telemetry/metrics.md)
- [Traces](../reference/telemetry/traces.md)
- [Logs](../reference/telemetry/logs.md)
