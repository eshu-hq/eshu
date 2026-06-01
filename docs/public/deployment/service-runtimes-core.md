# Core Runtime Services

Use this page for the long-running core services: API, MCP server, ingester,
workflow coordinator, webhook listener, and resolution engine. Use
[Service Runtimes](service-runtimes.md) for the short operator map.

## Incremental Refresh

Normal freshness is incremental:

- The ingester syncs repositories, parses content, emits facts, and hands off
  projection work.
- The webhook listener persists verified Git provider, AWS, PagerDuty, and Jira
  refresh triggers. It does not clone, parse, emit facts, or write graph truth.
- The workflow coordinator reconciles collector instances, creates supported
  claims when active, hands off authorized freshness triggers, and reaps
  expired leases.
- The resolution engine drains durable reducer and shared-projection work.
- Bootstrap indexing remains a one-shot empty-environment or recovery tool.

Check status, queue age, generation readiness, and collector completeness
before restarting services or reindexing.

## Core Runtime Map

| Runtime | Command | Owns | Helm template |
| --- | --- | --- | --- |
| API | `eshu api start --host 0.0.0.0 --port <service.port>` | HTTP query and admin reads. | `deployment.yaml` |
| MCP Server | Helm: `eshu mcp start --transport http`; Compose: `/usr/local/bin/eshu-mcp-server` | MCP transport over the query surface. | `deployment-mcp-server.yaml` |
| Ingester | `/usr/local/bin/eshu-ingester` | Repository workspace, sync, parsing, fact emission. | `statefulset.yaml` |
| Resolution Engine | `/usr/local/bin/eshu-reducer` | Reducer queue, projection, replay, retry, recovery. | `deployment-resolution-engine.yaml` |
| Workflow Coordinator | `/usr/local/bin/eshu-workflow-coordinator` | Collector instance reconciliation and claim lifecycle. | `deployment-workflow-coordinator.yaml` |
| Webhook Listener | `/usr/local/bin/eshu-webhook-listener` | Verified Git, AWS, PagerDuty, and Jira webhook intake. | `deployment-webhook-listener.yaml` |

The ingester is the only long-running Kubernetes runtime that should mount the
workspace PVC. Stdio MCP mode does not expose the HTTP admin surface.

## Scale And Tune

- Scale API and MCP for request traffic.
- Scale resolution-engine workers or lanes only when reducer telemetry shows
  queue age rising while workers are busy.
- Fix Postgres contention before adding reducer replicas.
- Keep `ESHU_REDUCER_CLAIM_DOMAIN(S)` out of global Helm `env` when
  `resolutionEngine.lanes` is configured; each lane owns its allowlist.
- Route only configured provider webhook paths publicly. Keep admin and metrics
  endpoints internal unless protected by an operator-owned layer.

The reducer-specific runtime contract lives in
[Resolution Engine](../services/resolution-engine.md). Helm lane and env
values live in [Runtime Values](../deploy/kubernetes/helm-runtime-values.md).

## Collector Claims

The coordinator ships dark by default:

- `workflowCoordinator.enabled=false`
- `workflowCoordinator.deploymentMode=dark`
- `workflowCoordinator.claimsEnabled=false`
- `workflowCoordinator.collectorInstances=[]`

Claim-driven Terraform-state, AWS cloud, and package-registry collectors require
an active coordinator with at least one configured collector instance. The Helm
render fails when that contract is missing.

## Verification

Use focused Go tests for one service boundary. Use Compose or Helm rendering
when the proof needs deployment topology. The current gate map lives in
[Local Testing](../reference/local-testing.md).
