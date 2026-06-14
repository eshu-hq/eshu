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
- Production resolution-engine renders reject
  `ESHU_GENERATION_RETENTION_ENABLED=false` in global, workload, or lane env so
  superseded generation cleanup always runs beside reducer work.
- Production Postgres DSNs should use `contentStore.secretName` and
  `contentStore.dsnKey`; inline `contentStore.dsn` is for local-only or private
  operator contexts.
- Ingress and Gateway API exposure are mutually exclusive.
- Helm-hook schema bootstrap cannot run against chart-managed NornicDB in the
  same install because hooks run before that backend exists.
- `api.extraVolumes`/`api.extraVolumeMounts` (and the `mcpServer` equivalents)
  mount extra read-only material such as an operator-managed scoped-token
  registry Secret. Pair them with `api.env`/`mcpServer.env` to point
  `ESHU_SCOPED_TOKENS_FILE` at the mounted path. Only set
  `ESHU_SCOPED_TOKENS_FILE` once the Secret exists: a missing file makes the API
  fail closed at startup.

## Verification

```bash
helm template eshu ./deploy/helm/eshu
scripts/verify_generation_retention_helm_guard.sh
scripts/verify-hosted-security-posture.sh
scripts/verify-hosted-network-policy-egress.sh
helm lint ./deploy/helm/eshu
```

## Performance / Observability Evidence

The `api.extraVolumes` / `api.extraVolumeMounts` and `mcpServer.extraVolumes` /
`mcpServer.extraVolumeMounts` hooks added for the two-team governance proof are
additive and default to `[]`, so the rendered API/MCP runtime is byte-identical
unless an operator opts in by mounting a read-only Secret (for example the
`ESHU_SCOPED_TOKENS_FILE` registry). The `ci/governance-two-team-k8s.values.yaml`
file is test-only and is not part of any shipped runtime profile.

No-Regression Evidence: these are opt-in, empty-by-default Pod volume hooks; they
add no Cypher, graph write, worker claim, lease, batch, queue, or concurrency
knob and do not change the default-rendered Deployment runtime. Live proof on
OrbStack Kubernetes v1.34.8 (single node): two-team scoped reads stay isolated
(each team count=1, other team's repo absent, API/MCP parity), out-of-grant
selector 403, unauthenticated 401, NetworkPolicy restricted egress applied; all
pods reached Ready and the namespace was torn down clean. The scoped-token
authorization itself is unchanged graph/SQL already exercised by the merged
scoped-read suites.

No-Observability-Change: the proof reads existing spans/metrics/status and the
documented `/api/v0/repositories` and MCP responses; no telemetry, metric label,
span, or status field is added or altered by the chart hooks.

## Related Docs

- `docs/public/deploy/kubernetes/helm-quickstart.md`
- `docs/public/deploy/kubernetes/helm-values.md`
- `docs/public/deploy/kubernetes/helm-runtime-values.md`
- `docs/public/deploy/kubernetes/helm-collector-and-webhook-values.md`
- `docs/public/deployment/service-runtimes.md`
