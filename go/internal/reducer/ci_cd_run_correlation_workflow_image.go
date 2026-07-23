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
	// Iterate runs on the outer loop so each run keeps only the workflow-image
	// evidence with the strongest match: a workflow file whose extraction
	// commit equals the run's commit (commit-matched) is preferred over the
	// commit-blind repository-wide fan-out (fallback). A run only takes the
	// fallback set when no workflow file was extracted at its commit, so a file
	// declared on another branch cannot lend a false-confident correlation
	// (#5424). Trimming both sides preserves the pre-migration payloadString
	// byte-parity a padded repository_id relied on.
	for _, ev := range runs {
		runRepositoryID := trimmedCICDPtr(ev.runDecoded.RepositoryID)
		if runRepositoryID == "" {
			continue
		}
		runCommit := trimmedCICDPtr(ev.runDecoded.CommitSHA)
		var commitMatched, fallback []*decodedCICDWorkflowImage
		for _, workflowImage := range workflowImages {
			if trimmedCICDField(workflowImage.evidence.RepositoryID) != runRepositoryID {
				continue
			}
			workflowCommit := trimmedCICDPtr(workflowImage.evidence.CommitSHA)
			if runCommit != "" && workflowCommit != "" && runCommit == workflowCommit {
				commitMatched = append(commitMatched, workflowImage)
				continue
			}
			fallback = append(fallback, workflowImage)
		}
		if len(commitMatched) > 0 {
			ev.workflowImages = commitMatched
			ev.workflowImagesCommitMatched = true
			continue
		}
		ev.workflowImages = fallback
	}
}

func classifyCICDWorkflowImageEvidence(
	decision CICDRunCorrelationDecision,
	workflowImages []*decodedCICDWorkflowImage,
	commitMatched bool,
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
			// A commit-matched workflow file is an exact, commit-scoped
			// correlation; a repository-wide fallback (no workflow file extracted
			// at this run's commit) is a real but lower-confidence correlation, so
			// it lands as derived rather than exact and says so in the reason
			// (#5424). Both still write the canonical container-image target.
			decision.Outcome = CICDRunCorrelationExact
			decision.Reason = "commit-matched workflow image ref matches one container image identity row"
			if !commitMatched {
				decision.Outcome = CICDRunCorrelationDerived
				decision.Reason = "workflow image ref matches one container image identity row via repository-wide fallback (no commit-matched workflow file)"
			}
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
