# Evidence: read-side live_instance_count (#5638)

Hot-path evidence for the `trace_deployment_chain` `live_instance_count`
addition (`impact_trace_deployment_live_evidence_count.go`,
`impact_trace_deployment_live_evidence_store.go`,
`impact_trace_deployment_resources.go`).

The env-state companion (`live_instance_environments`, a per-pair
`KubernetesNamespace {cluster_id, namespace}` Cypher lookup) was descoped from
this change: `KubernetesNamespace` is indexed only on `uid`, so that lookup was
a whole-graph scan `TestProductionQueryplanProfilesRejectWholeGraphScans`
correctly rejects. The count below is fully computed from Postgres before any
graph call, so it ships on its own; the env-state read moves to the index
follow-up (#5651) alongside the required `KubernetesNamespace(cluster_id,
namespace)` index and its own PROFILE proof.

No-Regression Evidence: this change is read-side only — no collector, schema,
materialization, graph-write, or index change. The new Postgres read
(`ListLiveIdentityMatches` /
`listLiveKubernetesPodTemplateIdentityMatchesQuery`) is the SELECT-columns
sibling of the already-shipped `HasLiveIdentityMatch` identity probe: the exact
same ACTIVE-generation join (`scope.active_generation_id`,
`scope_generations.status = 'active'`), the same `is_tombstone = FALSE`
predicate, the same `$4 OR image_refs ?| $5` optional image-ref intersection,
and the same #5167 scoped-grant variant — it only swaps `LIMIT 1` for a bound
`LIMIT $N` capped at `serviceStoryItemLimit` (50) and selects the observed
columns instead of a bare existence bit, so it reuses that query's existing
index/scope shape rather than introducing a new plan. Tracking-id fan-out is
bounded by `expectedArgoCDTrackingIDs` (already capped at
`serviceStoryItemLimit`). No new write, lock, transaction, worker, graph call,
or hot-path graph write is added, so there is no throughput or latency
regression surface to measure; store errors log-and-continue and never block
or slow the trace. Correctness of the aggregation (MAX-per-tracking-id then
SUM across distinct tracking-ids; absent ready_replicas ≠ 0; count never feeds
the deployment-truth tier) is proven by the unit tests in
`impact_trace_deployment_live_evidence_count_test.go` and end-to-end by the
B-12 golden snapshot pin (`live_instance_count = 3`, the MAX-not-SUM value)
driven through the full `scripts/verify-golden-corpus-gate.sh` replay.

Observability Evidence: adds one child span `impact.live_instance_count`
(`queryHandlerTracer`, sharing the existing Postgres/graph query
instrumentation of the underlying reads) with attributes
`eshu.expected_tracking_id_count`, `eshu.live_instance_count_observed`,
`eshu.live_instance_count`, and `eshu.live_instance_count_skip_reason`
(`no_identity_binding` / `store_unwired_or_no_image_refs` /
`scoped_caller_no_grants`) so an operator can see, per trace, whether the count
probe ran, why it skipped, and what it observed. The span is recorded in the
telemetry-coverage contract doc (`docs/public/observability/telemetry-coverage.md`).
