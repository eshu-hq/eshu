# Helm Quickstart

Use Helm when you want the supported split-service Kubernetes deployment. Keep
the first install small: API, MCP, ingester, resolution engine, schema
bootstrap, Postgres, and an existing graph backend.

## 1. Create Namespace

```bash
kubectl create namespace eshu
```

## 2. Create Required Secrets

The chart defaults expect:

- `eshu-api-auth` with key `api-key`
- `eshu-neo4j` with keys `username` and `password`
- `github-app-credentials` when `repoSync.auth.method=githubApp`
- optional collector credentials only for collectors you enable

For Confluence email/API-token auth:

```bash
kubectl -n eshu create secret generic confluence-collector-credentials \
  --from-literal=email="$CONFLUENCE_EMAIL" \
  --from-literal=api-token="$CONFLUENCE_API_TOKEN"
```

## 3. Write Minimal Values

```yaml
contentStore:
  dsn: postgresql://eshu:secret@postgres.platform.svc.cluster.local:5432/eshu

neo4j:
  uri: bolt://nornicdb.platform.svc.cluster.local:7687

env:
  ESHU_GRAPH_BACKEND: nornicdb
  DEFAULT_DATABASE: nornic
  NEO4J_DATABASE: nornic

repoSync:
  source:
    mode: githubOrg
    githubOrg: eshu-hq
    rules:
      - type: exact
        value: eshu-hq/eshu
```

For bundled NornicDB, read [Storage](storage.md) first. Helm-hook schema
bootstrap cannot run before chart-managed NornicDB exists.

## 4. Render, Then Install

```bash
helm template eshu ./deploy/helm/eshu -f values.eshu.yaml

helm upgrade --install eshu ./deploy/helm/eshu \
  --namespace eshu \
  -f values.eshu.yaml
```

## 5. Check Rollout

```bash
kubectl -n eshu get pods
kubectl -n eshu rollout status deployment/eshu-api
kubectl -n eshu rollout status deployment/eshu-mcp-server
kubectl -n eshu rollout status statefulset/eshu
kubectl -n eshu rollout status deployment/eshu-resolution-engine
```

Exact resource names depend on the release name and chart helpers. Use logs,
metrics, and `/admin/status` to diagnose ingester or resolution-engine progress;
pod health alone does not prove indexing is complete.

## 6. Add Optional Runtimes

Enable collectors one family at a time. Claim-driven collectors require an
active workflow coordinator and collector instances. Provider webhooks require a
webhook route plus the matching Secret.

Use [Collector and Webhook Values](helm-collector-and-webhook-values.md) for
those values and guardrails.
