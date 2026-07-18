// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	testIssuer   = "https://idp.example.test"
	testAudience = "https://eshu.example.test"
)

func testBufferLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func newTestResolver(t *testing.T, idp *testIdP, providers []BearerProvider, grantResolver oidclogin.GrantResolver, logger *slog.Logger) (*Resolver, *int) {
	t.Helper()
	calls := 0
	resolver, err := NewResolver(context.Background(), Config{
		Source:          &fakeProviderSource{providers: providers},
		GrantResolver:   grantResolver,
		Audience:        testAudience,
		VerifierFactory: idp.verifierFactory(&calls),
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	return resolver, &calls
}

func testProvider() BearerProvider {
	return BearerProvider{
		ProviderConfigID: "pc_1",
		IssuerURL:        testIssuer,
		TenantID:         "tenant_a",
		WorkspaceID:      "workspace_a",
		GroupsClaim:      "groups",
		SubjectClaim:     "sub",
		RevisionID:       "rev_1",
	}
}

func testGrantResolver() *fakeGrantResolver {
	return &fakeGrantResolver{
		grantedForGroupHash: oidclogin.SHA256Hash("engineering"),
		resolution: oidclogin.GrantResolution{
			RoleIDs:                   []string{"role_engineer"},
			PolicyRevisionHash:        "policy_rev_1",
			PermissionCatalogEnforced: true,
			AllowedScopeIDs:           []string{"scope_1"},
		},
	}
}

// TestResolveScopedToken_NonJWTCredential_FallsThrough proves an opaque
// (non-JWT) credential — the shape every Eshu-issued token has — falls
// through with (zero, false, nil) so the resolver chain keeps trying the
// next resolver, even when a bearer IdP is enabled and would otherwise match
// nothing about this credential anyway.
func TestResolveScopedToken_NonJWTCredential_FallsThrough(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	resolver, calls := newTestResolver(t, idp, []BearerProvider{testProvider()}, testGrantResolver(), nil)
	// One factory call already happened inside NewResolver's synchronous
	// initial rebuild (a real provider is enabled); capture that baseline so
	// this test proves resolving a non-JWT credential adds no further call,
	// rather than asserting an absolute zero that construction itself
	// already violates.
	before := *calls

	auth, ok, err := resolver.ResolveScopedToken(context.Background(), "sha256:deadbeef-opaque-token")
	if err != nil || ok {
		t.Fatalf("ResolveScopedToken() = %+v, %v, %v, want (zero, false, nil)", auth, ok, err)
	}
	if *calls != before {
		t.Fatalf("verifier factory calls = %d, want unchanged at %d for a non-JWT credential", *calls, before)
	}
}

// TestResolveScopedToken_EmptySnapshot_FallsThrough_NoFactoryCall proves the
// zero-provider fast path (AC #4): a JWT-shaped credential against a
// resolver with no enabled providers returns (zero, false, nil) instantly,
// and the verifier factory is never called.
func TestResolveScopedToken_EmptySnapshot_FallsThrough_NoFactoryCall(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	resolver, calls := newTestResolver(t, idp, nil, testGrantResolver(), nil)

	jwtShaped := "aaaa.bbbb.cccc"
	auth, ok, err := resolver.ResolveScopedToken(context.Background(), jwtShaped)
	if err != nil || ok {
		t.Fatalf("ResolveScopedToken() = %+v, %v, %v, want (zero, false, nil)", auth, ok, err)
	}
	if *calls != 0 {
		t.Fatalf("verifier factory calls = %d, want 0 for the zero-provider fast path", *calls)
	}
}

// TestResolveScopedToken_ValidToken_ResolvesAuthContext proves the full
// happy path: a correctly signed, correctly audienced token maps to an
// AuthContext carrying the grants the fake GrantResolver (standing in for
// the SAME resolver interactive login uses) returns for the token's groups.
func TestResolveScopedToken_ValidToken_ResolvesAuthContext(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	grantResolver := testGrantResolver()
	resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, grantResolver, nil)

	token := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	auth, ok, err := resolver.ResolveScopedToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ResolveScopedToken() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("ResolveScopedToken() ok = false, want true for a valid token")
	}
	if auth.Mode != query.AuthModeScoped {
		t.Fatalf("Mode = %q, want AuthModeScoped (inherits the F-6 scoped-route allowlist)", auth.Mode)
	}
	if auth.SubjectClass != subjectClassExternalOIDCUser {
		t.Fatalf("SubjectClass = %q, want %q", auth.SubjectClass, subjectClassExternalOIDCUser)
	}
	if auth.TenantID != "tenant_a" || auth.WorkspaceID != "workspace_a" {
		t.Fatalf("TenantID/WorkspaceID = %q/%q, want tenant_a/workspace_a", auth.TenantID, auth.WorkspaceID)
	}
	if len(auth.RoleIDs) != 1 || auth.RoleIDs[0] != "role_engineer" {
		t.Fatalf("RoleIDs = %v, want [role_engineer]", auth.RoleIDs)
	}
	if !auth.PermissionCatalogEnforced {
		t.Fatal("PermissionCatalogEnforced = false, want true (fake resolver sets it)")
	}
	if auth.ExternalProviderConfigID != "pc_1" {
		t.Fatalf("ExternalProviderConfigID = %q, want pc_1", auth.ExternalProviderConfigID)
	}
	wantSubjectHash := oidclogin.SHA256Hash("pc_1:user-1")
	if auth.SubjectIDHash != wantSubjectHash {
		t.Fatalf("SubjectIDHash = %q, want %q", auth.SubjectIDHash, wantSubjectHash)
	}
	if grantResolver.calls != 1 {
		t.Fatalf("grant resolver calls = %d, want 1", grantResolver.calls)
	}
}

// denialCase drives one distinct-outcome denial scenario.
type denialCase struct {
	name        string
	buildClaims func() tokenClaims
	badSig      bool
}

// TestResolveScopedToken_DistinctDenialOutcomes proves AC #2: a wrong
// audience, an unknown issuer, an expired token, and a bad signature must
// all deny with (zero, false, err) — never (zero, false, nil), since once a
// credential is JWT-shaped and a provider is enabled this resolver owns the
// verdict outright (see resolver.go's ResolveScopedToken doc comment).
func TestResolveScopedToken_DistinctDenialOutcomes(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)

	cases := []denialCase{
		{
			name: "wrong_audience",
			buildClaims: func() tokenClaims {
				return defaultTokenClaims(testIssuer, "https://not-eshu.example.test")
			},
		},
		{
			name: "unknown_issuer",
			buildClaims: func() tokenClaims {
				return defaultTokenClaims("https://not-enabled.example.test", testAudience)
			},
		},
		{
			name: "expired",
			buildClaims: func() tokenClaims {
				claims := defaultTokenClaims(testIssuer, testAudience)
				claims.issuedAt = claims.expiry.Add(-2 * time.Hour)
				claims.expiry = claims.issuedAt.Add(time.Minute) // already-expired window
				return claims
			},
		},
		{
			name:   "bad_signature",
			badSig: true,
			buildClaims: func() tokenClaims {
				return defaultTokenClaims(testIssuer, testAudience)
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, testGrantResolver(), nil)
			claims := tc.buildClaims()
			token := idp.sign(t, claims, tc.badSig)

			auth, ok, err := resolver.ResolveScopedToken(context.Background(), token)
			if ok {
				t.Fatalf("ResolveScopedToken() ok = true, want false for %s", tc.name)
			}
			if err == nil {
				t.Fatalf("ResolveScopedToken() error = nil, want a denial error for %s (must not silently fall through)", tc.name)
			}
			if auth.Mode != "" || auth.SubjectIDHash != "" || len(auth.RoleIDs) != 0 {
				t.Fatalf("ResolveScopedToken() auth = %+v, want zero value on denial", auth)
			}
		})
	}
}

// TestResolveScopedToken_UnknownIssuer_NeverCallsVerifier proves an
// unmatched issuer denies before ever touching a verifier/factory — routing
// happens on the unverified iss claim alone.
func TestResolveScopedToken_UnknownIssuer_NeverCallsVerifier(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	resolver, calls := newTestResolver(t, idp, []BearerProvider{testProvider()}, testGrantResolver(), nil)

	token := idp.sign(t, defaultTokenClaims("https://not-enabled.example.test", testAudience), false)
	_, ok, err := resolver.ResolveScopedToken(context.Background(), token)
	if ok || err == nil {
		t.Fatalf("ResolveScopedToken() = ok:%v err:%v, want denied", ok, err)
	}
	if *calls != 1 {
		// The one enabled provider's verifier is still built once during
		// NewResolver's synchronous initial rebuild; an unmatched issuer
		// must not trigger any additional factory call.
		t.Fatalf("verifier factory calls = %d, want exactly 1 (built once at construction, never again for an unmatched issuer)", *calls)
	}
}

// TestResolveScopedToken_EmptyGroups_DeniedNoGrants proves a verified token
// with no groups claim denies with no_grants rather than resolving an
// AuthContext with zero roles.
func TestResolveScopedToken_EmptyGroups_DeniedNoGrants(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, testGrantResolver(), nil)

	claims := defaultTokenClaims(testIssuer, testAudience)
	claims.groups = nil
	token := idp.sign(t, claims, false)

	_, ok, err := resolver.ResolveScopedToken(context.Background(), token)
	if ok || err == nil {
		t.Fatalf("ResolveScopedToken() = ok:%v err:%v, want denied (no_grants)", ok, err)
	}
}

// TestResolveScopedToken_GrantResolverFindsNothing_DeniedNoGrants proves a
// verified token whose groups resolve to zero roles denies, matching AC's
// "empty RoleIDs -> deny" step.
func TestResolveScopedToken_GrantResolverFindsNothing_DeniedNoGrants(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	grantResolver := &fakeGrantResolver{grantedForGroupHash: "no-such-hash-will-ever-match"}
	resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, grantResolver, nil)

	token := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	_, ok, err := resolver.ResolveScopedToken(context.Background(), token)
	if ok || err == nil {
		t.Fatalf("ResolveScopedToken() = ok:%v err:%v, want denied (no_grants)", ok, err)
	}
}

// TestResolveScopedToken_GrantResolverErrors_DeniedNoGrants proves a hard
// grant-resolution error (e.g. the DB is unavailable) still fails closed
// rather than propagating a 500 or, worse, granting access.
func TestResolveScopedToken_GrantResolverErrors_DeniedNoGrants(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	grantResolver := &fakeGrantResolver{err: errNoGrantsFixture}
	resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, grantResolver, nil)

	token := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	_, ok, err := resolver.ResolveScopedToken(context.Background(), token)
	if ok || err == nil {
		t.Fatalf("ResolveScopedToken() = ok:%v err:%v, want denied", ok, err)
	}
}

// TestResolveScopedToken_RawTokenNeverLogged proves the raw bearer token
// never appears in structured log output across both a successful resolve
// and a denied one — the invariant AGENTS.md calls out explicitly.
func TestResolveScopedToken_RawTokenNeverLogged(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	var buf bytes.Buffer
	logger := testBufferLogger(&buf)
	resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, testGrantResolver(), logger)

	validToken := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	if _, ok, err := resolver.ResolveScopedToken(context.Background(), validToken); err != nil || !ok {
		t.Fatalf("valid token resolve failed: ok=%v err=%v", ok, err)
	}

	badClaims := defaultTokenClaims(testIssuer, testAudience)
	badToken := idp.sign(t, badClaims, true)
	if _, ok, err := resolver.ResolveScopedToken(context.Background(), badToken); ok || err == nil {
		t.Fatalf("bad-signature token unexpectedly resolved: ok=%v err=%v", ok, err)
	}

	logged := buf.String()
	for _, raw := range []string{validToken, badToken} {
		if strings.Contains(logged, raw) {
			t.Fatalf("captured log output contains the raw token: %s", logged)
		}
		// Also check each dot-separated segment individually: a partial
		// leak (e.g. only the payload segment) would still be a token leak.
		for _, segment := range strings.Split(raw, ".") {
			if len(segment) > 12 && strings.Contains(logged, segment) {
				t.Fatalf("captured log output contains a raw token segment: %s", logged)
			}
		}
	}
}
