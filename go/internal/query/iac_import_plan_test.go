package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleTerraformImportPlanCandidatesReturnsSafeS3Candidate(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{{
			ID:                "fact:aws-s3",
			Provider:          "aws",
			ResourceType:      "s3",
			ResourceID:        "payments-prod-logs",
			ARN:               "arn:aws:s3:::payments-prod-logs",
			FindingKind:       findingKindOrphanedCloudResource,
			ManagementStatus:  managementStatusCloudOnly,
			Confidence:        0.96,
			ScopeID:           "aws:123456789012:us-east-1:s3",
			GenerationID:      "generation:aws-1",
			SourceSystem:      "aws",
			RecommendedAction: "triage_owner_and_import_or_retire",
			SafetyGate: IaCManagementSafetyGate{
				Outcome:        "read_only_allowed",
				ReadOnly:       true,
				ReviewRequired: false,
			},
			Evidence: []IaCManagementEvidenceRow{
				{ID: "cloud", EvidenceType: "aws_cloud_resource", Key: "arn", Value: "arn:aws:s3:::payments-prod-logs", Confidence: 1},
			},
		}}},
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
	if got, want := resp.Truth.Capability, "iac_management.propose_terraform_import_plan"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := data["ready_count"], float64(1); got != want {
		t.Fatalf("ready_count = %#v, want %#v", got, want)
	}
	candidates := data["candidates"].([]any)
	candidate := candidates[0].(map[string]any)
	if got, want := candidate["status"], "ready"; got != want {
		t.Fatalf("candidate.status = %q, want %q", got, want)
	}
	if got, want := candidate["terraform_resource_type"], "aws_s3_bucket"; got != want {
		t.Fatalf("candidate.terraform_resource_type = %q, want %q", got, want)
	}
	if got, want := candidate["import_id"], "payments-prod-logs"; got != want {
		t.Fatalf("candidate.import_id = %q, want %q", got, want)
	}
	if got, want := candidate["suggested_resource_address"], "aws_s3_bucket.payments_prod_logs"; got != want {
		t.Fatalf("candidate.suggested_resource_address = %q, want %q", got, want)
	}
	if got, want := candidate["account_id"], "123456789012"; got != want {
		t.Fatalf("candidate.account_id = %q, want %q", got, want)
	}
	if got, want := candidate["region"], "us-east-1"; got != want {
		t.Fatalf("candidate.region = %q, want %q", got, want)
	}
	providerHint := candidate["provider_hint"].(map[string]any)
	if got, want := providerHint["alias"], "resource_123456789012_us_east_1"; got != want {
		t.Fatalf("provider_hint.alias = %q, want %q", got, want)
	}
	artifact := data["terraform_import_plan"].(map[string]any)
	if got, want := artifact["format"], "terraform_import_blocks"; got != want {
		t.Fatalf("artifact.format = %q, want %q", got, want)
	}
	if got, want := artifact["hcl"], "import {\n  to = aws_s3_bucket.payments_prod_logs\n  id = \"payments-prod-logs\"\n}\n"; got != want {
		t.Fatalf("artifact.hcl = %q, want %q", got, want)
	}
}

func TestHandleTerraformImportPlanCandidatesReturnsSafeLambdaCandidate(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{{
			ID:               "fact:aws-lambda",
			Provider:         "aws",
			AccountID:        "123456789012",
			Region:           "us-east-1",
			ResourceType:     "lambda",
			ResourceID:       "function:payments-api",
			ARN:              "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
			FindingKind:      findingKindOrphanedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			Confidence:       0.96,
			ScopeID:          "aws:123456789012:us-east-1:lambda",
			GenerationID:     "generation:aws-1",
			SourceSystem:     "aws",
			SafetyGate: IaCManagementSafetyGate{
				Outcome:        "read_only_allowed",
				ReadOnly:       true,
				ReviewRequired: false,
			},
		}}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/terraform-import-plan/candidates", bytes.NewBufferString(`{
		"scope_id": "aws:123456789012:us-east-1:lambda",
		"resource_id": "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
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
	candidate := data["candidates"].([]any)[0].(map[string]any)
	if got, want := candidate["terraform_resource_type"], "aws_lambda_function"; got != want {
		t.Fatalf("candidate.terraform_resource_type = %q, want %q", got, want)
	}
	if got, want := candidate["import_id"], "payments-api"; got != want {
		t.Fatalf("candidate.import_id = %q, want %q", got, want)
	}
	if got, want := candidate["suggested_resource_address"], "aws_lambda_function.payments_api"; got != want {
		t.Fatalf("candidate.suggested_resource_address = %q, want %q", got, want)
	}
	artifact := data["terraform_import_plan"].(map[string]any)
	if got, want := artifact["hcl"], "import {\n  to = aws_lambda_function.payments_api\n  id = \"payments-api\"\n}\n"; got != want {
		t.Fatalf("artifact.hcl = %q, want %q", got, want)
	}
}

func TestHandleTerraformImportPlanCandidatesRejectsProviderResourceID(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/terraform-import-plan/candidates", bytes.NewBufferString(`{
		"account_id": "123456789012",
		"resource_id": "payments-prod-logs"
	}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleTerraformImportPlanCandidatesRefusesSensitiveFinding(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{rows: []IaCManagementFindingRow{{
			ID:               "fact:aws-sg",
			Provider:         "aws",
			AccountID:        "123456789012",
			Region:           "us-east-1",
			ResourceType:     "ec2",
			ResourceID:       "security-group/sg-123",
			ARN:              "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123",
			FindingKind:      findingKindOrphanedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			Confidence:       0.94,
			ScopeID:          "aws:123456789012:us-east-1:ec2",
			GenerationID:     "generation:aws-1",
			SourceSystem:     "aws",
			WarningFlags:     []string{"security_sensitive_resource"},
			SafetyGate: IaCManagementSafetyGate{
				Outcome:        "security_review_required",
				ReadOnly:       true,
				ReviewRequired: true,
				RefusedActions: []string{"terraform_import_plan"},
				Warnings:       []string{"security_sensitive_resource"},
			},
		}}},
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
	if got, want := data["ready_count"], float64(0); got != want {
		t.Fatalf("ready_count = %#v, want %#v", got, want)
	}
	if got, want := data["refused_count"], float64(1); got != want {
		t.Fatalf("refused_count = %#v, want %#v", got, want)
	}
	candidate := data["candidates"].([]any)[0].(map[string]any)
	if got, want := candidate["status"], "refused"; got != want {
		t.Fatalf("candidate.status = %q, want %q", got, want)
	}
	if _, ok := candidate["import_block"]; ok {
		t.Fatalf("refused candidate unexpectedly had import_block: %#v", candidate["import_block"])
	}
	refusalReasons := candidate["refusal_reasons"].([]any)
	if got, want := refusalReasons[0], "security_review_required"; got != want {
		t.Fatalf("refusal reason = %q, want %q", got, want)
	}
	artifact := data["terraform_import_plan"].(map[string]any)
	if got, want := artifact["hcl"], ""; got != want {
		t.Fatalf("artifact.hcl = %q, want empty", got)
	}
}

func TestOpenAPITerraformImportPlanCandidatesIncludesFindingKinds(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/openapi.json", nil)
	w := httptest.NewRecorder()

	ServeOpenAPI(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	paths := spec["paths"].(map[string]any)
	path := paths["/api/v0/iac/terraform-import-plan/candidates"].(map[string]any)
	post := path["post"].(map[string]any)
	responses := post["responses"].(map[string]any)
	okResponse := responses["200"].(map[string]any)
	content := okResponse["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	schema := jsonContent["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["finding_kinds"]; !ok {
		t.Fatal("terraform import-plan OpenAPI response schema missing finding_kinds")
	}
}
