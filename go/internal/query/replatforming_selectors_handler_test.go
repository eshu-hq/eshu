// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeReplatformingSelectorStore struct {
	page              ReplatformingSelectorPage
	requested         int
	requestedScopeIDs []string
	selectorCalls     int
	selectorErr       error
}

func (f *fakeReplatformingSelectorStore) ListUnmanagedCloudResources(context.Context, IaCManagementFilter) ([]IaCManagementFindingRow, error) {
	return nil, nil
}

func (f *fakeReplatformingSelectorStore) CountUnmanagedCloudResources(context.Context, IaCManagementFilter) (int, error) {
	return 0, nil
}

func (f *fakeReplatformingSelectorStore) ListReplatformingSelectors(
	_ context.Context,
	limit int,
	allowedScopeIDs []string,
) (ReplatformingSelectorPage, error) {
	f.requested = limit
	f.requestedScopeIDs = append([]string(nil), allowedScopeIDs...)
	f.selectorCalls++
	return f.page, f.selectorErr
}

func TestReplatformingSelectorsHandlerListsBoundedAuthorizedChoices(t *testing.T) {
	t.Parallel()

	store := &fakeReplatformingSelectorStore{page: ReplatformingSelectorPage{
		Scopes: []ReplatformingSelectorScope{
			{ScopeID: "aws:123456789012:us-east-1:lambda", AccountID: "123456789012", Region: "us-east-1", Service: "lambda", FindingCount: 3},
			{ScopeID: "aws:123456789012:us-west-2:s3", AccountID: "123456789012", Region: "us-west-2", Service: "s3", FindingCount: 0},
		},
		Truncated: true,
	}}
	handler := &IaCHandler{Management: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/replatforming/selectors?limit=2", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
	if got, want := store.requested, 2; got != want {
		t.Fatalf("selector limit = %d, want %d", got, want)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != replatformingSelectorInventoryCapability {
		t.Fatalf("truth = %#v, want selector-inventory capability", envelope.Truth)
	}
	data := envelope.Data.(map[string]any)
	if got, want := int(data["count"].(float64)), 2; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := data["readiness"].(map[string]any)["state"], "ready"; got != want {
		t.Fatalf("readiness.state = %#v, want %#v", got, want)
	}
	scopes := data["scopes"].([]any)
	first := scopes[0].(map[string]any)
	if got, want := first["label"], "lambda in us-east-1 (account ...9012)"; got != want {
		t.Fatalf("first scope label = %#v, want %#v", got, want)
	}
	second := scopes[1].(map[string]any)
	if got, want := second["finding_count"], float64(0); got != want {
		t.Fatalf("second finding_count = %#v, want authoritative zero", got)
	}
	if got, want := data["supported_scope_kinds"], []any{"account", "region", "service"}; !equalJSONValues(got, want) {
		t.Fatalf("supported_scope_kinds = %#v, want %#v", got, want)
	}
	if got, want := data["finding_kinds"], []any{
		"ambiguous_cloud_resource",
		"orphaned_cloud_resource",
		"unmanaged_cloud_resource",
		"unknown_cloud_resource",
	}; !equalJSONValues(got, want) {
		t.Fatalf("finding_kinds = %#v, want %#v", got, want)
	}
}

func TestReplatformingSelectorsHandlerDistinguishesMissingCollectorEvidence(t *testing.T) {
	t.Parallel()

	store := &fakeReplatformingSelectorStore{}
	handler := &IaCHandler{Management: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/replatforming/selectors", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, req)

	var envelope ResponseEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	readiness := envelope.Data.(map[string]any)["readiness"].(map[string]any)
	if got, want := readiness["state"], "collector_evidence_absent"; got != want {
		t.Fatalf("readiness.state = %#v, want %#v", got, want)
	}
	if got := readiness["next_action"]; got == "" {
		t.Fatal("readiness.next_action is empty, want useful collector action")
	}
}

func TestReplatformingSelectorsHandlerPassesScopedAWSGrantsToStore(t *testing.T) {
	t.Parallel()

	store := &fakeReplatformingSelectorStore{}
	handler := &IaCHandler{Management: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/replatforming/selectors", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedScopeIDs:      []string{"aws:123456789012:us-east-1:lambda", "aws:210987654321:us-west-2:s3"},
		AllowedRepositoryIDs: []string{"repository:r_example"},
	}))
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
	if got, want := store.requestedScopeIDs, []string{
		"aws:123456789012:us-east-1:lambda",
		"aws:210987654321:us-west-2:s3",
	}; !equalStringSlices(got, want) {
		t.Fatalf("allowed scope ids = %#v, want %#v", got, want)
	}
}

func TestReplatformingSelectorsHandlerReturnsEmptyWithoutStoreForScopedRepositoryOnlyGrant(t *testing.T) {
	t.Parallel()

	store := &fakeReplatformingSelectorStore{}
	handler := &IaCHandler{Management: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/replatforming/selectors", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"repository:r_example"},
	}))
	req.Header.Set("Accept", EnvelopeMIMEType)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
	if got := store.selectorCalls; got != 0 {
		t.Fatalf("selector store calls = %d, want 0 for unprovable AWS scope grants", got)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	if got, want := int(data["count"].(float64)), 0; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := data["readiness"].(map[string]any)["state"], "no_authorized_scopes"; got != want {
		t.Fatalf("readiness.state = %#v, want %#v", got, want)
	}
}

func equalJSONValues(got, want any) bool {
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	return string(gotJSON) == string(wantJSON)
}
