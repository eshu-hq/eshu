// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// This file unit-tests the headerless dev-open gate at the middleware
// boundary through the two production *AndEnforcement constructors. It pins
// the corrected contract: dev-mode-open reads apply only when the
// wiring-computed authEnforcementConfigured predicate is false, and a
// configured auth source (predicate true) denies a headerless request even
// when the shared key is empty. The predicate itself is computed in wiring
// (excluding the always-wired identity and browser-session resolvers) and is
// exercised end-to-end against the real resolver composition in
// cmd/api/auth_enforcement_wiring_test.go and
// cmd/mcp-server/auth_enforcement_wiring_test.go.
//
// The earlier version of this file gated on resolver != nil and used
// hand-built fakes; that missed the production reality that the identity and
// session resolvers are always non-nil, which is why the gate is now an
// explicit wiring-time boolean rather than a resolver-presence check.

// TestAuthMiddlewareEnforcementConstructorDeniesHeaderlessWhenEnforced covers
// the mcp-server production constructor: enforcement true (a credential source
// is configured) denies a headerless request with a 401 and the standard
// read-authorization-denied audit event, even though the shared token is empty.
func TestAuthMiddlewareEnforcementConstructorDeniesHeaderlessWhenEnforced(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{}
	audit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		"", resolver, mockHandler(), audit, true,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if resolver.called {
		t.Fatal("scoped token resolver was called for a headerless request; it must be denied before resolution")
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d", got, want)
	}
	if got, want := audit.events[0].Decision, governanceaudit.DecisionDenied; got != want {
		t.Fatalf("audit decision = %q, want %q", got, want)
	}
	if got, want := audit.events[0].ReasonCode, "authentication_required"; got != want {
		t.Fatalf("audit reason = %q, want %q", got, want)
	}
}

// TestAuthMiddlewareEnforcementConstructorOpenWhenNotEnforced is the
// non-regression guard that distinguishes this fix from the broken first
// attempt: with a scoped-token resolver configured but enforcement false (the
// demo posture — the resolver is the always-wired identity resolver, no
// explicit credential knob set), a headerless request is still served open.
func TestAuthMiddlewareEnforcementConstructorOpenWhenNotEnforced(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		"", resolver, mockHandler(), nil, false,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d in open posture; body = %s", got, want, rec.Body.String())
	}
}

// TestAuthMiddlewareRoutePolicyEnforcementConstructorDeniesHeaderlessWhenEnforced
// covers the cmd/api production constructor: enforcement true denies a
// headerless, cookieless request with a 401 and an audit event.
func TestAuthMiddlewareRoutePolicyEnforcementConstructorDeniesHeaderlessWhenEnforced(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{}
	session := &fakeBrowserSessionResolver{}
	audit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement(
		"", resolver, session, mockHandler(), audit, BrowserSessionRoutePolicy{}, true,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if session.called {
		t.Fatal("browser session resolver was called for a cookieless request")
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d", got, want)
	}
}

// TestAuthMiddlewareRoutePolicyEnforcementConstructorOpenWhenNotEnforced pins
// the demo posture through the cmd/api constructor: enforcement false with a
// session resolver present (always the case in production) keeps a headerless,
// cookieless request open. This is the exact shape that would have 401'd under
// the broken resolver != nil gate.
func TestAuthMiddlewareRoutePolicyEnforcementConstructorOpenWhenNotEnforced(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{}
	session := &fakeBrowserSessionResolver{}
	handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement(
		"", resolver, session, mockHandler(), nil, BrowserSessionRoutePolicy{}, false,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d in open posture; body = %s", got, want, rec.Body.String())
	}
}

// TestAuthMiddlewareEnforcementConstructorSharedKeyHeaderlessDenied pins that
// a shared-key deployment (enforcement true, derived by wiring from apiKey !=
// "") still denies headerless and admits the correct bearer token.
func TestAuthMiddlewareEnforcementConstructorSharedKeyHeaderlessDenied(t *testing.T) {
	t.Parallel()

	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		"shared-secret", nil, mockHandler(), nil, true,
	)

	headerless := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	headerlessRec := httptest.NewRecorder()
	handler.ServeHTTP(headerlessRec, headerless)
	if got, want := headerlessRec.Code, http.StatusUnauthorized; got != want {
		t.Fatalf("headerless status = %d, want %d", got, want)
	}

	authed := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	authed.Header.Set("Authorization", "Bearer shared-secret")
	authedRec := httptest.NewRecorder()
	handler.ServeHTTP(authedRec, authed)
	if got, want := authedRec.Code, http.StatusOK; got != want {
		t.Fatalf("bearer status = %d, want %d", got, want)
	}
}

// TestAuthMiddlewareEnforcementConstructorPublicRouteAlwaysOpen pins that a
// public route is served headerless under enforcement true, without consulting
// the resolver.
func TestAuthMiddlewareEnforcementConstructorPublicRouteAlwaysOpen(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
		"", resolver, mockHandler(), nil, true,
	)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if resolver.called {
		t.Fatal("resolver called for public route")
	}
}
