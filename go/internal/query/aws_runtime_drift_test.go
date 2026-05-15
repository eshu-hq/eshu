package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHandleAWSRuntimeDriftFindingsReturnsOutcomes(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
			rows: []IaCManagementFindingRow{
				{
					ID:               "fact:aws-lambda",
					Provider:         "aws",
					AccountID:        "123456789012",
					Region:           "us-east-1",
					ResourceType:     "lambda",
					ResourceID:       "function:payments-api",
					ARN:              "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
					FindingKind:      "unmanaged_cloud_resource",
					ManagementStatus: "terraform_state_only",
					Confidence:       0.91,
					ScopeID:          "aws:123456789012:us-east-1:lambda",
					GenerationID:     "generation:aws-1",
					SourceSystem:     "aws",
				},
				{
					ID:               "fact:aws-ambiguous",
					Provider:         "aws",
					AccountID:        "123456789012",
					Region:           "us-east-1",
					ResourceType:     "s3",
					ResourceID:       "ambiguous-bucket",
					ARN:              "arn:aws:s3:::ambiguous-bucket",
					FindingKind:      "ambiguous_cloud_resource",
					ManagementStatus: "ambiguous_management",
					Confidence:       0.5,
					ScopeID:          "aws:123456789012:us-east-1:s3",
					GenerationID:     "generation:aws-1",
					SourceSystem:     "aws",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/aws/runtime-drift/findings", bytes.NewBufferString(`{
		"account_id": "123456789012",
		"region": "us-east-1",
		"finding_kinds": ["unmanaged_cloud_resource", "ambiguous_cloud_resource"],
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := observed.AccountID, "123456789012"; got != want {
		t.Fatalf("observed.AccountID = %q, want %q", got, want)
	}
	if got, want := observed.FindingKinds, []string{"ambiguous_cloud_resource", "unmanaged_cloud_resource"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observed.FindingKinds = %#v, want %#v", got, want)
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := data["truth_basis"], "materialized_reducer_rows"; got != want {
		t.Fatalf("truth_basis = %q, want %q", got, want)
	}
	if got, want := resp.Truth.Capability, "aws_runtime_drift.findings.list"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	rawFindings := data["drift_findings"].([]any)
	if got, want := len(rawFindings), 2; got != want {
		t.Fatalf("drift_findings len = %d, want %d", got, want)
	}
	first := rawFindings[0].(map[string]any)
	if got, want := first["outcome"], "derived"; got != want {
		t.Fatalf("first outcome = %q, want %q", got, want)
	}
	if got, want := first["promotion_outcome"], "not_promoted"; got != want {
		t.Fatalf("first promotion_outcome = %q, want %q", got, want)
	}
	groups := data["outcome_groups"].([]any)
	if got, want := len(groups), 2; got != want {
		t.Fatalf("outcome_groups len = %d, want %d", got, want)
	}
}

func TestHandleAWSRuntimeDriftFindingsRequiresBoundedScope(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/aws/runtime-drift/findings", bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}
