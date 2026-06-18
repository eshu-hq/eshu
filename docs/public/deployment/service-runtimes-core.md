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
- Scale Helm ingesters with `ingester.replicas` when repository discovery and
  parsing need more throughput. The chart maps shard count from replicas and
  shard index from the StatefulSet pod ordinal on Kubernetes `1.32` or newer,
  and the ingester coordinates deferred relationship maintenance with a
  Postgres shard-drain epoch plus a shared/exclusive advisory barrier around
  commits and global maintenance.
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

Claim-driven Terraform-state, AWS cloud, package-registry, and trusted component
extension collectors require an active coordinator with at least one configured
or verified component-backed collector instance. The Helm render fails when the
charted collector contract is missing.

For component extensions, the coordinator reads `ESHU_COMPONENT_HOME` only when
deployment config sets it, applies `ESHU_COMPONENT_TRUST_MODE` and the
allow/revoke lists plus strict-mode Cosign provenance settings, and converts
verified claim-capable activations into normal `collector_instances` rows. It
then requires `ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` before planning
component-extension workflow rows; missing policy denies claims, restricted
mode needs a matching component allow rule, deny rules win, and broad mode is
the explicit opt-in.
Default Compose mounts the shared component registry path for the coordinator
but keeps trust disabled until the operator sets an allowlist or strict trust
policy. Revoked, unsigned, or incompatible components stop receiving new work
at reconciliation time. Use `eshu component list --json` with the same policy
values to inspect local policy failure reasons until the central inventory API
is enabled.

### Semantic-provider execution worker (default OFF, no live traffic)

The coordinator can also run the egress-gated semantic-provider execution
worker. It claims semantic extraction jobs and re-checks semantic egress
fail-closed before any provider call. It is **OFF by default and makes no
outbound provider traffic by default**:

- `ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED` (default `false`) turns the claim loop
  on. When unset, the worker is a no-op.
- `ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED` (default `false`) is the explicit,
  documented flag that permits real outbound provider traffic. It only takes
  effect when a concrete, security-reviewed provider client is also wired. The
  shipped build wires the no-network `DisabledSemanticProviderClient`, so even
  with this flag set, an egress-allowed claim terminates as
  `provider_execution_not_enabled` and no provider is contacted.
- `ESHU_SEMANTIC_PROVIDER_WORKER_SCOPE_IDS_JSON` is a JSON array of queue scope
  ids to drain; required when the worker is enabled.
- `ESHU_SEMANTIC_PROVIDER_WORKER_LEASE_TTL` (default `1m`),
  `ESHU_SEMANTIC_PROVIDER_WORKER_MAX_CLAIMS_PER_PASS` (default `32`), and
  `ESHU_SEMANTIC_PROVIDER_WORKER_LEASE_OWNER` tune the lease-fenced claim loop.
- The worker re-checks egress against `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`. A
  missing or non-allowlisted egress policy fails closed: the job is skipped to
  `skipped_policy` and a redacted governance audit event is recorded with no
  provider host, endpoint, URL, or credential.

Real provider traffic remains blocked on security and schema review. A concrete
network provider client is intentionally not shipped here and must arrive in a
future security-reviewed PR.

## Verification

Use focused Go tests for one service boundary. Use Compose or Helm rendering
when the proof needs deployment topology. The current gate map lives in
[Local Testing](../reference/local-testing.md).
