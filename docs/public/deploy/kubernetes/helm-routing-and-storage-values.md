# Helm Routing And Storage Values

Use this page as the operator map for graph backend selection, Postgres wiring,
API/MCP exposure, webhook ingress, NetworkPolicy, and observability values. The
chart source is `deploy/helm/eshu`.

## Storage contract

Eshu uses Postgres for facts, queues, content, status, and recovery data. It
uses a Bolt-compatible graph backend for graph projection and graph reads.

| Value | Default | Operator note |
| --- | --- | --- |
| `contentStore.dsn` | empty | Postgres DSN. Required for real deployments. |
| `env.ESHU_GRAPH_BACKEND` | `nornicdb` | Active graph adapter. |
| `env.DEFAULT_DATABASE` | `nornic` | Database name passed to the graph client. |
| `env.NEO4J_DATABASE` | `nornic` | Neo4j-driver database name. |
| `neo4j.uri` | `bolt://neo4j:7687` | Bolt URI for NornicDB or Neo4j. |
| `neo4j.auth.secretName` | `eshu-neo4j` | Secret containing Bolt auth. |
| `neo4j.auth.usernameKey` | `username` | Secret key for the Bolt username. |
| `neo4j.auth.passwordKey` | `password` | Secret key for the Bolt password. |
| `neo4j.auth.username` | `neo4j` | Literal username used only when `secretName` is empty. |
| `neo4j.auth.password` | `change-me` | Literal password used only when `secretName` is empty. |
| `ingester.persistence.enabled` | `true` | Renders the repository workspace PVC. |
| `ingester.persistence.existingClaim` | empty | Uses an existing workspace PVC. |
| `ingester.persistence.accessModes` | `ReadWriteOnce` | Workspace PVC access mode. |
| `ingester.persistence.size` | `100Gi` | Workspace PVC size. |
| `ingester.persistence.storageClass` | empty | Optional storage class. |

For bundled no-auth NornicDB, set `neo4j.auth.secretName=""`. Eshu still
renders non-empty Bolt client credentials because the shared client config
rejects empty auth fields even when the backend does not enforce auth.

## Bundled NornicDB

`nornicdb.enabled=false` by default. When enabled, the chart renders one
NornicDB Deployment, Service, and optional PVC.

| Value | Default | Operator note |
| --- | --- | --- |
| `nornicdb.enabled` | `false` | Renders bundled NornicDB. |
| `nornicdb.image.repository` | `timothyswt/nornicdb-amd64-cpu` | Backend image repository. |
| `nornicdb.image.tag` | `v1.1.0` | Backend image tag. |
| `nornicdb.image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `nornicdb.persistence.enabled` | `true` | Renders the graph PVC. |
| `nornicdb.persistence.size` | `500Gi` | Graph PVC size. |
| `nornicdb.persistence.storageClass` | empty | Optional storage class. |
| `nornicdb.env.NORNICDB_NO_AUTH` | `"true"` | Runs NornicDB without server auth. |
| `nornicdb.env.NORNICDB_ASYNC_WRITES_ENABLED` | `"false"` | Keeps async writes disabled for Eshu graph writes. |
| `nornicdb.env.NORNICDB_HEIMDALL_ENABLED` | `"false"` | Keeps Heimdall disabled for Eshu indexing. |
| `nornicdb.env.NORNICDB_QDRANT_GRPC_ENABLED` | `"false"` | Keeps Qdrant gRPC disabled. |
| `nornicdb.env.NORNICDB_PERSIST_SEARCH_INDEXES` | `"true"` | Persists search indexes across restarts. |
| `nornicdb.env.NORNICDB_EMBEDDING_ENABLED` | `"false"` | Keeps embedding workers off during indexing. |
| `nornicdb.env.GOMEMLIMIT` | `"48GiB"` | Go memory-limit hint for the bundled backend. |

Do not combine `nornicdb.enabled=true` with
`schemaBootstrap.useHelmHooks=true`; the chart rejects that render because the
hook would run before bundled NornicDB exists.

When using bundled NornicDB, also point `neo4j.uri` at the release's NornicDB
Service, use literal Bolt client credentials, and set
`schemaBootstrap.useHelmHooks=false`.

## API and MCP exposure

The chart always renders the main API Service. The default Service type is
`ClusterIP`.

| Value | Default | Operator note |
| --- | --- | --- |
| `service.type` | `ClusterIP` | Main Service type: `ClusterIP`, `LoadBalancer`, or `NodePort`. |
| `service.port` | `8080` | Main Service port. |
| `service.annotations` | `{}` | Main Service annotations. |
| `mcpServer.enabled` | `true` | Renders the MCP server Deployment and Service. |
| `exposure.ingress.enabled` | `false` | Renders one Ingress. |
| `exposure.ingress.backend` | `api` | Routes Ingress to `api` or `mcp`. |
| `exposure.ingress.hosts` | `[]` | Host and path list. |
| `exposure.ingress.tls` | `[]` | Optional Ingress TLS entries. |
| `exposure.gateway.enabled` | `false` | Renders one Gateway API `HTTPRoute`. |
| `exposure.gateway.backend` | `api` | Routes HTTPRoute to `api` or `mcp`. |
| `exposure.gateway.hostnames` | `[]` | Optional HTTPRoute hostnames. |
| `exposure.gateway.parentRefs` | `[]` | Gateway parent refs. |

The chart rejects `exposure.ingress.enabled=true` with
`exposure.gateway.enabled=true`, and it rejects `backend: mcp` when
`mcpServer.enabled=false`. If API and MCP need separate public hostnames, add a
second Ingress or HTTPRoute in an overlay or GitOps layer.

## Webhook ingress

Webhook listener ingress is separate from API/MCP exposure. It renders one
Ingress for the webhook listener Service and adds only enabled provider paths.
The defaults are exact paths for GitHub, GitLab, Bitbucket, and AWS freshness;
each provider path is configurable under `webhookListener.<provider>.path`.

Admin, health, and metrics endpoints stay on the internal webhook listener
Service unless an operator adds separate protected routing.

## NetworkPolicy

`networkPolicy.enabled=true` by default. The chart renders NetworkPolicies for
long-running workloads and for schema bootstrap when those resources are
enabled. The policies allow egress and expose only each workload's HTTP or
metrics ports on ingress. Keep the default on unless another cluster policy
system owns all pod traffic.

## Observability

The chart can render OpenTelemetry env vars, Prometheus runtime ports, metrics
Services, and optional `ServiceMonitor` resources.

| Value | Default | Operator note |
| --- | --- | --- |
| `observability.environment` | empty | Optional deployment environment attribute. |
| `observability.otel.enabled` | `false` | Renders OTEL env vars. |
| `observability.otel.endpoint` | empty | OTLP collector endpoint. |
| `observability.otel.protocol` | `grpc` | `grpc` or `http/protobuf`. |
| `observability.otel.insecure` | `true` | OTLP insecure flag. |
| `observability.otel.headers` | `{}` | Rendered as `OTEL_EXPORTER_OTLP_HEADERS`. |
| `observability.otel.resourceAttributes` | `{}` | Rendered as `OTEL_RESOURCE_ATTRIBUTES`. |
| `observability.otel.excludedUrls` | health and docs paths | FastAPI excluded URL list. |
| `observability.otel.metricExportIntervalSeconds` | `30` | Rendered in milliseconds. |
| `observability.prometheus.enabled` | `false` | Enables metrics env vars, ports, and Services. |
| `observability.prometheus.host` | `0.0.0.0` | Metrics bind host. |
| `observability.prometheus.port` | `9464` | Metrics port. |
| `observability.prometheus.path` | `/metrics` | ServiceMonitor scrape path. |
| `observability.prometheus.serviceMonitor.enabled` | `false` | Renders `ServiceMonitor` resources. |
| `observability.prometheus.serviceMonitor.interval` | `30s` | Scrape interval. |
| `observability.prometheus.serviceMonitor.scrapeTimeout` | `10s` | Scrape timeout. |
| `observability.prometheus.serviceMonitor.labels` | `{}` | Extra ServiceMonitor labels. |
