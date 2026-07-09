// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// fakeAdminProviderConfigMutationStore records the request it was asked to
// perform and returns a canned result, modeling the Postgres tenant filter so
// handler-level behavior can be proven without a database.
type fakeAdminProviderConfigMutationStore struct {
	gotCreate                      AdminProviderConfigCreateRequest
	gotUpdate                      AdminProviderConfigUpdateRequest
	gotRevert                      AdminProviderConfigRevertRequest
	gotEnableID, gotEnableTenant   string
	gotDisableID, gotDisableTenant string

	result   AdminProviderConfigWriteResult
	forceErr error
}

func (f *fakeAdminProviderConfigMutationStore) CreateProviderConfig(_ context.Context, req AdminProviderConfigCreateRequest) (AdminProviderConfigWriteResult, error) {
	f.gotCreate = req
	if f.forceErr != nil {
		return AdminProviderConfigWriteResult{}, f.forceErr
	}
	return f.result, nil
}

func (f *fakeAdminProviderConfigMutationStore) UpdateProviderConfig(_ context.Context, req AdminProviderConfigUpdateRequest) (AdminProviderConfigWriteResult, error) {
	f.gotUpdate = req
	if f.forceErr != nil {
		return AdminProviderConfigWriteResult{}, f.forceErr
	}
	return f.result, nil
}

func (f *fakeAdminProviderConfigMutationStore) RevertProviderConfig(_ context.Context, req AdminProviderConfigRevertRequest) (AdminProviderConfigWriteResult, error) {
	f.gotRevert = req
	if f.forceErr != nil {
		return AdminProviderConfigWriteResult{}, f.forceErr
	}
	return f.result, nil
}

func (f *fakeAdminProviderConfigMutationStore) EnableProviderConfig(_ context.Context, providerConfigID, tenantID string) (AdminProviderConfigWriteResult, error) {
	f.gotEnableID, f.gotEnableTenant = providerConfigID, tenantID
	if f.forceErr != nil {
		return AdminProviderConfigWriteResult{}, f.forceErr
	}
	return f.result, nil
}

func (f *fakeAdminProviderConfigMutationStore) DisableProviderConfig(_ context.Context, providerConfigID, tenantID string) (AdminProviderConfigWriteResult, error) {
	f.gotDisableID, f.gotDisableTenant = providerConfigID, tenantID
	if f.forceErr != nil {
		return AdminProviderConfigWriteResult{}, f.forceErr
	}
	return f.result, nil
}

// fakeProviderConfigConnectionTester returns a canned result and records the
// arguments it was called with.
type fakeProviderConfigConnectionTester struct {
	gotProviderConfigID, gotTenantID string
	result                           AdminProviderConfigConnectionTestResult
	forceErr                         error
}

func (f *fakeProviderConfigConnectionTester) TestProviderConnection(_ context.Context, providerConfigID, tenantID string) (AdminProviderConfigConnectionTestResult, error) {
	f.gotProviderConfigID, f.gotTenantID = providerConfigID, tenantID
	if f.forceErr != nil {
		return AdminProviderConfigConnectionTestResult{}, f.forceErr
	}
	return f.result, nil
}

const providerConfigAdminTenant = "tenant_a"

func providerConfigAdminAuth() AuthContext {
	return AuthContext{Mode: AuthModeShared, TenantID: providerConfigAdminTenant, AllScopes: true}
}

func newProviderConfigMutationMux(store AdminProviderConfigMutationStore, tester ProviderConfigConnectionTester, audit GovernanceAuditAppender) *http.ServeMux {
	handler := &AdminProviderConfigMutationHandler{Store: store, Tester: tester, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

const validOIDCCreateBody = `{"provider_kind":"oidc","issuer":"https://idp.example.test","client_id":"client-1","client_secret":"s3cr3t-value","scopes":["openid","email"],"group_claim":"groups"}`

func TestHandleCreateAdminProviderConfig(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{
		ProviderConfigID: "pc_1", RevisionID: "rev_1", Status: "draft", Found: true, Changed: true,
	}}
	audit := &recordingAuditAppender{}
	mux := newProviderConfigMutationMux(store, nil, audit)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs", strings.NewReader(validOIDCCreateBody))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("handleCreate status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotCreate.ProviderKind != "external_oidc" {
		t.Fatalf("store received ProviderKind = %q, want external_oidc", store.gotCreate.ProviderKind)
	}
	if store.gotCreate.PlaintextSecret == "" || !strings.Contains(store.gotCreate.PlaintextSecret, "s3cr3t-value") {
		t.Fatalf("store did not receive the plaintext secret to seal: %q", store.gotCreate.PlaintextSecret)
	}
	if !audit.hasReason("provider_config_created") {
		t.Errorf("create did not audit provider_config_created: %#v", audit.events)
	}
	// Negative-leakage proof at the HTTP-response boundary: the secret must
	// never appear in the JSON response body, using the shared hosted
	// -governance redaction registry's canary mechanism.
	registry := redact.HostedGovernanceRegistry()
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceAPIMCPBodies, rec.Body.Bytes()); err != nil {
		t.Errorf("response body failed canary check: %v", err)
	}
	if strings.Contains(rec.Body.String(), "s3cr3t-value") {
		t.Fatalf("create response leaked the plaintext secret: %s", rec.Body.String())
	}
}

func TestHandleCreateAdminProviderConfigRejectsMissingSecret(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{}
	mux := newProviderConfigMutationMux(store, nil, &recordingAuditAppender{})

	body := `{"provider_kind":"oidc","issuer":"https://idp.example.test","client_id":"client-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs", strings.NewReader(body))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if store.gotCreate.ProviderConfigID != "" {
		t.Fatal("store was called despite missing required secret")
	}
}

func TestHandleUpdateAdminProviderConfig(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{
		ProviderConfigID: "pc_1", RevisionID: "rev_2", Status: "active", Found: true, Changed: true,
	}}
	mux := newProviderConfigMutationMux(store, nil, &recordingAuditAppender{})

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1", strings.NewReader(validOIDCCreateBody))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("handleUpdate status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotUpdate.ProviderConfigID != "pc_1" {
		t.Fatalf("store received ProviderConfigID = %q, want pc_1", store.gotUpdate.ProviderConfigID)
	}
}

func TestHandleUpdateAdminProviderConfigNotFound(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: false}}
	mux := newProviderConfigMutationMux(store, nil, &recordingAuditAppender{})

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_missing", strings.NewReader(validOIDCCreateBody))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRevertAdminProviderConfig(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{
		ProviderConfigID: "pc_1", RevisionID: "rev_1", Status: "active", Found: true, Changed: true,
	}}
	audit := &recordingAuditAppender{}
	mux := newProviderConfigMutationMux(store, nil, audit)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/revert", strings.NewReader(`{"revision_id":"rev_1"}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("handleRevert status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotRevert.TargetRevisionID != "rev_1" {
		t.Fatalf("store received TargetRevisionID = %q, want rev_1", store.gotRevert.TargetRevisionID)
	}
	if !audit.hasReason("provider_config_reverted") {
		t.Errorf("revert did not audit provider_config_reverted: %#v", audit.events)
	}
}

func TestHandleEnableAdminProviderConfigRequiresPassingTest(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "active"}}
	tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: false, Detail: "discovery failed"}}
	mux := newProviderConfigMutationMux(store, tester, &recordingAuditAppender{})

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("enable with failing test status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if store.gotEnableID != "" {
		t.Fatal("store.EnableProviderConfig was called despite a failing connection test")
	}
}

func TestHandleEnableAdminProviderConfigPassingTest(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "active"}}
	tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok"}}
	mux := newProviderConfigMutationMux(store, tester, &recordingAuditAppender{})

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("enable with passing test status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotEnableID != "pc_1" {
		t.Fatalf("store.EnableProviderConfig id = %q, want pc_1", store.gotEnableID)
	}
}

func TestHandleDisableAdminProviderConfig(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "draft"}}
	mux := newProviderConfigMutationMux(store, nil, &recordingAuditAppender{})

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/disable", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.gotDisableID != "pc_1" {
		t.Fatalf("store.DisableProviderConfig id = %q, want pc_1", store.gotDisableID)
	}
}

func TestHandleTestConnectionAdminProviderConfig(t *testing.T) {
	t.Parallel()
	tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "discovery ok"}}
	mux := newProviderConfigMutationMux(&fakeAdminProviderConfigMutationStore{}, tester, &recordingAuditAppender{})

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/test-connection", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("test-connection status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if tester.gotProviderConfigID != "pc_1" || tester.gotTenantID != providerConfigAdminTenant {
		t.Fatalf("tester received (%q, %q), want (pc_1, %s)", tester.gotProviderConfigID, tester.gotTenantID, providerConfigAdminTenant)
	}
}

// providerConfigMutationCases enumerates every mutation route with a valid
// body so the auth gates can be asserted uniformly (mirrors
// adminMutationCases in admin_identity_mutations_test.go).
func providerConfigMutationCases() []struct {
	method string
	target string
	body   string
} {
	return []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, "/api/v0/auth/admin/provider-configs", validOIDCCreateBody},
		{http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1", validOIDCCreateBody},
		{http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/revert", `{"revision_id":"rev_1"}`},
		{http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", ""},
		{http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/disable", ""},
		{http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/test-connection", ""},
	}
}

// TestProviderConfigMutationsRequireAllScope verifies every mutation route
// returns 403 for a non-all-scope caller and audits the denial.
func TestProviderConfigMutationsRequireAllScope(t *testing.T) {
	t.Parallel()
	scoped := AuthContext{Mode: AuthModeScoped, TenantID: providerConfigAdminTenant, AllScopes: false}
	for _, tc := range providerConfigMutationCases() {
		audit := &recordingAuditAppender{}
		mux := newProviderConfigMutationMux(&fakeAdminProviderConfigMutationStore{}, &fakeProviderConfigConnectionTester{}, audit)
		var req *http.Request
		if tc.body == "" {
			req = httptest.NewRequest(tc.method, tc.target, nil)
		} else {
			req = httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		}
		req = req.WithContext(ContextWithAuthContext(req.Context(), scoped))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as non-admin = %d, want 403: %s", tc.method, tc.target, rec.Code, rec.Body.String())
		}
		if !audit.hasReason("admin_scope_required") {
			t.Errorf("%s %s: denied mutation did not audit admin_scope_required: %#v", tc.method, tc.target, audit.events)
		}
	}
}
