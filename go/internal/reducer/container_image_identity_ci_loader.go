// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// activeContainerImageCIFactLoader is the #5810 cross-scope bridge for ci.run
// and container-image-typed ci.artifact facts, mirroring
// activeContainerImageSLSAFactLoader for the CI-run-owned scope family: the
// ci_cd_run collector writes these facts in the CI run's OWN scope, a
// different scope than the repository whose Dockerfile the DERIVED_FROM
// base-image-lineage projection (#5460) is owner-scoped to
// (projectContainerImageDerivedFromEdges,
// container_image_derived_from_edges.go). Without this bridge, a
// repository-scoped refresh can never see CI build provenance for a digest
// evidenced only in the CI run's scope, so BuildProvenanceRepositoryIDs could
// never reach a value at the projection that actually writes the edge, and
// the CI tier of DERIVED_FROM could only ever apply within a same-scope
// refresh (issue #5810).
//
// This is a dedicated loader rather than an admission into
// identityFactFilterSQL deliberately: ci.run/ci.artifact is the highest-churn
// fact family in the system, and identityFactFilterSQL backs the drift-locked
// identity-epoch cache contract that the SLSA loader's doc comment
// (facts_active_container_image_slsa.go) already keeps that family separate
// from for the same reason -- coupling CI-run churn into that cache's probe
// query would move its fingerprint on every CI run.
type activeContainerImageCIFactLoader interface {
	ListActiveContainerImageCIFacts(ctx context.Context) ([]facts.Envelope, error)
}

func (h ContainerImageIdentityHandler) loadActiveContainerImageCIFacts(
	ctx context.Context,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeContainerImageCIFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveContainerImageCIFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

// dedupeEnvelopesByFactID collapses envelopes sharing the same FactID down to
// their first occurrence, preserving relative order (#5810). It exists
// because loadActiveContainerImageCIFacts is a cross-scope load with no way
// to exclude the triggering intent's own scope: a CI-scope intent's
// scope-local ci.run/ci.artifact facts (loaded via loadFactsForKinds) overlap
// this cross-scope load for the SAME envelopes once that scope's generation
// is active. A well-formed duplicate merges idempotently through
// extractContainerImageRefsWithQuarantine's byRef map, but a fact that fails
// typed decode is quarantined independently on every occurrence it is
// processed at -- an undeduplicated envelope list would record and count the
// same malformed fact twice for one intent.
func dedupeEnvelopesByFactID(envelopes []facts.Envelope) []facts.Envelope {
	seen := make(map[string]struct{}, len(envelopes))
	deduped := envelopes[:0]
	for _, envelope := range envelopes {
		if _, exists := seen[envelope.FactID]; exists {
			continue
		}
		seen[envelope.FactID] = struct{}{}
		deduped = append(deduped, envelope)
	}
	return deduped
}
