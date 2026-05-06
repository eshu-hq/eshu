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
	if candidates[0].Confidence != 0.99 {
		t.Errorf("confidence = %f, want 0.99", candidates[0].Confidence)
	}

	if len(resolved) != 1 {
		t.Fatalf("resolved = %d, want 1", len(resolved))
	}
	if resolved[0].ResolutionSource != ResolutionSourceInferred {
		t.Errorf("resolution_source = %q", resolved[0].ResolutionSource)
	}
	if resolved[0].Confidence != 0.99 {
		t.Errorf("confidence = %f, want 0.99", resolved[0].Confidence)
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
