// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// No-Observability-Change: the fetchWorkloadLiveEvidence path reads the
// existing Postgres active-fact read model through the same bounded,
// instrumented KubernetesCorrelationStore.ListKubernetesCorrelations
// query the list_kubernetes_correlations handler already uses. Operators
// diagnose the path through existing eshu_dp_postgres_query_duration_seconds,
// postgres.query spans, and the kubernetes_correlations read-model contract
// documented in go/internal/query/read-models.md.

package query

import "context"

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
	if h == nil || h.KubernetesCorrelations == nil || len(imageRefs) == 0 {
		return false, nil
	}
	// #5167 access-scoping discipline, mirroring listCorrelations
	// (go/internal/query/kubernetes.go:108-122): a scoped caller with no
	// granted repositories never queries the store.
	if access.empty() {
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
			return false, err
		}
		if len(rows) > 0 {
			return true, nil
		}
	}
	return false, nil
}
