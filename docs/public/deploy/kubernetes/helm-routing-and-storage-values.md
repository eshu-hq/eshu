# Helm Routing And Storage Values

Use this page as the operator map for graph backend selection, Postgres wiring,
API/MCP exposure, webhook ingress, NetworkPolicy, and observability values. The
chart source is `deploy/helm/eshu`.

## Storage contract

Eshu uses Postgres for facts, queues, content, status, and recovery data. It
uses a Bolt-compatible graph backend for graph projection and graph reads.

Defaults: `contentStore.dsn=""`, `env.ESHU_GRAPH_BACKEND=nornicdb`,
`env.DEFAULT_DATABASE=nornic`, `env.NEO4J_DATABASE=nornic`,
`neo4j.uri=bolt://neo4j:7687`, `neo4j.auth.secretName=eshu-neo4j`,
`usernameKey=username`, `passwordKey=password`, literal fallback username
`neo4j`, and literal fallback password `change-me`.

The ingester workspace PVC defaults to enabled, `ReadWriteOnce`, `100Gi`, no
existing claim, and no storage class override.

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

Defaults: `service.type=ClusterIP`, `service.port=8080`,
`service.annotations={}`, `mcpServer.enabled=true`,
`exposure.ingress.enabled=false`, `exposure.gateway.enabled=false`, and
`backend=api` for both exposure modes.

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

OpenTelemetry defaults to disabled, protocol `grpc`, insecure transport
enabled, empty endpoint, empty headers, empty resource attributes, no logs
exporter, OTLP traces and metrics exporters, and a `30` second metric export
interval. Prometheus defaults to disabled, host `0.0.0.0`, port `9464`, scrape
path `/metrics`, and disabled `ServiceMonitor` resources with `30s` interval
and `10s` scrape timeout.

`ServiceMonitor` resources are for long-running runtimes only: API, MCP,
ingester, resolution engine, workflow coordinator, webhook listener, and hosted
collectors. Schema bootstrap and bootstrap-index are excluded because they are
one-shot jobs, not steady-state scrape targets.
