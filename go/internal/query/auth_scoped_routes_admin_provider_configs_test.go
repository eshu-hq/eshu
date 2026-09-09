// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestScopedProviderConfigReadRoute verifies the matcher recognizes exactly
// the list, single-item, and revisions shapes — and rejects a deeper nested
// path, an unrelated sibling path that merely shares the "provider-configs"
// prefix, an unknown sub-resource, and an empty id — proving the matcher is
// not a naive strings.HasPrefix that would accidentally admit unintended
// paths.
func TestScopedProviderConfigReadRoute(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"list", "/api/v0/auth/admin/provider-configs", true},
		{"single item", "/api/v0/auth/admin/provider-configs/pc_1", true},
		{"revisions", "/api/v0/auth/admin/provider-configs/pc_1/revisions", true},
		{"deeper nesting under revisions rejected", "/api/v0/auth/admin/provider-configs/pc_1/revisions/extra", false},
		{"unknown sub-resource rejected", "/api/v0/auth/admin/provider-configs/pc_1/unknown-action", false},
		{"empty id rejected", "/api/v0/auth/admin/provider-configs/", false},
		{"unrelated sibling prefix rejected", "/api/v0/auth/admin/provider-configs-legacy", false},
		{"unrelated route rejected", "/api/v0/auth/admin/roles", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := scopedProviderConfigReadRoute(tc.path); got != tc.want {
				t.Fatalf("scopedProviderConfigReadRoute(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestScopedProviderConfigMutationRoute verifies the matcher recognizes
// exactly create, update, and the closed set of sub-resource actions
// (revert/enable/disable/test-connection) — and rejects an action outside
// that closed set, a deeper nested path under an action, an unrelated
// sibling prefix, and an empty id.
func TestScopedProviderConfigMutationRoute(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"create", "/api/v0/auth/admin/provider-configs", true},
		{"update", "/api/v0/auth/admin/provider-configs/pc_1", true},
		{"revert", "/api/v0/auth/admin/provider-configs/pc_1/revert", true},
		{"enable", "/api/v0/auth/admin/provider-configs/pc_1/enable", true},
		{"disable", "/api/v0/auth/admin/provider-configs/pc_1/disable", true},
		{"test-connection", "/api/v0/auth/admin/provider-configs/pc_1/test-connection", true},
		{"unknown action rejected", "/api/v0/auth/admin/provider-configs/pc_1/delete", false},
		{"deeper nesting under action rejected", "/api/v0/auth/admin/provider-configs/pc_1/revert/extra", false},
		{"empty id rejected", "/api/v0/auth/admin/provider-configs/", false},
		{"unrelated sibling prefix rejected", "/api/v0/auth/admin/provider-configs-legacy", false},
		{"unrelated route rejected", "/api/v0/auth/admin/role-assignments", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := scopedProviderConfigMutationRoute(tc.path); got != tc.want {
				t.Fatalf("scopedProviderConfigMutationRoute(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// providerConfigAdminTenant is the tenant the middleware proof below scopes
// its fakes to. It mirrors the provider-config family tests' tenant.
const providerConfigAdminTenant = "tenant_a"

// validOIDCCreateBody is a minimal valid OIDC create body. It mirrors the
// provider-config family tests' body.
const validOIDCCreateBody = `{"provider_kind":"oidc","issuer":"https://idp.example.test","client_id":"client-1","client_secret":"s3cr3t-value","scopes":["openid","email"],"group_claim":"groups"}`

// fakeAdminProviderConfigReadStore is a tenant-keyed read double for the
// middleware proof below. It mirrors the provider-config family tests'
// double, which this package cannot import; the bodies are identical on
// purpose so the proof runs against the same fake behavior.
type fakeAdminProviderConfigReadStore struct {
	details   map[string]AdminProviderConfigDetail
	list      map[string][]AdminProviderConfigDetail
	revisions map[string][]AdminProviderConfigRevisionItem
	forceErr  error
}

func (f *fakeAdminProviderConfigReadStore) GetProviderConfigDetail(_ context.Context, providerConfigID, _ string) (AdminProviderConfigDetail, bool, error) {
	if f.forceErr != nil {
		return AdminProviderConfigDetail{}, false, f.forceErr
	}
	detail, ok := f.details[providerConfigID]
	return detail, ok, nil
}

func (f *fakeAdminProviderConfigReadStore) ListProviderConfigDetails(_ context.Context, tenantID string) ([]AdminProviderConfigDetail, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	return f.list[tenantID], nil
}

func (f *fakeAdminProviderConfigReadStore) ListProviderConfigRevisions(_ context.Context, providerConfigID, _ string) ([]AdminProviderConfigRevisionItem, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	return f.revisions[providerConfigID], nil
}

// fakeAdminProviderConfigMutationStore records the request it was asked to
// perform and returns a canned result. It mirrors the provider-config family
// tests' double for the same reason as
// fakeAdminProviderConfigReadStore above.
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

// fakeProviderConfigConnectionTester scripts the connection test. It mirrors
// the provider-config family tests' double for the same reason as
// fakeAdminProviderConfigReadStore above.
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

// TestAuthMiddlewareWithBrowserSessionsAllowsProviderConfigAdminRoutes extends
// the #5004 fix to the admin provider-config routes (#4966): the same
// scoped-route allowlist gap that blocked sign-in-policy also blocked every
// one of these 9 routes, since none of them were listed in
// scopedHTTPRouteSupportsTenantFilter either. A real admin authenticated by a
// browser-session cookie — the console's only auth mode — got the same
// scoped-authorization 403 before the handler's own AllScopes check ever ran,
// making the Identity Provider admin panel (which #4971 Phase 3 depends on)
// unusable. This drives a real HTTP request carrying a browser-session cookie
// through the actual AuthMiddlewareWithBrowserSessionsAndScopedTokens
// middleware — not a direct ContextWithAuthContext injection — for all 9
// routes, and proves each one reaches the handler and succeeds.
func TestAuthMiddlewareWithBrowserSessionsAllowsProviderConfigAdminRoutes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"list", http.MethodGet, "/api/v0/auth/admin/provider-configs", ""},
		{"get", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1", ""},
		{"revisions", http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1/revisions", ""},
		{"create", http.MethodPost, "/api/v0/auth/admin/provider-configs", validOIDCCreateBody},
		{"update", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1", validOIDCCreateBody},
		{"revert", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/revert", `{"revision_id":"rev_1"}`},
		{"enable", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/enable", ""},
		{"disable", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/disable", ""},
		{"test-connection", http.MethodPost, "/api/v0/auth/admin/provider-configs/pc_1/test-connection", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			readHandler := &AdminProviderConfigReadHandler{
				Store: &fakeAdminProviderConfigReadStore{
					details: map[string]AdminProviderConfigDetail{
						"pc_1": {ProviderConfigID: "pc_1", ProviderKind: "external_oidc", Status: "active"},
					},
					list: map[string][]AdminProviderConfigDetail{
						providerConfigAdminTenant: {{ProviderConfigID: "pc_1"}},
					},
					revisions: map[string][]AdminProviderConfigRevisionItem{
						"pc_1": {{RevisionID: "rev_1", Status: "active"}},
					},
				},
			}
			mutationHandler := &AdminProviderConfigMutationHandler{
				Store: &fakeAdminProviderConfigMutationStore{
					result: AdminProviderConfigWriteResult{
						ProviderConfigID: "pc_1", RevisionID: "rev_1", Status: "active", Found: true, Changed: true,
					},
				},
				Tester: &fakeProviderConfigConnectionTester{
					result: AdminProviderConfigConnectionTestResult{OK: true, Detail: "ok", RevisionID: "rev_1"},
				},
				Audit: &querytestutil.FakeGovernanceAuditAppender{},
			}

			mux := http.NewServeMux()
			readHandler.Mount(mux)
			mutationHandler.Mount(mux)

			resolver := &fakeBrowserSessionResolver{
				context: AuthContext{
					Mode:        AuthModeBrowserSession,
					TenantID:    providerConfigAdminTenant,
					WorkspaceID: "workspace_a",
					AllScopes:   true,
				},
				ok: true,
			}
			handler := AuthMiddlewareWithBrowserSessionsAndScopedTokens("", nil, resolver, mux)

			var bodyReader io.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, bodyReader)
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			if browserSessionRequiresCSRF(tc.method) {
				req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusForbidden {
				t.Fatalf(
					"%s %s rejected before reaching the handler (scoped-route allowlist gap #5004/#4966): status=%d body=%s",
					tc.method, tc.path, rec.Code, rec.Body.String(),
				)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, want %d: %s", tc.method, tc.path, rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}
}
