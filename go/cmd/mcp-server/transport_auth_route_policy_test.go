// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// allScopeBearerResolver resolves any presented credential to the
// admin-equivalent bearer shape: AuthModeScoped, bound to one tenant and
// workspace, AllScopes set, no repository or scope ids. That is what an
// ESHU_SCOPED_TOKENS_FILE entry with "all_scopes": true and an OIDC provider's
// all-scopes grant set both produce.
type allScopeBearerResolver struct{}

func (allScopeBearerResolver) ResolveScopedToken(context.Context, string) (query.AuthContext, bool, error) {
	return query.AuthContext{
		Mode:        query.AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		AllScopes:   true,
	}, true, nil
}

// TestTransportAuthMiddlewareHonoursTheGovernanceRoutePolicy proves the route
// policy reaches the MCP transport middleware and changes what it admits.
//
// This is the trap #6450's residual item 1 had to avoid. cmd/mcp-server's only
// route into the auth middleware is buildTransportAuthMiddleware, and until
// this change the policy underneath it was a hardcoded fail-closed zero value
// nobody could see. Holding all-scope bearers to the same rule as console
// sessions without also threading the mode would have refused every all-scope
// token on every grant-bound MCP route in every deployment, a laptop included
// -- a real regression for the local CLI and MCP workflows, dressed up as a
// security fix.
//
// GET /api/v0/repositories is the route the sibling transport suites use, and
// it is grant-bound, which is the class the policy governs.
func TestTransportAuthMiddlewareHonoursTheGovernanceRoutePolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		mode       string
		wantStatus int
	}{
		{name: "unset local default admits", mode: "", wantStatus: http.StatusOK},
		{name: "local_no_policy admits", mode: "local_no_policy", wantStatus: http.StatusOK},
		{name: "hosted_single_tenant admits", mode: "hosted_single_tenant", wantStatus: http.StatusOK},
		{name: "hosted_multi_tenant refuses", mode: "hosted_multi_tenant", wantStatus: http.StatusForbidden},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			called := false
			inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})
			// The same composition wireAPI builds, with the same derivation of
			// the policy from the governance mode.
			transportAuth := buildTransportAuthMiddleware(
				"", allScopeBearerResolver{}, nil, true, nil, nil,
				query.ScopedRoutePolicyForGovernanceMode(query.GovernanceStatusConfig{Mode: tc.mode}),
			)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
			req.Header.Set("Authorization", "Bearer all-scope-token")
			rec := httptest.NewRecorder()
			transportAuth(inner).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d for mode %q; body = %s", rec.Code, tc.wantStatus, tc.mode, rec.Body.String())
			}
			// The status alone would still pass if a refusal happened after
			// the handler had already read.
			if want := tc.wantStatus == http.StatusOK; called != want {
				t.Fatalf("inner handler called = %t, want %t for mode %q", called, want, tc.mode)
			}
		})
	}
}

// TestTransportAuthMiddlewareResolvesNoBrowserSession pins the credential
// shapes the MCP transport can actually authenticate, because the operator
// documentation names a remedy per shape and one wrong entry sends an operator
// after a credential that cannot work.
//
// buildTransportAuthMiddleware passes no browser-session resolver, and the
// constructor it calls hands nil down to authMiddlewareWithAllowedReadAudit's
// sessionResolver parameter. So a console session cookie is never looked at on
// GET /sse or POST /mcp/message, no matter how narrow the session's grant is:
// the transport authenticates bearer credentials only. The mode here is
// local_no_policy, the most permissive one, so the refusal cannot be mistaken
// for the governance route policy doing its job -- there is simply no
// credential.
func TestTransportAuthMiddlewareResolvesNoBrowserSession(t *testing.T) {
	t.Parallel()

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	transportAuth := buildTransportAuthMiddleware(
		"", allScopeBearerResolver{}, nil, true, nil, nil,
		query.ScopedRoutePolicyForGovernanceMode(query.GovernanceStatusConfig{Mode: "local_no_policy"}),
	)

	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	req.AddCookie(&http.Cookie{Name: query.BrowserSessionCookieName, Value: "session-secret"})
	rec := httptest.NewRecorder()
	transportAuth(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d for a cookie-only request; body = %s",
			rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if called {
		t.Fatal("inner handler ran for a cookie-only request -- the MCP transport resolved a browser session it has no resolver for")
	}
}

// TestGovernanceModeEnvDerivesTheTransportRoutePolicy pins the other half of
// the seam: the value wireAPI hands buildTransportAuthMiddleware is derived
// from ESHU_GOVERNANCE_MODE by exactly these two calls, in this order, and a
// deployment that sets nothing gets the local default rather than a refusal.
//
// wireAPI itself needs a Postgres connection and a graph backend, so it cannot
// be driven here; this asserts the derivation, and the test above asserts the
// middleware honours what the derivation produces.
func TestGovernanceModeEnvDerivesTheTransportRoutePolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mode string
		want bool
	}{
		{name: "unset", want: true},
		{name: "local_no_policy", mode: "local_no_policy", want: true},
		{name: "hosted_single_tenant", mode: "hosted_single_tenant", want: true},
		{name: "hosted_multi_tenant", mode: "hosted_multi_tenant", want: false},
		{name: "an unrecognized mode is fail-closed", mode: "future_mode", want: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			getenv := func(key string) string {
				if key == "ESHU_GOVERNANCE_MODE" {
					return tc.mode
				}
				return ""
			}
			governanceStatus := query.GovernanceStatusConfigFromEnv(getenv, false)
			policy := query.ScopedRoutePolicyForGovernanceMode(governanceStatus)
			if got := policy.AllowTenantBoundAllScopes; got != tc.want {
				t.Fatalf("AllowTenantBoundAllScopes = %t, want %t for ESHU_GOVERNANCE_MODE=%q", got, tc.want, tc.mode)
			}
		})
	}
}
