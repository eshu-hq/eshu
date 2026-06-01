# Design: Kubernetes live-workload canonical node + readiness phase — prerequisite for the #388 graph edge

**Status:** NEEDS PRINCIPAL REVIEW — new canonical node type + keyspace +
readiness phase + a source-side join index (`risk:schema`, graph-write). No
auto-merge. This slice introduces a NEW canonical graph node label and
keyspace; a mis-keyed or fabricated canonical node is an accuracy failure, so
the node-identity, keyspace, and label decisions below are the principal-review
focus.

**Related:** #388 (reducer Kubernetes Git/runtime correlation and drift read
model), the merged #388 PR1 fact-only correlation domain
(`docs/internal/design/388-kubernetes-correlation-readmodel.md`,
`go/internal/reducer/kubernetes_correlation*.go`), the merged Kubernetes live
collector (`go/internal/collector/kuberneteslive/`), and the #805 AWS
relationship edge materialization
(`docs/internal/aws-relationship-edge-materialization-design.md`) whose
node→phase→gated-edge pattern this slice mirrors.

## Why

The #388 live-workload graph edge (a `RUNS` / `DRIFTS_FROM` edge between a live
Kubernetes workload and its deployment-source image identity) was correctly
blocked because two graph pieces did not exist:

1. A live Kubernetes workload has **no canonical graph node** to anchor the edge
   endpoint. The merged #388 PR1 correlation domain emits provenance-only
   reducer facts; it writes no node.
2. The source image side carries a **raw digest**, while the canonical
   `ContainerImage` / OCI manifest node is keyed by a descriptor **uid**, so the
   edge's source endpoint cannot resolve a digest to a node without a bridge.

This slice builds those missing prerequisites so the later edge PR (#388 PR3)
can be a clean mirror of #805's edge materialization: load facts → resolve both
endpoints against bounded in-memory indexes → write idempotent edges, gated on a
readiness phase. **This slice writes the node and publishes the phase; it does
NOT write the edge.** The edge is #388 PR3.

## Scope of this slice

1. **Live-workload canonical node.** A reducer domain
   (`DomainKubernetesWorkloadMaterialization`) that materializes
   `kubernetes_live.pod_template` facts into canonical `KubernetesWorkload`
   graph nodes, plus the backend-neutral `KubernetesWorkloadNodeWriter`
   (`go/internal/storage/cypher`) and the `KubernetesWorkload` uid uniqueness
   constraint + lookup indexes in the Go-owned graph schema.
2. **Readiness phase.** The handler publishes
   `GraphProjectionPhaseCanonicalNodesCommitted` on a NEW keyspace
   `GraphProjectionKeyspaceKubernetesWorkloadUID` after the node write succeeds,
   through the existing durable `graph_projection_phase_state` publisher — the
   same mechanism the AWS relationship edge gates on.
3. **Digest→uid join index.** `SourceImageDigestJoinIndex`
   (`go/internal/reducer`) resolves an image `SourceDigest` to the
   `ContainerImage` / OCI manifest node uid over OCI manifest / image-index /
   descriptor facts, so the later edge's source endpoint resolves.

Deferred (stated so reviewers do not expect them here):

- **#388 PR3** — the gated `RUNS` / `DRIFTS_FROM` edge between the
  `KubernetesWorkload` node and its deployment-source identity. It will gate on
  the keyspace/phase this slice publishes, add the per-edge-domain clause to the
  durable claim/blockage gate (see "Durable gate" below), and resolve its source
  endpoint via `SourceImageDigestJoinIndex`.

## Node-identity / keyspace / label decisions (principal-review focus)

These mirror the shipped #805 CloudResource node and the settled #388 collector
schema. None of them invent a key.

| Decision | Value | Why (not invented) |
| --- | --- | --- |
| **Node identity / `uid`** | the collector-emitted `object_id` | `object_id` is `kuberneteslive.ObjectIdentity.ObjectID()`, a deterministic StableID over `(cluster_id, api_group, version, resource, namespace, name, metadata.uid)`. It is already the `pod_template` fact's stable key and the value #388 PR1's correlation index reads. The raw Kubernetes `metadata.uid` is carried as the `workload_uid` **property only**, never the node identity, because `object_id` already folds `metadata.uid` into its tuple and a delete/recreate of the same name yields a new `object_id`. |
| **Keyspace** | `kubernetes_workload_uid` | Mirrors `cloud_resource_uid` (#805). A per-node-type keyspace lets the later edge slice gate on exactly the node family it joins. |
| **Node label** | `KubernetesWorkload` (new canonical label) | A new label distinct from the parser-sourced `K8sResource` (which is parsed-manifest IaC, not a live observation). Added to `uidConstraintLabels` so it receives the `kubernetes_workload_uid_unique` constraint and the `nornicdb_kubernetes_workload_uid_lookup` index automatically. |
| **Readiness phase** | reuse `GraphProjectionPhaseCanonicalNodesCommitted` on the new keyspace | Identical to how AWS resource materialization publishes on `cloud_resource_uid`; the edge slice's readiness lookup resolves it with zero store changes (the phase store is keyspace-generic). |
| **Domain** | `DomainKubernetesWorkloadMaterialization`, additive | Registered only when `KubernetesWorkloadNodeWriter` + `FactLoader` are wired, so an intent is never silently dropped by a handler that cannot write. |

## Idempotency, concurrency, ordering

- The node write is `MERGE (w:KubernetesWorkload {uid: row.uid})` on the
  `object_id` only, mutable properties `SET` separately. Duplicate facts
  (retries, overlapping snapshots) and reducer reprojection converge on one node
  via the uid uniqueness constraint — idempotent under concurrent execution. The
  conflict key is the per-node `object_id`; there is no contended write, so this
  is **not** a "serialization is not a fix" case — no worker-count reduction,
  single-threaded drain, or batch size 1 is introduced.
- `ExtractKubernetesWorkloadNodeRows` deduplicates by `object_id` and sorts by
  uid, so the batched write is byte-stable across retries and reprojections.
- The readiness phase is published **only after** the node write succeeds (or is
  a legitimate no-op for an empty generation). Publishing before a successful
  write would let the edge slice resolve edges against nodes that never
  committed; not publishing on an empty generation would block the edge slice
  forever. Both invariants are covered by tests.

## Edge cases (covered by tests)

- **Empty** — no pod-template facts → zero rows, no write, phase still published.
- **Tombstone** — a tombstoned pod template (deleted workload no longer running)
  materializes no node; a tombstone-only source digest does not resolve to a
  live node uid.
- **Stale / active override** — an active source-digest observation overrides a
  tombstone for the same digest regardless of envelope order.
- **Partial / missing identity** — a pod template without an `object_id`, or an
  OCI fact without a resolvable descriptor identity, is dropped, never
  fabricated.
- **Duplicate** — duplicate facts converge on one node / one index entry.

## Digest→uid join index

`SourceImageDigestJoinIndex` is the source-endpoint resolver. The canonical OCI
manifest / image-index / descriptor node uid is the fact's `descriptor_id`
(`oci-descriptor://<repository>@<digest>`), or — when absent — the same
deterministic derivation the registry projector uses
(`ociDescriptorUID(repository_id, digest)`), so the index resolves to the node
the projector actually wrote rather than a fabricated id. Build is O(M) over OCI
facts with O(1) map inserts; resolution is O(1) per edge (the #805 §5.1
bounded-join shape), so the later edge has no per-edge graph round trip and no
N+1. Tag observations are excluded: a tag is mutable evidence, not a
digest-addressed node identity.

## Durable gate (why no gate-query change here)

The AWS relationship edge is fenced in the durable Postgres claim/blockage gate
(`go/internal/storage/postgres/reducer_queue_batch.go`,
`status_blockage.go`) by a clause keyed on the *edge domain*
(`aws_relationship_materialization`) gating on the *node keyspace*
(`cloud_resource_uid`). The K8s edge domain does not exist yet (it is #388 PR3),
so adding a gate clause now would reference a non-existent domain. This slice
therefore publishes the `kubernetes_workload_uid` phase and adds **no** gate
clause; #388 PR3 adds the `kubernetes_*_edge_materialization` gate clause and the
`ReadinessLookup` wiring exactly as #805 PR2 did. The phase store and readiness
lookup are keyspace-generic, so the published phase is already resolvable.

## Performance and observability

Benchmark Evidence: `go test ./internal/storage/cypher -run '^$' -bench
BenchmarkKubernetesWorkloadNodeWriter -benchmem -benchtime=200x` on darwin/arm64
(Apple M3 Pro), no-op group executor: `5,000` node rows at the default
`500`/UNWIND ran `2.73 ms/op`, `6.33 MB/op`, `25,069 allocs/op` — within noise
of the proven `BenchmarkCloudResourceNodeWriter` baseline (`2.76 ms/op`,
`6.33 MB/op`, `25,068 allocs/op`) on the same machine and input shape, because
the writer reuses the identical UNWIND-batched MERGE-on-uid shape. The
reducer-side projection and join index ran `5.13 ms/op` (node-row extraction,
5,000 workloads) and `0.93 ms/op` (digest→uid index build, 5,000 manifests):
both bounded, no per-row graph round trip.

No-Regression Evidence: the node-write path adds no new per-row cost over the
established CloudResource node writer; the write is bounded by
`ceil(W/batchSize)` statements. The new uid uniqueness constraint and two lookup
indexes back the MERGE and per-cluster/namespace reads; the write-amplification
is one uid index entry per node, identical to every other canonical uid label.

Observability Evidence: the new `eshu_dp_kubernetes_workload_nodes_total` counter
(dimension `domain`), the `kubernetes workload materialization completed`
structured log with per-stage durations and node count, and the
InstrumentedExecutor's `eshu_dp_neo4j_query_duration_seconds` /
`eshu_dp_neo4j_batch_size` on each `phase=kubernetes_workload` /
`label=KubernetesWorkload` statement let an operator see live-workload node
throughput and graph-write cost, and spot a generation that committed zero
nodes, at 3 AM.

## Open items for principal review

1. **Node identity.** This slice keys the `KubernetesWorkload` node on the
   collector-emitted `object_id` and carries `metadata.uid` as the `workload_uid`
   property only. Confirm `object_id` (not raw `metadata.uid`) is the intended
   canonical node identity.
2. **Label fork.** A new `KubernetesWorkload` label distinct from the
   parser-sourced `K8sResource` label. Confirm live runtime workloads should be a
   distinct canonical label rather than overloading `K8sResource`.
3. **Keyspace / phase shape.** A new `kubernetes_workload_uid` keyspace reusing
   `canonical_nodes_committed`. Confirm this shape before #388 PR3 locks it into
   the durable gate (changing it later is a `risk:schema` migration).
4. **Digest→uid identity reproduction.** The join index reproduces the registry
   projector's `ociDescriptorUID` derivation in the reducer package (the
   projector helper is unexported). Confirm this is the right seam versus
   exporting the projector helper or querying already-materialized manifest
   nodes.
