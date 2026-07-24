// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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

// awsRuntimeDriftScopedFixtureRows returns the two-finding fixture keyed to a
// single exact scope_id, matching what a scoped caller's grant would need to
// contain for TestHandleAWSRuntimeDriftFindingsScopedInGrantReturnsRealRowData.
func awsRuntimeDriftScopedFixtureRows() []IaCManagementFindingRow {
	return []IaCManagementFindingRow{
		{
			ID:               "fact:aws-lambda-tenant-a",
			Provider:         "aws",
			AccountID:        "123456789012",
			Region:           "us-east-1",
			ResourceType:     "lambda",
			ResourceID:       "function:tenant-a-payments-api",
			ARN:              "arn:aws:lambda:us-east-1:123456789012:function:tenant-a-payments-api",
			FindingKind:      "unmanaged_cloud_resource",
			ManagementStatus: "terraform_state_only",
			Confidence:       0.91,
			ScopeID:          "aws:123456789012:us-east-1:lambda",
			GenerationID:     "generation:aws-1",
			SourceSystem:     "aws",
		},
	}
}

// TestHandleAWSRuntimeDriftFindingsScopedAccountOnlyNeverCallsStore proves the
// #5167 handler-side precheck (IaCManagementStore/IaCManagementFilter are
// shared with the iac/* and replatforming/* route families owned by other
// workstreams, so the fix is a caller-side grant check rather than a
// store/filter change): a scoped caller supplying only account_id (no exact
// scope_id) must never reach the store, because the underlying LIKE-prefix
// scan would otherwise fan out across every region/service scope under that
// account without this precheck being able to narrow it to the grant.
func TestHandleAWSRuntimeDriftFindingsScopedAccountOnlyNeverCallsStore(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
			rows:           awsRuntimeDriftScopedFixtureRows(),
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/aws/runtime-drift/findings", bytes.NewBufferString(`{
		"account_id": "123456789012",
		"region": "us-east-1",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"aws:123456789012:us-east-1:lambda"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if observed.AccountID != "" {
		t.Fatalf("store was called for an account_id-only scoped request: observed filter = %#v", observed)
	}
	if strings.Contains(w.Body.String(), "tenant-a-payments-api") {
		t.Fatalf("account_id-only scoped response leaked fixture row data: %s", w.Body.String())
	}
}

// TestHandleAWSRuntimeDriftFindingsScopedOutOfGrantScopeNeverCallsStore covers
// the exact-scope_id case: a scoped caller whose requested scope_id is
// outside its grant must never reach the store.
func TestHandleAWSRuntimeDriftFindingsScopedOutOfGrantScopeNeverCallsStore(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
			rows:           awsRuntimeDriftScopedFixtureRows(),
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/aws/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "aws:123456789012:us-east-1:lambda",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"aws:999999999999:us-east-1:lambda"}, // different account
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if observed.ScopeID != "" {
		t.Fatalf("store was called for an out-of-grant scope_id: observed filter = %#v", observed)
	}
}

// TestHandleAWSRuntimeDriftFindingsScopedInGrantReturnsRealRowData proves the
// paired positive case: a scoped caller whose requested exact scope_id IS
// granted reaches the store and gets the real fixture rows back.
func TestHandleAWSRuntimeDriftFindingsScopedInGrantReturnsRealRowData(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
			rows:           awsRuntimeDriftScopedFixtureRows(),
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/aws/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "aws:123456789012:us-east-1:lambda",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"aws:123456789012:us-east-1:lambda"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := observed.ScopeID, "aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("store was not called for an in-grant scoped request: observed.ScopeID = %q, want %q", got, want)
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	findings := data["drift_findings"].([]any)
	if len(findings) != 1 {
		t.Fatalf("drift_findings len = %d, want 1; body = %s", len(findings), w.Body.String())
	}
	first := findings[0].(map[string]any)
	if got, want := first["arn"], "arn:aws:lambda:us-east-1:123456789012:function:tenant-a-payments-api"; got != want {
		t.Fatalf("first arn = %q, want %q (real row data)", got, want)
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

// TestHandleAWSRuntimeDriftFindingsDefaultKindsIncludeImageVersionDrift
// proves the #5453 fix: unlike handleUnmanagedCloudResources (which
// intentionally keeps its narrower existence-only default, see
// TestHandleUnmanagedCloudResourcesDefaultsToActionableAWSFindingKinds), this
// route is the "runtime drift findings" surface. A caller who names no
// explicit finding_kinds must still see a managed-but-value-drifted resource
// (image_version_drift) on the default page, not just the four
// existence/ambiguity kinds -- a managed resource is not "unmanaged" and must
// not be structurally excluded from its own drift-findings default.
func TestHandleAWSRuntimeDriftFindingsDefaultKindsIncludeImageVersionDrift(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
			rows: []IaCManagementFindingRow{
				{
					ID:               "fact:aws-ami-drift",
					Provider:         "aws",
					AccountID:        "123456789012",
					Region:           "us-east-1",
					ResourceType:     "ec2_instance",
					ResourceID:       "i-0123456789abcdef0",
					ARN:              "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
					FindingKind:      "image_version_drift",
					ManagementStatus: "managed_by_terraform",
					Confidence:       0.95,
					ScopeID:          "aws:123456789012:us-east-1:ec2",
					GenerationID:     "generation:aws-1",
					SourceSystem:     "aws",
					DriftedAttributes: []DriftedAttributeView{
						{Attribute: "ami", Declared: "ami-0123456789abcdef0", Observed: "ami-000000000000000a"},
					},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/aws/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "aws:123456789012:us-east-1:ec2",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	wantKinds := []string{
		"ambiguous_cloud_resource",
		"image_version_drift",
		"orphaned_cloud_resource",
		"unknown_cloud_resource",
		"unmanaged_cloud_resource",
	}
	if got := observed.FindingKinds; !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("observed.FindingKinds = %#v, want %#v", got, wantKinds)
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	findings := data["drift_findings"].([]any)
	if len(findings) != 1 {
		t.Fatalf("drift_findings len = %d, want 1 (image_version_drift row); body = %s", len(findings), w.Body.String())
	}
	first := findings[0].(map[string]any)
	if got, want := first["finding_kind"], "image_version_drift"; got != want {
		t.Fatalf("first finding_kind = %q, want %q", got, want)
	}
}

// TestHandleAWSRuntimeDriftFindingsBlankKindsStillWidenDefault guards the #5453
// widening against a whitespace-only finding_kinds argument.
// normalizeIaCManagementFindingKinds strips blank entries before applying the
// existence-only default, so a raw-slice-length guard would treat ["  "] as
// caller-supplied kinds and skip the widening, silently excluding
// image_version_drift. The widening must key on whether any NON-BLANK kind was
// named.
func TestHandleAWSRuntimeDriftFindingsBlankKindsStillWidenDefault(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
			rows:           []IaCManagementFindingRow{},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/aws/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "aws:123456789012:us-east-1:ec2",
		"finding_kinds": ["  ", ""],
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	wantKinds := []string{
		"ambiguous_cloud_resource",
		"image_version_drift",
		"orphaned_cloud_resource",
		"unknown_cloud_resource",
		"unmanaged_cloud_resource",
	}
	if got := observed.FindingKinds; !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("observed.FindingKinds = %#v, want %#v (blank kinds must not skip the image_version_drift widening)", got, wantKinds)
	}
}
