# Evidence: live_instance_count truncation indicator (#5663)

Hot-path evidence for surfacing a truncation signal on the read-side
`live_instance_count` (`impact_trace_deployment_live_evidence_count.go`,
`impact_trace_deployment.go`, `impact_trace_deployment_resources.go`).

Problem: `ListLiveIdentityMatches` bounds every per-anchor read at
`serviceStoryItemLimit` (50). The aggregator `fetchWorkloadLiveInstanceSummary`
could not distinguish "exactly N matched objects" from "truncated at N, more
exist", so a workload whose anchor spans enough clusters x controller objects
silently under-counted. The fix flags the whole summary as truncated when any
anchor's read returns exactly `serviceStoryItemLimit` rows, so the count is
disclosed as a conservative lower bound (`deployment_fact_summary.live_instance_count_truncated`).

No-Regression Evidence: the change adds no query, no scan, and no new store
round-trip. `fetchWorkloadLiveInstanceSummary` already iterates the
already-fetched, already-bounded `ListLiveIdentityMatches` slice per anchor; the
truncation signal is a single `len(matches) == serviceStoryItemLimit` integer
comparison plus one boolean OR per anchor, evaluated on the slice the loop
already walks. The SQL query shapes (`hasLive*`, `listLive*` and their
declared-object siblings) are byte-unchanged -- no WHERE, LIMIT, ORDER BY, or
bound-parameter change -- so the existing `serviceStoryItemLimit` bound and the
#5639 `fact_records_kubernetes_live_pod_template_object_idx` index still back the
same reads at the same cost. The truncated flag rides only a non-nil summary
(the existing observed/nil-summary/present-zero semantics are unchanged), so no
workload that did not already run the count path pays anything new. Verified by
`cd go && go test ./internal/query/... -count=1 -race` (green) and the
golden-corpus gate (`go test ./cmd/golden-corpus-gate -count=1`, green). The B-12
snapshot's two `trace_deployment_chain` HTTP traces (the ArgoCD-anchor
`deployable-config` case and the #5639 declared-object `supply-chain-demo-db`
case) now pin `deployment_fact_summary.live_instance_count_truncated: false`
alongside their existing `live_instance_count` values (3 and 2), both far under
the `serviceStoryItemLimit` (50) cap, so a regression that drops or misserializes
the new field -- leaving `live_instance_count` silently unqualified -- fails the
gate at replay. The static snapshot pins are locked by
`TestGoldenSnapshotTraceDeploymentChainRequiresCanonicalPlatformIdentity` and
`TestGoldenSnapshotTraceDeploymentChainDeclaredObjectPinsLiveInstanceCount`.

Observability Evidence: the existing `impact.live_instance_count` span
(`queryHandlerTracer`) gains one new boolean attribute,
`eshu.live_instance_count_truncated`, set alongside the existing
`eshu.live_instance_count` attribute on an observed summary -- an operator can
now tell from the span alone whether a reported `live_instance_count` is exact
or a lower bound clipped at `serviceStoryItemLimit`, without reproducing the
trace. No new `eshu_dp_*` metric or pipeline stage is added, so the
`instruments.go` telemetry contract is unchanged; the new span attribute is
recorded in the X1 contract doc
(`docs/public/observability/telemetry-coverage.md`).
