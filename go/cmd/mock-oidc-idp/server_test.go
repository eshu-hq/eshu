// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// newTestServer starts an httptest.Server bound to an ephemeral port, builds
// a Server whose issuer is that server's own URL (discovery, the "iss" claim,
// and the JWKS endpoint must all agree on one issuer), and wires the mux in
// before starting. Callers get back both the running HTTP server and the
// Server it wraps so tests can also assert on unexported fields (kid,
// issuer) in the same package.
func newTestServer(t *testing.T, cfg ServerConfig) (*httptest.Server, *Server) {
	t.Helper()
	ts := httptest.NewUnstartedServer(http.NotFoundHandler())
	cfg.Issuer = "http://" + ts.Listener.Addr().String()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts.Config.Handler = srv.Mux()
	ts.Start()
	t.Cleanup(ts.Close)
	return ts, srv
}

func defaultIdentity() IdentityConfig {
	return IdentityConfig{
		Subject: "member-user-1",
		Email:   "member.user@example.test",
		Groups:  []string{"member"},
	}
}

func TestMockOIDCIDPDiscoveryDocumentMatchesIssuer(t *testing.T) {
	t.Parallel()

	ts, srv := newTestServer(t, ServerConfig{Identity: defaultIdentity()})

	resp, err := http.Get(ts.URL + "/.well-known/openid-configuration") //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("discovery request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discovery status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode discovery document: %v", err)
	}
	if got, want := doc["issuer"], srv.issuer; got != want {
		t.Fatalf("issuer = %v, want %v", got, want)
	}
	if got, want := doc["authorization_endpoint"], srv.issuer+"/authorize"; got != want {
		t.Fatalf("authorization_endpoint = %v, want %v", got, want)
	}
	if got, want := doc["token_endpoint"], srv.issuer+"/token"; got != want {
		t.Fatalf("token_endpoint = %v, want %v", got, want)
	}
	if got, want := doc["jwks_uri"], srv.issuer+"/jwks"; got != want {
		t.Fatalf("jwks_uri = %v, want %v", got, want)
	}
}

func TestMockOIDCIDPAuthorizeRedirectsWithCodeAndState(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authorizeURL := ts.URL + "/authorize?" + url.Values{
		"client_id":     {"eshu-console-test"},
		"redirect_uri":  {"https://console.example.test/api/v0/auth/oidc/callback"},
		"response_type": {"code"},
		"state":         {"state-value"},
		"nonce":         {"nonce-value"},
	}.Encode()

	resp, err := client.Get(authorizeURL) //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got, want := location.Query().Get("state"), "state-value"; got != want {
		t.Fatalf("redirect state = %q, want %q", got, want)
	}
	if location.Query().Get("code") == "" {
		t.Fatal("redirect location missing code")
	}
}

func TestMockOIDCIDPAuthorizeRequiresRedirectURI(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})

	resp, err := http.Get(ts.URL + "/authorize?client_id=eshu-console-test") //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorize status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestMockOIDCIDPTokenExchangeVerifiesAgainstOwnJWKS drives the same
// discovery -> authorize -> token exchange -> JWKS verification path
// oidclogin.NewOIDCConnector uses in production (go/internal/oidclogin/connector.go),
// through the real coreos/go-oidc and golang.org/x/oauth2 client libraries
// rather than a hand-rolled stand-in, so a green result proves the minted ID
// token actually verifies against this IdP's own JWKS and carries the
// expected group claim.
func TestMockOIDCIDPTokenExchangeVerifiesAgainstOwnJWKS(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, ts.URL)
	if err != nil {
		t.Fatalf("discover provider: %v", err)
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     "eshu-console-test",
		ClientSecret: "unused-mock-secret",
		Endpoint:     provider.Endpoint(),
		RedirectURL:  "https://console.example.test/api/v0/auth/oidc/callback",
	}

	authorizeURL := ts.URL + "/authorize?" + url.Values{
		"client_id":     {oauth2Cfg.ClientID},
		"redirect_uri":  {oauth2Cfg.RedirectURL},
		"response_type": {"code"},
		"state":         {"state-value"},
		"nonce":         {"nonce-value"},
	}.Encode()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(authorizeURL) //nolint:noctx // test-only synchronous fetch
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

	token, err := oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		t.Fatalf("exchange code: %v", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		t.Fatal("token response missing id_token")
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oauth2Cfg.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verify id token against jwks: %v", err)
	}
	if idToken.Nonce != "nonce-value" {
		t.Fatalf("id token nonce = %q, want %q", idToken.Nonce, "nonce-value")
	}
	if idToken.Subject != "member-user-1" {
		t.Fatalf("id token subject = %q, want %q", idToken.Subject, "member-user-1")
	}

	var claims struct {
		Email  string   `json:"email"`
		Groups []string `json:"groups"`
	}
	if err := idToken.Claims(&claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims.Email != "member.user@example.test" {
		t.Fatalf("email claim = %q, want %q", claims.Email, "member.user@example.test")
	}
	if len(claims.Groups) != 1 || claims.Groups[0] != "member" {
		t.Fatalf("groups claim = %#v, want [member]", claims.Groups)
	}
}

func TestMockOIDCIDPUsesConfiguredGroupClaim(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{
		Identity:   IdentityConfig{Subject: "member-user-1", Email: "member.user@example.test", Groups: []string{"member"}},
		GroupClaim: "roles",
	})
	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, ts.URL)
	if err != nil {
		t.Fatalf("discover provider: %v", err)
	}
	oauth2Cfg := oauth2.Config{
		ClientID:    "eshu-console-test",
		Endpoint:    provider.Endpoint(),
		RedirectURL: "https://console.example.test/api/v0/auth/oidc/callback",
	}

	code := authorizeAndExtractCode(t, oauth2Cfg)
	token, err := oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		t.Fatalf("exchange code: %v", err)
	}
	rawIDToken, _ := token.Extra("id_token").(string)
	verifier := provider.Verifier(&oidc.Config{ClientID: oauth2Cfg.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verify id token: %v", err)
	}

	var claims struct {
		Roles []string `json:"roles"`
	}
	if err := idToken.Claims(&claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "member" {
		t.Fatalf("roles claim = %#v, want [member]", claims.Roles)
	}
}

func authorizeAndExtractCode(t *testing.T, cfg oauth2.Config) string {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	authorizeURL := cfg.Endpoint.AuthURL + "?" + url.Values{
		"client_id":     {cfg.ClientID},
		"redirect_uri":  {cfg.RedirectURL},
		"response_type": {"code"},
		"state":         {"state-value"},
	}.Encode()
	resp, err := client.Get(authorizeURL) //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatal("redirect location missing code")
	}
	return code
}

func TestMockOIDCIDPTokenRejectsUnknownCode(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})

	resp, err := http.PostForm(ts.URL+"/token", url.Values{ //nolint:noctx // test-only synchronous request
		"grant_type": {"authorization_code"},
		"code":       {"does-not-exist"},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("token status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestMockOIDCIDPTokenRejectsRedirectURIMismatch(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authorizeURL := ts.URL + "/authorize?" + url.Values{
		"client_id":    {"eshu-console-test"},
		"redirect_uri": {"https://console.example.test/api/v0/auth/oidc/callback"},
		"state":        {"state-value"},
	}.Encode()
	resp, err := client.Get(authorizeURL) //nolint:noctx // test-only synchronous fetch
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
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {"https://attacker.example.test/callback"},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()
	if tokenResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("token status = %d, want %d", tokenResp.StatusCode, http.StatusBadRequest)
	}
}
