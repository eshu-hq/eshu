# Argo CD and GitOps

Eshu ships Argo CD examples under `deploy/argocd`.

```text
deploy/argocd/
├── base/
│   ├── application.yaml
│   ├── kustomization.yaml
│   └── values.yaml
└── overlays/
    └── aws/
        ├── application-patch.yaml
        ├── externalsecret-examples.yaml
        ├── kustomization.yaml
        └── values.yaml
```

The base points Argo CD at the Helm chart in
`deploy/helm/eshu`. The AWS overlay adds EKS-oriented
settings such as IRSA annotations, ALB ingress values, and External Secrets
examples.

Use the examples as starting points, not as credential sources. Replace secret
names, ExternalSecret references, ingress annotations, hostnames, and role ARNs
with your platform values.

## GitOps checklist

- Keep Postgres, NornicDB or Neo4j, and Eshu in a clear sync order.
- Store database and Git credentials in your secret manager.
- Keep environment-specific Helm overrides in overlays.
- Pin image tags for production.
- Review `contentStore.dsn`, `neo4j.uri`, and `env.ESHU_GRAPH_BACKEND` together.
- Run the rendered-diff preflight in CI before Argo CD sync.

For the chart values, use [Helm Values](helm-values.md).

## Rendered-Diff Preflight

Run the GitOps preflight before a controller applies an overlay:

```bash
scripts/verify-gitops-rendered-diff-preflight.sh \
  --overlay deploy/argocd/overlays/aws \
  --values values.private.yaml
```

The script renders `deploy/helm/eshu` with the Argo CD value files and any
private override file you pass. It fails before sync when the rendered shape
contains placeholder values, an unpinned production image tag, an impossible
Ingress/Gateway combination, chart-managed NornicDB with Helm-hook schema
bootstrap, or claim-driven collectors without an active workflow coordinator.

The output is safe to keep as CI evidence. It lists value file names, chart and
app versions, image refs, rendered resource names, ServiceMonitor state, and
whether Postgres and graph wiring are configured. It does not print raw DSNs,
bearer tokens, provider credentials, or private env-file contents.

Use the focused harness when editing the preflight itself:

```bash
scripts/test-verify-gitops-rendered-diff-preflight.sh
```
