# Design: Kubernetes Live Correlation Read Model — PR1 (fact-only) toward #388

**Status:** PR1 (reducer correlation domain + durable fact writer) and PR2
(query/MCP read surface) **LANDED** on `main` (`go/internal/reducer/kubernetes_correlation.go`,
`go/internal/reducer/kubernetes_correlation_writer.go`, `go/internal/query/kubernetes.go`,
`go/internal/mcp/dispatch_kubernetes.go`). PR3 (gated graph edge) still pending.
**Related:** #388 (reducer Git/runtime correlation and drift read model),
#390 service-catalog correlation, #391 observability-coverage correlation
(the two shipped six-outcome templates this mirrors), #805 bounded-join
readiness pattern, and the merged Kubernetes live collector
(`go/internal/collector/kuberneteslive/`,
`docs/internal/design/388-kubernetes-live-collector.md`).

## Why

#388 was blocked on the Kubernetes live collector, which shipped on `main`
(PR #1055) with a deliberate three-fact-kind subset:
`kubernetes_live.{pod_template,relationship,warning}`. This PR builds the first
slice of the read model on **that settled schema**: a reducer domain that
correlates live Kubernetes workload evidence to the deployment-source image and
identity evidence already in the graph, and emits provenance-only
`reducer_kubernetes_correlation` facts with the six-outcome contract plus a
bounded drift classification.

This design has been implemented in three slices. PR1 (reducer domain + durable
fact writer) and PR2 (query/MCP read surface) have **landed** on `main`. PR3
(gated canonical graph edge) is still pending, exactly as #391 split its query
surface (PR2) and COVERS graph edge (PR3) out of its fact-only PR1.

## Scope of this PR

- A `kubernetes_correlation` reducer domain (`DomainKubernetesCorrelation`).
- A pure classifier `BuildKubernetesCorrelationDecisions(envelopes)` over fact
  envelopes (no I/O) so the six-outcome contract is table-test friendly, exactly
  like `BuildObservabilityCoverageDecisions` (#391) and
  `BuildServiceCatalogCorrelationDecisions` (#390).
- A bounded in-memory index that partitions one scope generation's
  `kubernetes_live.*` facts plus the cross-scope active deployment-source image
  evidence into workloads, image references, and identity edges.
- A `KubernetesCorrelationHandler` that loads the bounded fact kinds, classifies,
  emits a correlation-outcome counter, and writes durable provenance-only facts
  through the shared `canonicalReducerFactInsertQuery` path.
- A `reducer_kubernetes_correlation` reducer fact kind written idempotently by a
  `PostgresKubernetesCorrelationWriter`.
- The `eshu_dp_kubernetes_correlations_total{domain,outcome,drift_kind}` counter.

Implementation status (stated so reviewers see what landed vs what remains):

- **PR1 — LANDED** on `main`: reducer correlation domain
  (`go/internal/reducer/kubernetes_correlation.go`), durable fact writer
  (`go/internal/reducer/kubernetes_correlation_writer.go`), and the
  six-outcome classifier.
- **PR2 — LANDED** on `main`: the query/MCP read surface
  (`GET /api/v0/kubernetes/correlations` in `go/internal/query/kubernetes.go`,
  `list_kubernetes_correlations` in `go/internal/mcp/dispatch_kubernetes.go`)
  that distinguishes live evidence, exact ownership, drift, unknown, and stale
  states (issue #388 acceptance criterion 3).
- **PR3 — pending**: the gated canonical graph edge (a `RUNS_IMAGE` edge between
  the live workload node and its deployment-source identity), gated on a graph
  readiness phase exactly like #391 PR3's COVERS edge and #805's
  `GraphProjectionPhaseCanonicalNodesCommitted`. PR1 writes **no** graph edge.
- Additional `kubernetes_live` fact kinds (StatefulSet/DaemonSet/Job/CronJob,
  endpoints, RBAC, CRDs) — the collector shipped three; the read model is built
  on those three and extends when the collector does.

## Ownership and flow

`go/internal/reducer/` owns this domain (cross-source, cross-scope,
non-canonical-write in PR1). The flow is the settled correlation shape:

```text
kubernetes_live.* facts (cluster scope generation)
  + active deployment-source image facts (cross-scope)
  -> bounded in-memory index
  -> per-image-ref / per-identity-edge classification (six outcomes + drift)
  -> provenance-only reducer_kubernetes_correlation facts
  -> [PR2 query surface] [PR3 gated graph edge]
```

The live K8s facts come from the intent's scope generation via
`loadFactsForKinds`. The "other side" — the deployment-source image evidence
that lives in repo/cloud/registry scopes, not the cluster scope — is loaded via
an optional `ListActiveKubernetesCorrelationSourceFacts(ctx)` loader interface,
mirroring `ListActiveContainerImageIdentityFacts` (#container image identity) and
`ListActiveRepositoryFacts` (#390). This is the established cross-scope join
mechanism: bounded, no per-edge graph round trip, no N+1, idempotent.

## Inputs (settled schema only)

| Fact kind | Fields the read model reads (metadata only) |
| --- | --- |
| `kubernetes_live.pod_template` | `object_id`, `cluster_id`, `namespace`, `name`, `uid`, `selector`, `labels`, `service_account`, `image_refs` (raw image strings), per-container `{name,image,init}`. |
| `kubernetes_live.relationship` | `relationship_type` (`owner_reference`, `ingress_to_service`), `from_object_id`, `to_object_id`. |
| `kubernetes_live.warning` | `reason`, `resource_scope` — surfaced as evidence that a workload's correlation may be incomplete (partial list / ambiguous selector). |

Deployment-source image evidence (cross-scope, active) is read from the same
image-bearing fact kinds the container-image-identity domain already consumes —
OCI registry digest/tag observations, AWS image references, and content-entity
container images — reusing the **settled** `parseContainerImageRef` /
`buildContainerImageRegistryIndex` digest-and-tag index. No new source schema is
introduced; PR1 reuses what is already proven.

## Six-outcome contract (mirrors #390/#391 exactly)

Per live image reference and per workload identity edge:

- **exact** — a live image digest matches a deployment-source digest observation
  (digest-first join), OR an `owner_reference` edge proves structural ownership.
  Canonical truth, not provenance.
- **derived** — a live `repository:tag` reference resolves to exactly one
  deployment-source digest (weaker than a digest, deterministic). Provenance-only
  until the gated edge.
- **ambiguous** — a live `repository:tag` resolves to multiple source digests,
  OR a **label-selector** edge matches an owner set that cannot prove exact
  ownership. Candidates are recorded; **never promoted to exact**; the explicit
  non-promotion is recorded in `reason` and `non_promotion`.
- **unresolved** — the live image reference is valid but no active
  deployment-source evidence matches it in this generation (the live workload
  runs an image Eshu has no source for — a real "missing source" signal).
- **stale** — the live image resolves only to a **tombstoned** source digest
  observation (lingering/removed source — a drift signal).
- **rejected** — the signal is too weak to promote: an unparseable image ref, a
  ref with no repository, or a selector edge that names no concrete owner.
  Suppressed, never promoted.

### Selector ambiguity is preserved (issue #388 acceptance criterion 2)

A `kubernetes_live.relationship` whose `relationship_type` is a selector-derived
workload→pod edge is classified **ambiguous** whenever it cannot prove exact
ownership — for example, a selector that matches zero objects or multiple
unrelated owners. It is **never** promoted to `exact`. Only an
`owner_reference` edge (a structural Kubernetes owner reference, not a label
match) is `exact`. The decision records `non_promotion` describing why the
selector match was not promoted, satisfying "carry ambiguity evidence and
non-promotion cases."

## Drift classification (provenance-only, derived from outcome)

`drift_kind` is a bounded, closed enum derived deterministically from the
correlation outcome. It is provenance-only and asserts nothing the outcome does
not already prove:

- **in_sync** — `exact` or `derived` image match to an active source digest.
- **image_drift** — the live tag resolved, but to a source digest the source
  also reports as superseded/previous (the live cluster is running a digest the
  source no longer points the tag at).
- **missing_source** — `unresolved`: the live image has no deployment-source
  evidence at all.
- **stale_source** — `stale`: the only matching source evidence is tombstoned.
- **unknown** — `ambiguous` or `rejected`: drift cannot be asserted without
  inventing truth.

`drift_kind` is a metric label (closed enum) and a payload field; it is **not**
a graph write in PR1.

## Idempotency, concurrency, and ordering

- One reducer fact per decision, keyed on a stable identity tuple
  (`scope_id`, `generation_id`, `cluster_id`, `workload_object_id`,
  `image_ref` | identity edge key). Repeated writes converge on one `fact_id`
  via the shared `ON CONFLICT (fact_id) DO UPDATE` insert — safe under reducer
  retry and reprojection.
- The classifier sorts decisions deterministically before the batched write so
  retries and reprojections produce a byte-stable batch.
- Conflict domain is the per-scope-generation reducer intent. There is no shared
  mutable state across workers: the classifier is a pure function and the write
  is keyed per decision. No serialization workaround is introduced; this is not a
  "serialization is not a fix" case because there is no contended write — each
  decision's `fact_id` is its own conflict key.

## Empty / stale / partial / duplicate

- **Empty** — no K8s facts → zero decisions, no panic (covered by test).
- **Stale** — tombstoned source digest → `stale` / `stale_source`, never exact.
- **Partial** — a `kubernetes_live.warning` for a workload's resource scope is
  attached as evidence so a partial snapshot does not silently read as
  "no drift."
- **Duplicate** — duplicate live image refs within a workload de-duplicate by
  parsed ref; duplicate source digest observations de-duplicate in the index.

## Telemetry

`eshu_dp_kubernetes_correlations_total{domain,outcome,drift_kind}` — one counter,
all dimensions already registered except `drift_kind` (registered, reused) and a
new `AttrDriftKind` helper. It lets an operator answer at 3 AM: "which drift
class is growing — missing_source, image_drift, or stale_source — and is it an
ambiguous selector or a rejected weak ref?" No high-cardinality value (image ref,
object id, digest) is a metric label; those live in the durable fact payload.

`No-Observability-Change:` does not apply — this adds the counter above.
`No-Regression Evidence:` PR1 adds no graph write, no new table, no schema
migration, and no hot-path Cypher. The classifier is O(W·C) over live workloads
and their containers plus O(1) map lookups against the bounded source-digest
index (the #805 §5.1 bounded-join shape). The durable write reuses the existing
`canonicalReducerFactInsertQuery` path. The touched reducer path adds no measured
graph or queue cost.

## Open items for principal review

1. **Image-join precedence.** PR1 reuses the settled `parseContainerImageRef`
   digest-first → repository+tag precedence (the same one #container-image-identity
   shipped). Confirm the read model should mirror that precedence rather than
   introduce a K8s-specific one.
2. **Selector-ambiguity semantics.** PR1 treats every selector-derived edge that
   cannot prove exact ownership as `ambiguous` with `non_promotion`, and reserves
   `exact` for `owner_reference` edges only. Confirm this is the intended
   non-promotion boundary.
3. **Drift state model.** The five-value `drift_kind` enum
   (`in_sync` / `image_drift` / `missing_source` / `stale_source` / `unknown`) is
   derived purely from the correlation outcome. Confirm the enum shape before the
   query surface (PR2) and gated graph edge (PR3) lock it in — changing it later
   is a `risk:schema` migration.
4. **Cross-scope source loader.** PR1 loads the deployment-source side via an
   optional `ListActiveKubernetesCorrelationSourceFacts` loader, mirroring
   #container-image-identity. Confirm this is the right cross-scope seam vs. a
   query against already-materialized image identity facts.

## PR3 — gated RUNS_IMAGE graph edge (pending)

**Status:** PENDING — gated graph-write (`risk:schema`), no
auto-merge. This slice is a direct mirror of the shipped #805 AWS relationship
edge and #391 PR3 COVERS edge. It writes the final edge that closes the #388
chain.

### What it materializes

A canonical `(:KubernetesWorkload)-[:RUNS_IMAGE]->(:OciImage{Manifest,Index,Descriptor})`
edge, from **exact image-digest** correlation outcomes only:

- Source endpoint: the live `KubernetesWorkload` node (uid = the
  collector-emitted `object_id`) committed by
  `DomainKubernetesWorkloadMaterialization`.
- Target endpoint: the digest-addressed OCI source node resolved by
  `SourceImageDigestJoinIndex.ResolveDigestNode` (the digest→uid bridge the node
  slice added). The index now also returns the node's uid-indexed label so the
  edge MATCH anchors per node kind.
- Domain: `DomainKubernetesCorrelationMaterialization`
  (`kubernetes_correlation_materialization`), additive, registered only when a
  `KubernetesCorrelationEdgeWriter` is wired.
- Gate: the durable Postgres claim/blockage gate now carries a
  `kubernetes_correlation_materialization` clause gating on the
  `kubernetes_workload_uid` keyspace's `canonical_nodes_committed` phase — a
  **separate** clause from the AWS/COVERS `cloud_resource_uid` clause because the
  workload node family is a different keyspace.

### Relationship-type decision (principal-review focus)

The PR1/node design placeholder named the edge `RUNS` / `DRIFTS_FROM`. The
shipped edge uses **`RUNS_IMAGE`**: a single static token from a closed
vocabulary, validated in the writer, kept out of the MERGE property map so
NornicDB keeps its relationship hot path (#805 §5.3). `DRIFTS_FROM` is **not**
shipped — drift is already a provenance-only `drift_kind` on the PR1 facts, and a
`RUNS_IMAGE` edge to the *resolved* digest is the accurate canonical claim; a
drift edge would assert a second relationship the exact outcome does not prove.
Confirm `RUNS_IMAGE` (workload→resolved image) is the intended canonical
relationship type.

### owner_reference deferral (the surfaced fork)

The PR1 classifier produces a second `exact` outcome: a structural
`owner_reference` identity edge (`from_object_id`→`to_object_id`). This slice
does **not** edge it, by design:

1. Both endpoints are K8s `object_id`s, i.e. a workload→workload structural edge,
   not a workload→image edge. The node + digest-join prerequisites (#1105) were
   built specifically for the image edge, not for an object_id→object_id edge.
2. The `KubernetesWorkload` node is materialized from `pod_template` facts only,
   so the owner target (a ReplicaSet/Deployment owner) is **not guaranteed** to
   have a node. Anchoring an edge there would either fabricate a node (forbidden)
   or dangle. Per "never fabricate / else SKIP", it is excluded — the
   `owner_reference` exact decision carries no `SourceDigest`, so it is naturally
   filtered out of the image-edge extractor.

A future PR may add a separate `OWNS` workload→workload edge once the node slice
materializes non-pod-template workload owners. That is out of scope here. Confirm
this deferral.

### Idempotency / concurrency / no-dangle

- Idempotent on `(workload_uid, RUNS_IMAGE, source_uid)`; rows deduplicated and
  sorted so retries and reprojections produce a byte-stable batch. Conflict key
  is per-edge — not a "serialization is not a fix" case.
- An exact decision whose digest resolves no canonical node (tag-only evidence,
  which is not a digest-addressed node) is counted skipped, never written as a
  dangling edge.
- Evidence-scoped, edge-`scope_id`-filtered retract (endpoint nodes carry no
  reducer `scope_id`).

### Evidence

Performance, no-regression, and observability evidence (benchmarks, gate
commands, telemetry) live in `go/internal/reducer/README.md` (the
"Live-workload RUNS_IMAGE edge projection" section) and
`go/internal/storage/cypher/README.md` (the `KubernetesCorrelationEdgeWriter`
entry), which are the gate-tracked evidence files.
