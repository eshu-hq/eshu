# Helm routing and storage values

Use this page for graph backend selection, Postgres wiring, API/MCP exposure,
NetworkPolicy, and observability values. The chart source is `deploy/helm/eshu`.

## Storage contract

Eshu uses Postgres for facts, queues, content, status, and recovery data. It
uses a Bolt-compatible graph backend for graph projection and graph reads.

| Value | Default | Purpose |
| --- | --- | --- |
| `contentStore.dsn` | empty | Postgres DSN. Required for real deployments. |
| `env.ESHU_GRAPH_BACKEND` | `nornicdb` | Active graph adapter. |
| `neo4j.uri` | `bolt://neo4j:7687` | Bolt URI for NornicDB or Neo4j. |
| `neo4j.auth.secretName` | `eshu-neo4j` | Secret for Bolt auth. |
| `neo4j.auth.usernameKey` | `username` | Secret key for Bolt username. |
| `neo4j.auth.passwordKey` | `password` | Secret key for Bolt password. |
| `neo4j.auth.username` / `neo4j.auth.password` | `neo4j` / `change-me` | Literal Bolt credentials used only when `neo4j.auth.secretName` is empty. |
| `ingester.persistence.enabled` | `true` | Render the repository workspace PVC. |
| `ingester.persistence.size` | `100Gi` | Workspace PVC size. |

For bundled no-auth NornicDB, set `neo4j.auth.secretName=""`. The chart then
renders literal Bolt client credentials from `neo4j.auth.username/password`
because Eshu requires non-empty Bolt auth fields even when NornicDB itself runs
without auth.

```yaml
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu

env:
  ESHU_GRAPH_BACKEND: nornicdb
  DEFAULT_DATABASE: nornic
  NEO4J_DATABASE: nornic

neo4j:
  uri: bolt://nornicdb:7687
  auth:
    secretName: ""
    username: neo4j
    password: change-me
```

## Bundled NornicDB

`nornicdb.enabled=false` by default. When enabled, the chart renders one
NornicDB `Deployment`, `Service`, and optional PVC.

| Value | Default | Purpose |
| --- | --- | --- |
| `nornicdb.enabled` | `false` | Render bundled NornicDB. |
| `nornicdb.image.repository` | `timothyswt/nornicdb-amd64-cpu` | NornicDB image repository. |
| `nornicdb.image.tag` | `v1.1.0` | NornicDB image tag. |
| `nornicdb.persistence.enabled` | `true` | Render graph PVC. |
| `nornicdb.persistence.size` | `500Gi` | Graph PVC size. |
| `nornicdb.env.NORNICDB_NO_AUTH` | `true` | Run NornicDB without server auth. |
| `nornicdb.env.NORNICDB_ASYNC_WRITES_ENABLED` | `false` | Keep async writes disabled for Eshu graph writes. |
| `nornicdb.env.NORNICDB_HEIMDALL_ENABLED` | `false` | Keep Heimdall disabled for Eshu indexing. |
| `nornicdb.env.NORNICDB_QDRANT_GRPC_ENABLED` | `false` | Keep Qdrant gRPC disabled. |
| `nornicdb.env.NORNICDB_PERSIST_SEARCH_INDEXES` | `true` | Persist search indexes across restarts. |
| `nornicdb.env.NORNICDB_EMBEDDING_ENABLED` | `false` | Keep embedding workers off during Eshu indexing. |
| `nornicdb.env.GOMEMLIMIT` | `48GiB` | Memory limit hint for the bundled backend. |

Do not combine `nornicdb.enabled=true` with
`schemaBootstrap.useHelmHooks=true`. Helm pre-install hooks run before bundled
NornicDB resources exist. Deploy NornicDB separately first, or set
`schemaBootstrap.useHelmHooks=false` and provide an external ordering mechanism.

## API and MCP routing

The chart always renders the main Service. The default Service type is
`ClusterIP`.

| Value | Default | Purpose |
| --- | --- | --- |
| `service.type` | `ClusterIP` | Main Service type: `ClusterIP`, `LoadBalancer`, or `NodePort`. |
| `service.port` | `8080` | Main Service port. |
| `exposure.ingress.enabled` | `false` | Render an Ingress. |
| `exposure.ingress.backend` | `api` | Route Ingress to `api` or `mcp`. |
| `exposure.gateway.enabled` | `false` | Render a Gateway API `HTTPRoute`. |
| `exposure.gateway.backend` | `api` | Route HTTPRoute to `api` or `mcp`. |

Use exactly one external routing mode at a time:

```yaml
exposure:
  ingress:
    enabled: true
    backend: api
    className: nginx
    hosts:
      - host: api.example.com
        paths:
          - path: /
            pathType: Prefix
```

Or route through Gateway API:

```yaml
exposure:
  gateway:
    enabled: true
    backend: mcp
    hostnames:
      - mcp.example.com
    parentRefs:
      - name: shared-gateway
        namespace: gateway-system
```

The chart rejects these routing mistakes during render:

- `exposure.ingress.enabled=true` and `exposure.gateway.enabled=true` together.
- `backend: mcp` while `mcpServer.enabled=false`.

`mcpServer.enabled=true` only deploys the MCP runtime. It does not make MCP
public unless `service.type=LoadBalancer` points at MCP through an overlay, or
Ingress/Gateway values route to `backend: mcp`.

If API and MCP need separate public hostnames, add a second Ingress or
HTTPRoute from an overlay or GitOps layer. The chart's built-in `exposure`
block targets one backend per release.

## Webhook ingress

Webhook listener ingress is separate from API/MCP exposure. It renders only
provider webhook paths for the webhook listener Service.

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

Admin, health, and metrics endpoints remain on the internal webhook listener
Service unless an operator adds separate protected routing.

## NetworkPolicy

`networkPolicy.enabled=true` by default. The chart renders NetworkPolicies for
long-running workloads and for schema bootstrap when bootstrap is enabled. Keep
this default on unless the cluster has a separate policy system that owns all
pod traffic.

## Observability

The chart can render OpenTelemetry environment variables, Prometheus runtime
ports, metrics Services, and optional `ServiceMonitor` resources.

| Value | Default | Purpose |
| --- | --- | --- |
| `observability.environment` | empty | Optional deployment environment attribute. |
| `observability.otel.enabled` | `false` | Render OTEL env vars. |
| `observability.otel.endpoint` | empty | OTLP collector endpoint. |
| `observability.otel.protocol` | `grpc` | OTLP protocol: `grpc` or `http/protobuf`. |
| `observability.prometheus.enabled` | `false` | Enable runtime Prometheus endpoint env vars and ports. |
| `observability.prometheus.port` | `9464` | Metrics port. |
| `observability.prometheus.serviceMonitor.enabled` | `false` | Render `ServiceMonitor` resources. |

```yaml
observability:
  environment: prod
  otel:
    enabled: true
    endpoint: http://otel-collector.monitoring.svc.cluster.local:4317
    protocol: grpc
    insecure: true
    tracesExporter: otlp
    metricsExporter: otlp
    logsExporter: none
  prometheus:
    enabled: true
    serviceMonitor:
      enabled: true
```
