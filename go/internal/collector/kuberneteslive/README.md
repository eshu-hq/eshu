# internal/collector/kuberneteslive

Read-only Kubernetes live collector source and fact-envelope contract. This is
the foundation toward issue #388 (Git/runtime correlation and drift read model);
correlation and drift remain reducer-owned and are not in this package.

## Responsibilities

- Connect to a configured cluster through a narrow, read-only `Client` and list
  a fixed core resource set: namespaces, pods, deployments, replicasets,
  services, ingresses.
- Map listed objects into three typed source facts via the shared envelope:
  - `kubernetes_live.pod_template` — container/init-container image refs,
    declared ports, environment variable NAMES, service account, selector and
    label metadata.
  - `kubernetes_live.relationship` — directed owner-reference and
    ingress-to-service edges between durable object identities.
  - `kubernetes_live.warning` — non-fatal capability gaps (forbidden resource,
    partial list, invalid owner reference, ambiguous selector).
- Yield one snapshot generation per cluster through `collector.Service`.

## What this package does not do

- It never mutates the cluster (no create/update/patch/delete, exec, attach,
  portforward, or logs).
- It never reads Secret values, ConfigMap data payloads, environment variable
  values, or container logs. Redaction is a construction invariant: only
  metadata is mapped.
- It never writes graph state, resolves canonical ownership, or decides drift.
  Those are reducer responsibilities (#388).
- It does not import client-go. The Kubernetes API is the `Client` interface;
  the real adapter lives in the `clientgo` subpackage.

## Identity and idempotency

- Object identity follows the ADR tuple `(cluster_id, api_group, version,
  resource, namespace, name, uid)`. `metadata.uid` anchors identity so a
  recreated object with the same name but a new UID is a new identity.
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
`go/internal/facts`, and `go/internal/telemetry`.

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
`go/internal/facts`, and `go/internal/telemetry`.

Collector Observability Evidence: `eshu_dp_kubernetes_api_calls_total{operation,result}`,
`eshu_dp_kubernetes_resources_listed_total{resource_scope,result}`,
`eshu_dp_kubernetes_facts_emitted_total{fact_kind}`,
`eshu_dp_kubernetes_warnings_total{reason}`, and
`eshu_dp_kubernetes_list_duration_seconds{resource_scope}`, plus the
`kubernetes_live.snapshot` and `kubernetes_live.api_call` spans and per-cluster
completion logs. Fake-client tests exercise the resources-listed, facts-emitted,
warning, and partial paths.

Collector Deployment Evidence: this foundation ships the binary and its build
registration in `scripts/install-local-binaries.sh` only. It is intentionally
NOT added to any Compose service, Helm Deployment, chart value, or ServiceMonitor
in this PR. Per the readiness doc, a charted workload and a claim-driven deployed
lane are deferred until the claim runtime, reducer projection, status path, and
proof exist (#388 follow-ups). No deployment surface changes here.

## Deferred to follow-up PRs (#388 and beyond)

- Claim-driven collection through `collector.ClaimedService` and the workflow
  coordinator.
- Watch mode with bookmarks, reconnects, and `410 Gone` relist recovery.
- Additional resource kinds (StatefulSet, DaemonSet, Job, CronJob, Service
  endpoints, RBAC subjects, CRDs) and the richer ADR fact families.
- The reducer projection and the Git/runtime correlation and drift read model.
