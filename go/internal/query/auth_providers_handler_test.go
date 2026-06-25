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
type fakeAuthProviderStore struct {
	items []AuthProviderItem
	err   error
}

func (f *fakeAuthProviderStore) ListLoginProviders(_ context.Context) ([]AuthProviderItem, error) {
	return f.items, f.err
}

func TestAuthProvidersHandlerReturnsConfiguredProviders(t *testing.T) {
	t.Parallel()

	store := &fakeAuthProviderStore{
		items: []AuthProviderItem{
			{ProviderConfigID: "okta-oidc", DisplayLabel: "Single sign-on (OIDC)", ProviderKind: "oidc"},
			{ProviderConfigID: "okta-saml", DisplayLabel: "Single sign-on (SAML)", ProviderKind: "saml"},
		},
	}
	handler := &AuthProviderListHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

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
}

func TestAuthProvidersHandlerReturnsEmptyListWhenNoneConfigured(t *testing.T) {
	t.Parallel()

	store := &fakeAuthProviderStore{items: nil}
	handler := &AuthProviderListHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

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
	if resp.Providers == nil || len(resp.Providers) != 0 {
		t.Fatalf("providers = %#v, want empty non-null array", resp.Providers)
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
