# Hosted Security Posture Gate

Use the hosted security posture gate before exposing API or MCP endpoints,
sharing hosted onboarding artifacts, or enabling optional hosted collectors.
It verifies the Helm-rendered deployment shape without printing credential
values.

## What The Gate Checks

`scripts/verify-hosted-security-posture.sh` renders the Helm chart and fails
when it finds:

- `ESHU_API_KEY` without a Secret-backed `apiAuth.secretName`;
- credential-shaped env vars such as tokens, passwords, private keys, or DSNs
  rendered as inline `value` entries instead of `secretKeyRef`;
- credential `secretKeyRef` entries with empty Secret names or keys;
- `ESHU_PPROF_ADDR` bound to a public listener;
- `ESHU_ENABLE_PUBLIC_DOCS=true` unless the operator passes
  `--allow-public-docs` to record the deliberate exposure decision.

The gate is posture evidence only. It does not prove API/MCP answer readiness,
queue convergence, source completeness, or tenant isolation.

## Safe Values Shape

Use Secret references for shared API auth, Postgres, graph auth, GitHub App
credentials, and enabled collector credentials:

```yaml
apiAuth:
  secretName: eshu-api-auth
  key: api-key

contentStore:
  secretName: eshu-postgres
  dsnKey: dsn

neo4j:
  auth:
    secretName: eshu-neo4j
    usernameKey: username
    passwordKey: password

repoSync:
  auth:
    method: githubApp
    githubApp:
      secretName: github-app-credentials
```

Do not put credential-bearing DSNs, bearer tokens, provider keys, Git private
keys, webhook secrets, or collector API tokens in public values files, issues,
PR text, or onboarding artifacts.

## Public Docs And Pprof

Hosted deployments default `ESHU_ENABLE_PUBLIC_DOCS` to `false`. If an operator
intentionally exposes API docs, run the verifier with `--allow-public-docs` and
record why that exposure is acceptable for the environment.

Enable pprof only for the runtime that owns a diagnosed slow stage. Bind it to
loopback or an internal listener, then use a private port-forward or private
network path. Do not expose pprof through public ingress, Gateway API, or a
public Service.

## Direct Kubernetes Secret Example

```bash
kubectl -n eshu create secret generic eshu-api-auth \
  --from-literal=api-key="$ESHU_API_KEY"

kubectl -n eshu create secret generic eshu-postgres \
  --from-literal=dsn="$ESHU_POSTGRES_DSN"

kubectl -n eshu create secret generic eshu-neo4j \
  --from-literal=username="$NORNICDB_USERNAME" \
  --from-literal=password="$NORNICDB_PASSWORD"
```

## Verification

```bash
scripts/test-verify-hosted-security-posture.sh
scripts/verify-hosted-security-posture.sh -f values.eshu.yaml
helm lint deploy/helm/eshu -f values.eshu.yaml
```

Docs or navigation changes also require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
