package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestAWSRuntimeDriftRowToIaCManagementRedactsSensitiveEvidenceValues(t *testing.T) {
	t.Parallel()

	row := postgres.AWSCloudRuntimeDriftFindingRow{
		FactID:           "fact:aws-secret-tag",
		ScopeID:          "aws:123456789012:us-east-1:lambda",
		GenerationID:     "generation:aws-1",
		SourceSystem:     "aws",
		ARN:              "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		FindingKind:      findingKindOrphanedCloudResource,
		ManagementStatus: managementStatusCloudOnly,
		Confidence:       0.95,
		Evidence: []postgres.AWSCloudRuntimeDriftEvidenceRow{
			{
				ID:           "evidence:secret-tag",
				SourceSystem: "aws",
				EvidenceType: "aws_raw_tag",
				ScopeID:      "aws:123456789012:us-east-1:lambda",
				Key:          "tag:Secret",
				Value:        "plaintext-secret-value",
				Confidence:   1,
			},
			{
				ID:           "evidence:env",
				SourceSystem: "aws",
				EvidenceType: "aws_cloud_resource",
				ScopeID:      "aws:123456789012:us-east-1:lambda",
				Key:          "environment_password",
				Value:        "plaintext-password-value",
				Confidence:   1,
			},
		},
	}

	finding := awsRuntimeDriftRowToIaCManagement(row)

	if got, want := finding.Tags["Secret"], "[REDACTED]"; got != want {
		t.Fatalf("redacted tag value = %q, want %q", got, want)
	}
	for _, evidence := range finding.Evidence {
		if strings.Contains(evidence.Value, "plaintext-") {
			t.Fatalf("evidence leaked plaintext value: %#v", evidence)
		}
	}
	if got, want := finding.SafetyGate.Redactions, []string{"sensitive_evidence_value"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SafetyGate.Redactions = %#v, want %#v", got, want)
	}
	if got, want := finding.SafetyGate.AuditExpectation, "log caller, scope, route, finding id, and safety outcome without resource secrets"; got != want {
		t.Fatalf("SafetyGate.AuditExpectation = %q, want %q", got, want)
	}
}

func TestHandleIaCManagementStatusCarriesSecurityReviewGate(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123"
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			rows: []IaCManagementFindingRow{{
				ID:                "fact:aws-sg",
				Provider:          "aws",
				AccountID:         "123456789012",
				Region:            "us-east-1",
				ResourceType:      "ec2",
				ResourceID:        "security-group/sg-123",
				ARN:               arn,
				FindingKind:       findingKindOrphanedCloudResource,
				ManagementStatus:  managementStatusCloudOnly,
				Confidence:        0.95,
				ScopeID:           "aws:123456789012:us-east-1:ec2",
				GenerationID:      "generation:aws-1",
				SourceSystem:      "aws",
				RecommendedAction: "triage_owner_and_import_or_retire",
				WarningFlags:      []string{"security_sensitive_resource"},
			}},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/management-status", bytes.NewBufferString(`{
		"account_id": "123456789012",
		"arn": "`+arn+`"
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
	gate := data["safety_gate"].(map[string]any)
	if got, want := gate["outcome"], "security_review_required"; got != want {
		t.Fatalf("safety_gate.outcome = %q, want %q", got, want)
	}
	refused := gate["refused_actions"].([]any)
	if got, want := refused[0], "terraform_import_plan"; got != want {
		t.Fatalf("refused action = %q, want %q", got, want)
	}
	if got, want := gate["review_required"], true; got != want {
		t.Fatalf("review_required = %v, want %v", got, want)
	}
}

func TestIaCManagementSafetySummaryCountsReviewAndRedactions(t *testing.T) {
	t.Parallel()

	summary := iacManagementSafetySummary([]IaCManagementFindingRow{
		{
			ID:               "finding:one",
			WarningFlags:     []string{"security_sensitive_resource"},
			ManagementStatus: managementStatusCloudOnly,
			SafetyGate: IaCManagementSafetyGate{
				Outcome:        "security_review_required",
				ReviewRequired: true,
				Redactions:     []string{"sensitive_evidence_value"},
			},
		},
		{
			ID:               "finding:two",
			WarningFlags:     []string{"ambiguous_ownership"},
			ManagementStatus: managementStatusAmbiguous,
		},
	})

	if got, want := summary.TotalFindings, 2; got != want {
		t.Fatalf("TotalFindings = %d, want %d", got, want)
	}
	if got, want := summary.ReviewRequiredCount, 2; got != want {
		t.Fatalf("ReviewRequiredCount = %d, want %d", got, want)
	}
	if got, want := summary.RedactedFindingsCount, 1; got != want {
		t.Fatalf("RedactedFindingsCount = %d, want %d", got, want)
	}
}
