# Service Runtimes

Use this page when you need the deployment ownership map: which Eshu runtimes
exist, which service owns a problem, and which focused page has the detail.

For Kubernetes install steps, use
[Helm Quickstart](../deploy/kubernetes/helm-quickstart.md). For local stack
choices, use [Docker Compose](../run-locally/docker-compose.md).

## Runtime Map

| Runtime | Owns | Kubernetes shape | Detail |
| --- | --- | --- | --- |
| Schema Bootstrap | Postgres and graph schema DDL only. | `Job` | [Bootstrap runtimes](service-runtimes-bootstrap.md) |
| API | HTTP API, query reads, and admin endpoints. | `Deployment` | [Core runtimes](service-runtimes-core.md) |
| MCP Server | MCP HTTP/SSE or stdio transport over the query surface. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Ingester | Repository sync, workspace PVC, parsing, fact emission. | `StatefulSet` | [Core runtimes](service-runtimes-core.md) |
| Webhook Listener | Verified Git and AWS freshness trigger intake. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Workflow Coordinator | Collector instance reconciliation, claim creation, expired-claim reaping. | optional `Deployment` | [Core runtimes](service-runtimes-core.md) |
| Resolution Engine | Durable queue drain, projection, retry, replay, recovery, and bounded superseded-generation cleanup. | `Deployment` | [Resolution engine](../services/resolution-engine.md) |
| Hosted Collectors | Confluence, OCI registry, Terraform-state, AWS cloud, package-registry, SBOM, attestation, provider security-alert, PagerDuty incident-context, and Jira work-item fact intake. | optional `Deployment` | [Collector runtimes](service-runtimes-collectors.md) |
| Scanner Worker | Claim-driven CPU-heavy and memory-heavy security analyzers that emit source facts only. | optional `Deployment` | [Security Intelligence](../reference/security-intelligence.md#scanner-worker-boundary) |
| Bootstrap Index | One-shot initial indexing. | operator-run helper, not chart steady state | [Bootstrap runtimes](service-runtimes-bootstrap.md) |

The direct service binaries are the release artifacts and support `--version`
checks. Helm starts API and MCP through the `eshu` CLI wrapper; most other
workloads use direct `/usr/local/bin/eshu-*` binaries.

Deployment binaries connect to external Bolt-compatible graph endpoints.
Embedded NornicDB is only the local `eshu graph start` path.

## Health Versus Completeness

Long-running HTTP runtimes expose the shared admin surface:

- `/healthz` for process health
- `/readyz` for dependency readiness
- `/metrics` for Prometheus signals
- `/admin/status` for runtime backlog, generation, and failure state
- `/admin/replay`, `/admin/refinalize`, and
  `/admin/replay-collector-generations` only on runtimes configured with the
  recovery handler

A green pod is not proof that indexing finished. Use the completeness routes
before treating a graph as current:

- `GET /api/v0/status/index`
- `GET /api/v0/index-status`
- `GET /api/v0/repositories/{repo_id}/coverage`

`bootstrap-index` is one-shot and does not mount the shared HTTP admin surface.

## Operational Defaults

- Keep the workspace PVC on the ingester only.
- Scale API and MCP for read traffic.
- Scale the resolution engine only after queue and Postgres telemetry show the
  reducer is the bottleneck.
- Use `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING` only for bounded
  resolution-engine proof runs that need repo-dependency retract substatement
  attribution; leave it disabled for normal grouped graph writes.
- Keep claim-driven collectors behind an active workflow coordinator.
- Keep scanner workers in their own Deployment or Compose service; do not move
  image unpacking, SBOM generation, source scanning, secret scanning, license
  scanning, OS package extraction, or misconfiguration analysis into the
  default reducer lane.
- Use `ServiceMonitor` only for long-running Kubernetes runtimes; schema
  bootstrap and bootstrap-index are excluded.
- Enable `ESHU_PPROF_ADDR` only on the runtime that owns the slow stage and keep
  it private.
- Keep `ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED` at its default (`true`)
  so the symbol→runtime edges (`Function-[:HANDLES_ROUTE]->Endpoint` and
  `Function-[:RUNS_IN]->Workload`) cannot drop on a cold first generation; it is
  independent of the secrets/IAM projection flag. See
  [Resolution engine](../services/resolution-engine.md) for the kill switch.

## Route Map

| Need | Use |
| --- | --- |
| Commands, templates, and scaling notes for core services | [Core Runtime Services](service-runtimes-core.md) |
| Collector control-plane and hosted collector matrix | [Collector Runtime Services](service-runtimes-collectors.md) |
| Schema bootstrap and bootstrap-index contract | [Bootstrap Runtime Services](service-runtimes-bootstrap.md) |
| Helm values and render-time guards | [Helm Values](../deploy/kubernetes/helm-values.md) |
| Metrics, traces, logs, and status signal names | [Telemetry Overview](../reference/telemetry/index.md) |
