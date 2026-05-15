package query

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestDeriveIaCManagementStatusCoversTaxonomy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input iacManagementStatusInput
		want  string
	}{
		{
			name: "managed by terraform requires cloud state and config",
			input: iacManagementStatusInput{
				HasCloudEvidence:           true,
				HasTerraformStateEvidence:  true,
				HasTerraformConfigEvidence: true,
			},
			want: managementStatusManagedByTerraform,
		},
		{
			name: "terraform state only requires cloud and state without config",
			input: iacManagementStatusInput{
				HasCloudEvidence:          true,
				HasTerraformStateEvidence: true,
			},
			want: managementStatusTerraformStateOnly,
		},
		{
			name: "terraform config only requires config without cloud or state",
			input: iacManagementStatusInput{
				HasTerraformConfigEvidence: true,
			},
			want: managementStatusTerraformConfigOnly,
		},
		{
			name: "cloud only requires cloud without IaC evidence",
			input: iacManagementStatusInput{
				HasCloudEvidence: true,
			},
			want: managementStatusCloudOnly,
		},
		{
			name: "other IaC evidence stays separate from Terraform ownership",
			input: iacManagementStatusInput{
				HasCloudEvidence:    true,
				HasOtherIaCEvidence: true,
			},
			want: managementStatusManagedByOtherIaC,
		},
		{
			name: "conflicting owners do not promote",
			input: iacManagementStatusInput{
				HasCloudEvidence:           true,
				HasTerraformStateEvidence:  true,
				HasTerraformConfigEvidence: true,
				HasConflictingEvidence:     true,
			},
			want: managementStatusAmbiguous,
		},
		{
			name: "coverage gaps do not promote",
			input: iacManagementStatusInput{
				HasCloudEvidence:       true,
				HasCoverageGapEvidence: true,
			},
			want: managementStatusUnknown,
		},
		{
			name: "stale IaC is first class",
			input: iacManagementStatusInput{
				HasTerraformStateEvidence: true,
				HasStaleIaCEvidence:       true,
			},
			want: managementStatusStaleIaCCandidate,
		},
		{
			name:  "empty evidence is unknown",
			input: iacManagementStatusInput{},
			want:  managementStatusUnknown,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := deriveIaCManagementStatus(test.input)
			if got != test.want {
				t.Fatalf("deriveIaCManagementStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAWSRuntimeDriftRowToIaCManagementExpandsReadModelFields(t *testing.T) {
	t.Parallel()

	row := postgres.AWSCloudRuntimeDriftFindingRow{
		FactID:       "fact:aws-cloud-runtime-drift:sg",
		ScopeID:      "aws:123456789012:us-east-1:ec2",
		GenerationID: "generation:aws-1",
		SourceSystem: "aws",
		ARN:          "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123",
		FindingKind:  findingKindOrphanedCloudResource,
		Confidence:   0.87,
		Evidence: []postgres.AWSCloudRuntimeDriftEvidenceRow{
			{
				ID:           "evidence:cloud",
				SourceSystem: "aws",
				EvidenceType: "aws_cloud_resource",
				ScopeID:      "aws:123456789012:us-east-1:ec2",
				Key:          "arn",
				Value:        "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123",
				Confidence:   0.92,
			},
			{
				ID:           "evidence:tag",
				SourceSystem: "aws",
				EvidenceType: "aws_raw_tag",
				ScopeID:      "aws:123456789012:us-east-1:ec2",
				Key:          "tag:service",
				Value:        "payments",
				Confidence:   1,
			},
		},
	}

	finding := awsRuntimeDriftRowToIaCManagement(row)

	if got, want := finding.ManagementStatus, managementStatusCloudOnly; got != want {
		t.Fatalf("ManagementStatus = %q, want %q", got, want)
	}
	if got, want := finding.MissingEvidence, []string{"terraform_state_resource", "terraform_config_resource"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("MissingEvidence = %#v, want %#v", got, want)
	}
	if got, want := finding.Tags, map[string]string{"service": "payments"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Tags = %#v, want %#v", got, want)
	}
	if got, want := finding.WarningFlags, []string{"raw_tags_provenance_only", "security_sensitive_resource"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("WarningFlags = %#v, want %#v", got, want)
	}
	if finding.MatchedTerraformStateAddress != "" {
		t.Fatalf("MatchedTerraformStateAddress = %q, want empty", finding.MatchedTerraformStateAddress)
	}
}
