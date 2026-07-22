// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Observability Evidence: fetchWorkloadLiveEvidence reads the same bounded,
// instrumented KubernetesPodTemplateStore.HasLiveIdentityMatch query used to
// serve the identity-bound pod-template read, so the underlying Postgres
// read is covered by eshu_dp_postgres_query_duration_seconds and
// postgres.query spans. That coverage does NOT extend to the tier-promotion
// DECISION this probe makes (config_only -> runtime_confirmed): the generic
// per-route "query.*" span and eshu_dp_api_request_duration_seconds
// (go/internal/query/handler_tracing.go, go/internal/query/request_metrics.go)
// record that trace_deployment_chain ran, not why a specific workload's tier
// did or did not flip. fetchWorkloadLiveEvidence therefore starts its own
// "impact.live_evidence_probe" child span (queryHandlerTracer, shared with
// handler_tracing.go) carrying the image_ref count probed, the number of
// expected ArgoCD tracking-ids computed, whether an identity-bound match was
// found, and the tier that decision implies -- an operator can read that
// span at 3 AM to see exactly why a workload's deployment truth tier changed
// (or did not) without reproducing the trace call (#5471 review round 2 P1,
// #5471 codex P1 identity-binding fix).

package query

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// fetchWorkloadLiveEvidence returns true when the Postgres active-fact read
// model contains at least one ACTIVE kubernetes_live.pod_template fact whose
// argocd.argoproj.io/tracking-id annotation matches an identity the traced
// workload's OWN declared ArgoCD Application + k8sResources would carry
// (expectedArgoCDTrackingIDs), and whose declared image_refs intersect the
// workload's config-declared imageRefs.
//
// #5471 codex P1: the pre-fix probe promoted on an image-digest-only exact
// kubernetes correlation match, with no binding to the traced workload's OWN
// declared identity. Two workloads sharing a base image (a shared digest)
// could therefore have workload A promote to runtime_confirmed on workload
// B's live row. Binding the match to the ArgoCD tracking-id the traced
// workload's live object would ACTUALLY carry closes that hole: a shared
// digest is no longer sufficient, an identity-bound live pod is required.
//
// The probe is fail-closed at the identity layer, not only at the store
// layer: when the traced workload has no argocd_application controller (or
// its declared k8sResources carry no computable kind+name), there is no
// ArgoCD identity to bind live evidence to at all, and the probe returns
// false WITHOUT querying the store -- a shared digest alone can never
// promote a workload with no resolvable declared identity. The store read
// itself is bounded: one bounded LIMIT-1 existence read per expected
// tracking-id (capped at expectedArgoCDTrackingIDsQueryLimit). The store is
// nil-safe (returns false when unwired). Access scoping is applied through
// the caller's repository grant set, mirroring listCorrelations.
func (h *ImpactHandler) fetchWorkloadLiveEvidence(
	ctx context.Context,
	controllers []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	access repositoryAccessFilter,
) (bool, error) {
	if h == nil {
		return false, nil
	}

	ctx, span := queryHandlerTracer.Start(ctx, "impact.live_evidence_probe")
	defer span.End()
	span.SetAttributes(attribute.Int("eshu.image_ref_count", len(imageRefs)))

	trackingIDs := expectedArgoCDTrackingIDs(controllers, k8sResources)
	span.SetAttributes(attribute.Int("eshu.expected_tracking_id_count", len(trackingIDs)))
	if len(trackingIDs) == 0 {
		// Core fail-closed fix: no ArgoCD identity was resolvable for the
		// traced workload, so the store is never queried -- the workload
		// stays config_only for lack of a declared identity to bind, never
		// promoted on a shared digest alone.
		span.SetAttributes(
			attribute.String("eshu.live_evidence_skip_reason", "no_identity_binding"),
			attribute.Bool("eshu.live_evidence_matched", false),
		)
		return false, nil
	}

	if h.KubernetesPodTemplates == nil || len(imageRefs) == 0 {
		span.SetAttributes(
			attribute.String("eshu.live_evidence_skip_reason", "store_unwired_or_no_image_refs"),
			attribute.Bool("eshu.live_evidence_matched", false),
		)
		return false, nil
	}
	// #5167 access-scoping discipline, mirroring listCorrelations
	// (go/internal/query/kubernetes.go:108-122): a scoped caller with no
	// granted repositories never queries the store.
	if access.empty() {
		span.SetAttributes(
			attribute.String("eshu.live_evidence_skip_reason", "scoped_caller_no_grants"),
			attribute.Bool("eshu.live_evidence_matched", false),
		)
		return false, nil
	}

	for _, trackingID := range trackingIDs {
		filter := KubernetesPodTemplateFilter{
			TrackingID:           trackingID,
			ImageRefs:            imageRefs,
			AllScopes:            !access.scoped(),
			AllowedRepositoryIDs: access.grantedRepositoryIDs(),
			AllowedScopeIDs:      access.grantedScopeIDs(),
		}
		matched, err := h.KubernetesPodTemplates.HasLiveIdentityMatch(ctx, filter)
		if err != nil {
			// Store errors fail closed to the config tier -- mirroring how
			// the handler treats enrichment fetch errors: log the error and
			// continue with the next tracking-id or return false. A
			// transient Postgres failure must not 500 the whole deployment
			// trace.
			span.RecordError(err)
			span.SetAttributes(attribute.Bool("eshu.live_evidence_matched", false))
			return false, err
		}
		if matched {
			span.SetAttributes(
				attribute.Bool("eshu.live_evidence_matched", true),
				attribute.String("eshu.deployment_truth_tier", string(truth.TierRuntimeConfirmed)),
			)
			return true, nil
		}
	}
	span.SetAttributes(attribute.Bool("eshu.live_evidence_matched", false))
	return false, nil
}
