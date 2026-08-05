// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

const cicdWorkflowImageProvider = "github_actions"

// attachWorkflowImagesToRuns joins each already-decoded
// ci.workflow_image_evidence (decodedCICDWorkflowImage, decoded once during
// the build phase and never re-decoded here) to the runs sharing its
// repository_id, preferring per run the workflow files whose extraction commit
// matches the run's commit over the commit-blind repository-wide fallback
// (#5424): a run takes the fallback set only when no workflow file matched its
// commit, and workflowImagesCommitMatched then downgrades that run's
// workflow-image correlation from exact to derived. A malformed workflow-image
// fact was already quarantined (or fatally failed the intent) during the
// build-phase decode, so only valid evidence reaches this function. A run whose
// own decoded RepositoryID is empty (RepositoryID is optional on cicdrunv1.Run)
// never matches any workflow image, matching pre-typing behavior where an empty
// repository_id segment could not equal another empty segment because the
// comparison already required a non-empty workflow-image repositoryID.
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
		if trimmedCICDField(ev.runDecoded.Provider) != cicdWorkflowImageProvider {
			continue
		}
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

// cicdWorkflowImageInputOnlyCommandKind is the one extracted command kind whose
// image the calling workflow does not itself build:
// workflowimage.evidenceFromReusableWorkflow stamps it on a
// `jobs.<job>.with.{image,image_ref,container_image}` value, which is typically
// a scanner, base, or tooling image passed into a reusable workflow.
//
// "Typically" is doing real work in that sentence. A shared build-and-push
// reusable workflow can take its TARGET image name as the same input, in which
// case the run does produce the image and capping it at derived is a
// false negative. That trade is deliberate: this evidence records what the
// calling workflow passed in, never what the callee did with it, so production
// cannot be established from it. Under-claiming a real build is recoverable;
// asserting a build that never happened is the failure this guards.
//
// This is a deny-list, not an allow-list, on purpose. command_kind is an
// optional free-string field, so an absent kind, a kind this reducer has not
// learned yet, or one a future collector adds all keep the pre-existing
// behavior. Only the kind proven to be input-only is denied, so a new
// produced-image kind is never silently degraded by a reducer that predates it.
const cicdWorkflowImageInputOnlyCommandKind = "reusable_workflow_input"

func cicdWorkflowImageIsInputOnly(workflowImage *decodedCICDWorkflowImage) bool {
	return trimmedCICDPtr(workflowImage.evidence.CommandKind) == cicdWorkflowImageInputOnlyCommandKind
}

// classifyCICDWorkflowImageEvidence resolves a run's workflow-image evidence to
// a container image identity.
//
// Produced-image evidence is considered first and input-only evidence second,
// so a run that both consumes a scanner image and builds its own image is
// decided by the image it built rather than by whichever fact was indexed
// first. An input-only image that resolves to exactly one identity row is
// capped at derived: calling it exact asserts the run produced the image, and
// that assertion is not free. incidentCICDPromotionCandidates prefers a digest
// exact match over every other candidate, and incidentCICDTruthLabel then
// stamps the incident's build/deploy and commit slots as exact truth, so a
// false exact on a scanner image can take build attribution away from a genuine
// derived candidate.
func classifyCICDWorkflowImageEvidence(
	decision CICDRunCorrelationDecision,
	workflowImages []*decodedCICDWorkflowImage,
	commitMatched bool,
	imageIndex map[string][]cicdImageIdentity,
) (CICDRunCorrelationDecision, bool) {
	if produced, handled := classifyCICDWorkflowImagePass(
		decision, workflowImages, commitMatched, imageIndex, false,
	); handled {
		return produced, true
	}
	return classifyCICDWorkflowImagePass(
		decision, workflowImages, commitMatched, imageIndex, true,
	)
}

// classifyCICDWorkflowImagePass runs one classification pass over the evidence
// whose input-only status matches inputOnly.
func classifyCICDWorkflowImagePass(
	decision CICDRunCorrelationDecision,
	workflowImages []*decodedCICDWorkflowImage,
	commitMatched bool,
	imageIndex map[string][]cicdImageIdentity,
	inputOnly bool,
) (CICDRunCorrelationDecision, bool) {
	for _, workflowImage := range workflowImages {
		if cicdWorkflowImageIsInputOnly(workflowImage) != inputOnly {
			continue
		}
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
			if inputOnly {
				// Consumed by this workflow, not produced by it, so it never
				// reaches exact regardless of commit matching.
				decision.Outcome = CICDRunCorrelationDerived
				decision.Reason = "workflow image ref is a reusable-workflow input (consumed by, not produced by, this workflow); one container image identity row matched"
			}
			decision.ProvenanceOnly = false
			decision.CanonicalWrites = 1
			decision.CanonicalTarget = "container_image"
			decision.CorrelationKind = "workflow_image"
			decision.ArtifactDigest = matches[0].digest
			decision.EvidenceFactIDs = append(
				decision.EvidenceFactIDs,
				cicdImageIdentityEvidenceFactIDs(matches[0])...,
			)
			decision.SourceLayerKinds = []string{"observed", "observed_resource"}
			return decision, true
		default:
			decision.Outcome = CICDRunCorrelationAmbiguous
			decision.Reason = "workflow image ref matches multiple container image identity rows"
			decision.CorrelationKind = "workflow_image"
			for _, match := range matches {
				decision.EvidenceFactIDs = append(
					decision.EvidenceFactIDs,
					cicdImageIdentityEvidenceFactIDs(match)...,
				)
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
