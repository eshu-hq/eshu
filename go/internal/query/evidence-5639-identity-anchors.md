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
