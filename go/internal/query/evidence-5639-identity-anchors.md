# Evidence: declared kind+namespace+name identity anchor (#5639)

Hot-path evidence for widening the live-evidence identity seam
(`impact_trace_deployment_live_evidence_identity.go`,
`impact_trace_deployment_live_evidence_identity_declared.go`,
`impact_trace_deployment_live_evidence.go`,
`impact_trace_deployment_live_evidence_count.go`,
`impact_trace_deployment_live_evidence_store.go`,
`impact_trace_deployment_live_evidence_store_declared.go`) beyond ArgoCD
tracking-ids to a declared kind+namespace+name object anchor.

Scope: this PR lands the declared kind+namespace+name anchor only. Helm
annotation anchors (`meta.helm.sh/*`) remain out of scope -- the collector's
`identityAnnotationAllowlist`
(`go/internal/collector/kuberneteslive/clientgo/client.go`) does not capture
`meta.helm.sh/release-name`/`meta.helm.sh/release-namespace`, only
`argocd.argoproj.io/tracking-id`, `app.kubernetes.io/instance`, and
`app.kubernetes.io/name`. Owner-reference-based anchors are also out of scope
-- they are a separate fact family from the declared k8sResources this PR
reads. Both are noted here for follow-up, not implemented.

No-Regression Evidence: the ArgoCD tracking-id anchor path is unchanged --
`hasLiveTrackingIDIdentityMatch`/`listLiveTrackingIDIdentityMatches` are the
pre-#5639 `HasLiveIdentityMatch`/`ListLiveIdentityMatches` bodies renamed
verbatim, still receiving `hasLiveKubernetesPodTemplateIdentityQuery` and its
scoped/list siblings unchanged. The new declared-object variants
(`hasLiveKubernetesPodTemplateDeclaredObjectIdentityQuery`,
`listLiveKubernetesPodTemplateDeclaredObjectIdentityMatchesQuery`, and their
`#5167` scoped siblings) reuse the identical ACTIVE-generation join
(`scope.active_generation_id`, `scope_generations.status = 'active'`), the
same `is_tombstone = FALSE` predicate, and the same optional image-ref
intersection (`$N OR image_refs ?| $N`) as the ArgoCD variants -- they only
swap the identity predicate from the ArgoCD annotation equality to
`group_version_resource`/`namespace`/`name` equality, so no new query plan
shape is introduced. `resolveLiveIdentityAnchors` is the single shared seam
both `fetchWorkloadLiveEvidence` and `fetchWorkloadLiveInstanceSummary`
consume (via `liveIdentityAnchorFilter`), so the anchor set never forks
between the existence probe and the count aggregation. The combined anchor
list is capped at the existing `expectedArgoCDTrackingIDsQueryLimit`
(`serviceStoryItemLimit`), truncating only the weaker declared-object tail,
so the store call fan-out bound is unchanged. No new write, lock,
transaction, worker, or graph call is added.

The #5471 codex P1 hazard (a shared image digest alone must never promote
workload A on workload B's live pod) is preserved and extended: every new
anchor is a genuine identity binding (exact
`group_version_resource`+`namespace`+`name` equality) paired with the
existing image-ref intersection, never a digest-alone or labels-alone match.
`TestFetchWorkloadLiveEvidenceDistinctWorkloadsSharedDigest` (the
non-negotiable ArgoCD-anchor hazard test) stays green with updated call-count
assertions for the wider per-trace anchor fan-out.
`TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorSharedDigestDistinctWorkloadsNoPromotion`
is the declared-object-anchor clone of that hazard test, RED-proven by
temporarily dropping the name/GVR equality from the test's stub match key.
`TestFetchWorkloadLiveEvidenceDeclaredObjectAnchorNamespaceGuard` proves the
same-kind-different-namespace guard, RED-proven by temporarily dropping the
namespace equality. Fail-closed absence (unmappable kind, empty namespace or
name on either side, or no anchor of any kind at all) is proven in
`impact_trace_deployment_live_evidence_identity_declared_test.go` and the
renamed
`TestFetchWorkloadLiveEvidenceNoAnchorOfAnyKindNeverQueriesStore`/`TestFetchWorkloadLiveInstanceSummaryNoAnchorOfAnyKindNeverQueriesStore`.

Verification run: `cd go && go test ./internal/query/... ./internal/truth/...
-count=1 -race` (5346 tests, 2 packages, all green);
`golangci-lint run ./internal/query/...` against the CI-pinned v2.12.2
binary (built via `tools/golangci-lint-filelength`) reports no issues.

Observability Evidence: the existing `impact.live_evidence_probe` span
(`queryHandlerTracer`, unchanged instrumentation contract) gains one new
attribute, `eshu.live_evidence_anchor_kind`, set only on a match, naming
which anchor family (`argocd_tracking_id` or `declared_object`) actually
promoted the workload -- an operator can now distinguish an ArgoCD-anchored
promotion from a declared-object-anchored one without reproducing the trace
call. No other span, metric, or log key changes; the sibling
`impact.live_instance_count` span keeps its existing attribute set
unchanged. The count aggregation itself dedups matched live objects by
`cluster_id`+`object_id` ACROSS anchor families, not per anchor: a workload
matched by both an ArgoCD tracking-id anchor and a declared-object anchor is
counted once, not summed twice, because the two anchors can legitimately
observe the same live fact through two independent identity paths (P1 fix,
`TestFetchWorkloadLiveInstanceSummaryArgoCDAndDeclaredObjectAnchorsNoDoubleCount`).
The span is recorded in the telemetry-coverage contract doc
(`docs/public/observability/telemetry-coverage.md`).

Performance Evidence: codex flagged a P1 on
`impact_trace_deployment_live_evidence_store_declared.go:34` -- the
declared-object anchor queries filter `fact_records` on
`payload->>'group_version_resource'`, `payload->>'namespace'`, and
`payload->>'name'` for `fact_kind = 'kubernetes_live.pod_template'` with no
supporting index, so the planner falls back to
`fact_records_scope_generation_idx` and filters every active pod-template
fact per scope in memory. Proven on a throwaway Postgres 16 container
(`eshu-idx5639-pg`, isolated port 35432, all migrations replayed through 074)
seeded with 40 active scopes/generations and 100,003
`kubernetes_live.pod_template` facts (100,000 bulk rows spread across the 40
scopes plus 3 rows sharing one rare target
`group_version_resource`+`namespace`+`name` tuple). `EXPLAIN (ANALYZE,
BUFFERS)` on the `has*` query shape (warm, 2nd run):

- Worst case, target tuple absent (LIMIT 1 cannot short-circuit, must
  exhaust every active scope's pod-template partition): OLD --
  `Bitmap Heap Scan` on `fact_records` via `fact_records_scope_generation_idx`
  filtering 2,500 rows x 40 scopes, `Buffers: shared hit=100244`,
  `Execution Time: 62.566 ms`. NEW -- `Index Scan` on
  `fact_records_kubernetes_live_pod_template_object_idx`, the
  `ingestion_scopes`/`scope_generations` joins never executed,
  `Buffers: shared hit=3`, `Execution Time: 0.026 ms` (~2,400x faster,
  ~33,000x fewer buffer hits).
- Existing rare target: OLD -- `Index Scan` on
  `fact_records_scope_generation_idx` with `Rows Removed by Filter: 2500`,
  `Buffers: shared hit=2508`, `Execution Time: 3.024 ms`. NEW -- `Index Scan`
  on the new index, `Buffers: shared hit=8`, `Execution Time: 0.042 ms`
  (~72x faster).

Row-equivalence: the `has*` shape returns the same existence result
(`rows=1`/`rows=0`) before and after for both the existing-target and
missing-target cases; the `list*` shape returns byte-identical rows in
identical `object_id` order before and after
(`cluster-1/obj-target-1`, `cluster-2/obj-target-2`, `cluster-3/obj-target-3`,
all `ready_replicas=3`), proven by dropping and recreating the candidate
index against the same seeded data. Migration:
`go/internal/storage/postgres/migrations/075_kubernetes_live_pod_template_object_index.sql`.
