# Argo CD and GitOps

Eshu ships Argo CD examples under `deploy/argocd`.

```text
deploy/argocd/
├── base/
│   ├── application.yaml
│   ├── kustomization.yaml
│   └── values.yaml
└── overlays/
    ├── aws/
    │   ├── application-patch.yaml
    │   ├── externalsecret-examples.yaml
    │   ├── kustomization.yaml
    │   └── values.yaml
    └── qa/
        ├── application-patch.yaml
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

## Schema bootstrap PreSync hook

Direct `helm install/upgrade` keeps the schema-bootstrap Helm
pre-install/pre-upgrade hooks. An ArgoCD Application instead sets
`schemaBootstrap.useHelmHooks=false` and enables
`schemaBootstrap.argocdPreSync` (off by default; the QA overlay enables it
first so the hook proves green before any production rollout). The same Job
then runs as an ArgoCD PreSync hook at negative sync wave with
`BeforeHookCreation` deletion: a sync with pending migrations runs the hook
to completion before workloads roll, a sync with a clean schema passes in
seconds, and a planted failing migration halts the sync with the hook Job's
logs as the cause. The Job runs the release image bootstrap command with its
xmin-precheck init container; there is no second migrations image. Do not
enable both hook families at once — ArgoCD also processes `helm.sh/hook`
annotations, so the chart fails the render as mutually exclusive.

Size the deadline from measured builds: the chart default
`activeDeadlineSeconds: 600` only bounds the bootstrap client pod while the
Postgres backend keeps building unattended, and a `CREATE INDEX CONCURRENTLY`
build behind a long transaction outlasts it. Environment overlays use at
least 3600 s. A deadline kill still leaves the schema safe for the next run
to resume through migration receipts, the advisory-lock wait, and the
invalid-index cleanup. If the xmin-precheck refuses to start, clear the
holder per the [park-holder runbook](../../operate/park-holder-runbook.md)
and re-sync.

Break-glass without ArgoCD: render the Job by hand and apply it as an
unmanaged one-shot, then wait on the component label:

```bash
helm template eshu ./deploy/helm/eshu \
  -f deploy/argocd/base/values.yaml \
  -f deploy/argocd/overlays/qa/values.yaml \
  --show-only templates/job-schema-bootstrap.yaml | kubectl apply -f -
kubectl wait --for=condition=complete job \
  -l app.kubernetes.io/component=schema-bootstrap --timeout=3600s \
  --namespace eshu
```

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
