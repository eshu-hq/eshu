// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// augmentedChallenge is the exact WWW-Authenticate value a gated 401 carries
// when the fake policy reports OAuth enabled. Kept as one constant so the
// decision-table rows assert against a single source of truth.
const augmentedChallenge = `Bearer resource_metadata="https://eshu.example.test/.well-known/oauth-protected-resource", scope="openid profile email groups"`

// enabledChallengePolicy returns a policy that reports OAuth enabled with the
// augmentedChallenge parameters.
func enabledChallengePolicy() *fakeOAuthChallengePolicy {
	return &fakeOAuthChallengePolicy{
		metadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		scope:       DefaultOAuthChallengeScope,
		ok:          true,
	}
}

// TestAuthMiddlewareOAuthChallenge_DecisionTable exercises the issue #5163 §C
// security-critical predicate end to end through the real production entry
// point (AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge)
// with a fake ScopedTokenResolver standing in for the composite
// scoped-token/oidcbearer chain. Each row asserts the EXACT WWW-Authenticate
// header, proving the augment-vs-bare decision for every credential shape the
// design table enumerates. Rows 1, 8, and 10 (headerless, malformed-scheme,
// browser-cookie) are covered by the dedicated tests above; this table covers
// the resolver-outcome-dependent rows 5, 6, 7, 9, and 11.
func TestAuthMiddlewareOAuthChallenge_DecisionTable(t *testing.T) {
	t.Parallel()

	sentinelWrapped := fmt.Errorf("oidcbearer: bearer token denied: unknown_issuer: %w", ErrBearerCredentialUnrecognized)

	cases := []struct {
		name          string
		resolver      *fakeScopedTokenResolver
		authorization string
		wantChallenge string
	}{
		{
			// Row 5: a recognized issuer's token that failed verification
			// (expired/bad-sig/wrong-aud/no-grants) — resolver denies with a
			// NON-sentinel error. Stays bare.
			name:          "row5_post_match_denial_bare",
			resolver:      &fakeScopedTokenResolver{err: errors.New("oidcbearer: bearer token denied: expired")},
			authorization: "Bearer expired.jwt.token",
			wantChallenge: "Bearer",
		},
		{
			// Row 6: a JWT-shaped credential whose issuer is not in the active
			// snapshot — resolver denies with the sentinel-wrapped error.
			// Augments.
			name:          "row6_unknown_issuer_augments",
			resolver:      &fakeScopedTokenResolver{err: sentinelWrapped},
			authorization: "Bearer eyJ.unknown.issuer",
			wantChallenge: augmentedChallenge,
		},
		{
			// Row 7: a JWT-shaped credential unparseable before verification —
			// resolver denies with the sentinel (pre-parse malformed). Augments.
			name:          "row7_preparse_malformed_augments",
			resolver:      &fakeScopedTokenResolver{err: fmt.Errorf("oidcbearer: bearer token denied: malformed: %w", ErrBearerCredentialUnrecognized)},
			authorization: "Bearer eyJ.broken",
			wantChallenge: augmentedChallenge,
		},
		{
			// Row 9: an opaque credential that matched no resolver (ok=false,
			// no error) and is not the shared token. Augments at the
			// token-mismatch site.
			name:          "row9_invalid_opaque_augments",
			resolver:      &fakeScopedTokenResolver{ok: false},
			authorization: "Bearer opaque-not-a-jwt",
			wantChallenge: augmentedChallenge,
		},
		{
			// Row 11: a resolver infra error (DB down) surfaced as a
			// NON-sentinel error. Fails safe to bare (never steer a client to
			// discovery on our own outage).
			name:          "row11_infra_error_bare",
			resolver:      &fakeScopedTokenResolver{err: errors.New("scopedtoken: identity store unavailable")},
			authorization: "Bearer opaque-cred",
			wantChallenge: "Bearer",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge(
				"shared-token", tc.resolver, mockHandler(), nil, true, enabledChallengePolicy(),
			)
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
			req.Header.Set("Authorization", tc.authorization)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != tc.wantChallenge {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, tc.wantChallenge)
			}
		})
	}
}

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
		"shared-token", nil, sessionResolver, mockHandler(), nil, BrowserSessionRoutePolicy{}, true, oauthPolicy, nil,
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
