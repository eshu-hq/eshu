// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTerraformImportMappingForFindingDeterministicIDs proves each supported
// AWS resource family resolves to its documented Terraform resource type and a
// deterministic import ID derived only from the finding ARN/ResourceID.
func TestTerraformImportMappingForFindingDeterministicIDs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		resourceType    string
		resourceID      string
		arn             string
		wantTFType      string
		wantImportID    string
		wantUnsupported bool
		wantEmptyImport bool
	}{
		{
			name:         "s3 bucket name from resource id",
			resourceType: "s3",
			resourceID:   "payments-prod-logs",
			arn:          "arn:aws:s3:::payments-prod-logs",
			wantTFType:   "aws_s3_bucket",
			wantImportID: "payments-prod-logs",
		},
		{
			name:         "s3 bucket name from arn fallback",
			resourceType: "s3",
			arn:          "arn:aws:s3:::payments-prod-logs",
			wantTFType:   "aws_s3_bucket",
			wantImportID: "payments-prod-logs",
		},
		{
			name:         "lambda function name strips function prefix",
			resourceType: "lambda",
			resourceID:   "function:payments-api",
			arn:          "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
			wantTFType:   "aws_lambda_function",
			wantImportID: "payments-api",
		},
		{
			name:         "sns topic import id is the full arn",
			resourceType: "sns",
			resourceID:   "payments-events",
			arn:          "arn:aws:sns:us-east-1:123456789012:payments-events",
			wantTFType:   "aws_sns_topic",
			wantImportID: "arn:aws:sns:us-east-1:123456789012:payments-events",
		},
		{
			name:            "sns subscription is ambiguous and refused",
			resourceType:    "sns",
			resourceID:      "payments-events:11111111-2222-3333-4444-555555555555",
			arn:             "arn:aws:sns:us-east-1:123456789012:payments-events:11111111-2222-3333-4444-555555555555",
			wantTFType:      "aws_sns_topic",
			wantEmptyImport: true,
		},
		{
			name:         "dynamodb table name strips table prefix",
			resourceType: "dynamodb",
			resourceID:   "table/GameScores",
			arn:          "arn:aws:dynamodb:us-east-1:123456789012:table/GameScores",
			wantTFType:   "aws_dynamodb_table",
			wantImportID: "GameScores",
		},
		{
			name:            "dynamodb table index is ambiguous and refused",
			resourceType:    "dynamodb",
			resourceID:      "table/GameScores/index/HighScores",
			arn:             "arn:aws:dynamodb:us-east-1:123456789012:table/GameScores/index/HighScores",
			wantTFType:      "aws_dynamodb_table",
			wantEmptyImport: true,
		},
		{
			name:         "ecr repository name strips repository prefix",
			resourceType: "ecr",
			resourceID:   "repository/test-service",
			arn:          "arn:aws:ecr:us-east-1:123456789012:repository/test-service",
			wantTFType:   "aws_ecr_repository",
			wantImportID: "test-service",
		},
		{
			name:         "cloudwatch log group name strips log-group prefix",
			resourceType: "logs",
			resourceID:   "log-group:/aws/lambda/payments-api",
			arn:          "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/payments-api",
			wantTFType:   "aws_cloudwatch_log_group",
			wantImportID: "/aws/lambda/payments-api",
		},
		{
			name:         "cloudwatch log group strips trailing wildcard",
			resourceType: "logs",
			resourceID:   "log-group:/aws/lambda/payments-api:*",
			arn:          "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/payments-api:*",
			wantTFType:   "aws_cloudwatch_log_group",
			wantImportID: "/aws/lambda/payments-api",
		},
		{
			name:            "log stream is ambiguous and refused",
			resourceType:    "logs",
			resourceID:      "log-group:/aws/lambda/payments-api:log-stream:2024",
			arn:             "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/payments-api:log-stream:2024",
			wantTFType:      "aws_cloudwatch_log_group",
			wantEmptyImport: true,
		},
		{
			name:            "unsupported family has no mapping",
			resourceType:    "sqs",
			resourceID:      "payments-queue",
			arn:             "arn:aws:sqs:us-east-1:123456789012:payments-queue",
			wantUnsupported: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			finding := IaCManagementFindingRow{
				ResourceType: tc.resourceType,
				ResourceID:   tc.resourceID,
				ARN:          tc.arn,
			}
			mapping, ok := terraformImportMappingForFinding(finding)
			if tc.wantUnsupported {
				if ok {
					t.Fatalf("terraformImportMappingForFinding() ok = true, want false for unsupported %q", tc.resourceType)
				}
				return
			}
			if !ok {
				t.Fatalf("terraformImportMappingForFinding() ok = false, want true for %q", tc.resourceType)
			}
			if got, want := mapping.ResourceType, tc.wantTFType; got != want {
				t.Fatalf("mapping.ResourceType = %q, want %q", got, want)
			}
			gotID := mapping.ImportID(finding)
			if tc.wantEmptyImport {
				if gotID != "" {
					t.Fatalf("mapping.ImportID() = %q, want empty (ambiguous identity must refuse)", gotID)
				}
				return
			}
			if got, want := gotID, tc.wantImportID; got != want {
				t.Fatalf("mapping.ImportID() = %q, want %q", got, want)
			}
		})
	}
}

// terraformImportPlanFixture builds a safety-approved cloud_only finding row for
// the expanded import-plan handler tests.
func terraformImportPlanFixture(resourceType, resourceID, arn string) IaCManagementFindingRow {
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
		Confidence:       0.95,
		ScopeID:          "aws:123456789012:us-east-1:" + resourceType,
		GenerationID:     "generation:aws-1",
		SourceSystem:     "aws",
		SafetyGate: IaCManagementSafetyGate{
			Outcome:        "read_only_allowed",
			ReadOnly:       true,
			ReviewRequired: false,
		},
	}
}

// TestHandleTerraformImportPlanCandidatesReadyForExpandedFamilies proves the
// expanded low-risk families produce ready import blocks end to end.
func TestHandleTerraformImportPlanCandidatesReadyForExpandedFamilies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		row          IaCManagementFindingRow
		wantTFType   string
		wantImportID string
		wantAddress  string
	}{
		{
			name:         "sns topic",
			row:          terraformImportPlanFixture("sns", "payments-events", "arn:aws:sns:us-east-1:123456789012:payments-events"),
			wantTFType:   "aws_sns_topic",
			wantImportID: "arn:aws:sns:us-east-1:123456789012:payments-events",
			wantAddress:  "aws_sns_topic.arn_aws_sns_us_east_1_123456789012_payments_events",
		},
		{
			name:         "dynamodb table",
			row:          terraformImportPlanFixture("dynamodb", "table/GameScores", "arn:aws:dynamodb:us-east-1:123456789012:table/GameScores"),
			wantTFType:   "aws_dynamodb_table",
			wantImportID: "GameScores",
			wantAddress:  "aws_dynamodb_table.gamescores",
		},
		{
			name:         "ecr repository",
			row:          terraformImportPlanFixture("ecr", "repository/test-service", "arn:aws:ecr:us-east-1:123456789012:repository/test-service"),
			wantTFType:   "aws_ecr_repository",
			wantImportID: "test-service",
			wantAddress:  "aws_ecr_repository.test_service",
		},
		{
			name:         "cloudwatch log group",
			row:          terraformImportPlanFixture("logs", "log-group:/aws/lambda/payments-api", "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/payments-api"),
			wantTFType:   "aws_cloudwatch_log_group",
			wantImportID: "/aws/lambda/payments-api",
			wantAddress:  "aws_cloudwatch_log_group.aws_lambda_payments_api",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidate := terraformImportPlanRequireReady(t, tc.row)
			if got, want := candidate["terraform_resource_type"], tc.wantTFType; got != want {
				t.Fatalf("terraform_resource_type = %q, want %q", got, want)
			}
			if got, want := candidate["import_id"], tc.wantImportID; got != want {
				t.Fatalf("import_id = %q, want %q", got, want)
			}
			if got, want := candidate["suggested_resource_address"], tc.wantAddress; got != want {
				t.Fatalf("suggested_resource_address = %q, want %q", got, want)
			}
		})
	}
}

// TestHandleTerraformImportPlanCandidatesRefusesAmbiguousExpandedIDs proves a
// cloud_only finding whose import identity is not exact stays refused with
// missing_provider_import_id rather than emitting a guessed block.
func TestHandleTerraformImportPlanCandidatesRefusesAmbiguousExpandedIDs(t *testing.T) {
	t.Parallel()

	row := terraformImportPlanFixture(
		"dynamodb",
		"table/GameScores/index/HighScores",
		"arn:aws:dynamodb:us-east-1:123456789012:table/GameScores/index/HighScores",
	)
	candidate := terraformImportPlanRequireRefused(t, row)
	refusal := candidate["refusal_reasons"].([]any)
	if got, want := refusal[0], "missing_provider_import_id"; got != want {
		t.Fatalf("refusal reason = %q, want %q", got, want)
	}
}

// TestHandleTerraformImportPlanCandidatesRefusesSecuritySensitiveExpanded proves
// the existing safety gate still refuses security-sensitive families such as IAM
// and KMS even though the wider AWS surface is now in scope.
func TestHandleTerraformImportPlanCandidatesRefusesSecuritySensitiveExpanded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		row  IaCManagementFindingRow
	}{
		{
			name: "iam role",
			row:  terraformImportPlanFixture("iam", "role/payments-task", "arn:aws:iam::123456789012:role/payments-task"),
		},
		{
			name: "kms key",
			row:  terraformImportPlanFixture("kms", "key/1234abcd", "arn:aws:kms:us-east-1:123456789012:key/1234abcd"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidate := terraformImportPlanRequireRefused(t, tc.row)
			refusal := candidate["refusal_reasons"].([]any)
			if got, want := refusal[0], "security_review_required"; got != want {
				t.Fatalf("refusal reason = %q, want %q", got, want)
			}
		})
	}
}

// TestHandleTerraformImportPlanCandidatesRefusesWrongManagementStatus proves an
// expanded family in a non-cloud_only status is refused as not importable.
func TestHandleTerraformImportPlanCandidatesRefusesWrongManagementStatus(t *testing.T) {
	t.Parallel()

	row := terraformImportPlanFixture("ecr", "repository/test-service", "arn:aws:ecr:us-east-1:123456789012:repository/test-service")
	row.ManagementStatus = managementStatusManagedByTerraform
	candidate := terraformImportPlanRequireRefused(t, row)
	refusal := candidate["refusal_reasons"].([]any)
	if got, want := refusal[0], "management_status_not_importable"; got != want {
		t.Fatalf("refusal reason = %q, want %q", got, want)
	}
}

func terraformImportPlanCandidateForRow(t *testing.T, row IaCManagementFindingRow) map[string]any {
	t.Helper()
	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{row}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/terraform-import-plan/candidates", bytes.NewBufferString(`{
		"account_id": "123456789012",
		"region": "us-east-1",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	return data["candidates"].([]any)[0].(map[string]any)
}

func terraformImportPlanRequireReady(t *testing.T, row IaCManagementFindingRow) map[string]any {
	t.Helper()
	candidate := terraformImportPlanCandidateForRow(t, row)
	if got, want := candidate["status"], "ready"; got != want {
		t.Fatalf("candidate.status = %q, want %q candidate=%#v", got, want, candidate)
	}
	return candidate
}

func terraformImportPlanRequireRefused(t *testing.T, row IaCManagementFindingRow) map[string]any {
	t.Helper()
	candidate := terraformImportPlanCandidateForRow(t, row)
	if got, want := candidate["status"], "refused"; got != want {
		t.Fatalf("candidate.status = %q, want %q candidate=%#v", got, want, candidate)
	}
	if _, ok := candidate["import_block"]; ok {
		t.Fatalf("refused candidate unexpectedly had import_block: %#v", candidate["import_block"])
	}
	return candidate
}
