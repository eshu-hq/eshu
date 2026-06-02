# Helm Routing And Storage Values

Use this page for graph backend selection, Postgres wiring, API/MCP exposure,
webhook ingress, NetworkPolicy, and observability values. Storage concepts live
in [Storage](storage.md).

## Storage Values

Eshu needs Postgres plus a Bolt-compatible graph backend.

| Value | Default | Owns |
| --- | --- | --- |
| `contentStore.dsn` | empty | Renders `ESHU_CONTENT_STORE_DSN` and `ESHU_POSTGRES_DSN`. |
| `env.ESHU_GRAPH_BACKEND` | `nornicdb` | Runtime graph adapter. |
| `env.DEFAULT_DATABASE`, `env.NEO4J_DATABASE` | `nornic` | Bolt database name. |
| `neo4j.uri` | `bolt://neo4j:7687` | Bolt endpoint for NornicDB or Neo4j. |
| `neo4j.auth.secretName` | `eshu-neo4j` | Secret for Bolt username/password keys. |
| `ingester.persistence.*` | enabled, `ReadWriteOnce`, `100Gi` | Repository workspace PVC. |

For bundled no-auth NornicDB, set `neo4j.auth.secretName=""`. Eshu still renders
literal Bolt client credentials because the shared client config rejects empty
auth fields even when the backend does not enforce auth.

## Bundled NornicDB

`nornicdb.enabled=false` by default. When enabled, the chart renders one
NornicDB Deployment, Service, and optional PVC.

Key defaults: image repository `timothyswt/nornicdb-cpu-bge`, image tag
`v1.1.3@sha256:42af69852ae0f34a905a0877668025d53b3783bb864549810d868e1bf94f3752`,
persistence enabled with `500Gi`, no server auth, async writes off, Heimdall
off, Qdrant gRPC off, embeddings off, BM25 and vector indexes disabled,
BM25/vector warming set to `lazy`, search index persistence off, and
`GOMEMLIMIT=48GiB`.

The bundled NornicDB deployment is the canonical graph lane. Search index
persistence is off because BM25/vector indexing is disabled for the graph lane.
Do not enable BM25/vector indexing unless a deployment proof records startup,
memory, index-size, document-count, vector-count, and failure-mode evidence.

Do not combine `nornicdb.enabled=true` with
`schemaBootstrap.useHelmHooks=true`; the chart rejects that render because the
hook runs before bundled NornicDB exists. Use an existing NornicDB endpoint or
provide release ordering outside the hook.

## Exposure

The chart always renders the main API Service. Defaults:

- `service.type=ClusterIP`
- `service.port=8080`
- `mcpServer.enabled=true`
- `exposure.ingress.enabled=false`
- `exposure.gateway.enabled=false`
- exposure `backend=api`

Ingress and Gateway API exposure are mutually exclusive. `backend: mcp`
requires `mcpServer.enabled=true`. If API and MCP need separate public hostnames,
add a second Ingress or HTTPRoute in an overlay or GitOps layer.

Webhook ingress is separate from API/MCP exposure. It routes only enabled
provider paths to the webhook listener Service. Keep admin, health, and metrics
paths internal unless a protected operator layer owns exposure.

## NetworkPolicy And Observability

`networkPolicy.enabled=true` renders NetworkPolicies for enabled long-running
workloads and schema bootstrap. Leave it on unless another cluster policy
system owns all pod traffic.

OpenTelemetry defaults to disabled with OTLP/gRPC settings available under
`observability.otel`. Prometheus defaults to disabled at `0.0.0.0:9464` with
scrape path `/metrics`. `ServiceMonitor` resources render only when both
Prometheus and `observability.prometheus.serviceMonitor.enabled` are true.

`ServiceMonitor` covers long-running workloads: API, MCP, ingester, resolution
engine, workflow coordinator, webhook listener, and hosted collectors. Schema
bootstrap and bootstrap-index are excluded.
