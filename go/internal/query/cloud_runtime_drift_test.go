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
	"strings"
	"testing"
)

// fakeMultiCloudRuntimeDriftStore is a fixture-backed MultiCloudRuntimeDriftStore
// so the handler can be unit tested without a live database or graph backend.
type fakeMultiCloudRuntimeDriftStore struct {
	observedFilter *MultiCloudRuntimeDriftFilter
	rows           []MultiCloudRuntimeDriftFindingRow
	total          int
	listErr        error
	countErr       error
}

func (s fakeMultiCloudRuntimeDriftStore) ListActiveMultiCloudRuntimeDriftFindings(
	_ context.Context,
	filter MultiCloudRuntimeDriftFilter,
) ([]MultiCloudRuntimeDriftFindingRow, error) {
	if s.observedFilter != nil {
		*s.observedFilter = filter
	}
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.rows, nil
}

func (s fakeMultiCloudRuntimeDriftStore) CountActiveMultiCloudRuntimeDriftFindings(
	_ context.Context,
	_ MultiCloudRuntimeDriftFilter,
) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	if s.total != 0 {
		return s.total, nil
	}
	return len(s.rows), nil
}

func multiCloudRuntimeDriftFixtureRows() []MultiCloudRuntimeDriftFindingRow {
	return []MultiCloudRuntimeDriftFindingRow{
		{
			FactID:           "fact:gcp-vm",
			ScopeID:          "cloud-scope:gcp:project-synthetic",
			GenerationID:     "gcp-gen-1",
			SourceSystem:     "gcp_cloud_inventory",
			Provider:         "gcp",
			CloudResourceUID: "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
			RawIdentity:      "//compute.googleapis.com/projects/project-synthetic/zones/us/instances/vm-1",
			FindingKind:      "orphaned_cloud_resource",
			ManagementStatus: "terraform_state_only",
			Confidence:       0.92,
		},
		{
			FactID:           "fact:azure-blob",
			ScopeID:          "cloud-scope:gcp:project-synthetic",
			GenerationID:     "gcp-gen-1",
			SourceSystem:     "azure_cloud_inventory",
			Provider:         "azure",
			CloudResourceUID: "azure:sub-1:Microsoft.Storage/storageAccounts:acct-ambiguous",
			RawIdentity:      "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/acct-ambiguous",
			FindingKind:      "ambiguous_cloud_resource",
			ManagementStatus: "ambiguous_management",
			Confidence:       0.5,
			WarningFlags:     []string{"ambiguous_ownership"},
		},
	}
}

func TestHandleCloudRuntimeDriftFindingsReturnsProviderNeutralFindings(t *testing.T) {
	t.Parallel()

	var observed MultiCloudRuntimeDriftFilter
	handler := &CloudRuntimeDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeMultiCloudRuntimeDriftStore{
			observedFilter: &observed,
			rows:           multiCloudRuntimeDriftFixtureRows(),
			total:          5,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/cloud/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "cloud-scope:gcp:project-synthetic",
		"finding_kinds": ["orphaned_cloud_resource", "ambiguous_cloud_resource"],
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := observed.ScopeID, "cloud-scope:gcp:project-synthetic"; got != want {
		t.Fatalf("observed.ScopeID = %q, want %q", got, want)
	}
	if got, want := observed.FindingKinds, []string{"ambiguous_cloud_resource", "orphaned_cloud_resource"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observed.FindingKinds = %#v, want %#v", got, want)
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := resp.Truth.Capability, cloudRuntimeDriftReadbackCapability; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := data["total_findings_count"], float64(5); got != want {
		t.Fatalf("total_findings_count = %v, want %v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %v, want %v", got, want)
	}

	findings := data["drift_findings"].([]any)
	if got, want := len(findings), 2; got != want {
		t.Fatalf("drift_findings len = %d, want %d", got, want)
	}
	first := findings[0].(map[string]any)
	if got, want := first["provider"], "gcp"; got != want {
		t.Fatalf("first provider = %q, want %q", got, want)
	}
	if got, want := first["cloud_resource_uid"], "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1"; got != want {
		t.Fatalf("first cloud_resource_uid = %q, want %q", got, want)
	}
	if got, want := first["finding_kind"], "orphaned_cloud_resource"; got != want {
		t.Fatalf("first finding_kind = %q, want %q", got, want)
	}
	if got, want := first["source_state"], string(ReplatformingSourceStateDerived); got != want {
		t.Fatalf("first source_state = %q, want %q", got, want)
	}
	// terraform_state_only is not safety-gated, so it is presented as derived.
	firstGate := first["safety_gate"].(map[string]any)
	if got, want := firstGate["review_required"], false; got != want {
		t.Fatalf("first safety_gate.review_required = %v, want %v", got, want)
	}
	// Raw provider locators must never leak through the readback projection.
	if _, present := first["raw_identity"]; present {
		t.Fatalf("raw_identity must not appear in finding payload: %#v", first)
	}
	if _, present := first["evidence"]; present {
		t.Fatalf("raw evidence atoms must not appear in finding payload: %#v", first)
	}

	// The ambiguous finding must be refused, not omitted: it stays in the list
	// but is reported as rejected with a refused action.
	second := findings[1].(map[string]any)
	if got, want := second["provider"], "azure"; got != want {
		t.Fatalf("second provider = %q, want %q", got, want)
	}
	if got, want := second["source_state"], string(ReplatformingSourceStateRejected); got != want {
		t.Fatalf("second source_state = %q, want %q", got, want)
	}
	secondGate := second["safety_gate"].(map[string]any)
	if got, want := secondGate["review_required"], true; got != want {
		t.Fatalf("second safety_gate.review_required = %v, want %v", got, want)
	}
	refused := secondGate["refused_actions"].([]any)
	if len(refused) == 0 {
		t.Fatalf("ambiguous finding must carry refused_actions, got %#v", secondGate)
	}
}

// TestHandleCloudRuntimeDriftFindingsScopedOutOfGrantNeverCallsStore proves
// the #5167 handler-side precheck (MultiCloudRuntimeDriftStore is shared with
// GET /api/v0/investigations/drift/packet, owned by a different workstream,
// so the fix is a caller-side grant check rather than a store/filter
// change): a scoped caller whose requested scope_id is outside its grant must
// get the zero-finding page without the store ever being invoked, proven by
// the observedFilter staying at its zero value (Store is never called for
// either Count or List).
func TestHandleCloudRuntimeDriftFindingsScopedOutOfGrantNeverCallsStore(t *testing.T) {
	t.Parallel()

	var observed MultiCloudRuntimeDriftFilter
	handler := &CloudRuntimeDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeMultiCloudRuntimeDriftStore{
			observedFilter: &observed,
			rows:           multiCloudRuntimeDriftFixtureRows(),
			total:          5,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/cloud/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "cloud-scope:gcp:project-synthetic",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		AllowedScopeIDs: []string{"cloud-scope:aws:tenant-a"}, // does not grant the requested gcp scope
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if observed.ScopeID != "" {
		t.Fatalf("store was called for an out-of-grant scoped request: observed filter = %#v", observed)
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	if got, want := data["total_findings_count"], float64(0); got != want {
		t.Fatalf("total_findings_count = %v, want %v", got, want)
	}
	findings := data["drift_findings"].([]any)
	if len(findings) != 0 {
		t.Fatalf("drift_findings = %#v, want empty for an out-of-grant scoped request", findings)
	}
	if strings.Contains(w.Body.String(), "compute.googleapis.com") {
		t.Fatalf("out-of-grant response leaked fixture row data: %s", w.Body.String())
	}
}

// TestHandleCloudRuntimeDriftFindingsScopedInGrantReturnsRealRowData proves
// the paired case: a scoped caller whose requested scope_id IS granted
// reaches the store and gets the real fixture rows back, so the #5167
// precheck is additive (blocks out-of-grant, passes in-grant) rather than a
// blanket denial that would make the out-of-grant test above pass vacuously.
func TestHandleCloudRuntimeDriftFindingsScopedInGrantReturnsRealRowData(t *testing.T) {
	t.Parallel()

	var observed MultiCloudRuntimeDriftFilter
	handler := &CloudRuntimeDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeMultiCloudRuntimeDriftStore{
			observedFilter: &observed,
			rows:           multiCloudRuntimeDriftFixtureRows(),
			total:          2,
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/cloud/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "cloud-scope:gcp:project-synthetic",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		AllowedScopeIDs: []string{"cloud-scope:gcp:project-synthetic"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := observed.ScopeID, "cloud-scope:gcp:project-synthetic"; got != want {
		t.Fatalf("store was not called for an in-grant scoped request: observed.ScopeID = %q, want %q", got, want)
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	findings := data["drift_findings"].([]any)
	if len(findings) != 2 {
		t.Fatalf("drift_findings len = %d, want 2; body = %s", len(findings), w.Body.String())
	}
	first := findings[0].(map[string]any)
	if got, want := first["cloud_resource_uid"], "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1"; got != want {
		t.Fatalf("first cloud_resource_uid = %q, want %q (real row data)", got, want)
	}
}

func TestHandleCloudRuntimeDriftFindingsRequiresScope(t *testing.T) {
	t.Parallel()

	handler := &CloudRuntimeDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store:   fakeMultiCloudRuntimeDriftStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/cloud/runtime-drift/findings", bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleCloudRuntimeDriftFindingsRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	handler := &CloudRuntimeDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store:   fakeMultiCloudRuntimeDriftStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/cloud/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "cloud-scope:gcp:project-synthetic",
		"provider": "oracle"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestHandleCloudRuntimeDriftFindingsUnsupportedProfile(t *testing.T) {
	t.Parallel()

	handler := &CloudRuntimeDriftHandler{
		Profile: ProfileLocalLightweight,
		Store:   fakeMultiCloudRuntimeDriftStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/cloud/runtime-drift/findings", bytes.NewBufferString(`{
		"scope_id": "cloud-scope:gcp:project-synthetic"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected error envelope, got %s", w.Body.String())
	}
	if got, want := resp.Error.Code, ErrorCodeUnsupportedCapability; got != want {
		t.Fatalf("error.code = %#v, want %#v", got, want)
	}
}
