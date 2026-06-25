// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAuthProviderStore implements AuthProviderStore for unit tests.
// It records the tenantID supplied on each call so tests can assert isolation.
type fakeAuthProviderStore struct {
	// items is keyed by tenantID; callers that pass an unregistered tenantID
	// receive an empty list (simulating no providers for that tenant).
	byTenant map[string][]AuthProviderItem
	err      error
	// lastTenantID records the most recent tenantID passed to ListLoginProviders.
	lastTenantID string
}

func (f *fakeAuthProviderStore) ListLoginProviders(_ context.Context, tenantID string) ([]AuthProviderItem, error) {
	f.lastTenantID = tenantID
	if f.err != nil {
		return nil, f.err
	}
	if f.byTenant == nil {
		return nil, nil
	}
	return f.byTenant[tenantID], nil
}

func TestAuthProvidersHandlerReturnsConfiguredProviders(t *testing.T) {
	t.Parallel()

	store := &fakeAuthProviderStore{
		byTenant: map[string][]AuthProviderItem{
			"tenant-a": {
				{ProviderConfigID: "okta-oidc", DisplayLabel: "Single sign-on (OIDC)", ProviderKind: "oidc"},
				{ProviderConfigID: "okta-saml", DisplayLabel: "Single sign-on (SAML)", ProviderKind: "saml"},
			},
		},
	}
	handler := &AuthProviderListHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/providers?tenant_id=tenant-a", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Providers) != 2 {
		t.Fatalf("providers count = %d, want 2", len(resp.Providers))
	}

	// Assert first provider fields.
	p0 := resp.Providers[0]
	if p0["provider_config_id"] != "okta-oidc" {
		t.Errorf("providers[0].provider_config_id = %q, want %q", p0["provider_config_id"], "okta-oidc")
	}
	if p0["display_label"] != "Single sign-on (OIDC)" {
		t.Errorf("providers[0].display_label = %q, want generic OIDC label", p0["display_label"])
	}
	if p0["provider_kind"] != "oidc" {
		t.Errorf("providers[0].provider_kind = %q, want %q", p0["provider_kind"], "oidc")
	}
	// Assert the store received the correct tenantID.
	if store.lastTenantID != "tenant-a" {
		t.Errorf("store received tenantID = %q, want %q", store.lastTenantID, "tenant-a")
	}
	// Assert NO private IdP fields in the JSON body.
	raw := rec.Body.String()
	for _, forbidden := range []string{
		"issuer", "metadata_url", "entity_id", "client_id",
		"credential", "domain", "org", "group",
		"provider_key_hash", "issuer_hash",
	} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("response body MUST NOT contain %q (private IdP field): %s", forbidden, raw)
		}
	}

	// Assert Cache-Control header is set.
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=60" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=60")
	}
}

func TestAuthProvidersHandlerReturnsEmptyListWhenNoneConfigured(t *testing.T) {
	t.Parallel()

	store := &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{}}
	handler := &AuthProviderListHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/providers?tenant_id=tenant-a", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Providers == nil || len(resp.Providers) != 0 {
		t.Fatalf("providers = %#v, want empty non-null array", resp.Providers)
	}
}

// TestAuthProvidersHandlerCrossTenantIsolation proves that a request for
// tenant-b never receives providers registered under tenant-a.
func TestAuthProvidersHandlerCrossTenantIsolation(t *testing.T) {
	t.Parallel()

	store := &fakeAuthProviderStore{
		byTenant: map[string][]AuthProviderItem{
			"tenant-a": {
				{ProviderConfigID: "oidc-tenant-a", DisplayLabel: "Single sign-on (OIDC)", ProviderKind: "oidc"},
			},
			// tenant-b has no providers registered.
		},
	}
	handler := &AuthProviderListHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Request for tenant-b must return an empty list, not tenant-a's providers.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/providers?tenant_id=tenant-b", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Providers) != 0 {
		t.Errorf("cross-tenant isolation FAILED: tenant-b received %d providers (want 0): %s",
			len(resp.Providers), rec.Body.String())
	}
	// Assert the body does NOT contain tenant-a's provider_config_id.
	if strings.Contains(rec.Body.String(), "oidc-tenant-a") {
		t.Errorf("cross-tenant isolation FAILED: tenant-b response contains tenant-a provider id: %s",
			rec.Body.String())
	}
	// Assert the store was called with tenant-b, not tenant-a.
	if store.lastTenantID != "tenant-b" {
		t.Errorf("store received tenantID = %q, want %q", store.lastTenantID, "tenant-b")
	}
}

// TestAuthProvidersHandlerEmptyTenantIDReturnsEmpty proves that omitting
// tenant_id returns an empty list without calling the store (no global scan).
func TestAuthProvidersHandlerEmptyTenantIDReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := &fakeAuthProviderStore{
		byTenant: map[string][]AuthProviderItem{
			"tenant-a": {
				{ProviderConfigID: "oidc-tenant-a", DisplayLabel: "Single sign-on (OIDC)", ProviderKind: "oidc"},
			},
		},
	}
	handler := &AuthProviderListHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// No tenant_id param → must return empty, never call store.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/providers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Providers) != 0 {
		t.Errorf("expected empty list when tenant_id omitted, got %d providers: %s",
			len(resp.Providers), rec.Body.String())
	}
	// Store must NOT have been called — no global scan.
	if store.lastTenantID != "" {
		t.Errorf("store was called with tenantID=%q when tenant_id was omitted; expected no store call",
			store.lastTenantID)
	}
}

func TestAuthProvidersRouteIsPublic(t *testing.T) {
	t.Parallel()

	// Mount a stub handler on the route and wrap it in AuthMiddleware (no token
	// supplied). A public route must respond 200, not 401.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/auth/providers", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	authenticated := AuthMiddleware("shared-token", mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/providers", nil)
	rec := httptest.NewRecorder()
	authenticated.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v0/auth/providers status = %d, want %d (must be public)", rec.Code, http.StatusOK)
	}
}

func TestAuthProvidersHandlerNonGETIsNotPublic(t *testing.T) {
	t.Parallel()

	// POST to /api/v0/auth/providers must NOT bypass auth (only GET is public).
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/auth/providers", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	authenticated := AuthMiddleware("shared-token", mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/providers", nil)
	rec := httptest.NewRecorder()
	authenticated.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/v0/auth/providers status = %d, want %d (must NOT be public)", rec.Code, http.StatusUnauthorized)
	}
}
