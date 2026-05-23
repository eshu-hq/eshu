# Eshu Helm Chart

This chart deploys Eshu as split Kubernetes workloads: API, MCP, ingester,
workflow coordinator, reducer, schema bootstrap, optional collectors, optional
webhook listener, and optional bundled NornicDB.

The chart source of truth is:

- `values.yaml` for defaults
- `values.schema.json` for type validation
- `templates/validate.yaml` for render-time guardrails
- `templates/` for rendered Kubernetes resources

For the operator guide, use the public docs:

- [Helm Quickstart](../../../docs/public/deploy/kubernetes/helm-quickstart.md)
- [Helm Values](../../../docs/public/deploy/kubernetes/helm-values.md)
- [Runtime values](../../../docs/public/deploy/kubernetes/helm-runtime-values.md)
- [Collector and webhook values](../../../docs/public/deploy/kubernetes/helm-collector-and-webhook-values.md)
- [Routing and storage values](../../../docs/public/deploy/kubernetes/helm-routing-and-storage-values.md)

## Render locally

Render locally whenever values or templates change:

```bash
helm template eshu ./deploy/helm/eshu
```

With overrides:

```bash
helm template eshu ./deploy/helm/eshu -f values.eshu.yaml
```

When Helm is available, lint the chart before applying or packaging:

```bash
helm lint ./deploy/helm/eshu
helm lint ./deploy/helm/eshu -f values.eshu.yaml
```

## Workload map

| Workload | Default | Purpose |
| --- | --- | --- |
| `schemaBootstrap` | enabled | Runs `eshu-bootstrap-data-plane` as one schema bootstrap Job. |
| `api` | enabled | Serves HTTP API, admin, and query surfaces. |
| `mcpServer` | enabled | Serves MCP over HTTP. |
| `ingester` | enabled | Syncs repositories, parses snapshots, and emits facts. |
| `resolutionEngine` | enabled | Drains reducer work and projects graph/content data. |
| `workflowCoordinator` | disabled | Creates and schedules durable collector claims. |
| `confluenceCollector` | disabled | Ingests Confluence documentation sections. |
| `ociRegistryCollector` | disabled | Ingests direct OCI registry image facts. |
| `terraformStateCollector` | disabled | Executes claim-driven Terraform-state collection. |
| `awsCloudCollector` | disabled | Executes claim-driven AWS cloud collection. |
| `packageRegistryCollector` | disabled | Executes claim-driven package registry collection. |
| `webhookListener` | disabled | Accepts provider webhooks and writes refresh triggers. |
| `nornicdb` | disabled | Optionally renders a bundled NornicDB graph backend. |

API and MCP pods currently start through the `eshu` CLI wrapper. The other
long-running workloads use direct `/usr/local/bin/eshu-*` service binaries.
Keep this README aligned with `templates/` and move operator setup details to
the public Helm docs.

## Guardrails

The chart fails fast for combinations that would render broken or idle
workloads:

- Ingress and Gateway API exposure cannot both be enabled.
- MCP routing requires `mcpServer.enabled=true`.
- Claim-driven collectors require an active workflow coordinator.
- The OCI registry collector requires at least one target when enabled.
- The webhook listener requires at least one enabled provider route.
- The Confluence collector requires a base URL, credentials, and exactly one
  crawl scope.
- Helm-hook schema bootstrap cannot be combined with chart-managed NornicDB.
