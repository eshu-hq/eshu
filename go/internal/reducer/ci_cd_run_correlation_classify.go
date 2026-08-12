// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/environment"
)

// This file holds the per-run classification half of the CI/CD run correlation
// domain: turning one run's evidence into one decision. The handler, its
// registration types, and the durable write path stay in
// ci_cd_run_correlation.go, which was at 491 lines against the repository's
// 500-line cap before this split.

func classifyCICDRunEvidence(ev *cicdRunEvidence, imageIndex map[string][]cicdImageIdentity) CICDRunCorrelationDecision {
	run := ev.runDecoded
	// Every identity/anchor field is trimmed to preserve byte-parity with the
	// pre-migration payloadString read path (see trimmedCICDField/
	// trimmedCICDPtr in ci_cd_run_correlation_decode.go). RepositoryID and
	// CommitSHA feed the unresolved-anchor emptiness check below, so a
	// whitespace-only value must count as empty exactly as the old trimmed
	// path did; Provider/RunID/RunAttempt are the persisted decision identity
	// and must match the trimmed join key.
	decision := CICDRunCorrelationDecision{
		Provider:         trimmedCICDField(run.Provider),
		RunID:            trimmedCICDField(run.RunID),
		RunAttempt:       defaultCICDRunAttempt(derefString(run.RunAttempt)),
		RepositoryID:     trimmedCICDPtr(run.RepositoryID),
		CommitSHA:        trimmedCICDPtr(run.CommitSHA),
		Outcome:          CICDRunCorrelationDerived,
		Reason:           "run has provider evidence but no explicit artifact identity anchor",
		ProvenanceOnly:   true,
		CorrelationKind:  "run_evidence",
		SourceLayerKinds: []string{"reported"},
		EvidenceFactIDs:  []string{ev.run.FactID},
	}
	// An attached deployment event (repo/commit-scoped evidence, see
	// attachDeploymentEventsToRuns) beats the declared job/step-level
	// environment observation: the GitHub Deployments API is the platform's
	// own record of what environment a commit was actually deployed to,
	// where an environment_observation is inferred from the run's own
	// job/step configuration and can drift from what really happened.
	if env, factID, ok := classifyCICDDeploymentEventEnvironment(ev.deploymentEvents); ok {
		decision.Environment = env
		decision.EnvironmentEvidence = supplyChainEnvironmentEvidenceDeployEvent
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, factID)
	} else if len(ev.environmentsDecoded) > 0 {
		decision.Environment = environment.Canonical(trimmedCICDPtr(ev.environmentsDecoded[0].Environment))
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, ev.environments[0].FactID)
		decision.EnvironmentEvidence = supplyChainEnvironmentEvidenceDeclared
	}
	for _, trigger := range ev.triggers {
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, trigger.FactID)
	}
	if decision.RepositoryID == "" || decision.CommitSHA == "" {
		decision.Outcome = CICDRunCorrelationUnresolved
		decision.Reason = "run is missing repository_id or commit_sha anchor"
		return decision
	}
	if len(ev.shellOnly) > 0 {
		decision.Outcome = CICDRunCorrelationRejected
		decision.Reason = "shell-only deployment hint suppressed without artifact identity"
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, ev.shellOnly[0].FactID)
		return decision
	}
	if workflowDecision, ok := classifyCICDWorkflowImageEvidence(decision, ev.workflowImages, ev.workflowImagesCommitMatched, imageIndex); ok {
		return workflowDecision
	}
	for i, artifact := range ev.artifactsDecoded {
		digest := trimmedCICDPtr(artifact.ArtifactDigest)
		if digest == "" {
			continue
		}
		decision.ArtifactDigest = digest
		decision.EvidenceFactIDs = append(decision.EvidenceFactIDs, ev.artifacts[i].FactID)
		matches := imageIndex[digest]
		if repoMatches := cicdImageMatchesForRepository(matches, decision.RepositoryID); len(repoMatches) > 0 {
			matches = repoMatches
		}
		switch len(matches) {
		case 0:
			continue
		case 1:
			decision.Outcome = CICDRunCorrelationExact
			decision.Reason = "artifact digest matches one container image identity row"
			decision.ProvenanceOnly = false
			decision.CanonicalWrites = 1
			decision.CanonicalTarget = "container_image"
			decision.CorrelationKind = "artifact_image"
			decision.ImageRef = matches[0].imageRef
			decision.EvidenceFactIDs = append(
				decision.EvidenceFactIDs,
				cicdImageIdentityEvidenceFactIDs(matches[0])...,
			)
			decision.SourceLayerKinds = []string{"reported", "observed_resource"}
			return decision
		default:
			decision.Outcome = CICDRunCorrelationAmbiguous
			decision.Reason = "artifact digest matches multiple container image identity rows"
			decision.CorrelationKind = "artifact_image"
			for _, match := range matches {
				decision.EvidenceFactIDs = append(
					decision.EvidenceFactIDs,
					cicdImageIdentityEvidenceFactIDs(match)...,
				)
			}
			return decision
		}
	}
	return decision
}

// cicdImageMatchesForRepository narrows a digest's container_image_identity
// matches to those the run's own repository built, so a single surviving row
// can promote the correlation to exact.
//
// It joins on build_provenance_repository_ids, and on nothing else:
//
//   - The identity's own repository_id is the OCI registry's identifier
//     ("oci-registry://ghcr.io/org/repo"), a namespace disjoint from the
//     canonical "repository:r_..." ids a ci.run carries. Narrowing compared
//     those directly, so it never matched and left the unfiltered multi-row set
//     to degrade an otherwise-exact correlation into ambiguous (#5766, the same
//     trap #5464 found on the supply-chain side). That field is no longer
//     decoded at all.
//   - source_repository_ids conflates "this repository built the image" with
//     "this repository's manifest merely references the digest". Narrowing on it
//     can select a reference-only row and promote a repository that only deploys
//     the image to exact -- the conflation #5796 fixed inside the identity
//     domain by gating its own BUILT_FROM projection on the narrower set
//     (#5823).
//
// A row published before #5823 carries no build-provenance key, so it can never
// be selected here. That is deliberate, and it is NOT a degradation: because the
// pre-#5766 predicate compared the OCI repository_id, narrowing was already a
// dead no-op for every such row, and a legacy multi-row digest already resolved
// ambiguous. Falling back to source_repository_ids for those rows would not
// recover a lost correlation -- there is none to recover -- it would only
// manufacture a false exact that the previous behavior never produced. The
// sharper join engages for a scope as soon as its identity intent republishes.
func cicdImageMatchesForRepository(matches []cicdImageIdentity, repositoryID string) []cicdImageIdentity {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return nil
	}
	out := make([]cicdImageIdentity, 0, len(matches))
	for _, match := range matches {
		for _, candidate := range match.buildProvenanceRepositoryIDs {
			if candidate == repositoryID {
				out = append(out, match)
				break
			}
		}
	}
	return out
}

func defaultCICDRunAttempt(attempt string) string {
	if strings.TrimSpace(attempt) == "" {
		return "1"
	}
	return strings.TrimSpace(attempt)
}
