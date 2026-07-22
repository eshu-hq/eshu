// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Observability Evidence: fetchWorkloadLiveInstanceSummary reuses the same
// bounded, instrumented KubernetesPodTemplateStore read
// (ListLiveIdentityMatches) fetchWorkloadLiveEvidence's HasLiveIdentityMatch
// sibling uses, so the underlying Postgres read is covered by the same
// eshu_dp_postgres_query_duration_seconds and postgres.query spans. Neither of
// that covers the AGGREGATION decision this probe makes (which matched facts
// contributed to the count, and why), so it starts its own
// "impact.live_instance_count" child span (queryHandlerTracer, shared with
// handler_tracing.go and impact_trace_deployment_live_evidence.go) carrying
// the expected tracking-id count, the resulting instance count, and whether an
// observation was found at all -- an operator can read that span at 3 AM to
// see exactly why a workload's live_instance_count is present, absent, or a
// particular value, mirroring the sibling live-evidence probe's telemetry
// contract (#5638).

package query

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

// liveInstanceSummary is the read-side result of fetchWorkloadLiveInstanceSummary:
// a total observed live instance count. A nil *liveInstanceSummary (returned
// alongside a nil error) means "no countable observation" -- the caller must
// omit the corresponding response fields entirely, never emit a fabricated
// zero.
type liveInstanceSummary struct {
	// count is the SUM, across every distinct identity anchor (ArgoCD
	// tracking-id or declared object, #5639), of the MAX observed
	// ready_replicas among that anchor's matched facts, after dedupping any
	// live object matched by more than one anchor (cluster_id+object_id) so
	// the same running object is never counted twice. Nil is never valid
	// here; fetchWorkloadLiveInstanceSummary returns a nil
	// *liveInstanceSummary instead of a non-nil summary with a nil count.
	count int
}

// fetchWorkloadLiveInstanceSummary derives a read-side live_instance_count
// from the same identity-bound kubernetes_live.pod_template facts
// fetchWorkloadLiveEvidence probes for, via the ListLiveIdentityMatches
// row-returning sibling of HasLiveIdentityMatch. It shares
// fetchWorkloadLiveEvidence's exact fail-closed preamble (nil handler, no
// resolvable ArgoCD identity, an unwired store or no declared image refs, and
// a scoped caller with no grants all return a nil summary without querying
// the store) and reuses expectedArgoCDTrackingIDs verbatim -- the anchor set
// is a single shared seam, never forked between the two probes.
//
// Aggregation happens in Go, not SQL, so it stays testable: per distinct
// anchor, take the MAX observed ready_replicas across that anchor's matched
// facts (a Deployment's annotations are copied onto its ReplicaSets, so two
// matched facts commonly share one tracking-id; SUMming them would
// double-count the same running pods twice, since a Deployment's
// status.readyReplicas already equals its active ReplicaSet's -- MAX
// de-duplicates that controller-copy fan-out). The per-anchor maxima are then
// summed across distinct anchors, since two different anchors genuinely are
// two different observed workloads -- EXCEPT when resolveLiveIdentityAnchors
// produced both an ArgoCD tracking-id anchor and a declared-object anchor for
// the SAME workload (#5639): the same live fact can then be matched by both
// anchors' independent queries, so a cluster_id+object_id dedup spanning the
// whole anchor loop collapses that re-hit before it reaches the per-cluster
// MAX, keeping the stronger (ArgoCD) anchor's count authoritative and never
// double-counting the one running object as two. A matched fact with a nil
// ReadyReplicas contributes nothing to its anchor's max (absent is never
// coerced to zero) and is never marked as seen; when every matched fact
// across every anchor is nil, the whole probe returns a nil summary (no
// countable observation), never a fabricated 0. A present ready_replicas of 0
// (a real scaled-to-zero observation) IS counted and reported.
//
// A store error returns (nil, err): the caller MUST log and continue without
// setting the count response field, mirroring fetchWorkloadLiveEvidence's
// convention -- a count failure must not 500 the trace and must never touch
// _has_live_evidence, which this probe never writes to at all.
func (h *ImpactHandler) fetchWorkloadLiveInstanceSummary(
	ctx context.Context,
	controllers []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	access repositoryAccessFilter,
) (*liveInstanceSummary, error) {
	if h == nil {
		return nil, nil
	}

	ctx, span := queryHandlerTracer.Start(ctx, "impact.live_instance_count")
	defer span.End()

	anchors := resolveLiveIdentityAnchors(controllers, k8sResources)
	span.SetAttributes(attribute.Int("eshu.expected_tracking_id_count", len(anchors)))
	if len(anchors) == 0 {
		span.SetAttributes(attribute.String("eshu.live_instance_count_skip_reason", "no_identity_binding"))
		return nil, nil
	}
	if h.KubernetesPodTemplates == nil || len(imageRefs) == 0 {
		span.SetAttributes(attribute.String("eshu.live_instance_count_skip_reason", "store_unwired_or_no_image_refs"))
		return nil, nil
	}
	if access.empty() {
		span.SetAttributes(attribute.String("eshu.live_instance_count_skip_reason", "scoped_caller_no_grants"))
		return nil, nil
	}

	var total int32
	observed := false
	// seen dedups matched live objects by cluster_id+object_id ACROSS anchor
	// families (#5639 P1 fix): an ArgoCD-managed workload that also has a
	// mappable declared object gets BOTH a tracking-id anchor and a
	// declared-object anchor from resolveLiveIdentityAnchors, and the same
	// live kubernetes_live.pod_template fact is legitimately matched by
	// both (via the ArgoCD annotation, and via
	// group_version_resource/namespace/name) -- it is one running object
	// observed through two independent identity paths, not two objects.
	// ArgoCD anchors sort first, so the declared-object anchor's re-hit of
	// an already-counted object is the one skipped here, keeping the
	// stronger anchor's count authoritative.
	seen := map[string]struct{}{}
	for _, anchor := range anchors {
		filter := liveIdentityAnchorFilter(anchor, imageRefs, access)
		matches, err := h.KubernetesPodTemplates.ListLiveIdentityMatches(ctx, filter)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		// The same tracking-id can span multiple clusters (one ArgoCD
		// Application deployed to many clusters), each a separate running
		// deployment. Within one cluster a Deployment copies its tracking-id
		// annotation onto its ReplicaSets, so take the MAX ready_replicas per
		// cluster (dedup the controller copies), then SUM across distinct
		// clusters. A cross-cluster MAX would under-count (clusters at 3 and 5
		// would report 5, not 8).
		maxByCluster := map[string]int32{}
		for _, match := range matches {
			if match.ReadyReplicas == nil {
				continue
			}
			key := match.ClusterID + "|" + match.ObjectID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if cur, ok := maxByCluster[match.ClusterID]; !ok || *match.ReadyReplicas > cur {
				maxByCluster[match.ClusterID] = *match.ReadyReplicas
			}
		}
		for _, ready := range maxByCluster {
			observed = true
			total += ready
		}
	}

	if !observed {
		span.SetAttributes(attribute.Bool("eshu.live_instance_count_observed", false))
		return nil, nil
	}

	summary := &liveInstanceSummary{count: int(total)}
	span.SetAttributes(
		attribute.Bool("eshu.live_instance_count_observed", true),
		attribute.Int("eshu.live_instance_count", summary.count),
	)
	return summary, nil
}
