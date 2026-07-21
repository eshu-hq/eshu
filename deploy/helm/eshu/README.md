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
- `workspace-setup` is a non-root init container. It must keep dropped
  capabilities, avoid ownership mutation, and rely on pod `fsGroup` handling for
  supported persistent volumes.
- `api.extraVolumes`/`api.extraVolumeMounts` (and the `mcpServer` equivalents)
  mount extra read-only material such as an operator-managed scoped-token
  registry Secret. Pair them with `api.env`/`mcpServer.env` to point
  `ESHU_SCOPED_TOKENS_FILE` at the mounted path. Only set
  `ESHU_SCOPED_TOKENS_FILE` once the Secret exists: a missing file makes the API
  fail closed at startup.
- Bootstrap identity seeding (epic #4962/#4963) reuses the same
  `api.extraVolumes`/`api.extraVolumeMounts` + `api.env` convention above —
  there is no dedicated chart value for it. Two operator-managed Secrets are
  relevant, both referenced the same way:
  - The data-encryption key (`ESHU_AUTH_SECRET_ENC_KEY`/`_FILE`/`_ID`, epic
    #4962 PR-1): mount an operator-managed Secret (for example
    `eshu-auth-secret-enc-key`) read-only via `extraVolumes`/
    `extraVolumeMounts` and point `ESHU_AUTH_SECRET_ENC_KEY_FILE` at the
    mounted path via `api.env`. Required only when
    `ESHU_AUTH_BOOTSTRAP_MODE=generated` (the default); `sso-only` and
    `disabled` never need it, and neither does an `ESHU_ADMIN_USERNAME`/
    `ESHU_ADMIN_PASSWORD`-seeded deployment unless a later reset needs it.
  - The bootstrap admin's own credential: for
    `ESHU_ADMIN_USERNAME`/`ESHU_ADMIN_PASSWORD` env-seeding, create a Secret
    named `eshu-initial-admin` holding the username and password, mount it
    read-only the same way, and point `ESHU_ADMIN_USERNAME`/
    `ESHU_ADMIN_PASSWORD_FILE` at it via `api.env`. For
    `ESHU_AUTH_BOOTSTRAP_MODE=generated`, the credential is instead generated
    at startup and sealed into Postgres; retrieve it with
    `eshu admin initial-credential` (or `eshu admin reset-initial-credential`
    to rotate it) and, if the deployment's runbook wants a
    Kubernetes-native copy for audit or GitOps, store that CLI output into
    the same `eshu-initial-admin` Secret name as a follow-up operator step —
    the chart does not write to it automatically; the API process holds no
    Kubernetes Secret-write RBAC.
- `componentExtensionCollector` is off by default. When enabled, it renders the
  process/OCI component extension host and passes the same component registry,
  trust allowlist, and extension egress policy env to the workflow coordinator
  and worker. Mount the same component registry volume into both workloads;
  strict trust mode is not charted until provenance verifier values are
  first-class chart inputs.
- `gcpCloudCollector` is off by default. When enabled, it starts only explicit
  claimed-live mode, requires active workflow claims, mounts the redaction key
  from a read-only Secret file, and expects read-only GCP credentials from pod
  identity rather than chart values.
- `kubernetesLiveCollector` is off by default and is not claim-driven. Each entry
  in `kubernetesLiveCollector.clusters` declares its own durable `cluster_id` and
  read-only `auth_mode` (`in_cluster` or `kubeconfig`). For `in_cluster` auth keep
  `serviceAccount.create=true` and `rbac.create=true` so the chart binds a
  read-only ClusterRole (namespaces, pods, services, serviceaccounts, deployments,
  replicasets, statefulsets, daemonsets, jobs, cronjobs, ingresses, roles,
  rolebindings, clusterroles, clusterrolebindings) to the collector
  ServiceAccount. The RBAC objects render only when at least one
  configured cluster uses `auth_mode: in_cluster`. For `kubeconfig` auth set
  `kubernetesLiveCollector.kubeconfig.secretName` to an operator-managed read-only
  Secret, point each cluster's `kubeconfig_path` at the mount path, and set
  `rbac.create=false` so kubeconfig-only deployments do not get an unused
  cluster-wide grant; leaving `rbac.create=true` with no `in_cluster` target fails
  render.
- `vaultLiveCollector` is off by default and is claim-driven. When enabled it
  requires an active workflow coordinator with an enabled claim-driven
  `vault_live` instance, a `collectorInstances` entry whose `collector_kind` is
  `vault_live` and whose `instance_id` matches `vaultLiveCollector.instanceId`
  (otherwise the pod would claim nothing and fail at startup), a read-only
  `redaction.secretName` Secret, and a read-only Vault token per target supplied
  through `extraEnv` and referenced by each target's `token_env`. Tokens never
  appear in the targets JSON.

## Verification

```bash
helm template eshu ./deploy/helm/eshu
scripts/verify_generation_retention_helm_guard.sh
scripts/verify-hosted-security-posture.sh
scripts/verify-hosted-network-policy-egress.sh
helm lint ./deploy/helm/eshu
```

## Performance / Observability Evidence

No-Regression Evidence: charted horizontal ingesters render only when each
replica keeps a StatefulSet-owned workspace claim. `ingester.replicas > 1`
sets `ESHU_REPO_SHARD_COUNT` from the replica count and
`ESHU_REPO_SHARD_INDEX` from `metadata.labels['apps.kubernetes.io/pod-index']`;
the chart requires Kubernetes 1.32 or newer for that stable StatefulSet label
and rejects static shard env overrides and shared
`ingester.persistence.existingClaim` storage. `go test ./internal/runtime -run
'TestHelm(RendersShardEnvForHorizontalIngester|RejectsHorizontalIngesterStaticShardEnvOverrides|RejectsHorizontalIngesterWithSharedExistingClaim|RejectsHorizontalIngesterOnOldKubernetes|IngesterDoesNotRenderShardPodIndexEnv)'
-count=1` covers the render contract and guards.

Observability Evidence: horizontal ingester rendering adds no metric, span,
status, or log schema. Shard identity stays visible through rendered env vars
and existing collector, queue, Postgres, and pod-level signals.

No-Regression Evidence: `workspace-setup` runs as UID/GID `10001`, keeps
`drop: ALL` with no added capabilities, creates `/data/.eshu` and `/data/repos`,
and replaces `.eshuignore` through a temp file on the same PVC before `mv -f`.
`go test ./internal/runtime -run
TestHelmWorkspaceSetupInitIsPersistentVolumeRetrySafe -count=1` covers the
default persistent-volume retry contract for horizontal ingesters.

No-Observability-Change: the workspace setup change adds no metric, span, log,
status, queue, or runtime data contract. Operators continue to diagnose init
success or failure through Kubernetes init-container state, pod events, and the
existing ingester probes after the container starts.

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

Performance Evidence: hosted resolution-engine pods render
`ESHU_SHARED_PROJECTION_PARTITION_COUNT=8`,
`ESHU_SHARED_PROJECTION_WORKERS=8`,
`ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT=8`, and
`ESHU_CODE_CALL_PROJECTION_WORKERS=4` by default so the charted `code_calls`
lane no longer falls back to the runtime's old `1/1` ceiling. The chart render
contract is covered by
`go test ./internal/runtime -run TestHelmResolutionEngineRendersCodeCallProjectionConcurrency -count=1`.

No-Observability-Change: these chart defaults only render environment values
for the existing reducer sidecar lanes. They add no metric, span, route, graph
query, queue table, Cypher, graph-write shape, or Kubernetes resource kind.
Operators continue to use resolution-engine logs, `/admin/status`,
partition-lease backlog, graph-write metrics, and pprof surfaces for code-call
partition throughput, retries, and dead-letter diagnosis.

The chart defers repo-dependency concurrency to the runtime's backend-aware
default: `4` for the remotely proven NornicDB path and `1` for unscaled Neo4j
compatibility. Operators may set the resolution-engine environment value to
`1`, `2`, or `4`; unsupported values fall back to the backend default. Repo-dependency
workers use fixed source-repository acceptance-unit shards: the same
repository's complete retract-then-rewrite cycle stays serialized and ordered,
while unrelated repositories can project concurrently. The runtime also
appends hostname, PID, and a boot-unique nonce to any configured lease-owner
prefix so separate pod boots cannot share one re-entrant owner identity. A
repo-dependency cycle has a `45s` caller deadline and a `5m` lease. The lease
must exceed the cycle deadline plus the configured canonical graph-write
timeout and a `30s` safety margin. An error, cancellation, or ambiguous commit
quarantines only that shard until its lease expires; other shards keep running.

Performance Evidence: charted ingesters render `SCIP_WORKERS=4` from
`ingester.scipWorkers` by default as a concurrency cap. SCIP
language/package-root indexing still requires an explicit `SCIP_INDEXER=1`,
`true`, `yes`, or `on` environment override; when enabled, SCIP no longer runs
serially unless an operator explicitly sets `SCIP_WORKERS=1`, while the runtime
limiter caps external SCIP indexer processes across concurrent repository
snapshots. The render contract is covered by `go test ./internal/runtime -run
'TestHelmIngesterRendersSCIPWorkers(Default|Override)' -count=1`; focused
collector proof on 2026-06-19 used
`go test ./internal/collector -run '^$' -bench BenchmarkSCIPLanguageSubtreeWorkers -benchtime=1x -benchmem -count=1`
and measured the four-subtree synthetic SCIP fixture at `workers_1` 25.367
ms/op and `workers_4` 6.388 ms/op on Apple M4 Pro.

Observability Evidence: `SCIP_WORKERS` saturation is visible through
`eshu_dp_scip_process_wait_seconds{language}` and bounded debug logs for SCIP
process slot acquisition with `wait_seconds`. Operators diagnose SCIP progress
and fallback through `eshu_dp_scip_snapshot_attempts_total`, bounded fallback
logs, parse stage summaries, parse metrics, and collector fact counters.

No-Regression Evidence: `componentExtensionCollector` is opt-in and defaults to
disabled, so the default chart render is unchanged. When enabled, it renders a
separate `eshu-collector-component-extension` Deployment, metrics Service,
ServiceMonitor, NetworkPolicy, and PodDisruptionBudget, while the workflow
coordinator receives the same component registry and trust policy env used by
the worker. The worker reads trusted component activations from the mounted
registry instead of static `ESHU_COLLECTOR_INSTANCES_JSON`, preserving the
existing claim planning and `collector.ClaimedService` retry/commit boundary.
Verified by `go test ./internal/runtime -run
TestHelmComponentExtensionCollector -count=1`,
`scripts/test-verify-remote-e2e-pagerduty-component-extension.sh`, and
`scripts/verify-remote-e2e-pagerduty-component-extension.sh --list`. The live
capture driver is
`scripts/run-remote-e2e-pagerduty-component-extension.sh --artifacts <run-dir>`
once a Compose stack has the trusted PagerDuty reference component installed
and enabled.

No-Observability-Change: the chart adds no new metric names, labels, spans,
queue domains, graph writes, or reducer paths. Operators use the existing
workflow coordinator reconcile metrics, collector `/admin/status`, `/metrics`,
health/readiness probes, failure classes, and workflow claim rows to diagnose
component extension progress or denial.

## Related Docs

- `docs/public/deploy/kubernetes/helm-quickstart.md`
- `docs/public/deploy/kubernetes/helm-values.md`
- `docs/public/deploy/kubernetes/helm-runtime-values.md`
- `docs/public/deploy/kubernetes/helm-collector-and-webhook-values.md`
- `docs/public/deployment/service-runtimes.md`
