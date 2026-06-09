# Helm Values

Use this page to choose the right values page before editing
`deploy/helm/eshu`. The chart source of truth is `values.yaml`,
`values.schema.json`, and `templates/`.

## Page Map

| Need | Read |
| --- | --- |
| First split-service install | [Helm Quickstart](helm-quickstart.md) |
| Postgres, graph backend, PVCs, exposure, NetworkPolicy, observability | [Routing and Storage Values](helm-routing-and-storage-values.md) and [Storage](storage.md) |
| Schema bootstrap, global env, connection tuning, reducer lanes, repo sync | [Runtime Values](helm-runtime-values.md) |
| Workflow coordinator, optional collectors, webhook listener | [Collector and Webhook Values](helm-collector-and-webhook-values.md) |
| Production readiness | [Production Checklist](production-checklist.md) |

## Defaults To Review First

| Area | Default |
| --- | --- |
| Image | `image.repository=ghcr.io/eshu-hq/eshu`, `image.tag=v0.0.2`, `image.pullPolicy=IfNotPresent`. |
| Storage | `contentStore.dsn=""`, `contentStore.secretName=""`, `env.ESHU_GRAPH_BACKEND=nornicdb`, `neo4j.uri=bolt://neo4j:7687`. |
| Schema bootstrap | `schemaBootstrap.enabled=true`, `schemaBootstrap.useHelmHooks=true`. |
| Core runtimes | API, MCP, ingester, and resolution engine enabled; ingester PVC size `100Gi`. |
| Optional runtimes | Workflow coordinator, webhook listener, and hosted collectors disabled. |
| Exposure | API Service `ClusterIP`; Ingress and Gateway API disabled. |
| Observability | Prometheus and ServiceMonitor disabled. |
| Security | Non-root pods, read-only root filesystem, dropped capabilities, runtime-default seccomp. |

Pin the image tag per rollout. For production, prefer
`contentStore.secretName` plus `contentStore.dsnKey` over inline
`contentStore.dsn` so Postgres credentials stay in Kubernetes Secrets or
External Secrets. Claim-driven collectors require an active workflow
coordinator.

## Render Before Applying

```bash
helm template eshu ./deploy/helm/eshu
helm template eshu ./deploy/helm/eshu -f values.eshu.yaml
scripts/verify-hosted-security-posture.sh -f values.eshu.yaml
helm lint ./deploy/helm/eshu
helm lint ./deploy/helm/eshu -f values.eshu.yaml
```

## Guardrails

The chart fails render for combinations that would create idle or unreachable
workloads:

- Ingress and Gateway API exposure cannot both be enabled.
- `backend: mcp` requires `mcpServer.enabled=true`.
- `repoSync.auth.method=ssh` cannot be used with
  `repoSync.source.mode=githubOrg`.
- Helm-hook schema bootstrap cannot run with chart-managed NornicDB.
- Claim-driven collectors require active workflow coordination and collector
  instances.
- OCI registry, Confluence, Terraform-state, and webhook listener enablement
  require their own target, credential, or provider values.

## Override Pattern

Use global values for shared defaults and workload values for deliberate
overrides. Workload `env` renders after global `env`.

```yaml
env:
  ESHU_GRAPH_BACKEND: nornicdb
  DEFAULT_DATABASE: nornic
  NEO4J_DATABASE: nornic

api:
  replicas: 2
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

Enable `ESHU_PPROF_ADDR` only on the workload that owns the slow stage and keep
the port private.

## Related References

- [Service Runtimes](../../deployment/service-runtimes.md)
- [Core Runtime Services](../../deployment/service-runtimes-core.md)
- [Collector Runtime Services](../../deployment/service-runtimes-collectors.md)
- [Bootstrap Runtime Services](../../deployment/service-runtimes-bootstrap.md)
