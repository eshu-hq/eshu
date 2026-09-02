// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	// splitTestSharedToken is the shared key the split table's middleware is
	// built with, so caller shape (e) can present it and every other shape
	// can prove it is NOT what admitted them.
	splitTestSharedToken = "shared-token"
	// splitTestHandlerPayload is what the stub next handler writes. A denial
	// must never contain it: a 403 whose body carries this string means the
	// handler ran before the middleware refused, which is the #6450 leak in
	// its most direct form.
	splitTestHandlerPayload = `{"secret_cross_tenant_data":true}`
)

// allScopesSplitShape is one caller shape driven over every ledger route. The
// shapes are the populations #6450 has to keep apart: a grant-bearing scoped
// bearer, an all-scope console session with and without the permissive
// policy, a restricted console session, the shared key, and the malformed
// tenantless all-scope session.
type allScopesSplitShape struct {
	name   string
	policy BrowserSessionRoutePolicy
	// session, when set, is resolved by the browser-session resolver from a
	// request cookie. bearer, when set, is resolved by the scoped-token
	// resolver from an Authorization header. sharedKey presents the shared
	// token instead. Exactly one of the three is used per shape.
	session   *AuthContext
	bearer    *AuthContext
	sharedKey bool
	// admits reports whether this shape is expected to reach the handler on a
	// route of the given class.
	admits func(scopedRouteClass) bool
}

func splitAdmitsEveryClass(scopedRouteClass) bool { return true }

func splitAdmitsByClass(c scopedRouteClass) bool {
	return c.admitsAllScopesSessionWithoutPolicy()
}

func allScopesSplitShapes() []allScopesSplitShape {
	tenantBoundAllScopes := &AuthContext{
		Mode:        AuthModeBrowserSession,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
		AllScopes:   true,
	}
	return []allScopesSplitShape{
		{
			name:   "a_grant_bearing_scoped_bearer",
			policy: BrowserSessionRoutePolicy{},
			bearer: &AuthContext{
				Mode:                 AuthModeScoped,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: []string{"repo-a"},
			},
			admits: splitAdmitsEveryClass,
		},
		{
			name:    "b_all_scope_session_fail_closed_policy",
			policy:  BrowserSessionRoutePolicy{},
			session: tenantBoundAllScopes,
			admits:  splitAdmitsByClass,
		},
		{
			name:    "c_all_scope_session_permissive_policy",
			policy:  BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			session: tenantBoundAllScopes,
			admits:  splitAdmitsEveryClass,
		},
		{
			name:   "d_restricted_browser_session",
			policy: BrowserSessionRoutePolicy{},
			session: &AuthContext{
				Mode:                 AuthModeBrowserSession,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: []string{"repo-a"},
			},
			admits: splitAdmitsEveryClass,
		},
		{
			name:      "e_shared_key",
			policy:    BrowserSessionRoutePolicy{},
			sharedKey: true,
			admits:    splitAdmitsEveryClass,
		},
		{
			name:   "f_tenantless_all_scope_session_permissive_policy",
			policy: BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			session: &AuthContext{
				Mode:      AuthModeBrowserSession,
				AllScopes: true,
			},
			admits: splitAdmitsByClass,
		},
	}
}

// runAllScopesSplitCase drives one "METHOD /path" surface name through the
// production constructor cmd/api wires (not a direct browserSessionRouteAllowed
// call) under one caller shape, and asserts the shape's expected admission for
// the route's class.
func runAllScopesSplitCase(t *testing.T, shape allScopesSplitShape, surfaceName string, class scopedRouteClass) {
	t.Helper()

	method, path, ok := strings.Cut(surfaceName, " ")
	if !ok {
		t.Fatalf("surface name %q has no METHOD/path separator", surfaceName)
	}

	var (
		sessionResolver BrowserSessionResolver
		tokenResolver   ScopedTokenResolver
	)
	if shape.session != nil {
		sessionResolver = &fakeBrowserSessionResolver{context: *shape.session, ok: true}
	}
	if shape.bearer != nil {
		tokenResolver = &fakeScopedTokenResolver{context: *shape.bearer, ok: true}
	}

	called := false
	handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement(
		splitTestSharedToken,
		tokenResolver,
		sessionResolver,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			_, _ = w.Write([]byte(splitTestHandlerPayload))
		}),
		nil,
		shape.policy,
		true,
	)

	req := httptest.NewRequest(method, path, nil)
	switch {
	case shape.sharedKey:
		req.Header.Set("Authorization", "Bearer "+splitTestSharedToken)
	case shape.bearer != nil:
		req.Header.Set("Authorization", "Bearer scoped-token")
	default:
		req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
		if browserSessionRequiresCSRF(method) {
			req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	wantAdmitted := shape.admits(class)
	wantStatus := http.StatusOK
	if !wantAdmitted {
		wantStatus = http.StatusForbidden
	}
	className := scopedRouteClassNames[class]
	if called != wantAdmitted {
		t.Fatalf(
			"%s (%s): handler called = %t, want %t; status = %d, body = %s",
			surfaceName, className, called, wantAdmitted, rec.Code, rec.Body.String(),
		)
	}
	if rec.Code != wantStatus {
		t.Fatalf(
			"%s (%s): status = %d, want %d; body = %s",
			surfaceName, className, rec.Code, wantStatus, rec.Body.String(),
		)
	}
	if !wantAdmitted && strings.Contains(rec.Body.String(), splitTestHandlerPayload) {
		t.Fatalf("%s (%s): denied response carries the handler payload: %s", surfaceName, className, rec.Body.String())
	}
}
