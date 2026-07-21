// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Observability Evidence: fetchWorkloadLiveEvidence reads the same bounded,
// instrumented KubernetesCorrelationStore.ListKubernetesCorrelations query
// the list_kubernetes_correlations handler uses, so the underlying Postgres
// read is already covered by eshu_dp_postgres_query_duration_seconds and
// postgres.query spans. That coverage does NOT extend to the tier-promotion
// DECISION this probe makes (config_only -> runtime_confirmed): the generic
// per-route "query.*" span and eshu_dp_api_request_duration_seconds
// (go/internal/query/handler_tracing.go, go/internal/query/request_metrics.go)
// record that trace_deployment_chain ran, not why a specific workload's tier
// did or did not flip. fetchWorkloadLiveEvidence therefore starts its own
// "impact.live_evidence_probe" child span (queryHandlerTracer, shared with
// handler_tracing.go) carrying the image_ref count probed, whether an exact
// correlation matched, and the tier that decision implies -- an operator can
// read that span at 3 AM to see exactly why a workload's deployment truth
// tier changed (or did not) without reproducing the trace call (#5471 review
// round 2, P1).

package query

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// fetchWorkloadLiveEvidence returns true when the Postgres active-fact
// read model contains at least one exact-outcome kubernetes correlation
// row whose image_ref matches a config-declared image_ref from the
// traced workload's deployment-source controllers.
//
// The kubernetes correlation domain only writes rows with outcome=exact
// when image identity evidence is exact by digest or fixed tag
// (go/internal/reducer/kubernetes_correlation_decisions.go). An exact
// correlation row means a live cluster observably runs the workload's
// declared image — that is the runtime_confirmed tier signal.
//
// The probe is bounded: one bounded read per config-declared image_ref
// (capped at serviceStoryItemLimit = 15), each fetching LIMIT 1 with
// the anchored outcome=exact filter. The store is nil-safe (returns false
// when unwired). Access scoping is applied through the caller's
// repository grant set, mirroring listCorrelations.
func (h *ImpactHandler) fetchWorkloadLiveEvidence(
	ctx context.Context,
	imageRefs []string,
	access repositoryAccessFilter,
) (bool, error) {
	if h == nil {
		return false, nil
	}

	ctx, span := queryHandlerTracer.Start(ctx, "impact.live_evidence_probe")
	defer span.End()
	span.SetAttributes(attribute.Int("eshu.image_ref_count", len(imageRefs)))

	if h.KubernetesCorrelations == nil || len(imageRefs) == 0 {
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

	for _, imageRef := range imageRefs {
		if imageRef == "" {
			continue
		}
		filter := KubernetesCorrelationFilter{
			ImageRef:             imageRef,
			Outcome:              "exact",
			Limit:                1,
			AllScopes:            !access.scoped(),
			AllowedRepositoryIDs: access.grantedRepositoryIDs(),
			AllowedScopeIDs:      access.grantedScopeIDs(),
		}
		rows, err := h.KubernetesCorrelations.ListKubernetesCorrelations(ctx, filter)
		if err != nil {
			// Store errors fail closed to the config tier —
			// mirroring how the handler treats enrichment fetch
			// errors: log the error and continue with the next
			// image_ref or return false. A transient Postgres
			// failure must not 500 the whole deployment trace.
			span.RecordError(err)
			span.SetAttributes(attribute.Bool("eshu.live_evidence_matched", false))
			return false, err
		}
		if len(rows) > 0 {
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
