// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// integrationProviderStore is a query.AuthProviderStore reporting one active
// provider so DeriveAuthPosture enables discovery. The real bearer issuer set
// comes from the real *Resolver (ActiveIssuers), not from this store.
type integrationProviderStore struct{}

func (integrationProviderStore) ListLoginProviders(context.Context, string) ([]query.AuthProviderItem, error) {
	return []query.AuthProviderItem{{ProviderConfigID: "pc_1", ProviderKind: "oidc"}}, nil
}

var resourceMetadataParam = regexp.MustCompile(`resource_metadata="([^"]+)"`)

// TestOAuthDiscovery_EndToEnd wires the REAL bearer resolver (as both the
// middleware's ScopedTokenResolver and the discovery document's issuer lister),
// the REAL auth middleware with an OAuth challenge policy, and the REAL RFC 9728
// discovery handler against the shared in-process test IdP (real go-oidc Verify
// path). It proves the three issue #5163 acceptance criteria end to end:
//
//	(a) a credential-less request is denied 401 with an augmented challenge whose
//	    resource_metadata URL resolves to a document naming the real issuer;
//	(b) a VALID bearer token is served 200 with NO WWW-Authenticate header at all
//	    (the anthropics/claude-code#59467 precedence regression);
//	(c) an expired token is denied 401 with the bare "Bearer" challenge.
func TestOAuthDiscovery_EndToEnd(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, testGrantResolver(), nil)

	const metadataURL = testAudience + "/.well-known/oauth-protected-resource"
	providers := integrationProviderStore{}
	discovery := &query.OAuthProtectedResourceHandler{
		Providers:       providers,
		TenantID:        "default",
		Issuers:         resolver, // *Resolver implements OAuthAuthorizationServerLister
		Resource:        testAudience,
		ScopesSupported: strings.Fields(query.DefaultOAuthChallengeScope),
		ResourceName:    "Eshu MCP Server",
	}
	challenge := &query.PostureOAuthChallengePolicy{
		Providers:   providers,
		TenantID:    "default",
		MetadataURL: metadataURL,
		Scope:       query.DefaultOAuthChallengeScope,
	}

	protected := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	// enforcement true, no shared token: the resolver is the only credential path.
	authed := query.AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge(
		"", resolver, protected, nil, true, challenge,
	)

	mux := http.NewServeMux()
	discovery.Mount(mux)
	mux.Handle("/api/v0/repositories", authed)

	// (a) credential-less -> 401 with an augmented challenge whose metadata URL
	// resolves to a document naming the real issuer.
	noCred := httptest.NewRecorder()
	mux.ServeHTTP(noCred, httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil))
	if noCred.Code != http.StatusUnauthorized {
		t.Fatalf("(a) no-cred status = %d, want 401", noCred.Code)
	}
	challengeHeader := noCred.Header().Get("WWW-Authenticate")
	m := resourceMetadataParam.FindStringSubmatch(challengeHeader)
	if m == nil {
		t.Fatalf("(a) WWW-Authenticate = %q, want an augmented resource_metadata directive", challengeHeader)
	}
	metaURL, err := url.Parse(m[1])
	if err != nil {
		t.Fatalf("(a) resource_metadata URL parse: %v", err)
	}
	docRec := httptest.NewRecorder()
	mux.ServeHTTP(docRec, httptest.NewRequest(http.MethodGet, metaURL.Path, nil))
	if docRec.Code != http.StatusOK {
		t.Fatalf("(a) discovery doc status = %d, want 200: %s", docRec.Code, docRec.Body.String())
	}
	if !strings.Contains(docRec.Body.String(), testIssuer) {
		t.Fatalf("(a) discovery doc = %s, want it to name the real issuer %q", docRec.Body.String(), testIssuer)
	}

	// (b) VALID token -> 200 with NO WWW-Authenticate (the #59467 regression).
	validToken := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	validRec := httptest.NewRecorder()
	validReq := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	validReq.Header.Set("Authorization", "Bearer "+validToken)
	mux.ServeHTTP(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("(b) valid token status = %d, want 200: %s", validRec.Code, validRec.Body.String())
	}
	if got := validRec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("(b) WWW-Authenticate = %q, want NO header for a valid credential (#59467 regression)", got)
	}

	// (c) expired token -> 401 bare.
	expiredClaims := defaultTokenClaims(testIssuer, testAudience)
	expiredClaims.issuedAt = expiredClaims.expiry.Add(-2 * time.Hour)
	expiredClaims.expiry = expiredClaims.issuedAt.Add(time.Minute)
	expiredToken := idp.sign(t, expiredClaims, false)
	expiredRec := httptest.NewRecorder()
	expiredReq := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	expiredReq.Header.Set("Authorization", "Bearer "+expiredToken)
	mux.ServeHTTP(expiredRec, expiredReq)
	if expiredRec.Code != http.StatusUnauthorized {
		t.Fatalf("(c) expired token status = %d, want 401", expiredRec.Code)
	}
	if got := expiredRec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("(c) WWW-Authenticate = %q, want bare %q for a recognized-but-expired token", got, "Bearer")
	}
}
