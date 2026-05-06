# Helm quickstart

Use Helm when you want the supported split-service Kubernetes deployment.

## 1. Create the namespace

```bash
kubectl create namespace eshu
```

## 2. Create required secrets

The defaults expect:

- `eshu-api-auth` with key `api-key`
- `eshu-neo4j` with keys `username` and `password`
- `github-app-credentials` when `repoSync.auth.method=githubApp`

## 3. Write values

Start with a small override file:

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

## 4. Install or upgrade

```bash
helm upgrade --install eshu ./deploy/helm/eshu \
  --namespace eshu \
  -f values.eshu.yaml
```

## 5. Check rollout

```bash
kubectl -n eshu get pods
kubectl -n eshu rollout status deployment/eshu
kubectl -n eshu rollout status deployment/eshu-mcp-server
kubectl -n eshu rollout status statefulset/eshu
kubectl -n eshu rollout status deployment/eshu-resolution-engine
```

Exact resource names depend on the release name and chart helpers. The API and
MCP workloads expose HTTP health endpoints through chart probes. Use logs,
metrics, and runtime status surfaces to diagnose ingester or resolution-engine
progress.
