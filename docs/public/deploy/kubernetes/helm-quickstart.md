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
- `eshu-postgres` with key `dsn`
- `eshu-neo4j` with keys `username` and `password`
- `github-app-credentials` when `repoSync.auth.method=githubApp`
- optional collector credentials only for collectors you enable

For a direct Kubernetes Secret setup:

```bash
kubectl -n eshu create secret generic eshu-api-auth \
  --from-literal=api-key="$ESHU_API_KEY"

kubectl -n eshu create secret generic eshu-postgres \
  --from-literal=dsn="$ESHU_POSTGRES_DSN"

kubectl -n eshu create secret generic eshu-neo4j \
  --from-literal=username="$NORNICDB_USERNAME" \
  --from-literal=password="$NORNICDB_PASSWORD"

kubectl -n eshu create secret generic github-app-credentials \
  --from-literal=app-id="$GITHUB_APP_ID" \
  --from-literal=installation-id="$GITHUB_INSTALLATION_ID" \
  --from-file=private-key="$GITHUB_APP_PRIVATE_KEY_FILE"
```

For Confluence email/API-token auth:

```bash
kubectl -n eshu create secret generic confluence-collector-credentials \
  --from-literal=email="$CONFLUENCE_EMAIL" \
  --from-literal=api-token="$CONFLUENCE_API_TOKEN"
```

## 3. Write Minimal Values

```yaml
contentStore:
  secretName: eshu-postgres
  dsnKey: dsn

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

For EKS-specific ingress, IRSA, and External Secrets guidance, use
[Deploy to EKS](../eks/index.md).

## 4. Render, Then Install

```bash
helm template eshu ./deploy/helm/eshu -f values.eshu.yaml

scripts/verify-hosted-helm-rollout-proof.sh \
  --out-dir .proof/helm-install \
  --namespace eshu \
  --release eshu \
  -f values.eshu.yaml

scripts/verify-hosted-security-posture.sh -f values.eshu.yaml

helm upgrade --install eshu ./deploy/helm/eshu \
  --namespace eshu \
  -f values.eshu.yaml
```

The proof script records the chart version, app version, image reference,
rendered workload set, schema-bootstrap hook, and Helm dry-run result before a
cluster change. Use [Helm Rollout Proof](helm-rollout-proof.md) for live
API/MCP readback and upgrade or rollback declarations.

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
