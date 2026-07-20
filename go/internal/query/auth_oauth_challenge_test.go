// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOAuthChallengePolicy implements OAuthChallengePolicy with a fixed
// return value for unit tests exercising the 401 WWW-Authenticate wiring in
// isolation from DeriveAuthPosture/PostureOAuthChallengePolicy (which have
// their own focused tests in auth_oauth_discovery_test.go).
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

// TestAuthMiddlewareWithOAuthChallenge_Unauthenticated_AddsResourceMetadata
// proves issue #5163 (F-2) acceptance criterion #1's challenge shape: an
// unauthenticated request against a stack whose OAuthChallengePolicy reports
// OAuth is enabled receives resource_metadata (and scope) on the 401. This
// exercises the real production entry point
// (AuthMiddlewareWithScopedTokensGovernanceAuditAndOAuthChallenge) against
// the already-auth-gated mux, proving the CHALLENGE SHAPE the same way
// cmd/mcp-server's actual /api/v0/* routes would once wired — see this
// package's proof notes for why the literal POST /mcp/message transport
// route is out of scope here (issue #5168/F-7 gates that route at all).
func TestAuthMiddlewareWithOAuthChallenge_Unauthenticated_AddsResourceMetadata(t *testing.T) {
	t.Parallel()

	policy := &fakeOAuthChallengePolicy{
		metadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		scope:       DefaultOAuthChallengeScope,
		ok:          true,
	}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge(
		"shared-token", nil, mockHandler(), nil, true, policy,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	want := `Bearer resource_metadata="https://eshu.example.test/.well-known/oauth-protected-resource", scope="openid profile email groups"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

// TestAuthMiddlewareWithOAuthChallenge_ValidSharedToken_NeverChallenged proves
// the issue #5163 AC #2 regression: a request bearing a VALID credential must
// never receive the OAuth challenge (or any WWW-Authenticate header at all) —
// the exact anthropics/claude-code#59467 precedence bug this issue calls out.
func TestAuthMiddlewareWithOAuthChallenge_ValidSharedToken_NeverChallenged(t *testing.T) {
	t.Parallel()

	policy := &fakeOAuthChallengePolicy{
		metadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		scope:       DefaultOAuthChallengeScope,
		ok:          true,
	}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge(
		"shared-token", nil, mockHandler(), nil, true, policy,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer shared-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a valid credential", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want no header at all for a valid credential (precedence bug regression)", got)
	}
}

// TestAuthMiddlewareWithOAuthChallenge_PolicyNotOK_ByteIdenticalToToday
// proves the issue #5163 AC #3 regression: a token-only deployment (policy
// reports not-ok, e.g. PostureOAuthChallengePolicy with zero providers) gets
// EXACTLY today's bare "Bearer" challenge — byte-identical, no new header
// parameters leak in.
func TestAuthMiddlewareWithOAuthChallenge_PolicyNotOK_ByteIdenticalToToday(t *testing.T) {
	t.Parallel()

	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge(
		"shared-token", nil, mockHandler(), nil, true, &fakeOAuthChallengePolicy{ok: false},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q, want bare %q (byte-identical to pre-#5163 behavior)", got, "Bearer")
	}
}

// TestAuthMiddlewareWithOAuthChallenge_NilPolicy_ByteIdenticalToToday proves
// the same byte-identical regression when no policy is wired at all (nil),
// which is exactly today's every-existing-caller state before any #5163
// wiring lands.
func TestAuthMiddlewareWithOAuthChallenge_NilPolicy_ByteIdenticalToToday(t *testing.T) {
	t.Parallel()

	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge(
		"shared-token", nil, mockHandler(), nil, true, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q, want bare %q", got, "Bearer")
	}
}

// TestAuthMiddleware_BrowserSessionDenial_NeverCarriesOAuthChallenge proves
// the design correction raised in review: a cookie-based browser-session
// denial (stale/refresh-required session, the tryBrowserSessionAuth path)
// must NEVER receive the RFC 9728 bearer challenge, even when an
// OAuthChallengePolicy IS wired for the SAME middleware instance's genuine
// bearer-credential paths. This calls authMiddlewareWithRoutePolicy directly
// (the one shared entry point both browser-session and bearer-credential
// denials flow through) with both a failing BrowserSessionResolver and an
// ok=true OAuthChallengePolicy, so a regression that leaked the challenge
// into the browser path would be caught here even though no current
// exported constructor combines browser sessions with an OAuthChallengePolicy.
func TestAuthMiddleware_BrowserSessionDenial_NeverCarriesOAuthChallenge(t *testing.T) {
	t.Parallel()

	sessionResolver := &fakeBrowserSessionResolver{err: ErrBrowserSessionRefreshRequired}
	oauthPolicy := &fakeOAuthChallengePolicy{
		metadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		scope:       DefaultOAuthChallengeScope,
		ok:          true,
	}
	handler := authMiddlewareWithRoutePolicy(
		"shared-token", nil, sessionResolver, mockHandler(), nil, BrowserSessionRoutePolicy{}, true, oauthPolicy,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q, want bare %q — a browser-session 401 must never carry the OAuth bearer challenge", got, "Bearer")
	}
}

// TestAuthMiddleware_SAMLHandlerDenial_NeverCarriesOAuthChallenge proves the
// same design correction for a handler-level unauthorizedResponse call site
// entirely outside authMiddlewareWithRoutePolicy (saml_handler.go and its
// siblings): since those call sites build their own plain *http.Request and
// never pass through requestWithOAuthChallenge, oauthChallengePolicyFromContext
// finds nothing regardless of what any middleware in the stack has wired —
// this exercises unauthorizedResponse's own context lookup directly to prove
// that absent-context invariant.
func TestAuthMiddleware_SAMLHandlerDenial_NeverCarriesOAuthChallenge(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/saml/providers/provider_a/metadata", nil)
	rec := httptest.NewRecorder()
	unauthorizedResponse(rec, req)

	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q, want bare %q for a handler-level 401 with no requestWithOAuthChallenge wrapping", got, "Bearer")
	}
}
