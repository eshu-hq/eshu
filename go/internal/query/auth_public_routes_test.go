// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http/httptest"
	"testing"
)

// TestPublicHTTPRoute_GitHubLoginAndCallback proves GET
// /api/v0/auth/github/login and GET /api/v0/auth/github/callback bypass
// AuthMiddleware, mirroring the OIDC login-start/callback public routes just
// above them in publicHTTPRoute. An anonymous browser beginning (or
// completing) a GitHub SSO login carries no session, bearer token, or shared
// key — exactly like the OIDC and SAML login routes this function already
// covers — so gating these behind auth would make GitHub login
// unreachable for the very users it exists to authenticate (F-9 E2E harness,
// issue #5170, discovered live: a fresh GitHub provider configured via the
// admin UI still 401'd every /login click before this fix).
func TestPublicHTTPRoute_GitHubLoginAndCallback(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"github login GET is public", "GET", "/api/v0/auth/github/login", true},
		{"github callback GET is public", "GET", "/api/v0/auth/github/callback", true},
		{"github login POST is not public", "POST", "/api/v0/auth/github/login", false},
		{"github callback POST is not public", "POST", "/api/v0/auth/github/callback", false},
		{"unrelated github-prefixed path stays authenticated", "GET", "/api/v0/auth/github/admin", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if got := publicHTTPRoute(req); got != tc.want {
				t.Fatalf("publicHTTPRoute(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}
