// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/facts"

func attachWorkflowImagesToRuns(runs map[string]*cicdRunEvidence, workflowImages []facts.Envelope) {
	if len(workflowImages) == 0 {
		return
	}
	for _, workflowImage := range workflowImages {
		repositoryID := payloadString(workflowImage.Payload, "repository_id")
		if repositoryID == "" {
			continue
		}
		for _, ev := range runs {
			if payloadString(ev.run.Payload, "repository_id") != repositoryID {
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
		if payloadString(workflowImage.Payload, "evidence_class") != "workflow_image_ref" {
			continue
		}
		imageRef := payloadString(workflowImage.Payload, "image_ref")
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
