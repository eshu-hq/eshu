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
- `confluence-collector-credentials` when `confluenceCollector.enabled=true`

For Confluence Cloud email/API-token auth:

```bash
kubectl -n eshu create secret generic confluence-collector-credentials \
  --from-literal=email="$JIRA_EMAIL" \
  --from-literal=api-token="$JIRA_API_TOKEN"
```

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

To deploy the Confluence collector in the same release, add one crawl scope and
point it at the Secret:

```yaml
confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  spaceId: "123456789"
  spaceKey: DEV
  pollInterval: 5m
  credentials:
    secretName: confluence-collector-credentials
```

Use `rootPageId` instead of `spaceId` when you want a smaller subtree crawl.
The collector writes documentation sections to Postgres and does not write back
to Confluence.

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

# If confluenceCollector.enabled=true:
kubectl -n eshu rollout status deployment/eshu-confluence-collector
```

Exact resource names depend on the release name and chart helpers. The API and
MCP workloads expose HTTP health endpoints through chart probes. Use logs,
metrics, and runtime status surfaces to diagnose ingester or resolution-engine
progress.

## 6. Validate Confluence in EKS

After rollout, confirm the collector is healthy and that it stored page content:

```bash
kubectl -n eshu logs deployment/eshu-confluence-collector --tail=100
kubectl -n eshu port-forward deployment/eshu-confluence-collector 8080:8080
curl -fsS http://127.0.0.1:8080/readyz
```

Then query the shared Postgres content store from your normal database access
path:

```sql
select count(*)
from fact_records
where fact_kind = 'documentation_section'
  and source_system = 'confluence';
```

If the count is zero, check the configured `baseUrl`, crawl scope, Secret keys,
and outbound network access from the EKS nodes to Atlassian Cloud.
