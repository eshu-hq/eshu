# Tutorial: Deploy On Kubernetes

Use this tutorial when you want a small, supported Helm deployment of Eshu's
core services.

## Outcome

The Helm chart renders cleanly, the core services install into a namespace, and
Kubernetes reports the API, MCP server, schema bootstrap, ingester, and
resolution engine as rolled out.

## Time

About 20-30 minutes after Postgres, graph backend, and GitHub App credentials
are available.

## Prerequisites

- A Kubernetes cluster and `kubectl` context.
- Helm installed.
- Postgres DSN and graph backend credentials.
- A GitHub App credential when repository sync uses `githubApp`.

## Steps

1. Create the namespace:

   ```bash
   kubectl create namespace eshu
   ```

2. Create the required secrets:

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

3. Write a minimal `values.eshu.yaml` that names the content store, graph
   backend, and repository sync selector.
4. Render before installing:

   ```bash
   helm template eshu ./deploy/helm/eshu -f values.eshu.yaml
   ```

5. Run the local rollout and posture proof scripts:

   ```bash
   scripts/verify-hosted-helm-rollout-proof.sh \
     --out-dir .proof/helm-install \
     --namespace eshu \
     --release eshu \
     -f values.eshu.yaml

   scripts/verify-hosted-security-posture.sh -f values.eshu.yaml
   ```

6. Install or upgrade:

   ```bash
   helm upgrade --install eshu ./deploy/helm/eshu \
     --namespace eshu \
     -f values.eshu.yaml
   ```

7. Check rollout:

   ```bash
   kubectl -n eshu get pods
   kubectl -n eshu rollout status deployment/eshu-api
   kubectl -n eshu rollout status deployment/eshu-mcp-server
   kubectl -n eshu rollout status deployment/eshu-resolution-engine
   ```

## Expected Result

Helm renders without errors, the proof scripts record the rendered workload set
and dry-run result, and the deployed API and MCP workloads roll out. Pod health
is only the start; use status and readiness endpoints before claiming indexed
data is ready for questions.

## Failure Hints

- If Helm renders but pods fail, inspect Secret names and keys first.
- If the bundled graph backend is enabled, read the storage page before relying
  on schema-bootstrap hooks.
- If API/MCP pods are healthy but answers are empty, check indexing and reducer
  readiness instead of redeploying.
- If collector credentials are missing, enable collectors one family at a time.

## Read Next

- [Helm Quickstart](../deploy/kubernetes/helm-quickstart.md) for detailed
  values and rollout commands.
- [Storage](../deploy/kubernetes/storage.md) before using bundled NornicDB.
- [Health Checks](../operate/health-checks.md) for readiness checks after
  rollout.
