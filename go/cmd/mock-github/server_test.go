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

	"github.com/eshu-hq/eshu/go/internal/githublogin"
)

func defaultTestIdentity() IdentityConfig {
	return IdentityConfig{
		Login:  "e2e-github-user",
		UserID: 1001,
		Email:  "e2e-github-user@example.test",
		Org:    "eshu-e2e-org",
		Teams:  []TeamHandle{{Org: "eshu-e2e-org", Slug: "platform-team"}},
	}
}

func newTestServer(t *testing.T, cfg ServerConfig) *httptest.Server {
	t.Helper()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts
}

// TestMockGitHubFullLoginFlowViaRealConnector drives the exact
// authorize -> exchange -> FetchIdentity path
// go/internal/githublogin/connector.go's githubConnector uses in
// production, through the real production client rather than a hand-rolled
// stand-in, so a green result proves this mock's response shapes actually
// satisfy that connector (mirroring mock-oidc-idp's
// TestMockOIDCIDPTokenExchangeVerifiesAgainstOwnJWKS pattern).
func TestMockGitHubFullLoginFlowViaRealConnector(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, ServerConfig{Identity: defaultTestIdentity()})
	ctx := context.Background()

	connector, err := githublogin.NewGitHubConnector(ctx, githublogin.ProviderConfig{
		BaseURL:      ts.URL,
		APIBaseURL:   ts.URL,
		ClientID:     "eshu-mcp-e2e-github-client",
		ClientSecret: "unused-mock-secret",
		RedirectURL:  "https://console.example.test/api/v0/auth/github/callback",
		Scopes:       []string{"read:org", "user:email"},
	})
	if err != nil {
		t.Fatalf("NewGitHubConnector: %v", err)
	}

	authorizeURL := connector.AuthCodeURL("state-value")
	code := followAuthorizeRedirectForCode(t, authorizeURL)

	tokens, err := connector.Exchange(ctx, code)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Fatal("Exchange returned empty access token")
	}

	identity, err := connector.FetchIdentity(ctx, tokens.AccessToken, []string{"eshu-e2e-org"})
	if err != nil {
		t.Fatalf("FetchIdentity: %v", err)
	}
	if identity.Subject != "1001" {
		t.Fatalf("Subject = %q, want %q", identity.Subject, "1001")
	}
	if identity.Login != "e2e-github-user" {
		t.Fatalf("Login = %q, want %q", identity.Login, "e2e-github-user")
	}
	if identity.Email != "e2e-github-user@example.test" {
		t.Fatalf("Email = %q, want %q", identity.Email, "e2e-github-user@example.test")
	}
	if len(identity.ActiveOrgs) != 1 || identity.ActiveOrgs[0] != "eshu-e2e-org" {
		t.Fatalf("ActiveOrgs = %#v, want [eshu-e2e-org]", identity.ActiveOrgs)
	}
	if len(identity.TeamHandles) != 1 || identity.TeamHandles[0] != "eshu-e2e-org/platform-team" {
		t.Fatalf("TeamHandles = %#v, want [eshu-e2e-org/platform-team]", identity.TeamHandles)
	}
}

// followAuthorizeRedirectForCode performs the browser's role in the OAuth
// flow: GET the authorize URL and extract the "code" query parameter from
// the 302 redirect, without following it.
func followAuthorizeRedirectForCode(t *testing.T, authorizeURL string) string {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
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
	code := location.Query().Get("code")
	if code == "" {
		t.Fatal("redirect location missing code")
	}
	return code
}

func TestMockGitHubAuthorizeRequiresRedirectURI(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, ServerConfig{Identity: defaultTestIdentity()})
	resp, err := http.Get(ts.URL + "/login/oauth/authorize?client_id=x") //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestMockGitHubAuthorizePreservesState(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, ServerConfig{Identity: defaultTestIdentity()})
	authorizeURL := ts.URL + "/login/oauth/authorize?" + url.Values{
		"client_id":    {"x"},
		"redirect_uri": {"https://console.example.test/callback"},
		"state":        {"state-value"},
	}.Encode()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(authorizeURL) //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got := location.Query().Get("state"); got != "state-value" {
		t.Fatalf("redirect state = %q, want %q", got, "state-value")
	}
}

func TestMockGitHubTokenExchangeReturnsErrorFieldForUnknownCode(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, ServerConfig{Identity: defaultTestIdentity()})
	// GitHub's real token endpoint returns HTTP 200 for both success and
	// failure; the error field, not the status code, signals failure
	// (githubConnector.Exchange's githubTokenResponse doc comment). This
	// mock must match that shape exactly or the connector's error path is
	// untested.
	resp, err := http.PostForm(ts.URL+"/login/oauth/access_token", url.Values{ //nolint:noctx // test-only synchronous request
		"code": {"does-not-exist"},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (GitHub always 200s /login/oauth/access_token)", resp.StatusCode, http.StatusOK)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] == "" || body["error"] == nil {
		t.Fatalf("body = %#v, want a non-empty error field", body)
	}
	if body["access_token"] != nil {
		t.Fatalf("body = %#v, want no access_token alongside an error", body)
	}
}

func TestMockGitHubTokenCodeIsOneTimeUse(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, ServerConfig{Identity: defaultTestIdentity()})
	authorizeURL := ts.URL + "/login/oauth/authorize?" + url.Values{
		"client_id":    {"x"},
		"redirect_uri": {"https://console.example.test/callback"},
	}.Encode()
	code := followAuthorizeRedirectForCode(t, authorizeURL)

	first, err := http.PostForm(ts.URL+"/login/oauth/access_token", url.Values{"code": {code}}) //nolint:noctx // test-only synchronous request
	if err != nil {
		t.Fatalf("first token request: %v", err)
	}
	_ = first.Body.Close()

	second, err := http.PostForm(ts.URL+"/login/oauth/access_token", url.Values{"code": {code}}) //nolint:noctx // test-only synchronous request
	if err != nil {
		t.Fatalf("second token request: %v", err)
	}
	defer func() { _ = second.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(second.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] == "" || body["error"] == nil {
		t.Fatalf("reused code body = %#v, want a non-empty error field", body)
	}
}

func TestMockGitHubUserEndpointsRequireBearerToken(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, ServerConfig{Identity: defaultTestIdentity()})
	for _, path := range []string{"/user", "/user/emails", "/user/memberships/orgs", "/user/teams"} {
		resp, err := http.Get(ts.URL + path) //nolint:noctx // test-only synchronous fetch
		if err != nil {
			t.Fatalf("%s: request: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want %d", path, resp.StatusCode, http.StatusUnauthorized)
		}
	}
}

func TestMockGitHubRootProbeSucceedsUnauthenticated(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, ServerConfig{Identity: defaultTestIdentity()})
	resp, err := http.Get(ts.URL + "/") //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("root request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestMockGitHubOrgMembershipPaginationStopsAfterOnePage(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t, ServerConfig{Identity: defaultTestIdentity()})
	token := mintTestAccessToken(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/user/memberships/orgs?state=active&per_page=100&page=2", nil) //nolint:noctx
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var memberships []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&memberships); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(memberships) != 0 {
		t.Fatalf("page 2 memberships = %#v, want empty (single-page fixture)", memberships)
	}
}

// mintTestAccessToken drives one authorize+exchange round trip and returns
// the resulting access token, for tests that only need an authenticated
// call and do not exercise the OAuth dance itself.
func mintTestAccessToken(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	authorizeURL := ts.URL + "/login/oauth/authorize?" + url.Values{
		"client_id":    {"x"},
		"redirect_uri": {"https://console.example.test/callback"},
	}.Encode()
	code := followAuthorizeRedirectForCode(t, authorizeURL)
	resp, err := http.PostForm(ts.URL+"/login/oauth/access_token", url.Values{"code": {code}}) //nolint:noctx // test-only synchronous request
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	token, _ := body["access_token"].(string)
	if token == "" {
		t.Fatal("token response missing access_token")
	}
	return token
}
