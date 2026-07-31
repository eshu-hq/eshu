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
// fabricated), and every shared field copied through. This fixture uses a
// non-security-sensitive resource type (lambda) with a status that adds no
// derived warning flags, so the expected WarningFlags/ManagementStatus below
// are exactly the row's own stored values passed through
// awsCloudRuntimeDriftDerivedStatus unchanged;
// TestAWSCloudRuntimeDriftRowToNeutralSafetyGateMatchesAWSSurface covers the
// case where that derivation actually adds a flag. The six AWS-only
// enrichment fields (MatchedTerraformConfigFile/ModulePath,
// MatchedOtherIaCSource, ServiceCandidates, EnvironmentCandidates,
// DependencyPaths) are deliberately absent from both the row and
// MultiCloudRuntimeDriftFindingRow (#5759 follow-up P1-2): no real payload
// can ever populate them, so this fixture does not hand-set an impossible
// shape; see TestAWSCloudRuntimeDriftFindingStoreRoundTripsRealEncodedPayload
// (go/internal/storage/postgres) for the real encode-to-decode proof.
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
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("awsCloudRuntimeDriftRowToNeutral() = %#v, want %#v", got, want)
	}
}

// TestAWSCloudRuntimeDriftRowToNeutralSafetyGateMatchesAWSSurface is the
// #5759 follow-up P1-1 contract test: the SAME reducer_aws_cloud_runtime_drift_finding
// row must produce the SAME safety verdict on both surfaces.
// awsRuntimeDriftRowToIaCManagement (list_aws_runtime_drift_findings,
// find_unmanaged_resources) does not trust the row's stored WarningFlags
// alone -- it additionally classifies the resource type parsed from the ARN
// via warningFlagsForManagementFinding/securitySensitiveAWSResource, adding
// security_sensitive_resource for iam/kms/secretsmanager/ssm/rds and other
// sensitive AWS resource types. The reducer writer never stores that flag; it
// is purely a read-time classification. Before this fix,
// awsCloudRuntimeDriftRowToNeutral copied row.WarningFlags verbatim and never
// ran that classification, so an iam-role finding that is cloud_only showed
// security_review_required/review_required=true on the AWS-specific surface
// but read_only_allowed/review_required=false on the aggregated neutral
// surface for the IDENTICAL row -- a caller trusting the neutral surface
// would import a security-sensitive resource the AWS surface would have
// gated.
func TestAWSCloudRuntimeDriftRowToNeutralSafetyGateMatchesAWSSurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  postgres.AWSCloudRuntimeDriftFindingRow
	}{
		{
			name: "iam role, cloud_only",
			row: postgres.AWSCloudRuntimeDriftFindingRow{
				FactID:           "fact:aws-iam-orphan",
				ScopeID:          "aws:123456789012:us-east-1:iam",
				GenerationID:     "generation:aws-1",
				SourceSystem:     "aws",
				ARN:              "arn:aws:iam::123456789012:role/prod-payments-admin",
				FindingKind:      "orphaned_cloud_resource",
				ManagementStatus: "cloud_only",
				Confidence:       1.0,
			},
		},
		{
			name: "rds instance, cloud_only",
			row: postgres.AWSCloudRuntimeDriftFindingRow{
				FactID:           "fact:aws-rds-orphan",
				ScopeID:          "aws:123456789012:us-east-1:rds",
				GenerationID:     "generation:aws-1",
				SourceSystem:     "aws",
				ARN:              "arn:aws:rds:us-east-1:123456789012:db:prod-payments-db",
				FindingKind:      "orphaned_cloud_resource",
				ManagementStatus: "cloud_only",
				Confidence:       1.0,
			},
		},
		{
			name: "lambda function, unmanaged (not security-sensitive)",
			row: postgres.AWSCloudRuntimeDriftFindingRow{
				FactID:                       "fact:aws-lambda-unmanaged",
				ScopeID:                      "aws:123456789012:us-east-1:lambda",
				GenerationID:                 "generation:aws-1",
				SourceSystem:                 "aws",
				ARN:                          "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
				FindingKind:                  "unmanaged_cloud_resource",
				ManagementStatus:             "terraform_state_only",
				Confidence:                   0.92,
				MatchedTerraformStateAddress: "module.app.aws_lambda_function.payments",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			awsSurface := awsRuntimeDriftRowToIaCManagement(tt.row)
			neutralViews := cloudRuntimeDriftFindingViews([]MultiCloudRuntimeDriftFindingRow{
				awsCloudRuntimeDriftRowToNeutral(tt.row),
			})
			if len(neutralViews) != 1 {
				t.Fatalf("cloudRuntimeDriftFindingViews() returned %d views, want 1", len(neutralViews))
			}
			neutralSurface := neutralViews[0]

			if got, want := neutralSurface.ManagementStatus, awsSurface.ManagementStatus; got != want {
				t.Fatalf("neutral ManagementStatus = %q, want %q (AWS surface)", got, want)
			}
			if !reflect.DeepEqual(neutralSurface.SafetyGate.Warnings, awsSurface.SafetyGate.Warnings) {
				t.Fatalf("neutral SafetyGate.Warnings = %#v, want %#v (AWS surface)",
					neutralSurface.SafetyGate.Warnings, awsSurface.SafetyGate.Warnings)
			}
			if got, want := neutralSurface.SafetyGate.ReviewRequired, awsSurface.SafetyGate.ReviewRequired; got != want {
				t.Fatalf("neutral SafetyGate.ReviewRequired = %v, want %v (AWS surface) -- the two surfaces disagree about whether this resource is safe to import",
					got, want)
			}
			if got, want := neutralSurface.SafetyGate.Outcome, awsSurface.SafetyGate.Outcome; got != want {
				t.Fatalf("neutral SafetyGate.Outcome = %q, want %q (AWS surface)", got, want)
			}
			if got, want := len(neutralSurface.SafetyGate.RefusedActions), len(awsSurface.SafetyGate.RefusedActions); (got == 0) != (want == 0) {
				t.Fatalf("neutral SafetyGate.RefusedActions len=%d, want len=%d (AWS surface) -- refusal posture must match",
					got, want)
			}
		})
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
