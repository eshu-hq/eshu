# Eshu Helm Chart

## Purpose

This chart renders Eshu split-service Kubernetes workloads: schema bootstrap,
API, MCP, ingester, reducer, workflow coordinator, webhook listener, optional
collectors, and optional bundled NornicDB.

## Ownership Boundary

`deploy/helm/eshu` owns Helm defaults, schema validation, render-time
guardrails, and Kubernetes templates. Operator walkthroughs and value-by-value
guidance belong in the public Kubernetes docs.

## Chart Surface

- `values.yaml` defines defaults.
- `values.schema.json` validates values shape.
- `templates/validate.yaml` fails impossible combinations early.
- `templates/` renders workloads, services, ServiceMonitors, policies, schema
  bootstrap, and optional collector resources.

## Gotchas / Invariants

- Render locally with `helm template eshu ./deploy/helm/eshu` after value or
  template changes.
- API and MCP pods currently start through the `eshu` CLI wrapper; most other
  long-running workloads use direct `/usr/local/bin/eshu-*` binaries.
- Claim-driven collectors require an active workflow coordinator with claims
  enabled.
- Production Postgres DSNs should use `contentStore.secretName` and
  `contentStore.dsnKey`; inline `contentStore.dsn` is for local-only or private
  operator contexts.
- Ingress and Gateway API exposure are mutually exclusive.
- Helm-hook schema bootstrap cannot run against chart-managed NornicDB in the
  same install because hooks run before that backend exists.

## Verification

```bash
helm template eshu ./deploy/helm/eshu
scripts/verify-hosted-security-posture.sh
helm lint ./deploy/helm/eshu
```

## Related Docs

- `docs/public/deploy/kubernetes/helm-quickstart.md`
- `docs/public/deploy/kubernetes/helm-values.md`
- `docs/public/deploy/kubernetes/helm-runtime-values.md`
- `docs/public/deploy/kubernetes/helm-collector-and-webhook-values.md`
- `docs/public/deployment/service-runtimes.md`
