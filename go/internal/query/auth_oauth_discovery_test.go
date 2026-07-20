// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOAuthIssuerLister implements OAuthAuthorizationServerLister for unit
// tests, standing in for internal/oidcbearer.Resolver.ActiveIssuers without
// this package importing oidcbearer (which itself imports query — see
// oidcbearer/resolver.go — so the dependency must run the other way).
type fakeOAuthIssuerLister struct {
	issuers []string
}

func (f *fakeOAuthIssuerLister) ActiveIssuers(context.Context) []string {
	return f.issuers
}

func TestOAuthProtectedResourceHandler_NoResourceConfigured_404(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID: "default",
		// Resource intentionally left empty.
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d when Resource is unconfigured", rec.Code, http.StatusNotFound)
	}
}

// TestOAuthProtectedResourceHandler_NoProviders_404 proves the issue #5163
// acceptance criterion "token-only stack: no /.well-known/oauth-protected-resource
// route (404)": zero configured providers means the route answers exactly as
// if it were never mounted, even though Mount was called.
func TestOAuthProtectedResourceHandler_NoProviders_404(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{}},
		TenantID:  "default",
		Resource:  "https://eshu.example.test",
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d when no provider is configured", rec.Code, http.StatusNotFound)
	}
}

// TestOAuthProtectedResourceHandler_ProvidersConfigured_ReturnsMetadata proves
// the RFC 9728 document shape: resource, authorization_servers sourced from
// the issuer lister (never from AuthProviderItem, which deliberately omits
// IssuerURL for login-picker privacy), and bearer_methods_supported naming
// only the header method Eshu actually accepts.
func TestOAuthProtectedResourceHandler_ProvidersConfigured_ReturnsMetadata(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID:        "default",
		Resource:        "https://eshu.example.test",
		Issuers:         &fakeOAuthIssuerLister{issuers: []string{"https://idp.example.test"}},
		ScopesSupported: []string{"openid", "profile", "email", "groups"},
		ResourceName:    "Eshu",
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var doc OAuthProtectedResourceMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Resource != "https://eshu.example.test" {
		t.Errorf("resource = %q, want %q", doc.Resource, "https://eshu.example.test")
	}
	if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != "https://idp.example.test" {
		t.Errorf("authorization_servers = %v, want [https://idp.example.test]", doc.AuthorizationServers)
	}
	if len(doc.BearerMethodsSupported) != 1 || doc.BearerMethodsSupported[0] != "header" {
		t.Errorf("bearer_methods_supported = %v, want [header]", doc.BearerMethodsSupported)
	}
	if doc.ResourceName != "Eshu" {
		t.Errorf("resource_name = %q, want %q", doc.ResourceName, "Eshu")
	}
	if len(doc.ScopesSupported) != 4 {
		t.Errorf("scopes_supported = %v, want 4 scopes", doc.ScopesSupported)
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Error("Cache-Control header missing")
	}
}

// TestOAuthProtectedResourceHandler_NilIssuerLister_404 proves the §D
// amendment: a discovery document with no authorization_servers is useless (a
// client cannot learn which issuer to obtain a token from), so an unwired
// Issuers dependency — which yields zero active issuers — answers 404 rather
// than serving a document that advertises a resource no client could ever
// complete an OAuth flow for.
func TestOAuthProtectedResourceHandler_NilIssuerLister_404(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID: "default",
		Resource: "https://eshu.example.test",
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d when no active issuer is available", rec.Code, http.StatusNotFound)
	}
}

// TestOAuthProtectedResourceHandler_EmptyActiveIssuers_404 proves the same §D
// 404 gate when the issuer lister IS wired but currently reports zero active
// issuers (every provider misconfigured, or the only providers are ambiguous
// shared-issuer rows fail-closed excluded from the routing table).
func TestOAuthProtectedResourceHandler_EmptyActiveIssuers_404(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID: "default",
		Resource: "https://eshu.example.test",
		Issuers:  &fakeOAuthIssuerLister{issuers: nil},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d when the issuer lister reports zero active issuers", rec.Code, http.StatusNotFound)
	}
}

// TestOAuthProtectedResourceHandler_ResourceVerbatim_MultiIssuerSorted proves
// the "resource" field is the ESHU_AUTH_RESOURCE_URI value verbatim (never
// reconstructed from Host) and authorization_servers reflects the issuer
// lister's already-sorted output, so multiple providers advertise
// deterministically.
func TestOAuthProtectedResourceHandler_ResourceVerbatim_MultiIssuerSorted(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID: "default",
		Resource: "https://eshu.example.test/mcp",
		Issuers:  &fakeOAuthIssuerLister{issuers: []string{"https://a.idp.test", "https://b.idp.test"}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Wrong Host header must not leak into the "resource" field.
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil)
	req.Host = "attacker.example.test"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var doc OAuthProtectedResourceMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Resource != "https://eshu.example.test/mcp" {
		t.Errorf("resource = %q, want the verbatim ESHU_AUTH_RESOURCE_URI, never Host-derived", doc.Resource)
	}
	want := []string{"https://a.idp.test", "https://b.idp.test"}
	if len(doc.AuthorizationServers) != 2 || doc.AuthorizationServers[0] != want[0] || doc.AuthorizationServers[1] != want[1] {
		t.Errorf("authorization_servers = %v, want sorted %v", doc.AuthorizationServers, want)
	}
}

// TestOAuthProtectedResourceHandler_PathSuffixRoute proves the RFC 9728 §3
// derivation: for a resource with a /mcp path, BOTH the bare root and the
// derived /.well-known/oauth-protected-resource/mcp serve the identical
// document, while any other suffix (a transport path like /sse or a wrong
// segment) answers 404 so a strict client falls back to the root.
func TestOAuthProtectedResourceHandler_PathSuffixRoute(t *testing.T) {
	t.Parallel()

	newHandler := func() http.Handler {
		h := &OAuthProtectedResourceHandler{
			Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
				"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
			}},
			TenantID: "default",
			Resource: "https://eshu.example.test/mcp",
			Issuers:  &fakeOAuthIssuerLister{issuers: []string{"https://idp.example.test"}},
		}
		mux := http.NewServeMux()
		h.Mount(mux)
		return mux
	}

	cases := []struct {
		name     string
		path     string
		wantCode int
	}{
		{"root serves", "/.well-known/oauth-protected-resource", http.StatusOK},
		{"derived mcp suffix serves", "/.well-known/oauth-protected-resource/mcp", http.StatusOK},
		{"sse transport suffix 404", "/.well-known/oauth-protected-resource/sse", http.StatusNotFound},
		{"message transport suffix 404", "/.well-known/oauth-protected-resource/mcp/message", http.StatusNotFound},
		{"trailing-slash suffix 404", "/.well-known/oauth-protected-resource/mcp/", http.StatusNotFound},
		{"wrong segment 404", "/.well-known/oauth-protected-resource/api", http.StatusNotFound},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			newHandler().ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("GET %s status = %d, want %d: %s", tc.path, rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}

// TestOAuthProtectedResourceHandler_NoPathResource_AnySuffix404 proves a
// resource with no path (https://host) has no valid RFC 9728 §3 suffix: only
// the root serves, and every suffixed request — including the empty
// trailing-slash form — answers 404.
func TestOAuthProtectedResourceHandler_NoPathResource_AnySuffix404(t *testing.T) {
	t.Parallel()

	h := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID: "default",
		Resource: "https://eshu.example.test",
		Issuers:  &fakeOAuthIssuerLister{issuers: []string{"https://idp.example.test"}},
	}
	mux := http.NewServeMux()
	h.Mount(mux)

	if rec := httptest.NewRecorder(); true {
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("root status = %d, want 200 for a no-path resource", rec.Code)
		}
	}
	for _, path := range []string{
		"/.well-known/oauth-protected-resource/",
		"/.well-known/oauth-protected-resource/mcp",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404 for a no-path resource", path, rec.Code)
		}
	}
}

func TestOAuthProtectedResourceHandler_StoreError_500(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{err: errors.New("db unavailable")},
		TenantID:  "default",
		Resource:  "https://eshu.example.test",
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d on a posture-derivation failure", rec.Code, http.StatusInternalServerError)
	}
}

func TestPostureOAuthChallengePolicy_NoProviders_NotOK(t *testing.T) {
	t.Parallel()

	policy := &PostureOAuthChallengePolicy{
		Providers:   &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{}},
		TenantID:    "default",
		MetadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		Scope:       DefaultOAuthChallengeScope,
	}
	metadataURL, scope, ok := policy.OAuthChallenge(context.Background())
	if ok {
		t.Fatalf("OAuthChallenge() = (%q, %q, %v), want ok=false with no providers configured", metadataURL, scope, ok)
	}
}

func TestPostureOAuthChallengePolicy_ProvidersConfigured_ReturnsChallengeParams(t *testing.T) {
	t.Parallel()

	policy := &PostureOAuthChallengePolicy{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID:    "default",
		MetadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		Scope:       DefaultOAuthChallengeScope,
	}
	metadataURL, scope, ok := policy.OAuthChallenge(context.Background())
	if !ok {
		t.Fatal("OAuthChallenge() ok = false, want true with a configured provider")
	}
	if metadataURL != "https://eshu.example.test/.well-known/oauth-protected-resource" {
		t.Errorf("metadataURL = %q, want the configured metadata URL", metadataURL)
	}
	if scope != DefaultOAuthChallengeScope {
		t.Errorf("scope = %q, want %q", scope, DefaultOAuthChallengeScope)
	}
}

func TestPostureOAuthChallengePolicy_DeriveError_FailsSafeToNotOK(t *testing.T) {
	t.Parallel()

	policy := &PostureOAuthChallengePolicy{
		Providers:   &fakeAuthProviderStore{err: errors.New("db unavailable")},
		TenantID:    "default",
		MetadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		Scope:       DefaultOAuthChallengeScope,
	}
	if _, _, ok := policy.OAuthChallenge(context.Background()); ok {
		t.Fatal("OAuthChallenge() ok = true, want false (fail safe to bare challenge) on a posture-derivation error")
	}
}

func TestPostureOAuthChallengePolicy_EmptyMetadataURL_NotOK(t *testing.T) {
	t.Parallel()

	policy := &PostureOAuthChallengePolicy{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID: "default",
		Scope:    DefaultOAuthChallengeScope,
	}
	if _, _, ok := policy.OAuthChallenge(context.Background()); ok {
		t.Fatal("OAuthChallenge() ok = true, want false when MetadataURL is unconfigured")
	}
}

func TestPostureOAuthChallengePolicy_NilPolicy_NotOK(t *testing.T) {
	t.Parallel()

	var policy *PostureOAuthChallengePolicy
	if _, _, ok := policy.OAuthChallenge(context.Background()); ok {
		t.Fatal("OAuthChallenge() on a nil policy ok = true, want false")
	}
}
