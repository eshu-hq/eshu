// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"testing"
)

// fakeOAuthChallengePolicy implements OAuthChallengePolicy with a fixed
// return value for unit tests exercising the 401 WWW-Authenticate wiring in
// isolation. Moved from root package query's auth_oauth_challenge_test.go
// (#6642) alongside the TestOAuthWWWAuthenticateChallenge_* tests below,
// which reach the unexported oauthWWWAuthenticateChallenge that moved with
// them. Root keeps its own copy of this fake (same name) for the
// middleware-stack tests that stayed there.
type fakeOAuthChallengePolicy struct {
	metadataURL string
	scope       string
	ok          bool
}

func (f *fakeOAuthChallengePolicy) OAuthChallenge(context.Context) (string, string, bool) {
	return f.metadataURL, f.scope, f.ok
}

func TestOAuthWWWAuthenticateChallenge_NilPolicy_BareBearer(t *testing.T) {
	t.Parallel()

	got := oauthWWWAuthenticateChallenge(context.Background(), nil)
	if got != "Bearer" {
		t.Fatalf("oauthWWWAuthenticateChallenge(nil) = %q, want %q", got, "Bearer")
	}
}

func TestOAuthWWWAuthenticateChallenge_PolicyNotOK_BareBearer(t *testing.T) {
	t.Parallel()

	got := oauthWWWAuthenticateChallenge(context.Background(), &fakeOAuthChallengePolicy{ok: false})
	if got != "Bearer" {
		t.Fatalf("oauthWWWAuthenticateChallenge() = %q, want bare %q when policy reports not-ok", got, "Bearer")
	}
}

func TestOAuthWWWAuthenticateChallenge_PolicyOK_AddsResourceMetadataAndScope(t *testing.T) {
	t.Parallel()

	got := oauthWWWAuthenticateChallenge(context.Background(), &fakeOAuthChallengePolicy{
		metadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		scope:       "openid profile email groups",
		ok:          true,
	})
	want := `Bearer resource_metadata="https://eshu.example.test/.well-known/oauth-protected-resource", scope="openid profile email groups"`
	if got != want {
		t.Fatalf("oauthWWWAuthenticateChallenge() = %q, want %q", got, want)
	}
}

func TestOAuthWWWAuthenticateChallenge_DelimiterInPolicyValue_BareBearer(t *testing.T) {
	t.Parallel()

	// Defense in depth: a policy that (contrary to wiring-time validation)
	// hands back a metadata URL or scope carrying a quote or CRLF must degrade
	// to a bare challenge, never inject the delimiter into the header value.
	for _, tc := range []struct {
		name        string
		metadataURL string
		scope       string
	}{
		{"quoted metadata url", `https://eshu.example.test/.well-known/oauth-protected-resource"`, "openid"},
		{"crlf metadata url", "https://eshu.example.test/.well-known/oauth-protected-resource\r\nX-Injected: 1", "openid"},
		{"quoted scope", "https://eshu.example.test/.well-known/oauth-protected-resource", `openid"`},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := oauthWWWAuthenticateChallenge(context.Background(), &fakeOAuthChallengePolicy{
				metadataURL: tc.metadataURL,
				scope:       tc.scope,
				ok:          true,
			})
			if got != "Bearer" {
				t.Fatalf("oauthWWWAuthenticateChallenge() = %q, want bare %q when a policy value carries a header delimiter", got, "Bearer")
			}
		})
	}
}

func TestOAuthWWWAuthenticateChallenge_PolicyOK_EmptyScopeOmitsScopeParam(t *testing.T) {
	t.Parallel()

	got := oauthWWWAuthenticateChallenge(context.Background(), &fakeOAuthChallengePolicy{
		metadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		ok:          true,
	})
	want := `Bearer resource_metadata="https://eshu.example.test/.well-known/oauth-protected-resource"`
	if got != want {
		t.Fatalf("oauthWWWAuthenticateChallenge() = %q, want %q", got, want)
	}
}

func TestOAuthWWWAuthenticateChallenge_PolicyOKEmptyMetadataURL_BareBearer(t *testing.T) {
	t.Parallel()

	got := oauthWWWAuthenticateChallenge(context.Background(), &fakeOAuthChallengePolicy{ok: true})
	if got != "Bearer" {
		t.Fatalf("oauthWWWAuthenticateChallenge() = %q, want bare %q when metadataURL is empty even if ok=true", got, "Bearer")
	}
}
