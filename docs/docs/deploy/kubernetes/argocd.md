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

For the chart values, use [Helm Values](helm-values.md).
