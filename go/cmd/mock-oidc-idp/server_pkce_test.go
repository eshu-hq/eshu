// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// --- RFC 7636 PKCE passthrough (F-9, issue #5170) ---
//
// `eshu mcp setup`'s SSO snippets advertise Code+PKCE (design §9 decision 3);
// the scripted MCP OAuth client (authMcpE2EOauthClient.ts) exercises the same
// real S256 challenge/verifier round trip a real MCP client performs, rather
// than skipping PKCE against the mock. Byte-stable for every existing caller
// that never sends code_challenge: PKCE is opt-in per authorization request.

// s256Challenge computes the RFC 7636 S256 code_challenge for verifier.
func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizeWithPKCE drives GET /authorize carrying the given PKCE params (either
// may be empty to omit it) and returns the issued code.
func authorizeWithPKCE(t *testing.T, ts *httptest.Server, codeChallenge, codeChallengeMethod string) string {
	t.Helper()
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	q := url.Values{
		"client_id":     {"eshu-mcp-e2e-client"},
		"redirect_uri":  {"https://console.example.test/callback"},
		"response_type": {"code"},
	}
	if codeChallenge != "" {
		q.Set("code_challenge", codeChallenge)
	}
	if codeChallengeMethod != "" {
		q.Set("code_challenge_method", codeChallengeMethod)
	}
	resp, err := client.Get(ts.URL + "/authorize?" + q.Encode()) //nolint:noctx // test-only synchronous fetch
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
	return code
}

func tokenRequestWithVerifier(ts *httptest.Server, code, codeVerifier string) (*http.Response, error) {
	form := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {code},
	}
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	return http.PostForm(ts.URL+"/token", form) //nolint:noctx // test-only synchronous request
}

func TestMockOIDCIDPDiscoveryAdvertisesS256PKCE(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	resp, err := http.Get(ts.URL + "/.well-known/openid-configuration") //nolint:noctx // test-only synchronous fetch
	if err != nil {
		t.Fatalf("discovery request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var doc struct {
		CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode discovery document: %v", err)
	}
	if len(doc.CodeChallengeMethodsSupported) != 1 || doc.CodeChallengeMethodsSupported[0] != "S256" {
		t.Fatalf("code_challenge_methods_supported = %v, want [S256]", doc.CodeChallengeMethodsSupported)
	}
}

func TestMockOIDCIDPTokenPKCERoundTripSucceedsWithMatchingVerifier(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	verifier := "e2e-test-code-verifier-with-enough-entropy-1234567890"
	code := authorizeWithPKCE(t, ts, s256Challenge(verifier), "S256")

	resp, err := tokenRequestWithVerifier(ts, code, verifier)
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d, want %d (matching PKCE verifier)", resp.StatusCode, http.StatusOK)
	}
}

func TestMockOIDCIDPTokenPKCERejectsWrongVerifier(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	code := authorizeWithPKCE(t, ts, s256Challenge("correct-verifier-1234567890123456"), "S256")

	resp, err := tokenRequestWithVerifier(ts, code, "wrong-verifier-abcdefghijklmnopqrst")
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("token status = %d, want %d (mismatched PKCE verifier)", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestMockOIDCIDPTokenPKCERejectsMissingVerifierWhenChallengeWasSet(t *testing.T) {
	t.Parallel()

	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	code := authorizeWithPKCE(t, ts, s256Challenge("some-verifier-value-1234567890123"), "S256")

	resp, err := tokenRequestWithVerifier(ts, code, "")
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("token status = %d, want %d (code_verifier omitted after a PKCE authorize)", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestMockOIDCIDPTokenNonPKCEFlowStaysUnaffected(t *testing.T) {
	t.Parallel()

	// No code_challenge on /authorize at all: byte-stable for #4971's suite
	// and every other non-PKCE caller. code_verifier on /token (if any) is
	// simply ignored since there is nothing to check it against.
	ts, _ := newTestServer(t, ServerConfig{Identity: defaultIdentity()})
	code := authorizeWithPKCE(t, ts, "", "")

	resp, err := tokenRequestWithVerifier(ts, code, "")
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d, want %d (non-PKCE flow)", resp.StatusCode, http.StatusOK)
	}
}
