// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeTerraformConfigStateDriftStore struct {
	observedFilter *TerraformConfigStateDriftFindingFilter
	rows           []TerraformConfigStateDriftFindingRow
	total          int
}

func (f fakeTerraformConfigStateDriftStore) ListActiveFindings(
	_ context.Context, filter TerraformConfigStateDriftFindingFilter,
) ([]TerraformConfigStateDriftFindingRow, error) {
	if f.observedFilter != nil {
		*f.observedFilter = filter
	}
	return f.rows, nil
}

func (f fakeTerraformConfigStateDriftStore) CountActiveFindings(
	_ context.Context, _ TerraformConfigStateDriftFindingFilter,
) (int, error) {
	if f.total > 0 {
		return f.total, nil
	}
	return len(f.rows), nil
}

func TestHandleTerraformConfigStateDriftFindingsReturnsOutcomes(t *testing.T) {
	t.Parallel()

	var observed TerraformConfigStateDriftFindingFilter
	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			observedFilter: &observed,
			rows: []TerraformConfigStateDriftFindingRow{
				{
					FactID: "fact:tf-1", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation:tf-1",
					SourceSystem: "collector/terraform-state", CanonicalID: "canonical:x",
					CandidateID: "candidate:x", CandidateKind: "terraform_config_state_drift",
					Outcome: "exact", Address: "aws_s3_bucket.x", DriftKind: "added_in_state",
					BackendKind: "s3", LocatorHash: "hash-1", Confidence: 1,
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/terraform/config-state-drift/findings", bytes.NewBufferString(`{
		"scope_id": "state_snapshot:s3:hash-1",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	if observed.ScopeID != "state_snapshot:s3:hash-1" {
		t.Fatalf("observed.ScopeID = %q, want state_snapshot:s3:hash-1", observed.ScopeID)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("payload[data] = %#v, want map", payload["data"])
	}
	findings, ok := data["drift_findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("data[drift_findings] = %#v, want one finding", data["drift_findings"])
	}
	if got, want := data["findings_count"], float64(1); got != want {
		t.Fatalf("data[findings_count] = %#v, want %v", got, want)
	}
	if _, ok := data["graph_projection_note"].(string); !ok {
		t.Fatalf("data[graph_projection_note] missing or not a string: %#v", data["graph_projection_note"])
	}
	if _, ok := data["limitations"].([]any); !ok {
		t.Fatalf("data[limitations] missing or not an array: %#v", data["limitations"])
	}
}

// TestHandleTerraformConfigStateDriftFindingsRequiresScopeID proves scope_id
// is mandatory: this domain has no account-wide fallback, so a request
// without it must fail fast rather than silently fan out.
func TestHandleTerraformConfigStateDriftFindingsRequiresScopeID(t *testing.T) {
	t.Parallel()

	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store:   fakeTerraformConfigStateDriftStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/terraform/config-state-drift/findings", bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// TestHandleTerraformConfigStateDriftFindingsScopedCallerWithoutGrantGetsEmptyPage
// proves the caller-side access precheck: a scoped caller that has not been
// granted the exact requested state-snapshot scope gets an honest
// zero-finding page, not a store call and not another tenant's findings.
func TestHandleTerraformConfigStateDriftFindingsScopedCallerWithoutGrantGetsEmptyPage(t *testing.T) {
	t.Parallel()

	storeCalled := false
	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStoreFunc(func() {
			storeCalled = true
		}),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/terraform/config-state-drift/findings", bytes.NewBufferString(`{
		"scope_id": "state_snapshot:s3:hash-1"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	if storeCalled {
		t.Fatal("store was called for a scoped caller with no matching grant; want zero-finding page without a store call")
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data := payload["data"].(map[string]any)
	if got, want := data["findings_count"], float64(0); got != want {
		t.Fatalf("data[findings_count] = %#v, want %v", got, want)
	}
}

// terraformConfigStateDriftScopedFixtureRows is the fixture row
// TestHandleTerraformConfigStateDriftFindingsScopedInGrantReturnsRealRowData
// asserts comes back for a scoped caller whose grant matches the requested
// scope_id.
func terraformConfigStateDriftScopedFixtureRows() []TerraformConfigStateDriftFindingRow {
	return []TerraformConfigStateDriftFindingRow{
		{
			FactID: "fact:tf-scoped-1", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation:tf-scoped-1",
			SourceSystem: "collector/terraform-state", CanonicalID: "canonical:tenant-a",
			CandidateID: "candidate:tenant-a", CandidateKind: "terraform_config_state_drift",
			Outcome: "exact", Address: "aws_s3_bucket.tenant_a", DriftKind: "added_in_state",
			BackendKind: "s3", LocatorHash: "hash-1", Confidence: 1,
		},
	}
}

// TestHandleTerraformConfigStateDriftFindingsScopedInGrantReturnsRealRowData
// proves the paired positive case to
// TestHandleTerraformConfigStateDriftFindingsScopedCallerWithoutGrantGetsEmptyPage:
// a scoped caller whose requested exact scope_id IS granted reaches the
// store, the store observes that exact scope_id, and real fixture rows come
// back in the response -- not just an empty page shape. Mirrors
// TestHandleAWSRuntimeDriftFindingsScopedInGrantReturnsRealRowData in
// aws_runtime_drift_test.go.
func TestHandleTerraformConfigStateDriftFindingsScopedInGrantReturnsRealRowData(t *testing.T) {
	t.Parallel()

	var observed TerraformConfigStateDriftFindingFilter
	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			observedFilter: &observed,
			rows:           terraformConfigStateDriftScopedFixtureRows(),
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/terraform/config-state-drift/findings", bytes.NewBufferString(`{
		"scope_id": "state_snapshot:s3:hash-1",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"state_snapshot:s3:hash-1"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d, body = %s", got, want, w.Body.String())
	}
	if got, want := observed.ScopeID, "state_snapshot:s3:hash-1"; got != want {
		t.Fatalf("store was not called for an in-grant scoped request: observed.ScopeID = %q, want %q", got, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("payload[data] = %#v, want map", payload["data"])
	}
	findings, ok := data["drift_findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("data[drift_findings] = %#v, want one finding", data["drift_findings"])
	}
	first, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatalf("findings[0] = %#v, want map", findings[0])
	}
	if got, want := first["address"], "aws_s3_bucket.tenant_a"; got != want {
		t.Fatalf("first[address] = %#v, want %#v (real row data)", got, want)
	}
}

type fakeTerraformConfigStateDriftStoreFunc func()

func (f fakeTerraformConfigStateDriftStoreFunc) ListActiveFindings(
	context.Context, TerraformConfigStateDriftFindingFilter,
) ([]TerraformConfigStateDriftFindingRow, error) {
	f()
	return nil, nil
}

func (f fakeTerraformConfigStateDriftStoreFunc) CountActiveFindings(
	context.Context, TerraformConfigStateDriftFindingFilter,
) (int, error) {
	f()
	return 0, nil
}
