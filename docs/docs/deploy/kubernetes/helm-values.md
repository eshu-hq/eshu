# Helm values

The chart lives at `deploy/helm/eshu`.

## Values to review first

| Value | Default | Purpose |
| --- | --- | --- |
| `image.repository` | `ghcr.io/eshu-hq/eshu` | Runtime image. |
| `image.tag` | `v0.0.1` | Runtime image tag. |
| `service.type` | `ClusterIP` | API service type. |
| `api.replicas` | `1` | API replica count. |
| `mcpServer.enabled` | `true` | Deploy the MCP runtime. |
| `ingester.persistence.size` | `100Gi` | Workspace PVC size. |
| `resolutionEngine.enabled` | `true` | Deploy the reducer runtime. |
| `workflowCoordinator.enabled` | `false` | Deploy dark-mode workflow coordinator. |
| `workflowCoordinator.deploymentMode` | `dark` | Keep coordinator claim ownership dark. The chart rejects active mode in this branch. |
| `workflowCoordinator.claimsEnabled` | `false` | Keep workflow claims off in Helm. Use Compose for active proof runs. |
| `workflowCoordinator.collectorInstances` | `[]` | Declarative collector instances for dark reconciliation only. |
| `contentStore.dsn` | empty | Postgres DSN. |
| `neo4j.uri` | `bolt://neo4j:7687` | Bolt URI for NornicDB or Neo4j. |
| `neo4j.auth.secretName` | `eshu-neo4j` | Secret for Bolt auth. Set to empty only for bundled NornicDB no-auth installs. |
| `neo4j.auth.username/password` | `neo4j` / `change-me` | Literal Bolt client credentials used when `neo4j.auth.secretName` is empty. |
| `env.ESHU_GRAPH_BACKEND` | `nornicdb` | Active graph adapter. |
| `observability.prometheus.serviceMonitor.enabled` | `false` | Render `ServiceMonitor` resources. |

Each runtime has `resources` and `connectionTuning` blocks. Connection tuning
supports Postgres pool settings and Bolt driver settings per workload.

The workflow coordinator chart is deliberately dark-only right now. Do not use
Helm values to promote coordinator-owned claims before the fenced claim,
fairness, Git collector, and remote full-corpus proof gates pass.

## Repository sync

`repoSync.source.rules` is rendered to `ESHU_REPOSITORY_RULES_JSON`. Use
`type: exact` or `type: regex` with a `value` field so the chart schema can
validate the file before install.

## Exposure

The default service type is `ClusterIP`. For external traffic, use one of:

- `service.type=LoadBalancer`
- `exposure.ingress.enabled=true`
- `exposure.gateway.enabled=true`

Do not enable ingress and gateway at the same time. Each ingress or gateway
block routes to one backend: `api` or `mcp`.
