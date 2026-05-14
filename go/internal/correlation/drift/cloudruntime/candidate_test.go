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
