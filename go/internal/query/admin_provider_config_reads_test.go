// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// fakeAdminProviderConfigReadStore is keyed by tenant_id, mirroring
// fakeAdminIdentityReadStore's convention in admin_identity_reads_test.go.
type fakeAdminProviderConfigReadStore struct {
	details   map[string]AdminProviderConfigDetail // provider_config_id -> detail
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

func newProviderConfigReadMux(store AdminProviderConfigReadStore) *http.ServeMux {
	handler := &AdminProviderConfigReadHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux
}

func adminReadRequest(method, target string, auth AuthContext) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(ContextWithAuthContext(req.Context(), auth))
}

func TestHandleListAdminProviderConfigs(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigReadStore{
		list: map[string][]AdminProviderConfigDetail{
			providerConfigAdminTenant: {
				{ProviderConfigID: "pc_1", ProviderKind: "external_oidc", Status: "active", HasSecret: true, SecretFingerprint: "abc12345", SecretKeyID: "k1", ManagedBy: "database"},
			},
		},
	}
	mux := newProviderConfigReadMux(store)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminReadRequest(http.MethodGet, "/api/v0/auth/admin/provider-configs", providerConfigAdminAuth()))

	if rec.Code != http.StatusOK {
		t.Fatalf("handleList status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	registry := redact.HostedGovernanceRegistry()
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceAPIMCPBodies, rec.Body.Bytes()); err != nil {
		t.Errorf("list response failed canary check: %v", err)
	}
}

func TestHandleGetAdminProviderConfigNotFound(t *testing.T) {
	t.Parallel()
	mux := newProviderConfigReadMux(&fakeAdminProviderConfigReadStore{details: map[string]AdminProviderConfigDetail{}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminReadRequest(http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_missing", providerConfigAdminAuth()))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("handleGet for missing config status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGetAdminProviderConfigFound(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigReadStore{
		details: map[string]AdminProviderConfigDetail{
			"pc_1": {ProviderConfigID: "pc_1", ProviderKind: "external_saml", Status: "draft", HasSecret: false, ManagedBy: "database"},
		},
	}
	mux := newProviderConfigReadMux(store)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminReadRequest(http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1", providerConfigAdminAuth()))

	if rec.Code != http.StatusOK {
		t.Fatalf("handleGet status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListRevisionsAdminProviderConfig(t *testing.T) {
	t.Parallel()
	store := &fakeAdminProviderConfigReadStore{
		revisions: map[string][]AdminProviderConfigRevisionItem{
			"pc_1": {
				{RevisionID: "rev_2", Status: "active", HasSecret: true},
				{RevisionID: "rev_1", Status: "superseded", HasSecret: true},
			},
		},
	}
	mux := newProviderConfigReadMux(store)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminReadRequest(http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1/revisions", providerConfigAdminAuth()))

	if rec.Code != http.StatusOK {
		t.Fatalf("handleListRevisions status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// TestProviderConfigReadsRequireAllScope verifies every read route returns
// 403 for a non-all-scope caller.
func TestProviderConfigReadsRequireAllScope(t *testing.T) {
	t.Parallel()
	scoped := AuthContext{Mode: AuthModeScoped, TenantID: providerConfigAdminTenant, AllScopes: false}
	cases := []struct{ method, target string }{
		{http.MethodGet, "/api/v0/auth/admin/provider-configs"},
		{http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1"},
		{http.MethodGet, "/api/v0/auth/admin/provider-configs/pc_1/revisions"},
	}
	for _, tc := range cases {
		mux := newProviderConfigReadMux(&fakeAdminProviderConfigReadStore{})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, adminReadRequest(tc.method, tc.target, scoped))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as non-admin = %d, want 403: %s", tc.method, tc.target, rec.Code, rec.Body.String())
		}
	}
}
