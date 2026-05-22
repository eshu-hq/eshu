# Helm values

The chart lives at `deploy/helm/eshu`. Treat `deploy/helm/eshu/values.yaml`,
`deploy/helm/eshu/values.schema.json`, and `deploy/helm/eshu/templates/` as the
source of truth.

This page is the operator route map. Use it to decide which values file section
to edit, then open the focused page for the details.

## Pick the right page

| Need | Read |
| --- | --- |
| Install the split-service chart | [Helm Quickstart](helm-quickstart.md) |
| Configure graph and Postgres storage | [Storage](storage.md) and [Routing and storage values](helm-routing-and-storage-values.md) |
| Configure schema bootstrap, runtime env, reducer lanes, or repo sync | [Runtime values](helm-runtime-values.md) |
| Enable Confluence, OCI registry, Terraform-state, AWS cloud, Package Registry, or webhooks | [Collector and webhook values](helm-collector-and-webhook-values.md) |
| Expose API or MCP over Ingress, Gateway API, or LoadBalancer | [Routing and storage values](helm-routing-and-storage-values.md) |
| Prepare production settings | [Production Checklist](production-checklist.md) |

## Render before applying

Render the chart locally whenever values change:

```bash
helm template eshu ./deploy/helm/eshu
helm template eshu ./deploy/helm/eshu -f values.eshu.yaml
```

Use `helm lint` when Helm is available in the local environment:

```bash
helm lint ./deploy/helm/eshu
helm lint ./deploy/helm/eshu -f values.eshu.yaml
```

## Values to review first

| Area | Defaults to check |
| --- | --- |
| Image | `image.repository=ghcr.io/eshu-hq/eshu`, `image.tag=v0.0.2`, `image.pullPolicy=IfNotPresent`. Pin the tag per rollout. |
| Storage | `contentStore.dsn` is empty and must be supplied for real deployments. `env.ESHU_GRAPH_BACKEND=nornicdb`, `env.DEFAULT_DATABASE=nornic`, `env.NEO4J_DATABASE=nornic`, and `neo4j.uri=bolt://neo4j:7687`. |
| Schema bootstrap | `schemaBootstrap.enabled=true` and `schemaBootstrap.useHelmHooks=true` render the pre-install/pre-upgrade schema Job. |
| Core runtimes | `repoSync.enabled=true`, `repoSync.source.mode=githubOrg`, `api.replicas=1`, `mcpServer.enabled=true`, `resolutionEngine.enabled=true`, `resolutionEngine.lanes=[]`, and `ingester.persistence.size=100Gi`. |
| Optional runtimes | `workflowCoordinator`, all hosted collectors, and `webhookListener` are disabled by default. Active claim-driven collectors require the workflow coordinator. |
| Exposure and policy | `service.type=ClusterIP`, `exposure.ingress.enabled=false`, `exposure.gateway.enabled=false`, `networkPolicy.enabled=true`, and `observability.prometheus.serviceMonitor.enabled=false`. |

Each long-running workload has `resources`, `env`, and `connectionTuning`
blocks. Workload-specific `env` maps are rendered after global `env`, so a pod
can override a global value or enable a diagnostic setting such as
`ESHU_PPROF_ADDR` without turning it on everywhere.

## Render-time guardrails

The chart fails during render for invalid combinations that would otherwise
produce idle or unreachable workloads.

| Guardrail | Source |
| --- | --- |
| `exposure.ingress.enabled` and `exposure.gateway.enabled` cannot both be true. | `deploy/helm/eshu/templates/validate.yaml` |
| `backend: mcp` requires `mcpServer.enabled=true`. | `deploy/helm/eshu/templates/validate.yaml` |
| `repoSync.auth.method=ssh` cannot be used with `repoSync.source.mode=githubOrg`. | `deploy/helm/eshu/templates/validate.yaml` |
| `schemaBootstrap.useHelmHooks=true` cannot be combined with `nornicdb.enabled=true`. | `deploy/helm/eshu/templates/job-schema-bootstrap.yaml` |
| Claim-driven collectors require `workflowCoordinator.enabled=true`, `deploymentMode=active`, and `claimsEnabled=true`. | `deploy/helm/eshu/templates/validate.yaml` |
| The OCI registry collector requires at least one target when enabled. | `deploy/helm/eshu/templates/validate.yaml` and `values.schema.json` |
| The webhook listener requires at least one enabled provider route. | `deploy/helm/eshu/templates/validate.yaml` |
| The Confluence collector requires a base URL, credentials, and exactly one crawl scope. | `deploy/helm/eshu/templates/validate.yaml` |

## Workload override pattern

Global settings belong under `env`, `connectionTuning`, `podLabels`,
`podAnnotations`, `nodeSelector`, `affinity`, and `tolerations`.
Workload-specific settings belong under the workload block.

```yaml
env:
  ESHU_GRAPH_BACKEND: nornicdb
  DEFAULT_DATABASE: nornic
  NEO4J_DATABASE: nornic

api:
  replicas: 2
  env:
    GOMEMLIMIT: "1536MiB"
  resources:
    requests:
      cpu: 250m
      memory: 512Mi

resolutionEngine:
  env:
    ESHU_PPROF_ADDR: "127.0.0.1:6061"
  connectionTuning:
    neo4j:
      maxConnectionPoolSize: "150"
```

Enable `ESHU_PPROF_ADDR` only on the workload that owns the slow stage. Keep it
private through loopback binding, port-forwarding, or an equivalent protected
network path; do not turn it on globally.

## Security defaults

The chart defaults to non-root pods, read-only root filesystems, dropped Linux
capabilities, runtime-default seccomp, and
`fsGroupChangePolicy: OnRootMismatch`. Use workload-specific ServiceAccounts
only when a collector needs different cloud permissions, such as AWS IRSA for
the AWS cloud collector.

## Related references

- [Service runtimes](../../deployment/service-runtimes.md)
- [Core service runtimes](../../deployment/service-runtimes-core.md)
- [Collector service runtimes](../../deployment/service-runtimes-collectors.md)
- [Bootstrap service runtimes](../../deployment/service-runtimes-bootstrap.md)
