// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// safeReadyFinding returns a cloud_only finding whose safety gate is read-only
// allowed for the given AWS family, so it produces a ready import candidate.
func safeReadyFinding(resourceType, resourceID, arn string) IaCManagementFindingRow {
	return IaCManagementFindingRow{
		ID:               "fact:" + resourceType,
		Provider:         "aws",
		AccountID:        "123456789012",
		Region:           "us-east-1",
		ResourceType:     resourceType,
		ResourceID:       resourceID,
		ARN:              arn,
		FindingKind:      findingKindOrphanedCloudResource,
		ManagementStatus: managementStatusCloudOnly,
		Confidence:       0.96,
		ScopeID:          "aws:123456789012:us-east-1:" + resourceType,
		GenerationID:     "generation:aws-1",
		SourceSystem:     "aws",
		Tags:             map[string]string{"Owner": "payments-secret-team", "secret_token": "AKIAEXAMPLESECRET"},
		SafetyGate: IaCManagementSafetyGate{
			Outcome:        "read_only_allowed",
			ReadOnly:       true,
			ReviewRequired: false,
		},
	}
}

func TestConfigShapeHintForEachSupportedResourceType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		finding      IaCManagementFindingRow
		resourceType string
		wantRequired []string
	}{
		{
			name:         "s3",
			finding:      safeReadyFinding("s3", "payments-prod-logs", "arn:aws:s3:::payments-prod-logs"),
			resourceType: "aws_s3_bucket",
			wantRequired: []string{"bucket"},
		},
		{
			name:         "lambda",
			finding:      safeReadyFinding("lambda", "function:payments-api", "arn:aws:lambda:us-east-1:123456789012:function:payments-api"),
			resourceType: "aws_lambda_function",
			wantRequired: []string{"function_name", "role"},
		},
		{
			name:         "sns",
			finding:      safeReadyFinding("sns", "payments-events", "arn:aws:sns:us-east-1:123456789012:payments-events"),
			resourceType: "aws_sns_topic",
			wantRequired: []string{"name"},
		},
		{
			name:         "dynamodb",
			finding:      safeReadyFinding("dynamodb", "table/payments", "arn:aws:dynamodb:us-east-1:123456789012:table/payments"),
			resourceType: "aws_dynamodb_table",
			wantRequired: []string{"billing_mode", "hash_key", "name"},
		},
		{
			name:         "ecr",
			finding:      safeReadyFinding("ecr", "repository/payments", "arn:aws:ecr:us-east-1:123456789012:repository/payments"),
			resourceType: "aws_ecr_repository",
			wantRequired: []string{"name"},
		},
		{
			name:         "logs",
			finding:      safeReadyFinding("logs", "log-group:/aws/lambda/payments", "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/payments"),
			resourceType: "aws_cloudwatch_log_group",
			wantRequired: []string{"name"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidate := terraformImportPlanCandidateForFinding(tc.finding, IaCManagementFilter{})
			if candidate.Status != "ready" {
				t.Fatalf("status = %q, want ready (refusal=%v)", candidate.Status, candidate.RefusalReasons)
			}
			if candidate.ConfigShapeHint == nil {
				t.Fatal("ready candidate missing config_shape_hint")
			}
			hint := candidate.ConfigShapeHint
			if hint.Format != "terraform_resource_skeleton" {
				t.Fatalf("hint.Format = %q, want terraform_resource_skeleton", hint.Format)
			}
			if hint.ResourceAddress != candidate.SuggestedResourceAddress {
				t.Fatalf("hint.ResourceAddress = %q, want %q", hint.ResourceAddress, candidate.SuggestedResourceAddress)
			}
			if !equalStringSlices(hint.RequiredArguments, tc.wantRequired) {
				t.Fatalf("hint.RequiredArguments = %v, want %v", hint.RequiredArguments, tc.wantRequired)
			}
			if len(hint.ManualFillWarnings) == 0 {
				t.Fatal("hint.ManualFillWarnings is empty")
			}
			if !strings.Contains(hint.HCLSkeleton, configShapeHintPlaceholder) {
				t.Fatalf("hint.HCLSkeleton lacks placeholder %q: %s", configShapeHintPlaceholder, hint.HCLSkeleton)
			}
			// The skeleton must reference the resource type label, never the
			// import identity as a value.
			if !strings.Contains(hint.HCLSkeleton, tc.resourceType) {
				t.Fatalf("hint.HCLSkeleton missing resource type %q: %s", tc.resourceType, hint.HCLSkeleton)
			}
		})
	}
}

// TestConfigShapeHintNeverLeaksSensitiveValues asserts the strongest security
// guarantee: no string the hint emits contains a tag value, secret, or ARN
// beyond the import identity already exposed on the candidate. Only the
// resource-type label and the placeholder may appear; argument names are a
// fixed structural vocabulary.
func TestConfigShapeHintNeverLeaksSensitiveValues(t *testing.T) {
	t.Parallel()

	finding := safeReadyFinding("s3", "payments-prod-logs", "arn:aws:s3:::payments-prod-logs")
	finding.MatchedTerraformStateAddress = "module.secret.aws_s3_bucket.x"
	finding.MatchedTerraformConfigFile = "secret/private.tf"
	candidate := terraformImportPlanCandidateForFinding(finding, IaCManagementFilter{})
	if candidate.ConfigShapeHint == nil {
		t.Fatal("ready candidate missing config_shape_hint")
	}

	banned := []string{
		"payments-secret-team", // tag value
		"AKIAEXAMPLESECRET",    // secret-like tag value
		"secret_token",         // tag key
		"arn:aws:s3:::",        // ARN prefix beyond identity
		"module.secret",        // state locator
		"private.tf",           // config file locator
	}
	for _, s := range collectHintStrings(candidate.ConfigShapeHint) {
		for _, bad := range banned {
			if strings.Contains(s, bad) {
				t.Fatalf("config_shape_hint string %q leaked banned token %q", s, bad)
			}
		}
	}
}

func TestConfigShapeHintAbsentForRefusedCandidate(t *testing.T) {
	t.Parallel()

	finding := safeReadyFinding("ec2", "security-group/sg-123", "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123")
	finding.SafetyGate = IaCManagementSafetyGate{
		Outcome:        "security_review_required",
		ReadOnly:       true,
		ReviewRequired: true,
		RefusedActions: []string{"terraform_import_plan"},
	}
	candidate := terraformImportPlanCandidateForFinding(finding, IaCManagementFilter{})
	if candidate.Status != "refused" {
		t.Fatalf("status = %q, want refused", candidate.Status)
	}
	if candidate.ConfigShapeHint != nil {
		t.Fatalf("refused candidate unexpectedly carried config_shape_hint: %#v", candidate.ConfigShapeHint)
	}
}

func TestConfigShapeHintAbsentForUnsupportedResourceType(t *testing.T) {
	t.Parallel()

	finding := safeReadyFinding("ec2", "instance/i-123", "arn:aws:ec2:us-east-1:123456789012:instance/i-123")
	candidate := terraformImportPlanCandidateForFinding(finding, IaCManagementFilter{})
	if candidate.Status != "refused" {
		t.Fatalf("status = %q, want refused", candidate.Status)
	}
	if candidate.ConfigShapeHint != nil {
		t.Fatal("unsupported finding unexpectedly carried config_shape_hint")
	}
}

func TestConfigShapeHintForResourceTypeRejectsUnknownType(t *testing.T) {
	t.Parallel()

	if _, ok := configShapeHintForResourceType("aws_instance", "aws_instance.x", ""); ok {
		t.Fatal("configShapeHintForResourceType returned ok for unmapped type")
	}
}

// collectHintStrings returns every string the hint serializes, so the security
// test can scan all of them for leaked values.
func collectHintStrings(hint *terraformConfigShapeHint) []string {
	out := []string{hint.Format, hint.ResourceAddress, hint.ProviderAlias, hint.HCLSkeleton}
	out = append(out, hint.RequiredArguments...)
	out = append(out, hint.NotableOptionalArguments...)
	out = append(out, hint.OmittedSensitiveArguments...)
	out = append(out, hint.ManualFillWarnings...)
	return out
}
