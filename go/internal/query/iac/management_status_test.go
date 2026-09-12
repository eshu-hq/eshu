// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestDeriveIaCManagementStatusCoversTaxonomy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input managementStatusInput
		want  string
	}{
		{
			name: "managed by terraform requires cloud state and config",
			input: managementStatusInput{
				HasCloudEvidence:           true,
				HasTerraformStateEvidence:  true,
				HasTerraformConfigEvidence: true,
			},
			want: ManagementStatusManagedByTerraform,
		},
		{
			name: "terraform state only requires cloud and state without config",
			input: managementStatusInput{
				HasCloudEvidence:          true,
				HasTerraformStateEvidence: true,
			},
			want: ManagementStatusTerraformStateOnly,
		},
		{
			name: "terraform config only requires config without cloud or state",
			input: managementStatusInput{
				HasTerraformConfigEvidence: true,
			},
			want: ManagementStatusTerraformConfigOnly,
		},
		{
			name: "cloud only requires cloud without IaC evidence",
			input: managementStatusInput{
				HasCloudEvidence: true,
			},
			want: ManagementStatusCloudOnly,
		},
		{
			name: "other IaC evidence stays separate from Terraform ownership",
			input: managementStatusInput{
				HasCloudEvidence:    true,
				HasOtherIaCEvidence: true,
			},
			want: ManagementStatusManagedByOtherIaC,
		},
		{
			name: "conflicting owners do not promote",
			input: managementStatusInput{
				HasCloudEvidence:           true,
				HasTerraformStateEvidence:  true,
				HasTerraformConfigEvidence: true,
				HasConflictingEvidence:     true,
			},
			want: ManagementStatusAmbiguous,
		},
		{
			name: "coverage gaps do not promote",
			input: managementStatusInput{
				HasCloudEvidence:       true,
				HasCoverageGapEvidence: true,
			},
			want: ManagementStatusUnknown,
		},
		{
			name: "stale IaC is first class",
			input: managementStatusInput{
				HasTerraformStateEvidence: true,
				HasStaleIaCEvidence:       true,
			},
			want: ManagementStatusStaleIaCCandidate,
		},
		{
			name:  "empty evidence is unknown",
			input: managementStatusInput{},
			want:  ManagementStatusUnknown,
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
		FindingKind:  FindingKindOrphanedCloudResource,
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

	finding := AWSRuntimeDriftRowToManagement(row)

	if got, want := finding.ManagementStatus, ManagementStatusCloudOnly; got != want {
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

func TestAWSRuntimeDriftRowToIaCManagementRejectsUnknownPayloadStatus(t *testing.T) {
	t.Parallel()

	row := postgres.AWSCloudRuntimeDriftFindingRow{
		FactID:           "fact:aws-cloud-runtime-drift:unknown-status",
		ScopeID:          "aws:123456789012:us-east-1:lambda",
		GenerationID:     "generation:aws-1",
		SourceSystem:     "aws",
		ARN:              "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		FindingKind:      FindingKindUnmanagedCloudResource,
		ManagementStatus: "terraform_state_onyl",
		Confidence:       0.87,
		Evidence: []postgres.AWSCloudRuntimeDriftEvidenceRow{
			{
				ID:           "evidence:cloud",
				SourceSystem: "aws",
				EvidenceType: "aws_cloud_resource",
				ScopeID:      "aws:123456789012:us-east-1:lambda",
				Key:          "arn",
				Value:        "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				Confidence:   0.92,
			},
			{
				ID:           "evidence:state",
				SourceSystem: "terraform_state",
				EvidenceType: "terraform_state_resource",
				ScopeID:      "terraform_state:s3:prod",
				Key:          "resource_address",
				Value:        "module.app.aws_lambda_function.payments",
				Confidence:   0.91,
			},
		},
	}

	finding := AWSRuntimeDriftRowToManagement(row)

	if got, want := finding.ManagementStatus, ManagementStatusTerraformStateOnly; got != want {
		t.Fatalf("ManagementStatus = %q, want fallback %q", got, want)
	}
}

func TestAWSRuntimeDriftRowToIaCManagementMarksRawTagsProvenanceCaseInsensitively(t *testing.T) {
	t.Parallel()

	row := postgres.AWSCloudRuntimeDriftFindingRow{
		FactID:       "fact:aws-cloud-runtime-drift:raw-tag-case",
		ScopeID:      "aws:123456789012:us-east-1:s3",
		GenerationID: "generation:aws-1",
		SourceSystem: "aws",
		ARN:          "arn:aws:s3:::payments-assets",
		FindingKind:  FindingKindOrphanedCloudResource,
		Confidence:   0.87,
		Evidence: []postgres.AWSCloudRuntimeDriftEvidenceRow{
			{
				ID:           "evidence:tag",
				SourceSystem: "aws",
				EvidenceType: "AWS_RAW_TAG",
				ScopeID:      "aws:123456789012:us-east-1:s3",
				Key:          "tag:service",
				Value:        "payments",
				Confidence:   1,
			},
		},
	}

	finding := AWSRuntimeDriftRowToManagement(row)

	if got, want := finding.Tags, map[string]string{"service": "payments"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Tags = %#v, want %#v", got, want)
	}
	if got, want := len(finding.Evidence), 1; got != want {
		t.Fatalf("Evidence len = %d, want %d", got, want)
	}
	if !finding.Evidence[0].ProvenanceOnly {
		t.Fatal("Evidence[0].ProvenanceOnly = false, want true for AWS_RAW_TAG")
	}
}
