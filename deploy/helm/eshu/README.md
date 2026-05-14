# Eshu Helm Chart

This chart deploys Eshu as separate API, MCP, ingester,
workflow-coordinator, resolution-engine, optional documentation collector,
optional OCI registry collector, and optional claim-driven collector workloads
with:

- External Bolt-compatible graph backend and Postgres connectivity
- NornicDB as the default graph adapter through the Bolt-compatible graph
  connection values; set `env.ESHU_GRAPH_BACKEND=neo4j` for the explicit Neo4j
  compatibility path
- A short-lived `eshu-bootstrap-data-plane` init container on every database-backed workload
- A stateless API `Deployment` for HTTP API
- A stateless MCP `Deployment` for MCP transport and mounted query routes
- A stateful repository ingester `StatefulSet` for repo sync and indexing
- An optional workflow-coordinator `Deployment` for dark-mode control-plane validation
- A stateless Resolution Engine `Deployment` for facts queue projection
- An optional Confluence collector `Deployment` that stores documentation sections in Postgres
- An optional OCI registry collector `Deployment` that stores digest-addressed image facts in Postgres
- Optional Terraform-state, AWS cloud, and Package Registry collector `Deployment`s that claim durable workflow work and emit facts to Postgres
- An optional public webhook listener `Deployment` that stores GitHub/GitLab/Bitbucket refresh triggers in Postgres
- Optional Prometheus scrape endpoints and `ServiceMonitor` resources for API, MCP, ingester, workflow-coordinator, resolution-engine, Confluence collector, OCI registry collector, Terraform-state collector, AWS cloud collector, Package Registry collector, and webhook listener
- Flexible service exposure (ClusterIP, LoadBalancer, Ingress, Gateway API)
- Hardened defaults such as public API docs disabled unless explicitly re-enabled

Important routing notes:

- `mcpServer.enabled=true` only makes the MCP runtime externally reachable when
  you also route ingress or gateway traffic to `backend: mcp`.
- Each `exposure.ingress` or `exposure.gateway` block targets exactly one
  backend at a time: `api` or `mcp`.
- If you want separate public API and MCP hostnames, add an additional
  Ingress or HTTPRoute from your overlay or GitOps layer.
- The webhook listener has its own ingress block. The chart routes only
  provider webhook paths there; admin and metrics paths remain internal by
  default.
- For bundled NornicDB, set `neo4j.auth.secretName=""`. The chart then renders
  literal Bolt client credentials from `neo4j.auth.username/password` because
  Eshu requires non-empty Bolt auth fields even when NornicDB itself runs
  without auth.

## Render locally

```bash
helm template eshu ./deploy/helm/eshu
```

## Typical value overrides

```yaml
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu

apiAuth:
  secretName: eshu-api-auth
  key: api-key

api:
  replicas: 2
  resources:
    requests:
      cpu: 250m
      memory: 512Mi

ingester:
  resources:
    requests:
      cpu: 500m
      memory: 1Gi
  persistence:
    size: 20Gi
  connectionTuning:
    postgres:
      maxOpenConns: "40"
      pingTimeout: 15s
    neo4j:
      connectionAcquisitionTimeout: 20s

resolutionEngine:
  connectionTuning:
    neo4j:
      maxConnectionPoolSize: "150"

confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  spaceId: "123456789"
  credentials:
    secretName: confluence-collector-credentials

ociRegistryCollector:
  enabled: true
  instanceId: oci-registry-primary
  aws:
    region: us-east-1
  targets:
    - provider: ecr
      registry_id: "123456789012"
      region: us-east-1
      repository: team/api
      references: ["latest"]
    - provider: dockerhub
      repository: library/busybox
      references: ["latest"]

webhookListener:
  enabled: true
  github:
    enabled: true
    secretName: github-webhook-secret
  bitbucket:
    enabled: true
    secretName: bitbucket-webhook-secret
  exposure:
    ingress:
      enabled: true
      hosts:
        - host: hooks.example.com

repoSync:
  source:
    rules:
      - exact: myorg/my-repo
      - regex: myorg/platform-.*

env:
  ESHU_GRAPH_BACKEND: nornicdb
  DEFAULT_DATABASE: nornic
  NEO4J_DATABASE: nornic
  ESHU_ENABLE_PUBLIC_DOCS: "true"

observability:
  environment: dev
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

See [Helm Quickstart](../../../docs/docs/deploy/kubernetes/helm-quickstart.md)
and [Helm Values](../../../docs/docs/deploy/kubernetes/helm-values.md) for the
deployment guide.
