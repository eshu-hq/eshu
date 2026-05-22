# Core Runtime Services

This page covers the long-running core services: API, MCP server, ingester,
workflow coordinator, webhook listener, and resolution engine. Use
[Service Runtimes](service-runtimes.md) as the high-level matrix.

## Incremental Refresh

Eshu refreshes incrementally by default:

- `ingester` reconciles changed repository scopes and generations.
- `webhook-listener` persists provider refresh triggers; it does not clone,
  parse, emit facts, or write graph truth.
- `workflow-coordinator` reconciles configured collector instances, creates
  supported collector claims when active, and reaps expired claim leases.
- `resolution-engine` drains queued follow-up work and shared corrections from
  durable state.
- `bootstrap-index` remains the one-shot escape hatch for empty environments or
  operator recovery.

Use status, queue age, generation state, and collector completeness before
choosing to restart or reindex. Full re-indexing is a recovery tool, not the
normal freshness path.

## Runtime Map

| Runtime | Owns | Does not own | Compose | Helm template | Command |
| --- | --- | --- | --- | --- | --- |
| API | HTTP query and admin requests over graph and content reads | repository sync, parsing, fact emission, queue drain | `eshu` | `deploy/helm/eshu/templates/deployment.yaml` | `eshu api start --host 0.0.0.0 --port <service.port>` |
| MCP Server | MCP HTTP/SSE or stdio transport over the query surface | repository sync, parsing, fact emission, graph writes | `mcp-server` | `deploy/helm/eshu/templates/deployment-mcp-server.yaml` | Helm: `eshu mcp start --transport http`; Compose: `/usr/local/bin/eshu-mcp-server` |
| Ingester | repository sync, workspace PVC, parsing, fact emission, projection work handoff | external cloud/registry reads, graph truth | `ingester` | `deploy/helm/eshu/templates/statefulset.yaml` | `/usr/local/bin/eshu-ingester` |
| Resolution Engine | reducer queue drain, graph/content projection, retry, replay, dead-letter, recovery | source collection, query serving | `resolution-engine` | `deploy/helm/eshu/templates/deployment-resolution-engine.yaml` | `/usr/local/bin/eshu-reducer` |
| Workflow Coordinator | collector instance reconciliation, claim creation, expired-claim reaping, run state | parsing, cloud reads, registry reads, graph truth | `workflow-coordinator` profile | `deploy/helm/eshu/templates/deployment-workflow-coordinator.yaml` | `/usr/local/bin/eshu-workflow-coordinator` |
| Webhook Listener | verified Git and AWS freshness trigger intake | cloning, parsing, fact emission, graph truth | `webhook-listener` profile | `deploy/helm/eshu/templates/deployment-webhook-listener.yaml` | `/usr/local/bin/eshu-webhook-listener` |

The ingester is the only long-running Kubernetes runtime that should mount the
workspace PVC. Scale API and MCP for request traffic. Scale the resolution
engine only when queue age rises and workers remain busy. If queue age rises
with Postgres contention, fix database pressure before adding reducer workers.

Stdio MCP mode does not expose `/healthz`, `/readyz`, `/metrics`, or
`/admin/status`.

## Resolution Engine Knobs

Important knobs:

- `ESHU_REDUCER_WORKERS`
- `ESHU_SHARED_PROJECTION_WORKERS`
- `ESHU_SHARED_PROJECTION_PARTITION_COUNT`
- `ESHU_SHARED_PROJECTION_BATCH_LIMIT`
- `ESHU_SHARED_PROJECTION_POLL_INTERVAL`
- `ESHU_SHARED_PROJECTION_LEASE_TTL`
- `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT`
- `ESHU_CODE_CALL_EDGE_BATCH_SIZE`

Helm can deploy domain-specific reducer lanes with `resolutionEngine.lanes`.
When lanes are set, the chart renders one `Deployment` per lane and passes
`ESHU_REDUCER_CLAIM_DOMAINS` to restrict queue claims to that lane's allowlist.
Size replicas, workers, and Postgres pools per lane.

The coordinator ships dark by default:

- `workflowCoordinator.enabled=false`
- `workflowCoordinator.deploymentMode=dark`
- `workflowCoordinator.claimsEnabled=false`
- `workflowCoordinator.collectorInstances=[]`

Claim-driven collector deployments require an active coordinator:
`workflowCoordinator.enabled=true`,
`workflowCoordinator.deploymentMode=active`,
`workflowCoordinator.claimsEnabled=true`, and at least one collector instance.

Only provider webhook paths should be publicly routed. Admin and metrics paths
should stay internal unless the operator explicitly protects them.

## Local Verification

Use focused `go test` or direct command runs for one service boundary. Use
Compose when the proof needs the same topology operators run. The current gate
map lives in [Local Testing](../reference/local-testing.md) and
[Verification Gates](../reference/local-testing/verification-gates.md).
