// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

func publicHTTPRoute(r *http.Request) bool {
	if publicHTTPPaths[r.URL.Path] {
		return true
	}
	if r.Method == http.MethodGet &&
		(r.URL.Path == "/api/v0/auth/oidc/login" ||
			r.URL.Path == "/api/v0/auth/oidc/callback" ||
			// GitHub login-start/callback (issue #5166, F-5) mirror the OIDC
			// routes immediately above: an anonymous browser beginning or
			// completing a GitHub SSO login carries no session, bearer token,
			// or shared key, so these must bypass AuthMiddleware exactly like
			// OIDC's equivalents. Missing this made every GitHub login 401
			// before it could even redirect to the provider (found live by the
			// F-9 MCP-identity E2E harness, issue #5170).
			r.URL.Path == "/api/v0/auth/github/login" ||
			r.URL.Path == "/api/v0/auth/github/callback" ||
			r.URL.Path == "/api/v0/auth/providers" ||
			// /api/v0/auth/sign-in-policy is public GET-only (issue #4968):
			// the login page must know require_sso BEFORE the user is
			// authenticated, to decide whether to hide the local password
			// form. It exposes only require_sso — see
			// SignInPolicyReadHandler.handlePublicGet. The admin PATCH route
			// at the same base path (/api/v0/auth/admin/sign-in-policy) is a
			// DIFFERENT path and stays authenticated.
			r.URL.Path == "/api/v0/auth/sign-in-policy") {
		return true
	}
	return publicSAMLHTTPRoute(r)
}

func publicSAMLHTTPRoute(r *http.Request) bool {
	const prefix = "/api/v0/auth/saml/providers/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	providerID, suffix, found := strings.Cut(rest, "/")
	if !found || providerID == "" {
		return false
	}
	switch suffix {
	case "metadata", "login":
		return r.Method == http.MethodGet
	case "acs":
		return r.Method == http.MethodPost
	default:
		return false
	}
}
