# Service Workflows

Use this page to understand how deployed Eshu services cooperate. For the full
runtime matrix, use [Service Runtimes](../deployment/service-runtimes.md). For
signals and proof gates, use [Telemetry Overview](telemetry/index.md) and
[Local Testing](local-testing.md).

## Workflow Map

| Stage | Owner | Operator checks |
| --- | --- | --- |
| Continuous ingestion | Ingester collects repositories, commits facts, and runs source-local projection. | Ingester `/admin/status`, fact commits, projector queue depth/oldest age, graph-write logs. |
| Reducer and shared projection | Resolution engine claims durable reducer work, admits cross-domain truth, writes shared graph/read-model state, and repairs phase publication. | Reducer `/admin/status`, reducer backlog, shared projection backlog, failure-class logs, dead letters. |
| Query reads | API reads canonical graph, content, status, and read models; MCP serves tool transport and delegates to the same query contracts. | API status, graph/content availability, reducer backlog, request scope, pagination/truncation fields. |
| Bootstrap and recovery | Bootstrap-index seeds empty or recovered environments through the facts-first data plane; replay targets durable queue rows. | Bootstrap logs, queue state, replay selectors, retry/dead-letter rows. |
| Collector workflow control | Workflow coordinator creates/reaps claims; hosted collectors emit external-source facts; webhooks create freshness triggers. | Coordinator claims, collector facts, webhook trigger rows, claim heartbeat/reap status. |

The resolution engine does not own the source-local projector queue in the
steady-state ingester path. It owns reducer domains, semantic materialization,
shared projection, retries, replay, and repair after source-local projection has
published durable work.

## Query Diagnosis

When reads look wrong, check in this order:

1. the query scope: repository, service, workload, environment, account, or
   other route-specific selector
2. source-local projection completion for the source generation
3. reducer and shared projection backlog for the relevant domain
4. graph backend or Postgres read-model rows
5. API/MCP response `count`, `limit`, `truncated`, and cursor fields

Do not diagnose missing graph truth from pod health alone. A healthy pod can
still have pending, retrying, or dead-lettered durable work.

## Runtime Shape

Use [Service Runtimes](../deployment/service-runtimes.md) as the only full
runtime matrix. The long-running platform includes API, MCP server, ingester,
workflow coordinator, webhook listener, resolution engine, and enabled hosted
collectors. Bootstrap data-plane and bootstrap-index are one-shot helper flows.

Compose, Helm, and local CLI reuse the same binaries but differ in command,
environment, process shape, volumes, ports, and health checks.

## Troubleshooting By Stage

| Symptom | Start here | Then check |
| --- | --- | --- |
| No new repository data | ingester `/admin/status` and ingester logs | repository selection, sync errors, parse failures |
| Facts written but graph state missing | projector queue metrics and ingester graph-write logs | source-local projection claims, graph backend writes, content writes |
| Shared infra or deployment traces missing | reducer shared projection backlog and logs | relationship evidence facts, reducer normalization, canonical edge writes |
| API answers stale or incomplete | API status plus reducer backlog | graph backend/content-store state, repository coverage/status |
| Replay did not recover work | recovery metrics and status | dead-letter rows, failure class, replay selector |

## Related Docs

- [System Architecture](../architecture.md)
- [Service Runtimes](../deployment/service-runtimes.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Telemetry Overview](telemetry/index.md)
- [Local Testing](local-testing.md)

Maintainer implementation details live in `go/cmd/ingester/README.md`,
`go/internal/reducer/README.md`, `go/internal/workflow/README.md`, and
`go/internal/query/README.md`.
