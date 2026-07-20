# Design: Kubernetes Live Collector Foundation (toward #388)

**Status:** Foundation SHIPPED on `main` (collector package
`go/internal/collector/kuberneteslive/`, `eshu-collector-kubernetes-live`
binary, three `kubernetes_live.*` fact kinds, and the `kubernetesLiveCollector`
Helm chart workload).
**Related:** #388 (reducer Git/runtime correlation and drift read model),
`docs/public/reference/collector-reducer-readiness.md` (Kubernetes live gated
row), the removed ADR `2026-05-10-kubernetes-live-cluster-collector.md`
(reconstructed from PR #134 git history).

## Why

#388 is blocked because the Kubernetes live collector was never implemented —
only a Proposed ADR existed. This PR builds the collector foundation: a
read-only, metadata-only source that emits typed source facts, so #388 can later
join live runtime evidence to Git/IaC/AWS/OCI/SBOM facts and compute drift.

This is the foundation, not the whole collector and not the #388 read model.

## Scope of this PR

- The `kuberneteslive` collector package (source + fact envelope contract) and
  its `clientgo` read-only adapter.
- The `eshu-collector-kubernetes-live` binary using `collector.Service`.
- Three new fact kinds, the `kubernetes_live` collector kind, telemetry, docs,
  and TDD tests.

Deferred to follow-up PRs (stated so reviewers do not expect them here):

- Claim-driven collection via `collector.ClaimedService` and the workflow
  coordinator.
- Watch mode (bookmarks, reconnect, `410 Gone` relist recovery).
- Additional resource kinds (StatefulSet, DaemonSet, Job, CronJob, endpoints,
  RBAC subjects, CRDs) and the richer ADR fact families
  (`kubernetes_resource`, `kubernetes_workload`, `kubernetes_image_reference`,
  `kubernetes_service`, `kubernetes_rbac_subject`).
- The #388 reducer projection, correlation, and drift read model — LANDED
  (`go/internal/reducer/kubernetes_correlation.go`,
  `go/internal/query/kubernetes.go`; see
  `docs/internal/design/388-kubernetes-correlation-readmodel.md`).
- Helm chart values / charted workload — LANDED (`kubernetesLiveCollector`,
  off by default).

## Auth model

The collector connects read-only and lists; it never mutates. Two auth modes,
both standard client-go:

- `in_cluster` — `rest.InClusterConfig()` using the mounted service-account
  token. Use when the collector runs as a pod.
- `kubeconfig` — `clientcmd` deferred loading from a kubeconfig file path plus an
  optional context. Use for out-of-cluster collection.

All client-go dependence is confined to the `clientgo` subpackage behind the
narrow `kuberneteslive.Client` interface, so the source is unit-testable with
fakes and the rest of the collector carries no client-go import.

RBAC posture is least privilege: `get`, `list`, `watch` only on the configured
families, Secrets excluded, no wildcards, no `cluster-admin`.

## Resource set (core, foundation)

namespaces, pods, deployments, replicasets, services, ingresses — listed with
bounded pagination (`Limit` + `Continue`).

## Cluster targeting

One configured cluster boundary per target, anchored on an operator-declared
durable `cluster_id` (ADR requirement). The collector never infers cluster
identity from the API server URL. Scope kind is the existing `scope.KindCluster`;
the collector kind is the new `scope.CollectorKubernetesLive` (`kubernetes_live`).

## Fact schema (the three foundation kinds)

All three carry the shared envelope: `collector_kind=kubernetes_live`,
`collector_instance_id`, `scope_id`, `generation_id`,
`source_confidence=reported`, `fence_token`, `correlation_anchors`, schema
version `1.0.0`.

Object identity tuple (ADR): `(cluster_id, api_group, version, resource,
namespace, name, uid)`. `metadata.uid` anchors identity so a recreated object
with the same name but a new UID is a new identity. `object_id` is a stable
hash of that tuple.

| Fact kind | Payload (metadata only) |
| --- | --- |
| `kubernetes_live.pod_template` | object identity, `service_account`, `selector`, `labels`, and per-container `{name, image, init, ports, env_keys, env_from_secret}`. Env var NAMES only. |
| `kubernetes_live.relationship` | directed edge `from_object_id -> to_object_id` with `relationship_type` (`owner_reference`, `ingress_to_service`) and both GVRs. |
| `kubernetes_live.warning` | `reason` (closed enum), `resource_scope`, sanitized `message`. |

Generation id depends only on `cluster_id` + observation time, so every fact in
one snapshot shares a generation id regardless of partial state, and re-listing
at the same instant is replay-stable (idempotent fact IDs).

## Redaction contract (METADATA-ONLY, enforced by construction and tests)

The collector NEVER reads or emits:

- Secret values, or Secret object `data`/`stringData`.
- ConfigMap data payloads.
- Environment variable VALUES (`env.Value`), or values behind secret/configmap
  refs. It records env var NAMES and a boolean `env_from_secret` only.
- Container logs.

It never issues create/update/patch/delete, exec, attach, portforward, or log
requests. The `Client` interface exposes only list methods, so a mutating call
cannot be added without changing the contract. A redaction unit test asserts no
secret-shaped value reaches a payload.

## Partial and forbidden handling

A forbidden or mid-stream-failed list for one resource family does not abort the
snapshot. The family is marked partial, a warning fact is emitted, the generation
freshness hint becomes `partial`, and the snapshot still commits so operators see
reachable evidence plus explicit gaps. An unreachable cluster (ping failure)
fails the whole pass so it is retried, not silently emitted as empty.

## Concurrency

A `Source` is a serial producer driven by `collector.Service.Next`; each cluster
yields exactly one generation and the durable conflict domain is the per-cluster
scope id. This is not a serialization workaround: one cluster maps to one
generation that commits atomically. Parallel multi-cluster collection is a
follow-up that must partition by the per-cluster scope id (its natural conflict
key) rather than serialize, per the repo's "serialization is not a fix" rule.

## Telemetry

Prefix `eshu_dp_kubernetes_`, no namespace/object/image names in labels:

- `api_calls_total{operation,result}`
- `resources_listed_total{resource_scope,result}`
- `facts_emitted_total{fact_kind}`
- `warnings_total{reason}`
- `list_duration_seconds{resource_scope}`

Spans: `kubernetes_live.snapshot`, `kubernetes_live.api_call`. Per-cluster snapshot
completion logs scope id, generation id, partial flag, fact count, and duration.

## Performance and evidence

Performance impact declaration: this PR adds a new collector stage that issues
read-only Kubernetes list calls bounded by page size (200) and continue tokens.
Cost is proportional to listed object count per cluster, one generation per
cluster per poll. There is no graph write, reducer change, or hot-path Cypher in
this PR, so there is no existing-path regression to measure; the new path is
bounded by design.

No-Regression Evidence: no existing runtime path changed; the collector is a new
opt-in binary not wired into any default Compose/Helm profile, and no reducer,
graph writer, query handler, or schema DDL was touched. Focused package tests
(`go/internal/collector/kuberneteslive/...`, `go/cmd/collector-kubernetes-live`,
`go/internal/facts`, `go/internal/telemetry`) pass with `-count=1`.

Observability Evidence: the five `eshu_dp_kubernetes_*` metrics and two spans
above are emitted by `Source` and recorded on every snapshot; the fake-client
unit tests exercise the resources-listed, facts-emitted, warning, and partial
paths that drive those signals, and per-cluster completion is logged for 3 AM
diagnosis.

## Open items for principal review

- Fact schema shape and the three-kind foundation subset (vs. the ADR's full
  eight-kind table). Getting the schema wrong is expensive to undo.
- The direct `collector.Service` lane for the foundation vs. starting
  claim-driven; this PR mirrors how OCI registry started (direct first).
- client-go version pin (`v0.34.1`) and the dependency footprint it adds.
