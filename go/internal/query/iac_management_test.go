// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// fakeIaCManagementStore is the shared test double for IaCManagementStore
// used across the iac/replatforming handler tests. Its List/Count methods
// mirror postgres.AWSCloudRuntimeDriftFindingStore's #5167 W4 grant contract
// (aws_cloud_runtime_drift_findings.go) rather than being a dumb row holder:
// when filter.Scoped is true, a row is only returned if its ScopeID is in
// filter.AllowedScopeIDs (the same `fact.scope_id = ANY(allowed)` SQL
// predicate), and an empty AllowedScopeIDs short-circuits to zero rows
// without touching f.rows at all (dbTouched stays false), the same
// defense-in-depth double guard ListActiveFindings/CountActiveFindings
// implement. filter.Scoped false (the zero value) is unaffected and returns
// every row unfiltered, so every pre-#5167-W4 test that never sets Scoped
// keeps its original behavior.
type fakeIaCManagementStore struct {
	rows           []IaCManagementFindingRow
	observedFilter *IaCManagementFilter
	// dbTouched, when non-nil, is set true the first time either method reads
	// f.rows -- i.e. would have issued a real Postgres query. It stays false
	// for a scoped call with an empty AllowedScopeIDs grant.
	dbTouched *bool
}

// scopedRows applies filter.ARN (when set) alongside the Scoped grant
// intersection -- the same two predicates buildAWSCloudRuntimeDriftFindingQuery
// combines in its WHERE clause (fact.payload->>'arn' = $arn AND
// fact.scope_id = ANY($allowed)) -- so an exact-lookup request naming an
// out-of-grant ARN correctly resolves to zero rows here too, not just an
// in-grant ARN that happens to be first in f.rows. Both extra predicates are
// gated on filter.Scoped so every pre-#5167-W4 test that never sets Scoped
// keeps its original unfiltered-by-request-fields behavior (those tests only
// ever rely on Offset/Limit slicing, not ARN/scope_id/account_id filtering).
func (f fakeIaCManagementStore) scopedRows(filter IaCManagementFilter) []IaCManagementFindingRow {
	if !filter.Scoped {
		return f.rows
	}
	if len(filter.AllowedScopeIDs) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(filter.AllowedScopeIDs))
	for _, scopeID := range filter.AllowedScopeIDs {
		allowed[scopeID] = struct{}{}
	}
	filtered := make([]IaCManagementFindingRow, 0, len(f.rows))
	for _, row := range f.rows {
		if _, ok := allowed[row.ScopeID]; !ok {
			continue
		}
		if filter.ARN != "" && row.ARN != filter.ARN {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func (f fakeIaCManagementStore) touchDB() {
	if f.dbTouched != nil {
		*f.dbTouched = true
	}
}

func (f fakeIaCManagementStore) ListUnmanagedCloudResources(
	_ context.Context,
	filter IaCManagementFilter,
) ([]IaCManagementFindingRow, error) {
	if f.observedFilter != nil {
		*f.observedFilter = filter
	}
	if filter.Scoped && len(filter.AllowedScopeIDs) == 0 {
		return nil, nil
	}
	f.touchDB()
	rows := append([]IaCManagementFindingRow(nil), f.scopedRows(filter)...)
	if filter.Offset > len(rows) {
		return nil, nil
	}
	rows = rows[filter.Offset:]
	if filter.Limit > 0 && len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
	}
	return rows, nil
}

func (f fakeIaCManagementStore) CountUnmanagedCloudResources(_ context.Context, filter IaCManagementFilter) (int, error) {
	if filter.Scoped && len(filter.AllowedScopeIDs) == 0 {
		return 0, nil
	}
	f.touchDB()
	return len(f.scopedRows(filter)), nil
}

func TestHandleUnmanagedCloudResourcesRequiresBoundedScope(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/unmanaged-resources", bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleUnmanagedCloudResourcesRejectsWildcardAccountScope(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile:    ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/unmanaged-resources", bytes.NewBufferString(`{
		"account_id": "123%"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleUnmanagedCloudResourcesReturnsMaterializedFindings(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
			rows: []IaCManagementFindingRow{
				{
					ID:                "fact:aws-unmanaged-lambda",
					Provider:          "aws",
					AccountID:         "123456789012",
					Region:            "us-east-1",
					ResourceType:      "lambda",
					ResourceID:        "function:payments-api",
					ARN:               "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
					FindingKind:       "unmanaged_cloud_resource",
					ManagementStatus:  "terraform_state_only",
					Confidence:        0.92,
					ScopeID:           "aws:123456789012:us-east-1:lambda",
					GenerationID:      "generation:aws-1",
					SourceSystem:      "aws",
					CandidateID:       "candidate:lambda:payments-api",
					RecommendedAction: "restore_config_or_prepare_import_block",
					MissingEvidence:   []string{"terraform_config_resource"},
					Evidence: []IaCManagementEvidenceRow{
						{
							ID:             "evidence:state",
							SourceSystem:   "terraform_state",
							EvidenceType:   "terraform_state_resource",
							ScopeID:        "tfstate:prod",
							Key:            "arn",
							Value:          "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
							Confidence:     0.95,
							ProvenanceOnly: false,
						},
					},
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/unmanaged-resources", bytes.NewBufferString(`{
		"account_id": "123456789012",
		"region": "us-east-1",
		"finding_kinds": ["unmanaged_cloud_resource"],
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
	if got, want := observed.Region, "us-east-1"; got != want {
		t.Fatalf("observed.Region = %q, want %q", got, want)
	}
	if got, want := observed.FindingKinds, []string{"unmanaged_cloud_resource"}; !reflect.DeepEqual(got, want) {
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
	rawFindings := data["findings"].([]any)
	if got, want := len(rawFindings), 1; got != want {
		t.Fatalf("findings len = %d, want %d", got, want)
	}
	finding := rawFindings[0].(map[string]any)
	if got, want := finding["management_status"], "terraform_state_only"; got != want {
		t.Fatalf("management_status = %q, want %q", got, want)
	}
	if got, want := finding["recommended_action"], "restore_config_or_prepare_import_block"; got != want {
		t.Fatalf("recommended_action = %q, want %q", got, want)
	}
	if got, want := resp.Truth.Capability, "iac_management.find_unmanaged_resources"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
}

func TestHandleUnmanagedCloudResourcesDefaultsToActionableAWSFindingKinds(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/unmanaged-resources", bytes.NewBufferString(`{
		"account_id": "123456789012"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	wantKinds := []string{
		"ambiguous_cloud_resource",
		"orphaned_cloud_resource",
		"unmanaged_cloud_resource",
		"unknown_cloud_resource",
	}
	if got := observed.FindingKinds; !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("observed.FindingKinds = %#v, want %#v", got, wantKinds)
	}
}

func TestHandleIaCManagementStatusReturnsExactARNStatus(t *testing.T) {
	t.Parallel()

	var observed IaCManagementFilter
	arn := "arn:aws:s3:::unknown-bucket"
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			observedFilter: &observed,
			rows: []IaCManagementFindingRow{{
				ID:                "fact:aws-unknown-s3",
				Provider:          "aws",
				ResourceType:      "s3",
				ResourceID:        "unknown-bucket",
				ARN:               arn,
				FindingKind:       "unknown_cloud_resource",
				ManagementStatus:  "unknown_management",
				Confidence:        1,
				ScopeID:           "aws:123456789012:us-east-1:s3",
				GenerationID:      "generation:aws-1",
				SourceSystem:      "aws",
				RecommendedAction: "expand_collector_coverage_or_permissions",
				MissingEvidence:   []string{"terraform_config_owner"},
				WarningFlags:      []string{"insufficient_coverage"},
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
	if got, want := observed.ARN, arn; got != want {
		t.Fatalf("observed.ARN = %q, want %q", got, want)
	}
	if got, want := observed.Limit, 1; got != want {
		t.Fatalf("observed.Limit = %d, want %d", got, want)
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := data["management_status"], "unknown_management"; got != want {
		t.Fatalf("management_status = %q, want %q", got, want)
	}
	if _, ok := data["story"].(string); !ok {
		t.Fatalf("story missing or non-string: %#v", data["story"])
	}
	if got, want := resp.Truth.Capability, "iac_management.get_status"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
}

func TestHandleIaCManagementExplanationGroupsEvidence(t *testing.T) {
	t.Parallel()

	arn := "arn:aws:lambda:us-east-1:123456789012:function:payments-api"
	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Management: fakeIaCManagementStore{
			rows: []IaCManagementFindingRow{{
				ID:               "fact:aws-unmanaged-lambda",
				Provider:         "aws",
				AccountID:        "123456789012",
				Region:           "us-east-1",
				ResourceType:     "lambda",
				ResourceID:       "function:payments-api",
				ARN:              arn,
				FindingKind:      "unmanaged_cloud_resource",
				ManagementStatus: "terraform_state_only",
				Confidence:       0.92,
				ScopeID:          "aws:123456789012:us-east-1:lambda",
				GenerationID:     "generation:aws-1",
				SourceSystem:     "aws",
				Evidence: []IaCManagementEvidenceRow{
					{ID: "cloud", EvidenceType: "aws_cloud_resource", Key: "arn", Value: arn, Confidence: 1},
					{ID: "state", EvidenceType: "terraform_state_resource", Key: "resource_address", Value: "aws_lambda_function.payments", Confidence: 1},
					{ID: "tag", EvidenceType: "aws_raw_tag", Key: "tag:Service", Value: "payments", Confidence: 1, ProvenanceOnly: true},
				},
			}},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/management-status/explain", bytes.NewBufferString(`{
		"account_id": "123456789012",
		"region": "us-east-1",
		"resource_id": "`+arn+`"
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
	groups := data["evidence_groups"].([]any)
	if got, want := len(groups), 3; got != want {
		t.Fatalf("len(evidence_groups) = %d, want %d", got, want)
	}
	if got, want := resp.Truth.Capability, "iac_management.explain_status"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
}
