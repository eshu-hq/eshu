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

// See admin_provider_config_leakage_test.go for providerConfigSecretCanary
// and assertNoCanaryInResponseAndAudit, shared negative-leakage helpers used
// by both this file and that one.

// fakeAdminProviderConfigMutationStore records the request it was asked to
// perform and returns a canned result, modeling the Postgres tenant filter so
// handler-level behavior can be proven without a database.
type fakeAdminProviderConfigMutationStore struct {
	gotCreate                                                 AdminProviderConfigCreateRequest
	gotUpdate                                                 AdminProviderConfigUpdateRequest
	gotRevert                                                 AdminProviderConfigRevertRequest
	gotEnableID, gotEnableTenant, gotEnableExpectedRevisionID string
	gotDisableID, gotDisableTenant                            string

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

func (f *fakeAdminProviderConfigMutationStore) EnableProviderConfig(_ context.Context, providerConfigID, tenantID, expectedActiveRevisionID string) (AdminProviderConfigWriteResult, error) {
	f.gotEnableID, f.gotEnableTenant, f.gotEnableExpectedRevisionID = providerConfigID, tenantID, expectedActiveRevisionID
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

// newProviderConfigMutationMux builds a mux over a fresh
// AdminProviderConfigMutationHandler. readStore is optional (variadic so
// every pre-existing call site is unaffected): when omitted, it defaults to
// defaultProviderConfigLoginReadyReadStore so the issue #5604 enable-time
// login-readiness guard (see admin_provider_config_login_readiness.go) never
// trips for tests that are not specifically exercising it. Tests that DO
// exercise the guard pass their own readStore explicitly.
func newProviderConfigMutationMux(store AdminProviderConfigMutationStore, tester ProviderConfigConnectionTester, audit GovernanceAuditAppender, readStore ...AdminProviderConfigReadStore) *http.ServeMux {
	handler := &AdminProviderConfigMutationHandler{Store: store, Tester: tester, Audit: audit, ReadStore: defaultProviderConfigLoginReadyReadStore()}
	if len(readStore) > 0 {
		handler.ReadStore = readStore[0]
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

// defaultProviderConfigLoginReadyReadStore returns a fake ReadStore whose
// pc_1 detail already carries every field ResolveSealedProviderConfig
// requires for login (redirect_url, for the default external_oidc kind), so
// the default mux used by every pre-existing create/update/revert/enable/
// disable/test-connection test in this package is unaffected by the issue
// #5604 guard added to handleStatusChange.
func defaultProviderConfigLoginReadyReadStore() AdminProviderConfigReadStore {
	return &fakeAdminProviderConfigReadStore{details: map[string]AdminProviderConfigDetail{
		"pc_1": {
			ProviderConfigID: "pc_1",
			ProviderKind:     "external_oidc",
			Status:           "draft",
			Configuration: map[string]any{
				"issuer": "https://idp.example.test", "client_id": "client-1",
				"redirect_url": "https://admin.example.test/auth/oidc/callback",
			},
		},
	}}
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

// TestHandleUpdateAdminProviderConfigKindMismatch proves the handler surfaces
// ErrAdminProviderConfigKindMismatch as 400, and that the store still
// received the request's actual ProviderKind (so a lower layer, not the
// handler, is what enforces immutability — the handler must not silently drop
// the field).
func TestHandleUpdateAdminProviderConfigKindMismatch(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{forceErr: ErrAdminProviderConfigKindMismatch}
	mux := newProviderConfigMutationMux(store, nil, &recordingAuditAppender{})

	samlBody := `{"provider_kind":"saml","entity_id":"https://sp.example.test","metadata_xml":"<md/>","sp_private_key":"k","sp_certificate":"c"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1", strings.NewReader(samlBody))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if store.gotUpdate.ProviderKind != "external_saml" {
		t.Fatalf("store.gotUpdate.ProviderKind = %q, want external_saml", store.gotUpdate.ProviderKind)
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

// TestHandleEnableAdminProviderConfigPassingTest also proves the handler
// closes the test-connection/enable TOCTOU: the revision id the tester
// reports as tested (testResult.RevisionID) must be exactly what reaches
// Store.EnableProviderConfig's compare-and-swap parameter, not silently
// dropped.
func TestHandleEnableAdminProviderConfigPassingTest(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{result: AdminProviderConfigWriteResult{Found: true, Changed: true, Status: "active"}}
	tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok", RevisionID: "rev_tested_1"}}
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
	if store.gotEnableExpectedRevisionID != "rev_tested_1" {
		t.Fatalf("store.EnableProviderConfig expectedActiveRevisionID = %q, want rev_tested_1 (the tested revision)", store.gotEnableExpectedRevisionID)
	}
}

// TestHandleEnableAdminProviderConfigRevisionChanged proves a 409 surfaces
// when the store rejects Enable because the active revision changed since it
// was tested (ErrAdminProviderConfigRevisionChanged).
func TestHandleEnableAdminProviderConfigRevisionChanged(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigMutationStore{forceErr: ErrAdminProviderConfigRevisionChanged}
	tester := &fakeProviderConfigConnectionTester{result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok", RevisionID: "rev_tested_1"}}
	mux := newProviderConfigMutationMux(store, tester, &recordingAuditAppender{})

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("enable after a revision change status = %d, want 409: %s", rec.Code, rec.Body.String())
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

// TestHandleCreateAdminProviderConfigAuditsWhenStoreUnavailable proves
// storeReady audits provider_config_store_unavailable on a nil Store exactly
// like handleTestConnection audits its nil-Tester 503 — every allowed and
// denied provider-config attempt must be governance-audited, and this 503 is
// a denied attempt. handleCreate is representative of all four storeReady
// call sites (create, update, revert, enable/disable), which all share the
// same storeReady helper.
func TestHandleCreateAdminProviderConfigAuditsWhenStoreUnavailable(t *testing.T) {
	t.Parallel()
	audit := &recordingAuditAppender{}
	mux := newProviderConfigMutationMux(nil, &fakeProviderConfigConnectionTester{}, audit)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs", strings.NewReader(validOIDCCreateBody))
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("create with nil Store status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if !audit.hasReason("provider_config_store_unavailable") {
		t.Errorf("create with nil Store did not audit provider_config_store_unavailable: %#v", audit.events)
	}
}

// TestHandleTestConnectionAdminProviderConfigAuditsWhenTesterUnavailable
// proves handleTestConnection audits provider_config_connection_tester_unavailable
// on a nil Tester exactly like handleStatusChange does for the identical nil
// condition (see admin_provider_config_mutations.go) — every allowed and
// denied provider-config attempt must be governance-audited, and this 503 is
// a denied attempt.
func TestHandleTestConnectionAdminProviderConfigAuditsWhenTesterUnavailable(t *testing.T) {
	t.Parallel()
	audit := &recordingAuditAppender{}
	mux := newProviderConfigMutationMux(&fakeAdminProviderConfigMutationStore{}, nil, audit)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/test-connection", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), providerConfigAdminAuth()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("test-connection with nil Tester status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if !audit.hasReason("provider_config_connection_tester_unavailable") {
		t.Errorf("test-connection with nil Tester did not audit provider_config_connection_tester_unavailable: %#v", audit.events)
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

// See admin_provider_config_leakage_test.go for
// TestProviderConfigMutationResponsesAndAuditNeverLeakCanary and
// TestProviderConfigReadResponsesNeverLeakCanary.
