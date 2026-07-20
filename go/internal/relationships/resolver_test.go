// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"
)

func TestDedupeEvidenceFactsEmpty(t *testing.T) {
	t.Parallel()

	result := DedupeEvidenceFacts(nil)
	if result != nil {
		t.Errorf("got %v, want nil", result)
	}
}

func TestDedupeEvidenceFactsRemovesDuplicates(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindTerraformAppRepo,
			RelationshipType: RelProvisionsDependencyFor,
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-app",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at the target",
		},
		{
			EvidenceKind:     EvidenceKindTerraformAppRepo,
			RelationshipType: RelProvisionsDependencyFor,
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-app",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo points at the target",
		},
		{
			EvidenceKind:     EvidenceKindHelmChart,
			RelationshipType: RelDeploysFrom,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-app",
			Confidence:       0.90,
			Rationale:        "Helm chart references target",
		},
	}

	result := DedupeEvidenceFacts(facts)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].EvidenceKind != EvidenceKindTerraformAppRepo {
		t.Errorf("first = %q", result[0].EvidenceKind)
	}
	if result[1].EvidenceKind != EvidenceKindHelmChart {
		t.Errorf("second = %q", result[1].EvidenceKind)
	}
}

func TestResolveInferredOnly(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindTerraformAppRepo,
			RelationshipType: RelProvisionsDependencyFor,
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-payments",
			Confidence:       0.99,
			Rationale:        "Terraform app_repo",
		},
		{
			EvidenceKind:     EvidenceKindTerraformAppName,
			RelationshipType: RelProvisionsDependencyFor,
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-payments",
			Confidence:       0.94,
			Rationale:        "Terraform app_name",
		},
	}

	candidates, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)

	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if candidates[0].EvidenceCount != 2 {
		t.Errorf("evidence_count = %d, want 2", candidates[0].EvidenceCount)
	}
	if candidates[0].Confidence <= 0.99 || candidates[0].Confidence >= 1.0 {
		t.Errorf("confidence = %f, want corroboration uplift above 0.99 and below 1.0", candidates[0].Confidence)
	}

	if len(resolved) != 1 {
		t.Fatalf("resolved = %d, want 1", len(resolved))
	}
	if resolved[0].ResolutionSource != ResolutionSourceInferred {
		t.Errorf("resolution_source = %q", resolved[0].ResolutionSource)
	}
	if resolved[0].Confidence != candidates[0].Confidence {
		t.Errorf("confidence = %f, want candidate confidence %f", resolved[0].Confidence, candidates[0].Confidence)
	}
}

func TestResolveBelowThresholdFiltered(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindTerraformConfigPath,
			RelationshipType: RelProvisionsDependencyFor,
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-api",
			Confidence:       0.50,
			Rationale:        "Low confidence match",
		},
	}

	candidates, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)

	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if len(resolved) != 0 {
		t.Fatalf("resolved = %d, want 0 (below threshold)", len(resolved))
	}
}

func TestResolveWithRejection(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindTerraformAppRepo,
			RelationshipType: RelProvisionsDependencyFor,
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-app",
			Confidence:       0.99,
			Rationale:        "strong match",
		},
	}
	assertions := []Assertion{
		{
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-app",
			RelationshipType: RelProvisionsDependencyFor,
			Decision:         "reject",
			Reason:           "false positive",
			Actor:            "admin",
		},
	}

	_, resolved := Resolve(facts, assertions, DefaultConfidenceThreshold)

	if len(resolved) != 0 {
		t.Fatalf("resolved = %d, want 0 (rejected)", len(resolved))
	}
}

func TestResolveWithExplicitAssertion(t *testing.T) {
	t.Parallel()

	assertions := []Assertion{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-api",
			RelationshipType: RelDeploysFrom,
			Decision:         "assert",
			Reason:           "known deployment link",
			Actor:            "platform-team",
		},
	}

	_, resolved := Resolve(nil, assertions, DefaultConfidenceThreshold)

	if len(resolved) != 1 {
		t.Fatalf("resolved = %d, want 1", len(resolved))
	}
	if resolved[0].ResolutionSource != ResolutionSourceAssertion {
		t.Errorf("resolution_source = %q", resolved[0].ResolutionSource)
	}
	if resolved[0].Confidence != 1.0 {
		t.Errorf("confidence = %f, want 1.0", resolved[0].Confidence)
	}
	if resolved[0].Rationale != "known deployment link" {
		t.Errorf("rationale = %q", resolved[0].Rationale)
	}
}

func TestResolveAssertionOverridesInferred(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindHelmChart,
			RelationshipType: RelDeploysFrom,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-api",
			Confidence:       0.90,
			Rationale:        "Helm chart match",
		},
	}
	assertions := []Assertion{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-api",
			RelationshipType: RelDeploysFrom,
			Decision:         "assert",
			Reason:           "confirmed by team",
			Actor:            "ops-team",
		},
	}

	_, resolved := Resolve(facts, assertions, DefaultConfidenceThreshold)

	if len(resolved) != 1 {
		t.Fatalf("resolved = %d, want 1", len(resolved))
	}
	if resolved[0].Confidence != 1.0 {
		t.Errorf("confidence = %f, want 1.0 (assertion override)", resolved[0].Confidence)
	}
	if resolved[0].ResolutionSource != ResolutionSourceAssertion {
		t.Errorf("resolution_source = %q", resolved[0].ResolutionSource)
	}
	if resolved[0].Rationale != "confirmed by team" {
		t.Errorf("rationale = %q", resolved[0].Rationale)
	}
}

func TestResolveSkipsEmptyIdentities(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindTerraformAppRepo,
			RelationshipType: RelProvisionsDependencyFor,
			SourceRepoID:     "",
			TargetRepoID:     "repo-app",
			Confidence:       0.99,
			Rationale:        "missing source",
		},
	}

	candidates, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)

	if len(candidates) != 0 {
		t.Errorf("candidates = %d, want 0 (empty source)", len(candidates))
	}
	if len(resolved) != 0 {
		t.Errorf("resolved = %d, want 0", len(resolved))
	}
}

func TestResolveMultipleGroups(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindTerraformAppRepo,
			RelationshipType: RelProvisionsDependencyFor,
			SourceRepoID:     "repo-infra",
			TargetRepoID:     "repo-api",
			Confidence:       0.99,
			Rationale:        "match 1",
		},
		{
			EvidenceKind:     EvidenceKindHelmChart,
			RelationshipType: RelDeploysFrom,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-api",
			Confidence:       0.90,
			Rationale:        "match 2",
		},
	}

	candidates, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)

	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(candidates))
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved = %d, want 2", len(resolved))
	}
}

func TestResolveEntityIDTakesPrecedence(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindArgoCDAppSource,
			RelationshipType: RelDeploysFrom,
			SourceRepoID:     "repo-gitops",
			TargetRepoID:     "repo-app",
			SourceEntityID:   "platform:gitops:cluster",
			TargetEntityID:   "workload:app:prod",
			Confidence:       0.95,
			Rationale:        "ArgoCD Application source",
		},
	}

	candidates, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)

	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if candidates[0].SourceEntityID != "platform:gitops:cluster" {
		t.Errorf("SourceEntityID = %q", candidates[0].SourceEntityID)
	}
	if candidates[0].TargetEntityID != "workload:app:prod" {
		t.Errorf("TargetEntityID = %q", candidates[0].TargetEntityID)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved = %d, want 1", len(resolved))
	}
}

// TestAggregateCandidateSourceRevisionHighestConfidenceWins proves the
// #5441 P0-follow-up winner rule: when two evidence facts in one candidate
// disagree on a value, the highest-confidence fact wins, not map order
// (which is randomized per process and would make the result
// nondeterministic).
func TestAggregateCandidateSourceRevisionHighestConfidenceWins(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindArgoCDAppSource,
			RelationshipType: RelDeploysFrom,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			Confidence:       0.80,
			Rationale:        "lower-confidence fact",
			Details:          map[string]any{"source_revision": "stale-branch"},
		},
		{
			EvidenceKind:     EvidenceKindArgoCDAppSource,
			RelationshipType: RelDeploysFrom,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-service",
			Confidence:       0.95,
			Rationale:        "higher-confidence fact",
			Details:          map[string]any{"source_revision": "main"},
		},
	}

	_, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)
	if got, want := len(resolved), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
	}
	if got, want := resolved[0].SourceRevision, "main"; got != want {
		t.Fatalf("SourceRevision = %q, want %q (the higher-confidence fact's value)", got, want)
	}
}

// TestAggregateCandidateSourceRevisionTiebreakKeepsFirstInInputOrder proves
// the deterministic tiebreak: when two facts carry equal confidence, the
// fact appearing first in the input order wins, and the result is stable
// regardless of which value happens to sort first alphabetically (ruling
// out an accidental sort- or map-order dependency).
func TestAggregateCandidateSourceRevisionTiebreakKeepsFirstInInputOrder(t *testing.T) {
	t.Parallel()

	buildFacts := func(firstRevision, secondRevision string) []EvidenceFact {
		return []EvidenceFact{
			{
				EvidenceKind:     EvidenceKindArgoCDAppSource,
				RelationshipType: RelDeploysFrom,
				SourceRepoID:     "repo-deploy",
				TargetRepoID:     "repo-service",
				Confidence:       0.90,
				Rationale:        "first fact",
				Details:          map[string]any{"source_revision": firstRevision},
			},
			{
				EvidenceKind:     EvidenceKindArgoCDAppSource,
				RelationshipType: RelDeploysFrom,
				SourceRepoID:     "repo-deploy",
				TargetRepoID:     "repo-service",
				Confidence:       0.90,
				Rationale:        "second fact",
				Details:          map[string]any{"source_revision": secondRevision},
			},
		}
	}

	_, resolvedZFirst := Resolve(buildFacts("zzz-later-alphabetically", "aaa-earlier-alphabetically"), nil, DefaultConfidenceThreshold)
	if got, want := len(resolvedZFirst), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
	}
	if got, want := resolvedZFirst[0].SourceRevision, "zzz-later-alphabetically"; got != want {
		t.Fatalf("SourceRevision = %q, want %q (the first fact in input order, not alphabetical order)", got, want)
	}

	_, resolvedAFirst := Resolve(buildFacts("aaa-earlier-alphabetically", "zzz-later-alphabetically"), nil, DefaultConfidenceThreshold)
	if got, want := len(resolvedAFirst), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
	}
	if got, want := resolvedAFirst[0].SourceRevision, "aaa-earlier-alphabetically"; got != want {
		t.Fatalf("SourceRevision = %q, want %q (the first fact in input order)", got, want)
	}
}

// TestAggregateCandidateFirstPartyRefVersionSkipsFactsWithoutValue proves a
// lower-confidence fact that DOES carry a value still wins over a
// higher-confidence fact that carries none — the winner rule only compares
// facts that have a non-empty value for the field, so a stronger but silent
// fact never blanks out a weaker but informative one.
func TestAggregateCandidateFirstPartyRefVersionSkipsFactsWithoutValue(t *testing.T) {
	t.Parallel()

	facts := []EvidenceFact{
		{
			EvidenceKind:     EvidenceKindTerraformModuleSource,
			RelationshipType: RelUsesModule,
			SourceRepoID:     "repo-app",
			TargetRepoID:     "repo-module",
			Confidence:       0.99,
			Rationale:        "higher-confidence fact with no pin",
			Details:          map[string]any{"source_ref": "terraform-aws-modules/vpc/aws"},
		},
		{
			EvidenceKind:     EvidenceKindTerraformModuleSource,
			RelationshipType: RelUsesModule,
			SourceRepoID:     "repo-app",
			TargetRepoID:     "repo-module",
			Confidence:       0.80,
			Rationale:        "lower-confidence fact with a pin",
			Details:          map[string]any{"source_ref": "git::https://example.test/org/mod.git?ref=v3.0.0"},
		},
	}

	_, resolved := Resolve(facts, nil, DefaultConfidenceThreshold)
	if got, want := len(resolved), 1; got != want {
		t.Fatalf("len(resolved) = %d, want %d", got, want)
	}
	if got, want := resolved[0].FirstPartyRefVersion, "v3.0.0"; got != want {
		t.Fatalf("FirstPartyRefVersion = %q, want %q (the only fact carrying a pin)", got, want)
	}
}
