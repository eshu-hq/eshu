# ADR: Kubernetes Live Cluster Collector

**Date:** 2026-05-10
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-05-09-optional-component-boundary.md`
- `docs/docs/guides/collector-authoring.md`
- `docs/docs/languages/kubernetes.md`
- Issue: #14

---

## Context

Eshu already parses Kubernetes YAML from Git. That parser tells us what a repo
declares. It does not prove what is running in a cluster.

The Kubernetes live collector adds a separate source-truth boundary: the
Kubernetes API server. It should observe configured clusters with read-only API
credentials, emit versioned facts, and leave canonical admission, live-vs-Git
drift, deployment mapping, image joins, and graph writes to reducers.

This collector belongs in the next collector wave after Terraform state and
AWS. It must not block the AWS/Terraform-state path, and it should remain
optional unless an operator explicitly configures a cluster instance.

## Source Contracts

The design uses Kubernetes API semantics directly:

- API discovery decides which served groups/resources are collectible.
- List calls use pagination with `limit` and `continue`.
- Kubernetes `resourceVersion` is checkpoint evidence for list/watch, not the
  Eshu `generation_id`.
- Watch mode must handle bookmarks, reconnects, and `410 Gone` by relisting.
- Object identity uses `(cluster_id, api_group, version, resource, namespace,
  name, uid)`, with `metadata.uid` preserving historical uniqueness.
- Labels and selectors are grouping signals, not unique identity.
- Owner references are first-class relationship evidence and must preserve
  Kubernetes namespace rules.
- RBAC should grant only the verbs and resources needed by the configured
  collection scope.

## Decision

Add a future collector family named `kubernetes_live`.

The collector owns:

- cluster API discovery
- cluster scope assignment
- collector generation assignment
- list/watch checkpoint handling
- redaction
- typed fact emission

The collector does not own:

- canonical service admission
- Git-to-live correlation
- runtime workload materialization
- drift decisions
- canonical graph writes
- answer shaping

## Scope And Generation

Collector instance:

- one configured Kubernetes API access boundary
- one operator-declared durable `cluster_id`
- optional metadata such as provider, account, region, environment, and
  cluster display name

The collector must not infer durable cluster identity from API server URL
alone. URLs can change, proxy through shared endpoints, or point at local test
clusters.

Work item identity:

```text
(collector_instance_id, cluster_id, api_group, version, resource, namespace_scope)
```

Generation:

- Eshu `generation_id` is coordinator-assigned per cluster collection run.
- Kubernetes `resourceVersion` is stored as provider checkpoint evidence per
  GVR/list/watch stream.
- A completed snapshot generation means the configured resource set was listed
  to completion or explicitly marked partial with warning facts.

## Collection Modes

### Snapshot Mode

Snapshot mode is the first implementation slice.

It must:

- discover API groups and resources
- apply an operator allowlist before listing
- page list calls with `limit` and `continue`
- persist checkpoints for each GVR and namespace scope
- emit idempotent facts
- record forbidden, unsupported, skipped, and partial resources as warnings

### Watch Mode

Watch mode is a later completion gate.

It is not complete until it:

- starts from a known snapshot resource version
- requests bookmarks where supported
- treats bookmarks as progress hints, not guaranteed heartbeat intervals
- reconnects from the last observed resource version
- handles `410 Gone` by clearing that GVR cache, relisting, and restarting the
  watch from the relist resource version
- exposes freshness lag and relist counts in status and metrics

## Fact Families

Initial fact kinds:

| Fact kind | Purpose |
| --- | --- |
| `kubernetes_resource` | Generic object fact with GVK/GVR, namespace, name, UID, resource version, labels, redacted annotations, owner refs, status digest, and source timestamps. |
| `kubernetes_workload` | Deployment, StatefulSet, DaemonSet, ReplicaSet, Job, CronJob, and Pod-template-backed controller evidence. |
| `kubernetes_pod_template` | Containers, init containers, image refs, ports, env key names, volume refs, service account, selectors, tolerations, and affinity summaries. |
| `kubernetes_image_reference` | Image string plus parsed registry, repository, tag, and digest where available. |
| `kubernetes_service` | Service, EndpointSlice, Ingress, and future Gateway API evidence when enabled by discovery. |
| `kubernetes_relationship` | Owner edges, selector-derived edges, service-to-endpoint edges, workload-to-pod edges, ingress-to-service edges, and config/service-account refs. |
| `kubernetes_rbac_subject` | ServiceAccount, Role, ClusterRole, RoleBinding, and ClusterRoleBinding metadata without token material. |
| `kubernetes_warning` | Forbidden resource, watch expired, partial discovery, list budget exhausted, secret skipped, unsupported API, selector ambiguity, invalid owner reference, and relist required. |

CRDs start as generic `kubernetes_resource` facts. Schema-aware CRD facts can
follow for Argo CD, Crossplane, Flux, Istio, Gateway API, and similar families.

Every fact must carry the shared envelope:

- `collector_kind=kubernetes_live`
- `collector_instance_id`
- `scope_id`
- `generation_id`
- `source_confidence=reported`
- `fence_token`
- `correlation_anchors`

## Redaction And Security

The collector is read-only and must avoid sensitive value collection.

It must not call:

- `create`, `update`, `patch`, or `delete`
- pod `exec`, `attach`, `portforward`, or logs
- Secret value reads by default

It may collect Secret object metadata only if explicitly enabled, and even then
must never emit Secret `data` or `stringData`.

Pod template facts should emit environment variable names and safe references,
not plaintext environment values. Annotations are redacted by default, with an
allowlist for known-safe annotations such as standard workload metadata.

The default RBAC posture is least privilege:

- prefer namespace `RoleBinding` where namespace-scoped collection is enough
- use `get`, `list`, and `watch` only
- avoid wildcard resources and verbs
- avoid `cluster-admin`
- keep Secrets excluded unless the operator explicitly opts into metadata-only
  collection

## Reducer And Query Contracts

Reducer/projector ownership:

- A Kubernetes-live projector may materialize `(:KubernetesResource)` and
  `(:KubernetesWorkload)` nodes from live facts.
- The DSL owns joins from live Kubernetes evidence to Git, Helm, Argo CD,
  Kustomize, Terraform state, AWS, OCI registry, SBOM, and vulnerability facts.
- Runtime image refs should join by digest first, then repository+tag as weaker
  evidence.
- Selector-derived relationships must carry ambiguity evidence when a selector
  matches zero objects or multiple unrelated owners.
- Query paths may show live Kubernetes evidence before full correlation, but
  exact ownership and drift claims require reducer readiness.

Suggested readiness keyspaces:

| Keyspace | Phase owner |
| --- | --- |
| `kubernetes_resource_uid` | Kubernetes-live projector |
| `kubernetes_workload_uid` | Kubernetes-live projector |
| `kubernetes_relationship_uid` | Kubernetes-live projector |
| `cross_source_anchor_ready` | DSL evaluator |

## Operational Model

The collector should run as a claim-driven Go runtime, matching Terraform
state and AWS collector direction:

- configuration declares collector instances and cluster credentials
- workflow coordinator enqueues cluster/GVR/namespace work items
- collector workers claim work, heartbeat, emit facts, and complete or fail
  claims through the shared workflow store
- the runtime mounts the shared `/healthz`, `/readyz`, `/admin/status`, and
  `/metrics` surface

Required status fields:

- configured clusters
- active GVRs
- skipped GVRs and reasons
- last completed generation per cluster
- per-GVR resource version checkpoint
- forbidden resource counts
- partial generation counts
- watch reconnect and relist counts
- freshness lag by cluster and GVR

Metrics use the prefix `eshu_dp_kubernetes_`:

- `api_calls_total{operation,result}`
- `list_duration_seconds{resource_scope}`
- `watch_reconnects_total{reason}`
- `facts_emitted_total{fact_kind}`
- `forbidden_resources_total{resource_scope}`
- `freshness_lag_seconds{resource_scope}`

Metric labels must not include namespace names, object names, image names, or
private registry paths.

## Edge Cases

The design must account for:

- empty clusters
- forbidden resources
- partial API discovery
- CRDs with unknown schemas
- list pagination token expiry
- watch disconnects
- `410 Gone` relist recovery
- deleted and recreated objects with the same name but different UID
- overlapping selectors
- invalid cross-namespace owner references
- stale Git intent where the live object still exists
- live objects with no matching Git/IaC evidence
- repeated generation replay

## Alternatives Considered

### Extend The Git Kubernetes Parser

Rejected. Git manifests and live API state answer different questions. Merging
them in the parser would confuse declared intent with runtime truth.

### Write Graph Edges Directly From The Collector

Rejected. It violates the collector contract and would fork canonical truth
outside the reducer.

### Watch-Only First

Rejected. Watch depends on a trustworthy initial state. Snapshot collection
must land first so replay and completeness are deterministic.

### Hardcode Only Built-In Resource Types

Rejected. The API is extensible. The collector should discover served
resources and apply an allowlist, while keeping CRDs generic until a schema
aware reducer contract exists.

## Rollout Plan

1. Add the `kubernetes_live` collector kind and workflow contract.
2. Define fact schemas and validation.
3. Implement snapshot discovery, pagination, redaction, and fact emission.
4. Add least-privilege RBAC manifests for configured resource families.
5. Add projector scaffolding for Kubernetes live resources and readiness rows.
6. Add reducer/DSL joins to Git, Helm, Argo CD, Kustomize, Terraform state,
   AWS, and OCI registry evidence.
7. Add watch mode only after snapshot mode has replay and idempotency proof.
8. Add operator runbook, telemetry docs, and local fixture validation.

## Acceptance Criteria

- New collector kind and workflow contract exist for Kubernetes live
  collection.
- Collector emits facts only and never writes graph state directly.
- Least-privilege RBAC supports read/list/watch for configured resources and
  excludes Secret values by default.
- Snapshot mode discovers resources, paginates lists, persists checkpoints, and
  emits idempotent facts.
- Watch mode handles bookmarks, reconnects, and `410 Gone` relist recovery
  before it is considered complete.
- Reducer projects live Kubernetes resources and publishes readiness before
  queries claim exact truth.
- Tests cover discovery, pagination, watch recovery, forbidden resources,
  redaction, selector ambiguity, owner refs, and replay idempotency.

## References

- Kubernetes API overview:
  https://kubernetes.io/docs/reference/using-api/
- Kubernetes API concepts:
  https://kubernetes.io/docs/reference/using-api/api-concepts/
- Kubernetes generated API reference:
  https://kubernetes.io/docs/reference/generated/kubernetes-api/
- Kubernetes object names and UIDs:
  https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
- Kubernetes labels and selectors:
  https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
- Kubernetes owners and dependents:
  https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
- Kubernetes RBAC reference:
  https://kubernetes.io/docs/reference/access-authn-authz/rbac/
- Kubernetes RBAC good practices:
  https://kubernetes.io/docs/concepts/security/rbac-good-practices/
- `client-go`:
  https://github.com/kubernetes/client-go
