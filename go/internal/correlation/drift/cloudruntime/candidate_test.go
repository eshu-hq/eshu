package cloudruntime

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
)

func TestBuildCandidatesUsesARNAsPrimaryJoinKey(t *testing.T) {
	t.Parallel()

	rows := []AddressedRow{
		{
			ARN:          "arn:aws:lambda:us-east-1:123456789012:function:worker",
			ResourceType: "aws_lambda_function",
			Cloud:        &ResourceRow{ARN: "arn:aws:lambda:us-east-1:123456789012:function:worker"},
		},
		{
			ARN:          "arn:aws:ecs:us-east-1:123456789012:service/prod/api",
			ResourceType: "aws_ecs_service",
			Cloud:        &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
			State:        &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
		},
		{
			ARN:          "arn:aws:eks:us-east-1:123456789012:cluster/prod",
			ResourceType: "aws_eks_cluster",
			Cloud:        &ResourceRow{ARN: "arn:aws:eks:us-east-1:123456789012:cluster/prod"},
			State:        &ResourceRow{ARN: "arn:aws:eks:us-east-1:123456789012:cluster/prod"},
			Config:       &ResourceRow{ARN: "arn:aws:eks:us-east-1:123456789012:cluster/prod"},
		},
	}

	got := BuildCandidates(rows, "aws_account:123456789012:us-east-1")
	if len(got) != 2 {
		t.Fatalf("len(BuildCandidates()) = %d, want 2", len(got))
	}

	wantKeys := []string{
		"arn:aws:ecs:us-east-1:123456789012:service/prod/api",
		"arn:aws:lambda:us-east-1:123456789012:function:worker",
	}
	for i, want := range wantKeys {
		if got[i].CorrelationKey != want {
			t.Fatalf("candidate[%d].CorrelationKey = %q, want %q", i, got[i].CorrelationKey, want)
		}
		if got[i].Kind != rules.AWSCloudRuntimeDriftPackName {
			t.Fatalf("candidate[%d].Kind = %q, want %q", i, got[i].Kind, rules.AWSCloudRuntimeDriftPackName)
		}
		if got[i].State != model.CandidateStateProvisional {
			t.Fatalf("candidate[%d].State = %q, want provisional", i, got[i].State)
		}
	}
}

func TestBuildCandidatesPreservesRawTagsAsEvidence(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ecs:us-east-1:123456789012:service/prod/api"
	got := BuildCandidates([]AddressedRow{{
		ARN:          arn,
		ResourceType: "aws_ecs_service",
		Cloud: &ResourceRow{
			ARN: arn,
			Tags: map[string]string{
				"Environment": "prod",
				"Service":     "api",
			},
		},
	}}, "aws_account:123456789012:us-east-1")

	if len(got) != 1 {
		t.Fatalf("len(BuildCandidates()) = %d, want 1", len(got))
	}

	var tagKeys []string
	for _, atom := range got[0].Evidence {
		if atom.EvidenceType == EvidenceTypeRawTag {
			tagKeys = append(tagKeys, atom.Key)
		}
	}
	want := []string{"tag:Environment", "tag:Service"}
	if !slices.Equal(tagKeys, want) {
		t.Fatalf("raw tag evidence keys = %v, want %v", tagKeys, want)
	}
}

func TestBuildCandidatesKeepsWeakTagsAsProvenanceOnly(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ecs:us-east-1:123456789012:service/prod/api"
	candidates := BuildCandidates([]AddressedRow{{
		ARN:          arn,
		ResourceType: "aws_ecs_service",
		Cloud: &ResourceRow{
			ARN: arn,
			Tags: map[string]string{
				"Name":    "module.api.aws_ecs_service.this",
				"Service": "api",
			},
		},
	}}, "aws_account:123456789012:us-east-1")

	if len(candidates) != 1 {
		t.Fatalf("len(BuildCandidates()) = %d, want 1", len(candidates))
	}
	if got, want := findingKindValue(candidates[0]), string(FindingKindOrphanedCloudResource); got != want {
		t.Fatalf("finding kind = %q, want %q", got, want)
	}
	if hasEvidenceType(candidates[0], EvidenceTypeStateResource) {
		t.Fatalf("weak tag/name evidence promoted to %q ownership evidence", EvidenceTypeStateResource)
	}
}

func TestBuildCandidatesEmitsUnknownAndAmbiguousManagementEvidence(t *testing.T) {
	t.Parallel()

	unknownARN := "arn:aws:lambda:us-east-1:123456789012:function:worker"
	ambiguousARN := "arn:aws:s3:::shared-bucket"
	candidates := BuildCandidates([]AddressedRow{
		{
			ARN:              unknownARN,
			ResourceType:     "aws_lambda_function",
			Cloud:            &ResourceRow{ARN: unknownARN},
			State:            &ResourceRow{ARN: unknownARN, Address: "aws_lambda_function.worker"},
			FindingKind:      FindingKindUnknownCloudResource,
			ManagementStatus: ManagementStatusUnknown,
			MissingEvidence:  []string{"terraform_config_owner"},
			WarningFlags:     []string{"unresolved_terraform_backend_owner"},
		},
		{
			ARN:              ambiguousARN,
			ResourceType:     "aws_s3_bucket",
			Cloud:            &ResourceRow{ARN: ambiguousARN},
			State:            &ResourceRow{ARN: ambiguousARN, Address: "aws_s3_bucket.shared"},
			FindingKind:      FindingKindAmbiguousCloudResource,
			ManagementStatus: ManagementStatusAmbiguous,
			MissingEvidence:  []string{"single_terraform_state_owner"},
			WarningFlags:     []string{"ambiguous_terraform_state_owner"},
		},
	}, "aws_account:123456789012:us-east-1")

	if len(candidates) != 2 {
		t.Fatalf("len(BuildCandidates()) = %d, want 2", len(candidates))
	}
	byKind := map[string]model.Candidate{}
	for _, candidate := range candidates {
		byKind[findingKindValue(candidate)] = candidate
	}
	ambiguous := byKind[string(FindingKindAmbiguousCloudResource)]
	unknown := byKind[string(FindingKindUnknownCloudResource)]
	if ambiguous.ID == "" {
		t.Fatalf("missing %q candidate", FindingKindAmbiguousCloudResource)
	}
	if unknown.ID == "" {
		t.Fatalf("missing %q candidate", FindingKindUnknownCloudResource)
	}
	if !hasEvidenceType(ambiguous, EvidenceTypeAmbiguousManagement) {
		t.Fatalf("ambiguous candidate missing %q evidence", EvidenceTypeAmbiguousManagement)
	}
	if !hasEvidenceType(unknown, EvidenceTypeCoverageGap) {
		t.Fatalf("unknown candidate missing %q evidence", EvidenceTypeCoverageGap)
	}
}

func TestBuildCandidatesValidatesStructuralEvidenceWithRulePack(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:lambda:us-east-1:123456789012:function:worker"
	candidates := BuildCandidates([]AddressedRow{{
		ARN:          arn,
		ResourceType: "aws_lambda_function",
		Cloud:        &ResourceRow{ARN: arn},
	}}, "aws_account:123456789012:us-east-1")

	if len(candidates) != 1 {
		t.Fatalf("len(BuildCandidates()) = %d, want 1", len(candidates))
	}
	if err := candidates[0].Validate(); err != nil {
		t.Fatalf("candidate.Validate() error = %v, want nil", err)
	}
	if err := rules.AWSCloudRuntimeDriftRulePack().Validate(); err != nil {
		t.Fatalf("AWSCloudRuntimeDriftRulePack().Validate() error = %v, want nil", err)
	}
	if !hasEvidenceType(candidates[0], EvidenceTypeCloudResourceARN) {
		t.Fatalf("candidate evidence missing %q", EvidenceTypeCloudResourceARN)
	}
}

func hasEvidenceType(candidate model.Candidate, evidenceType string) bool {
	for _, atom := range candidate.Evidence {
		if atom.EvidenceType == evidenceType {
			return true
		}
	}
	return false
}

func findingKindValue(candidate model.Candidate) string {
	for _, atom := range candidate.Evidence {
		if atom.EvidenceType == EvidenceTypeFindingKind {
			return atom.Value
		}
	}
	return ""
}
