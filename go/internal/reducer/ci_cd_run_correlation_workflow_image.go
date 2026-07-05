// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/facts"

// attachWorkflowImagesToRuns joins ci.workflow_image_evidence envelopes to
// every run sharing the same repository_id, decoding each workflow-image
// envelope through the typed seam. A workflow-image envelope that fails to
// decode here is silently skipped: buildCICDRunCorrelationDecisionsWithQuarantine
// already quarantined it (or fatally failed the intent) during its own
// ingestion pass over the same envelope before calling this function, so
// re-quarantining it here would double-count the same malformed fact. A run
// whose own decoded RepositoryID is empty (RepositoryID is optional on
// cicdrunv1.Run) never matches any workflow image, matching pre-typing
// behavior where an empty repository_id segment could not equal another
// empty segment because the comparison already required a non-empty
// workflow-image repositoryID.
func attachWorkflowImagesToRuns(runs map[string]*cicdRunEvidence, workflowImages []facts.Envelope) {
	if len(workflowImages) == 0 {
		return
	}
	for _, workflowImage := range workflowImages {
		evidence, err := decodeCICDWorkflowImageEvidence(workflowImage)
		if err != nil {
			continue
		}
		if evidence.RepositoryID == "" {
			continue
		}
		for _, ev := range runs {
			if derefString(ev.runDecoded.RepositoryID) != evidence.RepositoryID {
				continue
			}
			ev.workflowImages = append(ev.workflowImages, workflowImage)
		}
	}
}

func classifyCICDWorkflowImageEvidence(
	decision CICDRunCorrelationDecision,
	workflowImages []facts.Envelope,
	imageIndex map[string][]cicdImageIdentity,
) (CICDRunCorrelationDecision, bool) {
	for _, workflowImage := range workflowImages {
		evidence, err := decodeCICDWorkflowImageEvidence(workflowImage)
		if err != nil {
			continue
		}
		if derefString(evidence.EvidenceClass) != "workflow_image_ref" {
			continue
		}
		imageRef := derefString(evidence.ImageRef)
		if imageRef == "" {
			continue
		}
		decision.ImageRef = imageRef
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, workflowImage.FactID)
		matches := cicdImageIdentityMatchesByRef(imageIndex, imageRef)
		if repoMatches := cicdImageMatchesForRepository(matches, decision.RepositoryID); len(repoMatches) > 0 {
			matches = repoMatches
		}
		switch len(matches) {
		case 0:
			decision.Reason = "workflow image ref has no matching container image identity row"
			decision.CorrelationKind = "workflow_image"
			return decision, true
		case 1:
			decision.Outcome = CICDRunCorrelationExact
			decision.Reason = "workflow image ref matches one container image identity row"
			decision.ProvenanceOnly = false
			decision.CanonicalWrites = 1
			decision.CanonicalTarget = "container_image"
			decision.CorrelationKind = "workflow_image"
			decision.ArtifactDigest = matches[0].digest
			decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, matches[0].factID)
			decision.SourceLayerKinds = []string{"observed", "observed_resource"}
			return decision, true
		default:
			decision.Outcome = CICDRunCorrelationAmbiguous
			decision.Reason = "workflow image ref matches multiple container image identity rows"
			decision.CorrelationKind = "workflow_image"
			for _, match := range matches {
				decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, match.factID)
			}
			return decision, true
		}
	}
	return decision, false
}

func cicdImageIdentityMatchesByRef(
	imageIndex map[string][]cicdImageIdentity,
	imageRef string,
) []cicdImageIdentity {
	var out []cicdImageIdentity
	for _, matches := range imageIndex {
		for _, match := range matches {
			if match.imageRef == imageRef {
				out = append(out, match)
			}
		}
	}
	return out
}
