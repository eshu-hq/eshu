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
`v1.1.6@sha256:e448ccf5cd1c1ff994c6316a1a2c5b06b19b4a3c6545660fa04f43c457625692`,
persistence enabled with `500Gi`, no server auth, async writes off, Heimdall
off, Qdrant gRPC off, embeddings off, BM25 and vector indexes disabled,
BM25/vector warming set to `lazy`, search index persistence off, and
`GOMEMLIMIT=48GiB`. The chart preserves the image entrypoint and sets
`NORNICDB_ADDRESS` from `nornicdb.bindAddress`; the default `0.0.0.0` makes the
charted HTTP and Bolt ports reachable through the ClusterIP Service.

No-Regression Evidence: Helm render proof on Kubernetes 1.32 showed the
bundled NornicDB Deployment preserves the pinned image entrypoint, sets
`NORNICDB_ADDRESS=0.0.0.0`, and exposes the charted HTTP and Bolt ports through
the Service. A Linux amd64 Docker proof with the same pinned backend image and
entrypoint-preserving environment reached HTTP health and accepted a Bolt TCP
connection through published ports. This changes only the Kubernetes startup
contract for the bundled graph backend; it does not change Eshu queue workers,
graph query text, reducer batching, or API/MCP read paths.

No-Observability-Change: the chart keeps the existing NornicDB HTTP health
probes, named `http` and `bolt` container ports, and the existing Service
targetPorts. Operators still diagnose this path through the same pod readiness,
container logs, Service endpoints, and graph-backed Eshu readiness checks.

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

`networkPolicy.egress.mode=broad` is the compatibility default and renders
unrestricted outbound egress. It is explicit because hosted operators should
treat it as a governance risk, not as least-privilege proof. Set
`networkPolicy.egress.mode=restricted` for hosted deployments that enforce
Kubernetes NetworkPolicy.

Restricted egress always renders DNS rules unless `networkPolicy.egress.dns` is
disabled, and it adds only the configured datastore, graph, internal-service,
API, MCP, collector-provider, semantic-provider, and extension destinations.
Policies are additive in Kubernetes: do not combine restricted values with
another policy that grants unrestricted egress to the same pods.

Example restricted shape:

```yaml
networkPolicy:
  enabled: true
  egress:
    mode: restricted
    datastores:
      to:
        - podSelector:
            matchLabels:
              app.kubernetes.io/component: postgres
    graph:
      to:
        - podSelector:
            matchLabels:
              app.kubernetes.io/component: nornicdb
    classes:
      collectorProviders:
        to:
          - namespaceSelector:
              matchLabels:
                egress.eshu.io/class: collector-provider
      semanticProviders:
        to:
          - namespaceSelector:
              matchLabels:
                egress.eshu.io/class: semantic-provider
      extensions:
        to:
          - namespaceSelector:
              matchLabels:
                egress.eshu.io/class: extension
```

The example uses label selectors only. Keep concrete provider, gateway, and
database destination details in private operator values.

Restricted egress fails closed. A provider class with no `to` destinations
renders no outbound rule for that class, so missing policy denies provider
egress by default. Setting a class `enabled: false` suppresses its rule even
when destinations are still configured, so denying a provider or revoking an
extension wins over a stale destination. `scripts/verify-hosted-network-policy-egress.sh`
proves all of these cases against the rendered chart: allowed provider, denied
provider, missing policy, broad egress opt-in, and extension revocation;
`scripts/test-verify-hosted-network-policy-egress.sh` self-tests the verifier.

OpenTelemetry defaults to disabled with OTLP/gRPC settings available under
`observability.otel`. Prometheus defaults to disabled at `0.0.0.0:9464` with
scrape path `/metrics`. `ServiceMonitor` resources render only when both
Prometheus and `observability.prometheus.serviceMonitor.enabled` are true.

`ServiceMonitor` covers long-running workloads: API, MCP, ingester, resolution
engine, workflow coordinator, webhook listener, and hosted collectors. Schema
bootstrap and bootstrap-index are excluded.
