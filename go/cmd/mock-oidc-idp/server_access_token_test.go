// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- RFC 8707 resource-triggered JWT access tokens (F-9, issue #5170) ---
//
// go/internal/oidcbearer's Resolver validates a JWT access token (iss/aud/exp/
// groups against JWKS); the scripted MCP OAuth client (issue #5170) needs
// this mock IdP to mint one on request, while staying byte-stable for the
// #4971 browser-auth suite that never asks for it.

// tokenExchangeParams captures the query/form parameters one authorize+token
// round trip needs, letting the JWT-minting tests vary where `resource` is
// carried without repeating the full HTTP dance inline.
type tokenExchangeParams struct {
	authorizeResource string
	tokenResource     string
}

// exchangeForToken drives one /authorize -> /token round trip against ts and
// returns the decoded token response body.
func exchangeForToken(t *testing.T, ts *httptest.Server, p tokenExchangeParams) map[string]any {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	authorizeQuery := url.Values{
		"client_id":     {"eshu-mcp-e2e-client"},
		"redirect_uri":  {"https://console.example.test/callback"},
		"response_type": {"code"},
		"state":         {"state-value"},
	}
	if p.authorizeResource != "" {
		authorizeQuery.Set("resource", p.authorizeResource)
	}
	resp, err := client.Get(ts.URL + "/authorize?" + authorizeQuery.Encode()) //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatal("redirect location missing code")
	}

	tokenForm := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {code},
	}
	if p.tokenResource != "" {
		tokenForm.Set("resource", p.tokenResource)
	}
	tokenResp, err := http.PostForm(ts.URL+"/token", tokenForm) //nolint:noctx // test-only synchronous request
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()
	if tokenResp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d, want %d", tokenResp.StatusCode, http.StatusOK)
	}
	var body map[string]any
	if err := json.NewDecoder(tokenResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return body
}

func TestMockOIDCIDPTokenAccessTokenStaysOpaqueByDefault(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	body := exchangeForToken(t, ts, tokenExchangeParams{})

	if got, want := body["access_token"], "mock-access-token"; got != want {
		t.Fatalf("access_token = %v, want %v (byte-stable default for the #4971 suite)", got, want)
	}
}

func TestMockOIDCIDPTokenMintsJWTAccessTokenWhenResourceParamPresentOnTokenCall(t *testing.T) {
	t.Parallel()

	ts, srv := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	body := exchangeForToken(t, ts, tokenExchangeParams{tokenResource: "https://mcp.example.test"})

	assertJWTAccessToken(t, srv, body, "https://mcp.example.test")
}

func TestMockOIDCIDPTokenMintsJWTAccessTokenWhenResourceThreadedFromAuthorize(t *testing.T) {
	t.Parallel()

	ts, srv := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	// resource is carried on /authorize only, matching RFC 8707's
	// authorization-request placement and the scripted MCP client's flow
	// (authMcpE2EOauthClient.ts step 4): the /token call never repeats it.
	body := exchangeForToken(t, ts, tokenExchangeParams{authorizeResource: "https://mcp.example.test"})

	assertJWTAccessToken(t, srv, body, "https://mcp.example.test")
}

func TestMockOIDCIDPTokenMintsJWTAccessTokenWhenForcedViaConfig(t *testing.T) {
	t.Parallel()

	ts, srv := newTestServer(t, ServerConfig{
		Identity:            defaultIdentity(),
		AccessTokenJWT:      true,
		AccessTokenAudience: "https://mcp.example.test",
	})
	body := exchangeForToken(t, ts, tokenExchangeParams{})

	assertJWTAccessToken(t, srv, body, "https://mcp.example.test")
}

func TestMockOIDCIDPTokenRejectsJWTMintWithoutAudience(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity(), AccessTokenJWT: true})
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	authorizeQuery := url.Values{
		"client_id":     {"eshu-mcp-e2e-client"},
		"redirect_uri":  {"https://console.example.test/callback"},
		"response_type": {"code"},
	}
	resp, err := client.Get(ts.URL + "/authorize?" + authorizeQuery.Encode()) //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	code := location.Query().Get("code")

	tokenResp, err := http.PostForm(ts.URL+"/token", url.Values{ //nolint:noctx // test-only synchronous request
		"grant_type": {"authorization_code"},
		"code":       {code},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()
	if tokenResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("token status = %d, want %d (MOCK_OIDC_ACCESS_TOKEN_JWT=true with no resource/audience)", tokenResp.StatusCode, http.StatusBadRequest)
	}
}

func TestMockOIDCIDPTokenAccessTokenTTLConfigurable(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts, _ := newTestServer(t, ServerConfig{
		Identity:            defaultIdentity(),
		AccessTokenTTL:      time.Second,
		AccessTokenAudience: "https://mcp.example.test",
		AccessTokenJWT:      true,
		Now:                 func() time.Time { return fixedNow },
	})
	body := exchangeForToken(t, ts, tokenExchangeParams{})

	claims := parseJWTClaimsUnverified(t, body["access_token"].(string))
	wantExp := float64(fixedNow.Add(time.Second).Unix())
	if got := claims["exp"].(float64); got != wantExp {
		t.Fatalf("exp claim = %v, want %v (1s TTL from fixed clock)", got, wantExp)
	}
}

// assertJWTAccessToken verifies body's access_token is an RS256 JWT signed by
// srv's own key, carrying iss=srv.issuer, aud=wantAudience, sub/groups from
// the configured identity, and expires_in / token_type sibling fields
// consistent with a bearer access token.
func assertJWTAccessToken(t *testing.T, srv *Server, body map[string]any, wantAudience string) {
	t.Helper()
	if got, want := body["token_type"], "Bearer"; got != want {
		t.Fatalf("token_type = %v, want %v", got, want)
	}
	raw, ok := body["access_token"].(string)
	if !ok || raw == "" {
		t.Fatal("access_token missing or not a string")
	}

	parsed, err := jwt.Parse(raw, func(token *jwt.Token) (any, error) {
		return &srv.privateKey.PublicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !parsed.Valid {
		t.Fatalf("access_token did not verify against the mock IdP's own key: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("access_token claims are not a map: %T", parsed.Claims)
	}
	if got := claims["iss"]; got != srv.issuer {
		t.Fatalf("access_token iss = %v, want %v", got, srv.issuer)
	}
	if got := claims["aud"]; got != wantAudience {
		t.Fatalf("access_token aud = %v, want %v", got, wantAudience)
	}
	if got := claims["sub"]; got != srv.identity.Subject {
		t.Fatalf("access_token sub = %v, want %v", got, srv.identity.Subject)
	}
	groups, ok := claims[srv.groupClaim].([]any)
	if !ok || len(groups) != len(srv.identity.Groups) {
		t.Fatalf("access_token %s claim = %#v, want %#v", srv.groupClaim, claims[srv.groupClaim], srv.identity.Groups)
	}
}

// parseJWTClaimsUnverified decodes claims without signature verification,
// for tests that only need to inspect a claim value (e.g. exp under an
// injected clock) rather than re-prove the signing key.
func parseJWTClaimsUnverified(t *testing.T, raw string) jwt.MapClaims {
	t.Helper()
	parser := jwt.NewParser()
	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(raw, claims); err != nil {
		t.Fatalf("parse jwt claims: %v", err)
	}
	return claims
}
