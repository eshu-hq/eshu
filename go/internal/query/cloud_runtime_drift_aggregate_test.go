// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestAWSCloudRuntimeDriftRowToNeutralMapsEveryField is the #5759 follow-up
// P1 field-shape reconciliation proof: an AWS-origin row must carry
// Provider="aws", a genuinely computed CloudResourceUID (not blank, not
// fabricated), every shared field copied directly, and the AWS-only
// enrichment fields carried through rather than dropped.
func TestAWSCloudRuntimeDriftRowToNeutralMapsEveryField(t *testing.T) {
	t.Parallel()

	row := postgres.AWSCloudRuntimeDriftFindingRow{
		FactID:                       "fact:aws-unmanaged",
		ScopeID:                      "aws:123456789012:us-east-1:lambda",
		GenerationID:                 "generation:aws-1",
		SourceSystem:                 "aws",
		ARN:                          "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		FindingKind:                  "unmanaged_cloud_resource",
		ManagementStatus:             "terraform_state_only",
		Confidence:                   0.92,
		MatchedTerraformStateAddress: "module.app.aws_lambda_function.payments",
		MatchedTerraformConfigFile:   "services/payments/lambda.tf",
		MatchedTerraformModulePath:   "module.app",
		MatchedOtherIaCSource:        "",
		ServiceCandidates:            []string{"payments"},
		EnvironmentCandidates:        []string{"prod"},
		DependencyPaths:              []string{"service:payments -> lambda:payments-api"},
		MissingEvidence:              []string{"terraform_config_resource"},
		WarningFlags:                 []string{"security_sensitive_resource"},
		RecommendedAction:            "restore_config_or_prepare_import_block",
		Evidence: []postgres.AWSCloudRuntimeDriftEvidenceRow{
			{Key: "declared_ami", Value: "ami-0123456789abcdef0"},
			{Key: "observed_ami", Value: "ami-000000000000000a"},
		},
	}

	got := awsCloudRuntimeDriftRowToNeutral(row)

	wantUID := cloudinventory.ResolveProviderIdentity(
		cloudinventory.ProviderAWS,
		"arn:aws:lambda:us-east-1:123456789012:function:payments-api",
	).CloudResourceUID
	if wantUID == "" {
		t.Fatal("test fixture ARN must resolve to a non-empty canonical uid")
	}

	want := MultiCloudRuntimeDriftFindingRow{
		FactID:                       "fact:aws-unmanaged",
		ScopeID:                      "aws:123456789012:us-east-1:lambda",
		GenerationID:                 "generation:aws-1",
		SourceSystem:                 "aws",
		Provider:                     "aws",
		CloudResourceUID:             wantUID,
		RawIdentity:                  "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		FindingKind:                  "unmanaged_cloud_resource",
		ManagementStatus:             "terraform_state_only",
		Confidence:                   0.92,
		MatchedTerraformStateAddress: "module.app.aws_lambda_function.payments",
		MissingEvidence:              []string{"terraform_config_resource"},
		WarningFlags:                 []string{"security_sensitive_resource"},
		RecommendedAction:            "restore_config_or_prepare_import_block",
		DriftedAttributes: []DriftedAttributeView{
			{Attribute: "ami", Declared: "ami-0123456789abcdef0", Observed: "ami-000000000000000a"},
		},
		MatchedTerraformConfigFile: "services/payments/lambda.tf",
		MatchedTerraformModulePath: "module.app",
		ServiceCandidates:          []string{"payments"},
		EnvironmentCandidates:      []string{"prod"},
		DependencyPaths:            []string{"service:payments -> lambda:payments-api"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("awsCloudRuntimeDriftRowToNeutral() = %#v, want %#v", got, want)
	}
}

// TestAWSCloudRuntimeDriftRowToNeutralNeverFabricatesUIDForUnresolvableARN
// proves a malformed/unresolvable ARN yields an empty CloudResourceUID
// rather than a fabricated one, matching cloudinventory.ResolveProviderIdentity's
// "never invent canonical truth" contract.
func TestAWSCloudRuntimeDriftRowToNeutralNeverFabricatesUIDForUnresolvableARN(t *testing.T) {
	t.Parallel()

	got := awsCloudRuntimeDriftRowToNeutral(postgres.AWSCloudRuntimeDriftFindingRow{
		FactID: "fact:aws-malformed",
		ARN:    "",
	})
	if got.CloudResourceUID != "" {
		t.Fatalf("CloudResourceUID = %q, want empty for a blank ARN", got.CloudResourceUID)
	}
	if got.Provider != "aws" {
		t.Fatalf("Provider = %q, want %q even when the uid cannot resolve", got.Provider, "aws")
	}
}

// TestCloudRuntimeDriftAggregateRowFromStoreDispatchesByFactKind proves the
// FactKind-tagged dispatch selects the right mapping and never bleeds a
// zero-valued MultiCloud struct's fields onto an AWS row or vice versa.
func TestCloudRuntimeDriftAggregateRowFromStoreDispatchesByFactKind(t *testing.T) {
	t.Parallel()

	awsRow := cloudRuntimeDriftAggregateRowFromStore(postgres.CloudRuntimeDriftAggregateFindingRow{
		FactKind: postgres.AWSCloudRuntimeDriftFindingFactKind,
		AWS:      postgres.AWSCloudRuntimeDriftFindingRow{FactID: "fact:aws-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0a"},
	})
	if got, want := awsRow.Provider, "aws"; got != want {
		t.Fatalf("aws dispatch Provider = %q, want %q", got, want)
	}
	if got, want := awsRow.FactID, "fact:aws-1"; got != want {
		t.Fatalf("aws dispatch FactID = %q, want %q", got, want)
	}

	gcpRow := cloudRuntimeDriftAggregateRowFromStore(postgres.CloudRuntimeDriftAggregateFindingRow{
		FactKind: postgres.MultiCloudRuntimeDriftFindingFactKind,
		MultiCloud: postgres.MultiCloudRuntimeDriftFindingRow{
			FactID: "fact:gcp-1", Provider: "gcp", CloudResourceUID: "cloud_resource:abc",
		},
	})
	if got, want := gcpRow.Provider, "gcp"; got != want {
		t.Fatalf("gcp dispatch Provider = %q, want %q", got, want)
	}
	if got, want := gcpRow.FactID, "fact:gcp-1"; got != want {
		t.Fatalf("gcp dispatch FactID = %q, want %q", got, want)
	}
}
