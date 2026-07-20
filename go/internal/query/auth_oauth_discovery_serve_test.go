// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleMetadataRootServesAtExactPath exercises the handleMetadataRoot
// handler: with a resource URI carrying no path, the root
// /.well-known/oauth-protected-resource path serves the document naming the
// active issuer, and any suffix 404s.
func TestHandleMetadataRootServesAtExactPath(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID: "default",
		Resource: "https://eshu.example.test",
		Issuers:  &fakeOAuthIssuerLister{issuers: []string{"https://acme.okta.com/oauth2/default"}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("root path status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "https://acme.okta.com/oauth2/default") || !strings.Contains(body, "https://eshu.example.test") {
		t.Fatalf("root document missing issuer or resource: %s", body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("suffix path status = %d, want 404 for a path-less resource URI", rec.Code)
	}
}

// TestHandleMetadataSuffixServesDerivedPath exercises the handleMetadataSuffix
// handler: with a resource URI carrying a /mcp path, the RFC 9728 §3 derived
// /.well-known/oauth-protected-resource/mcp path serves the identical document,
// while a non-matching suffix 404s.
func TestHandleMetadataSuffixServesDerivedPath(t *testing.T) {
	t.Parallel()

	handler := &OAuthProtectedResourceHandler{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID: "default",
		Resource: "https://eshu.example.test/mcp",
		Issuers:  &fakeOAuthIssuerLister{issuers: []string{"https://acme.okta.com/oauth2/default"}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("derived suffix status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "https://eshu.example.test/mcp") {
		t.Fatalf("derived-suffix document missing the path-carrying resource: %s", body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/wrong", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-matching suffix status = %d, want 404", rec.Code)
	}
}
