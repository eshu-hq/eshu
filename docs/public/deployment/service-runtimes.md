# Service Runtimes

Use this page when you need the operator model for Eshu:

- which long-running services exist
- what each service owns
- which command starts each service
- which service should be tuned or scaled
- where health, status, and metrics live

For installation steps, use the
[Kubernetes deployment lane](../deploy/kubernetes/index.md). For local stack
choices, use [Docker Compose](../run-locally/docker-compose.md). This page is
the runtime ownership map.

## Runtime Contract

| Runtime | Owns | Deployed command | Storage access | Kubernetes shape |
| --- | --- | --- | --- | --- |
| Schema Bootstrap | Postgres and graph schema DDL | `/usr/local/bin/eshu-bootstrap-data-plane` | Postgres DDL + graph DDL | `Job` |
| API | HTTP API, query reads, admin endpoints | `eshu api start --host 0.0.0.0 --port <port>` | graph + content reads | `Deployment` |
| MCP Server | MCP tool transport and mounted query passthrough | `eshu mcp start --transport http` | graph + content reads | optional `Deployment` |
| Ingester | repo sync, parsing, fact emission, workspace ownership | `/usr/local/bin/eshu-ingester` | workspace PVC + Postgres + graph backend | `StatefulSet` |
| Webhook Listener | Git and AWS freshness webhook intake | `/usr/local/bin/eshu-webhook-listener` | Postgres trigger tables | optional `Deployment` |
| Workflow Coordinator | collector-instance reconciliation, claim scheduling, expired-claim reaping | `/usr/local/bin/eshu-workflow-coordinator` | Postgres workflow/control tables | optional `Deployment` |
| Resolution Engine | queue draining, projection, retries, replay, recovery | `/usr/local/bin/eshu-reducer` | Postgres + graph backend | `Deployment` |
| Confluence Collector | bounded Confluence documentation reads and documentation fact emission | `/usr/local/bin/eshu-collector-confluence` | Postgres fact store | optional `Deployment` |
| OCI Registry Collector | OCI registry scan, tag observation, manifest/referrer fact emission | `/usr/local/bin/eshu-collector-oci-registry` | Postgres fact store | optional `Deployment` |
| Terraform State Collector | claim-driven Terraform-state reads and redacted fact emission | `/usr/local/bin/eshu-collector-terraform-state` | Postgres workflow + fact store | optional `Deployment` |
| AWS Cloud Collector | claim-driven AWS observation and fact emission | `/usr/local/bin/eshu-collector-aws-cloud` | Postgres workflow + fact store | optional `Deployment` |
| Package Registry Collector | claim-driven package metadata fetch and fact emission | `/usr/local/bin/eshu-collector-package-registry` | Postgres workflow + fact store | optional `Deployment` |
| Bootstrap Index | one-shot initial indexing | `/usr/local/bin/eshu-bootstrap-index` | workspace + Postgres + graph backend | one-shot local or operator helper |

Every direct service binary accepts `--version` and `-v` as a single argument.
That path prints the embedded application version and exits before telemetry,
datastore, graph, queue, or HTTP setup. Use it for image checks and support
bundles.

Most Go service binaries also honor the opt-in `ESHU_PPROF_ADDR` profiler
listener for focused CPU, heap, goroutine, and trace capture. Leave it unset in
normal deployment defaults; enable it only on the runtime that owns the slow
stage and keep the endpoint private through loopback binding, port-forwarding,
or equivalent network controls.

Deployment binaries do not embed NornicDB. Compose, Helm, and Kubernetes
connect to NornicDB or Neo4j as external Bolt-compatible graph endpoints.
Embedded NornicDB is only the local `eshu graph start` path.

## Route Map

| Need | Use |
| --- | --- |
| API, MCP, ingester, reducer, workflow-coordinator, webhook listener, local verification runtimes | [Core Runtime Services](service-runtimes-core.md) |
| Confluence, OCI registry, Terraform-state, AWS cloud, and package-registry collectors | [Collector Runtime Services](service-runtimes-collectors.md) |
| Schema bootstrap and one-shot bootstrap indexing | [Bootstrap Runtime Services](service-runtimes-bootstrap.md) |
| Concrete Compose files and local ports | [Docker Compose](../run-locally/docker-compose.md) |
| Helm values and render-time guards | [Helm Values](../deploy/kubernetes/helm-values.md) |
| Metric, trace, log, and admin/status signal names | [Telemetry Overview](../reference/telemetry/index.md) |

## Health, Status, And Completeness

Long-running runtimes that mount the shared runtime admin surface expose:

- `/healthz` for process health
- `/readyz` for readiness
- `/metrics` for runtime and backlog signals
- `/admin/status` for the shared status/report shape

The MCP HTTP runtime also exposes transport-specific health, SSE, JSON-RPC
message, and mounted API routes. Stdio MCP mode does not start an HTTP listener.

A service can be healthy while indexing is incomplete. Use the API completeness
routes before assuming a run has finished:

- `GET /api/v0/status/index`
- `GET /api/v0/index-status`
- `GET /api/v0/repositories/{repo_id}/coverage`

Code graph and dead-code query readiness also depends on shared projection
domain backlog. `/admin/status` remains `progressing` while pending shared
projection intents still need reducer-owned edges to become graph-visible.

`bootstrap-index` remains a one-shot helper for empty or recovered
environments. It does not mount `/healthz`, `/readyz`, `/metrics`, or
`/admin/status`.

## Schema Bootstrap

`eshu-bootstrap-data-plane` applies Postgres and graph schema DDL, records the
graph schema fingerprint after a successful graph apply, and exits. It writes no
application data.

Read [Bootstrap Runtime Services](service-runtimes-bootstrap.md) for the
Compose, Helm, and operator contract.

## Bootstrap Index

Bootstrap indexing is a one-shot operator activity, not a long-running
Kubernetes workload in the public Helm chart.

Use it when you need to materialize an initial repository set, reduce cold-start
time on a brand-new environment, or validate end-to-end indexing against a known
repository set. Treat repeated restarts or long-running bootstrap activity as an
incident.

## Metrics And ServiceMonitor

Compose exposes direct runtime scrape endpoints. Helm can render
`ServiceMonitor` resources for API, MCP server, ingester, workflow coordinator,
resolution engine, webhook listener, Confluence collector, OCI registry
collector, Terraform-state collector, AWS cloud collector, and package registry
collector.

`ServiceMonitor` does not apply to schema bootstrap or bootstrap-index because
they are not steady-state Kubernetes services in the public chart.

## Operator Defaults

- Treat API, MCP server, ingester, workflow coordinator, resolution engine,
  webhook listener, and collectors as separate scaling units.
- Keep the workspace mounted only on the ingester in Kubernetes.
- Use direct `/metrics` endpoints for local verification.
- Use `ServiceMonitor` only for long-running Kubernetes runtimes.
- Prefer incremental scope refresh and reconciliation over platform-wide
  re-indexing.
- Use [Telemetry Overview](../reference/telemetry/index.md) to decide which
  signal to inspect first.
- Use [Local Testing](../reference/local-testing.md) before calling a runtime
  change ready.
