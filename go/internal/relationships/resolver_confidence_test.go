// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"fmt"
	"math"
	"testing"
)

func TestResolveWeightsCorroboratingEquivalentEvidence(t *testing.T) {
	t.Parallel()

	singleFact := relationshipEvidenceFact("repo-api-single", EvidenceKindHelmChart, 0.70, 0)
	corroboratingFacts := relationshipEvidenceFacts("repo-api-corroborated", 5, 0.70)
	facts := append([]EvidenceFact{singleFact}, corroboratingFacts...)

	candidates, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)

	singleCandidate := requireCandidateByTarget(t, candidates, "repo-api-single")
	corroboratedCandidate := requireCandidateByTarget(t, candidates, "repo-api-corroborated")
	if corroboratedCandidate.Confidence <= singleCandidate.Confidence {
		t.Fatalf(
			"corroborated confidence = %.6f, single confidence = %.6f; want corroborated higher",
			corroboratedCandidate.Confidence,
			singleCandidate.Confidence,
		)
	}
	if corroboratedCandidate.Confidence > 1.0 {
		t.Fatalf("corroborated confidence = %.6f, want bounded to 1.0", corroboratedCandidate.Confidence)
	}
	if corroboratedCandidate.EvidenceCount != 5 {
		t.Fatalf("evidence_count = %d, want 5", corroboratedCandidate.EvidenceCount)
	}

	if len(resolved) != 1 {
		t.Fatalf("resolved = %d, want 1 corroborated relationship", len(resolved))
	}
	if resolved[0].TargetEntityID != "repo-api-corroborated" {
		t.Fatalf("resolved target = %q, want repo-api-corroborated", resolved[0].TargetEntityID)
	}
}

func TestResolveKeepsSingleEvidenceConfidence(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		relationshipEvidenceFact("repo-api", EvidenceKindTerraformAppRepo, 0.99, 0),
	}

	candidates, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)

	candidate := requireCandidateByTarget(t, candidates, "repo-api")
	if candidate.Confidence != 0.99 {
		t.Fatalf("candidate confidence = %.6f, want 0.99", candidate.Confidence)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved = %d, want 1", len(resolved))
	}
	if resolved[0].Confidence != 0.99 {
		t.Fatalf("resolved confidence = %.6f, want 0.99", resolved[0].Confidence)
	}
}

func TestResolveRejectSuppressesWeightedCandidate(t *testing.T) {
	t.Parallel()

	facts := relationshipEvidenceFacts("repo-api", 5, 0.70)
	assertions := []Assertion{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-api",
			RelationshipType: RelDeploysFrom,
			Decision:         "reject",
			Reason:           "known false positive",
			Actor:            "platform-team",
		},
	}

	candidates, resolved := Resolve(facts, assertions, DefaultConfidenceThreshold)

	candidate := requireCandidateByTarget(t, candidates, "repo-api")
	if candidate.Confidence <= DefaultConfidenceThreshold {
		t.Fatalf("candidate confidence = %.6f, want above threshold before rejection", candidate.Confidence)
	}
	if len(resolved) != 0 {
		t.Fatalf("resolved = %d, want 0 after rejection", len(resolved))
	}
}

func TestResolveConfidenceConvergesAcrossEvidenceOrder(t *testing.T) {
	t.Parallel()

	forwardFacts := relationshipEvidenceFacts("repo-api", 5, 0.70)
	reversedFacts := reverseEvidenceFacts(forwardFacts)

	forwardCandidates, forwardResolved := Resolve(forwardFacts, nil, DefaultConfidenceThreshold)
	reversedCandidates, reversedResolved := Resolve(reversedFacts, nil, DefaultConfidenceThreshold)

	forwardCandidate := requireCandidateByTarget(t, forwardCandidates, "repo-api")
	reversedCandidate := requireCandidateByTarget(t, reversedCandidates, "repo-api")
	if math.Abs(forwardCandidate.Confidence-reversedCandidate.Confidence) > 0.000001 {
		t.Fatalf(
			"confidence changed with evidence order: forward %.6f, reversed %.6f",
			forwardCandidate.Confidence,
			reversedCandidate.Confidence,
		)
	}
	if len(forwardResolved) != len(reversedResolved) {
		t.Fatalf("resolved count changed with evidence order: forward %d, reversed %d", len(forwardResolved), len(reversedResolved))
	}
}

func TestResolveDoesNotLiftExactDuplicatesAfterDedupe(t *testing.T) {
	t.Parallel()

	duplicate := relationshipEvidenceFact("repo-api", EvidenceKindHelmChart, 0.70, 0)
	facts := []EvidenceFact{duplicate, duplicate, duplicate, duplicate, duplicate}

	candidates, resolved := Resolve(DedupeEvidenceFacts(facts), nil, DefaultConfidenceThreshold)

	candidate := requireCandidateByTarget(t, candidates, "repo-api")
	if candidate.EvidenceCount != 1 {
		t.Fatalf("evidence_count = %d, want 1 after dedupe", candidate.EvidenceCount)
	}
	if candidate.Confidence != 0.70 {
		t.Fatalf("candidate confidence = %.6f, want 0.70", candidate.Confidence)
	}
	if len(resolved) != 0 {
		t.Fatalf("resolved = %d, want 0 for deduped low-confidence evidence", len(resolved))
	}
}

func relationshipEvidenceFacts(targetRepoID string, count int, confidence float64) []EvidenceFact {
	kinds := []EvidenceKind{
		EvidenceKindHelmChart,
		EvidenceKindArgoCDAppSource,
		EvidenceKindKustomizeImage,
		EvidenceKindGitHubActionsCheckoutRepository,
		EvidenceKindDockerComposeImage,
	}
	facts := make([]EvidenceFact, 0, count)
	for i := range count {
		facts = append(facts, relationshipEvidenceFact(targetRepoID, kinds[i%len(kinds)], confidence, i))
	}
	return facts
}

func relationshipEvidenceFact(targetRepoID string, kind EvidenceKind, confidence float64, ordinal int) EvidenceFact {
	return EvidenceFact{
		EvidenceKind:     kind,
		RelationshipType: RelDeploysFrom,
		SourceRepoID:     "repo-deploy",
		TargetRepoID:     targetRepoID,
		Confidence:       confidence,
		Rationale:        fmt.Sprintf("corroborating deploy signal %d", ordinal),
		Details: map[string]any{
			"path": fmt.Sprintf("deploy/%d.yaml", ordinal),
		},
	}
}

func requireCandidateByTarget(t *testing.T, candidates []Candidate, targetEntityID string) Candidate {
	t.Helper()

	for i := range candidates {
		if candidates[i].TargetEntityID == targetEntityID {
			return candidates[i]
		}
	}
	t.Fatalf("missing candidate with target %q in %#v", targetEntityID, candidates)
	return Candidate{}
}

func reverseEvidenceFacts(facts []EvidenceFact) []EvidenceFact {
	reversed := make([]EvidenceFact, len(facts))
	for i := range facts {
		reversed[len(facts)-1-i] = facts[i]
	}
	return reversed
}
