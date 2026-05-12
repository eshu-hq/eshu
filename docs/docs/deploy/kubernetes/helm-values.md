# Helm values

The chart lives at `deploy/helm/eshu`.

## Values to review first

| Value | Default | Purpose |
| --- | --- | --- |
| `image.repository` | `ghcr.io/eshu-hq/eshu` | Runtime image. |
| `image.tag` | `v0.0.2` | Runtime image tag. |
| `service.type` | `ClusterIP` | API service type. |
| `api.replicas` | `1` | API replica count. |
| `mcpServer.enabled` | `true` | Deploy the MCP runtime. |
| `ingester.persistence.size` | `100Gi` | Workspace PVC size. |
| `resolutionEngine.enabled` | `true` | Deploy the reducer runtime. |
| `workflowCoordinator.enabled` | `false` | Deploy dark-mode workflow coordinator. |
| `workflowCoordinator.deploymentMode` | `dark` | Keep coordinator claim ownership dark. The chart rejects active mode in this branch. |
| `workflowCoordinator.claimsEnabled` | `false` | Keep workflow claims off in Helm. Use Compose for active proof runs. |
| `workflowCoordinator.collectorInstances` | `[]` | Declarative collector instances for dark reconciliation only. |
| `confluenceCollector.enabled` | `false` | Deploy the Confluence documentation collector. |
| `confluenceCollector.baseUrl` | empty | Atlassian wiki base URL, for example `https://example.atlassian.net/wiki`. |
| `confluenceCollector.spaceId` | empty | Confluence space ID to crawl. Set this or `rootPageId`, not both. |
| `confluenceCollector.rootPageId` | empty | Root page ID for a bounded crawl. Set this or `spaceId`, not both. |
| `confluenceCollector.credentials.secretName` | empty | Secret containing Confluence auth material. |
| `webhookListener.enabled` | `false` | Deploy the public GitHub/GitLab webhook intake runtime. |
| `webhookListener.github.enabled` | `false` | Enable the GitHub route. Requires `github.secretName`. |
| `webhookListener.gitlab.enabled` | `false` | Enable the GitLab route. Requires `gitlab.secretName`. |
| `webhookListener.exposure.ingress.enabled` | `false` | Render provider-only ingress paths for webhook delivery. |
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

## Confluence collector

The Confluence collector is off by default. When enabled, it stores
documentation sections in the configured Postgres content store and keeps the
runtime read-only against Confluence.

Use email/API-token credentials:

```yaml
confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  spaceId: "123456789"
  credentials:
    secretName: confluence-collector-credentials
    emailKey: email
    apiTokenKey: api-token
```

Or use a bearer token:

```yaml
confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  rootPageId: "987654321"
  credentials:
    secretName: confluence-collector-credentials
    bearerTokenKey: token
```

The chart rejects installs where the collector is enabled without a base URL,
credential Secret, or exactly one crawl scope.

## Webhook listener

The webhook listener is off by default. When enabled, it accepts provider
webhook deliveries, verifies provider secrets, and writes refresh triggers to
Postgres. It does not mount the repository workspace PVC or graph credentials.

```yaml
webhookListener:
  enabled: true
  github:
    enabled: true
    secretName: github-webhook-secret
  exposure:
    ingress:
      enabled: true
      hosts:
        - host: hooks.example.com
```

Only provider webhook paths are routed by the chart ingress. Set those paths
with `webhookListener.github.path` and `webhookListener.gitlab.path`; ingress
hosts only select hostnames. Runtime health, status, and metrics endpoints stay
on the internal service unless an operator adds separate protected routing.

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
