// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// terraformConfigStateDriftAmbiguousFixtureRow returns one ambiguous finding
// whose ambiguous_owner_candidates carries two competing config repos:
// repo-a (inside a scoped caller's grant in the tests below) and repo-b
// (outside it). Used by the #5442 P1 cross-tenant-candidate-leak regression
// tests: an ambiguous finding's competing-owner list is per-candidate
// evidence, not gated by the finding's own scope_id, so it can leak another
// tenant's repo_id/scope_id/commit_id even when the caller's grant for the
// finding's own state-snapshot scope is legitimate.
func terraformConfigStateDriftAmbiguousFixtureRow() TerraformConfigStateDriftFindingRow {
	return TerraformConfigStateDriftFindingRow{
		FactID: "fact:tf-ambiguous-1", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation:tf-ambiguous-1",
		SourceSystem: "collector/terraform-state", CanonicalID: "canonical:ambiguous-1",
		CandidateID: "candidate:ambiguous-1", CandidateKind: "terraform_config_state_drift",
		Outcome: "ambiguous", BackendKind: "s3", LocatorHash: "hash-1", Confidence: 1,
		AmbiguousOwnerCandidates: []map[string]any{
			{"repo_id": "repo-a", "scope_id": "repo:repo-a@abc", "commit_id": "commit-a", "commit_observed_at": "2026-07-01T00:00:00Z"},
			{"repo_id": "repo-b", "scope_id": "repo:repo-b@def", "commit_id": "commit-b", "commit_observed_at": "2026-07-02T00:00:00Z"},
		},
	}
}

// TestHandleTerraformConfigStateDriftFindingsFiltersAmbiguousOwnerCandidatesOutsideGrant
// is the #5442 P1 regression: a scoped caller granted the finding's own
// state-snapshot scope AND repo-a, but NOT repo-b, must not see repo-b's
// repo_id/scope_id/commit_id/commit_observed_at in ambiguous_owner_candidates
// -- and the finding must still be reported ambiguous (not silently
// downgraded because the caller cannot see every competing repo).
func TestHandleTerraformConfigStateDriftFindingsFiltersAmbiguousOwnerCandidatesOutsideGrant(t *testing.T) {
	t.Parallel()

	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			rows: []TerraformConfigStateDriftFindingRow{terraformConfigStateDriftAmbiguousFixtureRow()},
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
		AllowedRepositoryIDs: []string{"state_snapshot:s3:hash-1", "repo-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, "repo-b") || strings.Contains(body, "repo:repo-b@def") || strings.Contains(body, "commit-b") {
		t.Fatalf("response leaked out-of-grant repo-b identifiers: %s", body)
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
	if got, want := first["outcome"], "ambiguous"; got != want {
		t.Fatalf("first[outcome] = %#v, want %#v -- filtering out-of-grant candidates must not silently downgrade an ambiguous finding", got, want)
	}
	candidates, _ := first["ambiguous_owner_candidates"].([]any)
	if len(candidates) != 1 {
		t.Fatalf("ambiguous_owner_candidates = %#v, want exactly the one in-grant candidate", candidates)
	}
	candidate, ok := candidates[0].(map[string]any)
	if !ok {
		t.Fatalf("candidates[0] = %#v, want map", candidates[0])
	}
	if got, want := candidate["repo_id"], "repo-a"; got != want {
		t.Fatalf("candidate[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := first["ambiguous_owner_candidates_withheld_count"], float64(1); got != want {
		t.Fatalf("first[ambiguous_owner_candidates_withheld_count] = %#v, want %v", got, want)
	}
}

// TestHandleTerraformConfigStateDriftFindingsUnscopedCallerSeesAllAmbiguousOwnerCandidates
// is the paired positive case: an unscoped (admin/local/shared-key) caller is
// unaffected by the #5442 P1 candidate filter and keeps seeing every
// competing repo.
func TestHandleTerraformConfigStateDriftFindingsUnscopedCallerSeesAllAmbiguousOwnerCandidates(t *testing.T) {
	t.Parallel()

	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			rows: []TerraformConfigStateDriftFindingRow{terraformConfigStateDriftAmbiguousFixtureRow()},
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
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data := payload["data"].(map[string]any)
	findings := data["drift_findings"].([]any)
	if len(findings) != 1 {
		t.Fatalf("data[drift_findings] = %#v, want one finding", data["drift_findings"])
	}
	first := findings[0].(map[string]any)
	candidates, _ := first["ambiguous_owner_candidates"].([]any)
	if len(candidates) != 2 {
		t.Fatalf("unscoped caller candidates = %#v, want both competing repos", candidates)
	}
	if _, ok := first["ambiguous_owner_candidates_withheld_count"]; ok {
		t.Fatalf("unscoped caller must not carry a withheld count, got %#v", first["ambiguous_owner_candidates_withheld_count"])
	}
}

// TestHandleTerraformConfigStateDriftFindingsReportsAmbiguousWhenAllCandidatesWithheld
// covers the zero-candidate-after-filter edge case: a scoped caller granted
// only the finding's own state-snapshot scope (neither competing repo) must
// still see outcome "ambiguous", with an explicit withheld count, rather than
// a payload that looks like a clean/exact finding.
func TestHandleTerraformConfigStateDriftFindingsReportsAmbiguousWhenAllCandidatesWithheld(t *testing.T) {
	t.Parallel()

	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			rows: []TerraformConfigStateDriftFindingRow{terraformConfigStateDriftAmbiguousFixtureRow()},
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

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data := payload["data"].(map[string]any)
	findings := data["drift_findings"].([]any)
	if len(findings) != 1 {
		t.Fatalf("data[drift_findings] = %#v, want one finding", data["drift_findings"])
	}
	first := findings[0].(map[string]any)
	if got, want := first["outcome"], "ambiguous"; got != want {
		t.Fatalf("first[outcome] = %#v, want %#v -- withholding every candidate must not downgrade the outcome", got, want)
	}
	if candidates, ok := first["ambiguous_owner_candidates"]; ok && len(candidates.([]any)) != 0 {
		t.Fatalf("first[ambiguous_owner_candidates] = %#v, want empty/absent", candidates)
	}
	if got, want := first["ambiguous_owner_candidates_withheld_count"], float64(2); got != want {
		t.Fatalf("first[ambiguous_owner_candidates_withheld_count] = %#v, want %v", got, want)
	}
}
