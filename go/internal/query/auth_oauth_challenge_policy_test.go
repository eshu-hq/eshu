// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
)

// These tests exercise PostureOAuthChallengePolicy.OAuthChallenge, the 401
// WWW-Authenticate augmentation gate (issue #5163, F-2). They share the
// fakeAuthProviderStore and fakeOAuthIssuerLister package-test fakes defined in
// auth_oauth_discovery_test.go; the handler-side (serveMetadata) tests live
// there and in auth_oauth_discovery_serve_test.go. The split keeps every file
// under the 500-line cap.

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
		// A configured provider AND an active issuer — the production shape the
		// metadata route serves 200 for. (A configured provider with no active
		// issuer is covered by the *_ProvidersButNoActiveIssuers_NotOK case.)
		Issuers: &fakeOAuthIssuerLister{issuers: []string{"https://acme.okta.com/oauth2/default"}},
	}
	metadataURL, scope, ok := policy.OAuthChallenge(context.Background())
	if !ok {
		t.Fatal("OAuthChallenge() ok = false, want true with a configured provider and active issuer")
	}
	if metadataURL != "https://eshu.example.test/.well-known/oauth-protected-resource" {
		t.Errorf("metadataURL = %q, want the configured metadata URL", metadataURL)
	}
	if scope != DefaultOAuthChallengeScope {
		t.Errorf("scope = %q, want %q", scope, DefaultOAuthChallengeScope)
	}
}

// TestPostureOAuthChallengePolicy_ProvidersButNoActiveIssuers_NotOK proves the
// challenge gate agrees with OAuthProtectedResourceHandler's §D active-issuer
// 404 gate: a provider row can exist while zero bearer issuers are active
// (browser-login-only providers, a shared-issuer fail-closed exclusion, a
// verifier-build failure, or the snapshot-TTL startup window). In that state
// the metadata route 404s, so OAuthChallenge must NOT mint a challenge pointing
// a client at a URL that would itself 404.
func TestPostureOAuthChallengePolicy_ProvidersButNoActiveIssuers_NotOK(t *testing.T) {
	t.Parallel()

	policy := &PostureOAuthChallengePolicy{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID:    "default",
		MetadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		Scope:       DefaultOAuthChallengeScope,
		Issuers:     &fakeOAuthIssuerLister{}, // reports zero active issuers
	}
	if _, _, ok := policy.OAuthChallenge(context.Background()); ok {
		t.Fatal("OAuthChallenge() ok = true with a provider but zero active issuers; want false (route would 404)")
	}

	// Control: the same provider posture WITH an active issuer challenges.
	policy.Issuers = &fakeOAuthIssuerLister{issuers: []string{"https://acme.okta.com/oauth2/default"}}
	if _, _, ok := policy.OAuthChallenge(context.Background()); !ok {
		t.Fatal("OAuthChallenge() ok = false with a provider and an active issuer; want true")
	}
}

// TestPostureOAuthChallengePolicy_NilIssuers_NotOK proves the challenge gate
// treats a nil Issuers lister identically to one reporting zero active issuers:
// not OK. OAuthProtectedResourceHandler.serveMetadata 404s on nil Issuers
// (len of a nil-lister's list is zero), so a challenge minted here would point a
// client at a URL that itself 404s — the exact invariant the type doc comment
// forbids. Production never constructs a nil-Issuers policy (the *Resolver
// always implements the lister when ESHU_AUTH_RESOURCE_URI is set), so this
// guards a future wiring change rather than a current path.
func TestPostureOAuthChallengePolicy_NilIssuers_NotOK(t *testing.T) {
	t.Parallel()

	policy := &PostureOAuthChallengePolicy{
		Providers: &fakeAuthProviderStore{byTenant: map[string][]AuthProviderItem{
			"default": {{ProviderConfigID: "okta-oidc", ProviderKind: "oidc"}},
		}},
		TenantID:    "default",
		MetadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		Scope:       DefaultOAuthChallengeScope,
		Issuers:     nil, // no lister wired: handler would 404, so no challenge
	}
	if _, _, ok := policy.OAuthChallenge(context.Background()); ok {
		t.Fatal("OAuthChallenge() ok = true with a nil Issuers lister; want false (route would 404)")
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
