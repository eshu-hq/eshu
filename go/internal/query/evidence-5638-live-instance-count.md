# Evidence: read-side live_instance_count + environment-unbound state (#5638)

Hot-path evidence for the `trace_deployment_chain` `live_instance_count` /
`live_instance_environments` additions (`impact_trace_deployment_live_evidence_count.go`,
`impact_trace_deployment_live_evidence_store.go`,
`impact_trace_deployment_resources.go`).

No-Regression Evidence: this change is read-side only — no collector, schema,
materialization, graph-write, or index change. The new Postgres read
(`ListLiveIdentityMatches` /
`listLiveKubernetesPodTemplateIdentityMatchesQuery`) is the SELECT-columns
sibling of the already-shipped `HasLiveIdentityMatch` identity probe: the exact
same ACTIVE-generation join (`scope.active_generation_id`,
`scope_generations.status = 'active'`), the same `is_tombstone = FALSE`
predicate, the same `$4 OR image_refs ?| $5` optional image-ref intersection,
and the same #5167 scoped-grant variant — it only swaps `LIMIT 1` for a bound
`LIMIT $N` capped at `serviceStoryItemLimit` (50) and selects five columns
instead of a bare existence bit, so it reuses that query's existing index/scope
shape rather than introducing a new plan. Tracking-id fan-out is bounded by
`expectedArgoCDTrackingIDs` (already capped at `serviceStoryItemLimit`), and the
environment lookup is one bounded `MATCH`-only Cypher read per distinct
(cluster_id, namespace) pair — pairs are the distinct namespaces of the
identity-matched facts, typically one, anchored on the `KubernetesNamespace`
`{cluster_id, namespace}` identity the reducer already writes. No new write,
lock, transaction, worker, or hot-path graph write is added, so there is no
throughput or latency regression surface to measure; store/graph errors
log-and-continue and never block or slow the trace. Correctness of the
aggregation (MAX-per-tracking-id then SUM across distinct tracking-ids;
absent ready_replicas ≠ 0; count never feeds the deployment-truth tier) is
proven by the unit tests in `impact_trace_deployment_live_evidence_count_test.go`
and end-to-end by the B-12 golden snapshot pin (`live_instance_count = 3`, the
MAX-not-SUM value, plus the environment-unbound entry) driven through the full
`scripts/verify-golden-corpus-gate.sh` replay.

Observability Evidence: adds one child span `impact.live_instance_count`
(`queryHandlerTracer`, sharing the existing Postgres/graph query
instrumentation of the underlying reads) with attributes
`eshu.expected_tracking_id_count`, `eshu.live_instance_count_observed`,
`eshu.live_instance_count`, and `eshu.live_instance_count_skip_reason`
(`no_identity_binding` / `store_unwired_or_no_image_refs` /
`scoped_caller_no_grants`) so an operator can see, per trace, whether the count
probe ran, why it skipped, and what it observed. The span is recorded in the
telemetry-coverage contract doc (`docs/public/observability/telemetry-coverage.md`).
