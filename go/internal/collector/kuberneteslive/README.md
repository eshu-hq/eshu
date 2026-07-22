# internal/collector/kuberneteslive

Read-only Kubernetes live collector source and fact-envelope contract. This is
the foundation toward issue #388 (Git/runtime correlation and drift read model);
correlation and drift remain reducer-owned and are not in this package.

## Responsibilities

- Connect to a configured cluster through a narrow, read-only `Client` and list
  a fixed core resource set: namespaces, pods, deployments, replicasets,
  statefulsets, daemonsets, jobs, cronjobs, services, ingresses,
  ServiceAccounts, Roles, ClusterRoles, RoleBindings, and ClusterRoleBindings.
- Map listed workload and topology objects into Kubernetes live source facts via
  the shared envelope:
  - `kubernetes_live.pod_template` — container/init-container image refs,
    declared ports, environment variable NAMES, service account, selector and
    label metadata.
  - `kubernetes_live.relationship` — directed owner-reference and
    ingress-to-service edges between durable object identities.
  - `kubernetes_live.warning` — non-fatal capability gaps (forbidden resource,
    partial list, invalid owner reference, ambiguous selector).
- Each container in a `pod_template` fact may carry an optional
  `resolved_image_digest` — the CRI-resolved digest from
  `pod.Status.ContainerStatuses[].ImageID`, normalized to the bare
  `repo@sha256:<digest>` form. This field is metadata-only (a digest is a
  content fingerprint, never a secret), populated only for Pod objects (which
  carry container statuses), and empty for Deployments, ReplicaSets, and any
  object that only exposes a pod template spec without status. The
  normalization strips container-runtime scheme prefixes (`docker-pullable://`,
  `docker://`, `cri-o://`, etc.) and rejects bare `sha256:` values with no
  repository (unjoinable). See `NormalizeCRIImageID` in `envelope.go` and the
  `clientgo` adapter's `workloadFromPod`, which reads
  `pod.Status.ContainerStatuses`/`InitContainerStatuses` for this digest
  (#5432). This is one of the adapter's bounded `.Status` reads; the others —
  `pod.Status.Phase` and the Deployment/ReplicaSet
  `.Status.ReadyReplicas`/`.Status.AvailableReplicas` — are described next.
- A `pod_template` fact also carries optional observed-vs-desired runtime-status
  fields, self-describing by name (#5431, extended #5433): `desired_replicas`
  (DESIRED, from a Deployment/ReplicaSet/StatefulSet's `.Spec.Replicas`),
  `ready_replicas` and `available_replicas` (OBSERVED, from
  `.Status.ReadyReplicas` / `.Status.AvailableReplicas`), and `pod_phase`
  (OBSERVED, from a Pod's `.Status.Phase`). The replica fields are populated for
  Deployment, ReplicaSet, and StatefulSet objects. A DaemonSet has no replica
  spec, so its per-node scheduling counts stand in as the replica-equivalent:
  `desired_replicas` from `.Status.DesiredNumberScheduled`, `ready_replicas`
  from `.Status.NumberReady`, `available_replicas` from
  `.Status.NumberAvailable` — all OBSERVED, never a desired spec value. A Job
  or CronJob has no replica concept at all, so all three fields are absent;
  only the pod template spec is emitted (a CronJob's template is read from the
  nested `.Spec.JobTemplate.Spec.Template.Spec`). `pod_phase` is populated only
  for Pod objects and absent for every other workload kind. This is
  fact-level emission only — nothing here materializes onto the graph node or
  adds a query surface; that is deferred to the materialization capstone
  (#5435).
- Map listed ServiceAccount and RBAC objects into `secrets_iam_posture` source
  facts for the Kubernetes secrets/IAM evidence lane:
  - `k8s_service_account`
  - `k8s_service_account_token_posture`
  - `k8s_rbac_role`
  - `k8s_rbac_binding`
  - `k8s_workload_identity_use`
  - `k8s_gcp_workload_identity_binding`
  - `eks_irsa_annotation`
  - `secrets_iam_coverage_warning`
- Yield one snapshot generation per cluster through `collector.Service`.

## What this package does not do

- It never mutates the cluster (no create/update/patch/delete, exec, attach,
  portforward, or logs).
- It never reads Secret values, ConfigMap data payloads, environment variable
  values, projected tokens, or container logs. Redaction is a construction
  invariant: only metadata is mapped.
- It never writes graph state, resolves canonical ownership, or decides drift.
  Those are reducer responsibilities (#388).
- It never decides effective RBAC permissions, IAM trust-chain posture, or
  workload access paths. The collector emits source facts; reducers own joins.
- It does not import client-go. The Kubernetes API is the `Client` interface;
  the real adapter lives in the `clientgo` subpackage.

## Identity and idempotency

- Object identity follows the ADR tuple `(cluster_id, api_group, version,
  resource, namespace, name, uid)`. `metadata.uid` anchors identity so a
  recreated object with the same name but a new UID is a new identity.
- ServiceAccount, RBAC subject, role, binding, resource-version, and namespace
  names are fingerprinted before entering `secrets_iam_posture` facts. The
  source keeps raw names only long enough to form deterministic local join keys.
- A GKE `iam.gke.io/gcp-service-account` annotation emits a
  `k8s_gcp_workload_identity_binding` fact only when the cluster target declares
  `GCPWorkloadPool`. The fact carries a GCP service-account email digest and
  Workload Identity subject fingerprint, not the raw email, workload pool,
  namespace, or ServiceAccount name.
- Cluster scope identity is keyed on the operator-declared durable `cluster_id`,
  never the API server URL.
- The generation id depends only on `cluster_id` plus the observation time, so
  every fact in one snapshot shares a generation id regardless of how partial
  the snapshot turned out to be. Re-listing at the same instant is replay-stable
  and emits identical fact IDs.

## Partial and forbidden handling

A forbidden or mid-stream-failed list for one resource family does not abort the
snapshot. The family is recorded as partial, a warning fact is emitted, and the
generation freshness hint becomes `partial`. The snapshot still commits so
operators see what was reachable plus explicit evidence of what was not.

## Telemetry

Metrics use the `eshu_dp_kubernetes_` prefix and never carry namespace or object
names as labels:

- `eshu_dp_kubernetes_api_calls_total{operation,result}`
- `eshu_dp_kubernetes_resources_listed_total{resource_scope,result}`
- `eshu_dp_kubernetes_facts_emitted_total{fact_kind}`
- `eshu_dp_kubernetes_warnings_total{reason}`
- `eshu_dp_kubernetes_list_duration_seconds{resource_scope}`

Spans: `kubernetes_live.snapshot`, `kubernetes_live.api_call`.

## Concurrency

A `Source` is a serial producer driven by `collector.Service.Next`; each cluster
yields exactly one generation, and the durable conflict domain is the per-cluster
scope id. Parallel multi-cluster collection is deferred and must partition by the
per-cluster scope id (its natural conflict key) rather than serialize, per the
repository's "serialization is not a fix" rule.

## Performance and observability evidence

No-Regression Evidence: this package is a new opt-in collector binary that is not
wired into any default Compose or Helm profile, and it touches no reducer, graph
writer, query handler, hot-path Cypher, or schema DDL. There is no existing path
to regress. The new path is bounded by design: read-only Kubernetes list calls
with page size 200 and continue tokens, one generation per cluster per poll, cost
proportional to listed object count. Focused tests pass with `-count=1`:
`go/internal/collector/kuberneteslive/...`, `go/cmd/collector-kubernetes-live`,
`go/internal/collector/secretsiam`, `go/internal/facts`, and
`go/internal/telemetry`.

Observability Evidence: every snapshot records `eshu_dp_kubernetes_api_calls_total`,
`eshu_dp_kubernetes_resources_listed_total`, `eshu_dp_kubernetes_facts_emitted_total`,
`eshu_dp_kubernetes_warnings_total`, and `eshu_dp_kubernetes_list_duration_seconds`,
emits the `kubernetes_live.snapshot` and `kubernetes_live.api_call` spans, and logs
per-cluster completion (scope id, generation id, partial flag, fact count,
duration). The fake-client unit tests exercise the resources-listed,
facts-emitted, warning, and partial paths that drive these signals, so an
operator can diagnose a stuck or partial snapshot at 3 AM.

## Collector authoring evidence

Collector Performance Evidence: read-only Kubernetes list calls bounded by page
size 200 and continue tokens; one generation per cluster per poll; cost
proportional to listed object count. No reducer, graph write, hot-path Cypher,
or schema DDL is touched, so there is no existing collector path to regress.
Focused tests pass with `-count=1` for
`go/internal/collector/kuberneteslive/...`, `go/cmd/collector-kubernetes-live`,
`go/internal/collector/secretsiam`, `go/internal/facts`, and
`go/internal/telemetry`.

Collector Observability Evidence: `eshu_dp_kubernetes_api_calls_total{operation,result}`,
`eshu_dp_kubernetes_resources_listed_total{resource_scope,result}`,
`eshu_dp_kubernetes_facts_emitted_total{fact_kind}`,
`eshu_dp_kubernetes_warnings_total{reason}`, and
`eshu_dp_kubernetes_list_duration_seconds{resource_scope}`, plus the
`kubernetes_live.snapshot` and `kubernetes_live.api_call` spans and per-cluster
completion logs. Fake-client tests exercise the resources-listed, facts-emitted,
warning, and partial paths.

Collector Deployment Evidence: this foundation ships the binary and its build
registration in `scripts/install-local-binaries.sh`. The Helm chart now wires the
collector — `deploy/helm/eshu/templates/deployment-kubernetes-live-collector.yaml`,
`service-kubernetes-live-collector-metrics.yaml`,
`rbac-kubernetes-live-collector.yaml`, and the ServiceMonitor entry in
`deploy/helm/eshu/templates/servicemonitor.yaml` — but the entire lane is gated
off by default behind `kubernetesLiveCollector.enabled: false`
(`deploy/helm/eshu/values.yaml`), so an operator must explicitly opt in. The
metrics Service additionally requires `observability.prometheus.enabled` (it is
gated on both flags). The ServiceMonitor entry's own `if` reads as gated only on
`kubernetesLiveCollector.enabled`, but `servicemonitor.yaml` opens with a
file-wide `{{- if and .Values.observability.prometheus.enabled
.Values.observability.prometheus.serviceMonitor.enabled }}` that is not closed
until the file's final `{{- end }}`, so every per-collector block inside it —
this one included — already requires `observability.prometheus.enabled` (and
`observability.prometheus.serviceMonitor.enabled`) before its own flag is even
consulted. The ServiceMonitor therefore cannot render without the metrics
Service also existing; see
`TestHelmKubernetesLiveCollectorServiceMonitorGatingMatchesService` in
`go/internal/runtime/helm_live_collectors_contract_test.go` for the regression
proof. The in-cluster RBAC additionally requires `kubernetesLiveCollector.rbac.create`.
It is still NOT added to any Compose service. Per the readiness doc, the
claim-driven deployed lane remains deferred until the claim runtime, status
path, and proof exist (#388 follow-ups). The reducer `kubernetes_correlation`
domain (`go/internal/reducer/kubernetes_correlation.go`), the drift read
model (`GET /api/v0/kubernetes/correlations`, `go/internal/query/kubernetes.go`),
and the MCP tool (`list_kubernetes_correlations`) have landed, including the
readiness-gated `RUNS_IMAGE` graph edge
(`go/internal/reducer/kubernetes_correlation_materialization.go`). As of
#5436, `RUNS_IMAGE` also has a graph read path through
`analyze_infra_relationships` (`what_runs_image` query type) and
`POST /api/v0/infra/relationships`, resolving a KubernetesWorkload to the
OciImageManifest/OciImageIndex/OciImageDescriptor it runs, and the reverse.

## Deferred to follow-up PRs (#388 and beyond)

- Claim-driven collection through `collector.ClaimedService` and the workflow
  coordinator.
- Watch mode with bookmarks, reconnects, and `410 Gone` relist recovery.
- Additional resource kinds (Service endpoints, CRDs) and the richer ADR fact
  families.
- Effective RBAC interpretation, AWS IAM joins, Vault joins, stale-generation
  handling, and secrets/IAM posture read models.
