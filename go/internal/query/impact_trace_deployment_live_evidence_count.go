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
	// count is the SUM, across every distinct expected ArgoCD tracking-id,
	// of the MAX observed ready_replicas among that tracking-id's matched
	// facts. Nil is never valid here; fetchWorkloadLiveInstanceSummary
	// returns a nil *liveInstanceSummary instead of a non-nil summary with a
	// nil count.
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
// tracking-id, take the MAX observed ready_replicas across that tracking-id's
// matched facts (a Deployment's annotations are copied onto its ReplicaSets,
// so two matched facts commonly share one tracking-id; SUMming them would
// double-count the same running pods twice, since a Deployment's
// status.readyReplicas already equals its active ReplicaSet's -- MAX
// de-duplicates that controller-copy fan-out). The per-tracking-id maxima are
// then summed across distinct tracking-ids, since two different tracking-ids
// genuinely are two different observed workloads. A matched fact with a nil
// ReadyReplicas contributes nothing to its tracking-id's max (absent is never
// coerced to zero); when every matched fact across every tracking-id is nil,
// the whole probe returns a nil summary (no countable observation), never a
// fabricated 0. A present ready_replicas of 0 (a real scaled-to-zero
// observation) IS counted and reported.
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

	trackingIDs := expectedArgoCDTrackingIDs(controllers, k8sResources)
	span.SetAttributes(attribute.Int("eshu.expected_tracking_id_count", len(trackingIDs)))
	if len(trackingIDs) == 0 {
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
	for _, trackingID := range trackingIDs {
		filter := KubernetesPodTemplateFilter{
			TrackingID:           trackingID,
			ImageRefs:            imageRefs,
			AllScopes:            !access.scoped(),
			AllowedRepositoryIDs: access.grantedRepositoryIDs(),
			AllowedScopeIDs:      access.grantedScopeIDs(),
		}
		matches, err := h.KubernetesPodTemplates.ListLiveIdentityMatches(ctx, filter)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}

		var maxReady *int32
		for _, match := range matches {
			if match.ReadyReplicas == nil {
				continue
			}
			if maxReady == nil || *match.ReadyReplicas > *maxReady {
				ready := *match.ReadyReplicas
				maxReady = &ready
			}
		}
		if maxReady != nil {
			observed = true
			total += *maxReady
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
