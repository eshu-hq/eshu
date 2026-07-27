// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"

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

// loadActiveContainerImageCIFacts returns the cross-scope ci.run/ci.artifact
// envelopes admissible for the intent owning scopeID, or nil when the loader
// seam is absent.
//
// The load is owner-gated (#5810 follow-up, adjudicated against the #5817 B-12
// pin): the bridge exists for exactly one consumer, the owner-scoped
// DERIVED_FROM projection, whose child gate (containerImageDerivedFromRows)
// only ever reads BuildProvenanceRepositoryIDs entries EQUAL to the owning
// repository. Cross-scope CI evidence naming any OTHER repository has no
// consumer in this intent, and admitting it anyway is not neutral:
//
//   - applyCIRunDigestRevision folds the foreign builder into every competing
//     decision for the digest, turning a deploying repository's clean
//     single-entry SourceRepositoryIDs into an ambiguous two-entry set (and,
//     symmetrically, stamping foreign build provenance). On the live 20-repo
//     corpus that demoted all ten of the deploying repository's rows for
//     sha256:abcdef… out of anchor tier A and flipped the
//     list_supply_chain_impact_findings repository_id pin from
//     repository:r_217415d9 (the deploying repository, the runtime-joinable
//     impact anchor #5817 deliberately kept) to the builder — a semantic
//     redefinition of the finding that needs owner sign-off, not a loader
//     side effect.
//   - addCICDArtifactImageReference mints a bare-digest identity row for the
//     foreign builder's digest in EVERY scope that refreshes, multiplying one
//     CI run's claim across unrelated scopes with no consumer.
//
// A non-repository scope (owner == "") admits nothing from this loader: a
// CI-run scope's own ci.run/ci.artifact facts already arrive scope-local via
// loadFactsForKinds, which also keeps quarantine accounting for a malformed
// CI fact in the one scope that owns it instead of repeating it per intent.
// Skipping the query outright for those scopes also keeps the highest-churn
// fact family in the system off every non-repository refresh.
func (h ContainerImageIdentityHandler) loadActiveContainerImageCIFacts(
	ctx context.Context,
	scopeID string,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeContainerImageCIFactLoader)
	if !ok {
		return nil, nil
	}
	ownerRepositoryID := repositoryIDFromReducerScope(scopeID)
	if ownerRepositoryID == "" {
		return nil, nil
	}
	envelopes, err := loader.ListActiveContainerImageCIFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return filterContainerImageCIFactsForOwner(envelopes, ownerRepositoryID), nil
}

// filterContainerImageCIFactsForOwner keeps only the ci.run envelopes whose
// decoded repository is ownerRepositoryID, plus the ci.artifact envelopes
// joined (by the shared provider/run_id/run_attempt key) to one of those kept
// runs. Everything else — foreign runs, artifacts of foreign or absent runs,
// runs with no repository anchor, and envelopes that fail typed decode — is
// dropped: a fact that cannot be attributed to the owning repository cannot
// contribute owner build provenance, and its quarantine (for the malformed
// case) belongs to its own scope's intent, not to every repository refresh
// that happens to load it cross-scope.
func filterContainerImageCIFactsForOwner(
	envelopes []facts.Envelope,
	ownerRepositoryID string,
) []facts.Envelope {
	ownedRunKeys := make(map[string]struct{})
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDRunFactKind {
			continue
		}
		run, err := decodeCICDRun(envelope)
		if err != nil {
			continue
		}
		if strings.TrimSpace(run.Provider) == "" || strings.TrimSpace(run.RunID) == "" {
			continue
		}
		if trimmedCICDPtr(run.RepositoryID) != ownerRepositoryID {
			continue
		}
		if key := cicdRunKeyFromParts(run.Provider, run.RunID, run.RunAttempt); key != "" {
			ownedRunKeys[key] = struct{}{}
		}
	}
	if len(ownedRunKeys) == 0 {
		return nil
	}
	kept := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.CICDRunFactKind:
			run, err := decodeCICDRun(envelope)
			if err != nil {
				continue
			}
			if _, owned := ownedRunKeys[cicdRunKeyFromParts(run.Provider, run.RunID, run.RunAttempt)]; owned {
				kept = append(kept, envelope)
			}
		case facts.CICDArtifactFactKind:
			artifact, err := decodeCICDArtifact(envelope)
			if err != nil {
				continue
			}
			if _, owned := ownedRunKeys[cicdRunKeyFromParts(artifact.Provider, artifact.RunID, artifact.RunAttempt)]; owned {
				kept = append(kept, envelope)
			}
		}
	}
	return kept
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
