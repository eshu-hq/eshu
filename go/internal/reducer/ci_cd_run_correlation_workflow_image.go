// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// attachWorkflowImagesToRuns joins each already-decoded
// ci.workflow_image_evidence (decodedCICDWorkflowImage, decoded once during
// the build phase and never re-decoded here) to every run sharing the same
// repository_id. A malformed workflow-image fact was already quarantined (or
// fatally failed the intent) during that build-phase decode, so only
// valid evidence reaches this function. A run whose own decoded RepositoryID
// is empty (RepositoryID is optional on cicdrunv1.Run) never matches any
// workflow image, matching pre-typing behavior where an empty repository_id
// segment could not equal another empty segment because the comparison
// already required a non-empty workflow-image repositoryID.
func attachWorkflowImagesToRuns(runs map[string]*cicdRunEvidence, workflowImages []*decodedCICDWorkflowImage) {
	if len(workflowImages) == 0 {
		return
	}
	for _, workflowImage := range workflowImages {
		// The evidence was decoded once during the build phase; read the
		// cached typed value rather than re-decoding the envelope here.
		// Trim both the workflow-image repository_id and the run's own
		// repository_id before comparing, so the join matches byte-for-byte
		// with the pre-migration payloadString path (both sides were trimmed
		// there). A padded repository_id on either side must not miss the join.
		repositoryID := trimmedCICDField(workflowImage.evidence.RepositoryID)
		if repositoryID == "" {
			continue
		}
		for _, ev := range runs {
			if trimmedCICDPtr(ev.runDecoded.RepositoryID) != repositoryID {
				continue
			}
			ev.workflowImages = append(ev.workflowImages, workflowImage)
		}
	}
}

func classifyCICDWorkflowImageEvidence(
	decision CICDRunCorrelationDecision,
	workflowImages []*decodedCICDWorkflowImage,
	imageIndex map[string][]cicdImageIdentity,
) (CICDRunCorrelationDecision, bool) {
	for _, workflowImage := range workflowImages {
		// Read the once-decoded typed value cached on decodedCICDWorkflowImage
		// rather than re-decoding the envelope for every run in the repo.
		evidence := workflowImage.evidence
		if trimmedCICDPtr(evidence.EvidenceClass) != "workflow_image_ref" {
			continue
		}
		imageRef := trimmedCICDPtr(evidence.ImageRef)
		if imageRef == "" {
			continue
		}
		decision.ImageRef = imageRef
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, workflowImage.envelope.FactID)
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
