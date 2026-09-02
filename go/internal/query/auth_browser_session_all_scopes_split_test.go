// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger drives every
// scoped-route allowlist entry through the production middleware constructor
// under all six caller shapes #6450 has to keep apart. The point is coverage
// of the whole ledger, not of a hand-picked sample: the defect was invisible
// precisely because the routes that had to keep working (the /api/v0/auth/
// admin console) and the routes that had to stop working (everything
// grant-filtered) sat in the same allowlist with nothing distinguishing them.
//
// Only shapes (b) and (f) -- an all-scope session under the fail-closed
// policy, and a malformed tenantless all-scope session even under the
// permissive one -- vary by class. The grant-bearing bearer, the restricted
// session, the shared key, and the all-scope session under the permissive
// policy reach every entry, which is what keeps this a split rather than a
// blanket refusal.
func TestAuthMiddlewareAllScopesBrowserSessionSplitAcrossLedger(t *testing.T) {
	t.Parallel()

	for _, shape := range allScopesSplitShapes() {
		shape := shape
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			for surfaceName, class := range scopedTokenAdvertisedRoutes {
				surfaceName, class := surfaceName, class
				t.Run(surfaceName, func(t *testing.T) {
					t.Parallel()
					runAllScopesSplitCase(t, shape, surfaceName, class)
				})
			}
		})
	}
}

// TestAuthMiddlewareAllScopesBrowserSessionSplitOnUnledgeredTransportRoutes
// covers the two allowlisted routes with no ledger entry and therefore no
// class: the MCP transport paths. They take the fail-closed default, so an
// all-scope session reaches them only under the permissive policy. Without
// this, the split table would silently skip the only allowlisted routes whose
// class is implicit rather than written down.
func TestAuthMiddlewareAllScopesBrowserSessionSplitOnUnledgeredTransportRoutes(t *testing.T) {
	t.Parallel()

	shapes := map[string]allScopesSplitShape{}
	for _, shape := range allScopesSplitShapes() {
		shapes[shape.name] = shape
	}

	for _, shapeName := range []string{
		"b_all_scope_session_fail_closed_policy",
		"c_all_scope_session_permissive_policy",
	} {
		shape, ok := shapes[shapeName]
		if !ok {
			t.Fatalf("caller shape %q is gone from allScopesSplitShapes", shapeName)
		}
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			for _, surfaceName := range []string{"GET /sse", "POST /mcp/message"} {
				surfaceName := surfaceName
				t.Run(surfaceName, func(t *testing.T) {
					t.Parallel()
					// scopedRouteGrantBound is the zero value an unledgered
					// route effectively carries, which is exactly the
					// fail-closed behaviour being asserted.
					runAllScopesSplitCase(t, shape, surfaceName, scopedRouteGrantBound)
				})
			}
		})
	}
}

// TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy
// is the named regression test for issue #6450 in its reported shape. Before
// the split, browserSessionRouteDenialReason's predecessor returned true as soon as
// scopedHTTPRouteSupportsTenantFilter(r) did, so an all-scope console session
// reached GET /api/v0/repositories in a hosted_multi_tenant deployment that
// had deliberately left BrowserSessionRoutePolicy at its fail-closed zero
// value. RepositoryHandler's grant predicate cannot save it: an all-scope
// caller's RepositoryAccessFilterFromContext is not Scoped(), and no
// data-plane table carries a tenant column, so the read crossed tenants.
//
// The first three cases are the whole fix: the all-scope session is refused
// under the fail-closed policy, the same session is admitted when the
// operator explicitly opts in, and a restricted session -- whose grant the
// handler can actually bind -- is unaffected.
//
// The last two cases pin the governance-audit reason code apart from the two
// refusals that predate this change. An operator reading
// governance_audit_events has to be able to tell "this route has no scoped
// authorization at all" from "this route has it, and a restricted session
// still enters it, but an all-scope caller's grant is inert here", because
// the remedies differ: the first is a route to wire up, the second is a
// policy or a credential to narrow. Emitting scoped_route_not_enabled for
// both would make the new refusal undiagnosable at 3 AM.
func TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy(t *testing.T) {
	t.Parallel()

	allScopes := AuthContext{
		Mode:               AuthModeBrowserSession,
		TenantID:           "tenant-a",
		WorkspaceID:        "workspace-a",
		SubjectClass:       "local_user",
		SubjectIDHash:      "sha256:abcdef12",
		PolicyRevisionHash: "sha256:01234567",
		AllScopes:          true,
	}
	restricted := AuthContext{
		Mode:                 AuthModeBrowserSession,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "local_user",
		SubjectIDHash:        "sha256:abcdef12",
		PolicyRevisionHash:   "sha256:01234567",
		AllowedRepositoryIDs: []string{"repo-a"},
	}

	for _, tc := range []struct {
		name string
		// method and path default to GET /api/v0/repositories, the reported
		// grant-bound route, unless a case overrides them.
		method         string
		path           string
		session        AuthContext
		policy         BrowserSessionRoutePolicy
		wantStatus     int
		wantCalled     bool
		wantReasonCode string
	}{
		{
			name:           "all scopes under fail-closed policy is refused",
			session:        allScopes,
			policy:         BrowserSessionRoutePolicy{},
			wantStatus:     http.StatusForbidden,
			wantCalled:     false,
			wantReasonCode: "scoped_route_all_scope_grant_required",
		},
		{
			name:       "all scopes under permissive policy is admitted",
			session:    allScopes,
			policy:     BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name:       "restricted session is unaffected",
			session:    restricted,
			policy:     BrowserSessionRoutePolicy{},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			// Never on the allowlist, so the route genuinely has no scoped
			// authorization. The pre-existing reason code is the right one and
			// must not drift to the new one.
			name:           "never-allowlisted route keeps the old reason code",
			path:           "/api/v0/graph/entities",
			session:        allScopes,
			policy:         BrowserSessionRoutePolicy{},
			wantStatus:     http.StatusForbidden,
			wantCalled:     false,
			wantReasonCode: "scoped_route_not_enabled",
		},
		{
			// IsSharedKeyOnlyRoute refuses ahead of everything, including the
			// permissive policy, and keeps the old reason code.
			name:           "shared-key-only route keeps the old reason code",
			method:         http.MethodPost,
			path:           "/api/v0/supply-chain/impact/suppressions",
			session:        allScopes,
			policy:         BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true},
			wantStatus:     http.StatusForbidden,
			wantCalled:     false,
			wantReasonCode: "scoped_route_not_enabled",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			path := tc.path
			if path == "" {
				path = "/api/v0/repositories"
			}

			audit := &fakeGovernanceAuditAppender{}
			called := false
			handler := AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement(
				splitTestSharedToken,
				nil,
				&fakeBrowserSessionResolver{context: tc.session, ok: true},
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					_, _ = w.Write([]byte(splitTestHandlerPayload))
				}),
				audit,
				tc.policy,
				true,
			)

			req := httptest.NewRequest(method, path, nil)
			req.AddCookie(&http.Cookie{Name: BrowserSessionCookieName, Value: "session-secret"})
			if browserSessionRequiresCSRF(method) {
				req.Header.Set(BrowserSessionCSRFHeaderName, "csrf-secret")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if called != tc.wantCalled {
				t.Fatalf("handler called = %t, want %t; status = %d, body = %s", called, tc.wantCalled, rec.Code, rec.Body.String())
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !tc.wantCalled && strings.Contains(rec.Body.String(), splitTestHandlerPayload) {
				t.Fatalf("denied response exposed handler data: %s", rec.Body.String())
			}

			if tc.wantReasonCode == "" {
				if len(audit.events) != 0 {
					t.Fatalf("admitted request emitted %d governance-audit event(s), want 0: %#v", len(audit.events), audit.events)
				}
				return
			}
			if len(audit.events) != 1 {
				t.Fatalf("len(audit.events) = %d, want 1: %#v", len(audit.events), audit.events)
			}
			event := audit.events[0]
			if got, want := event.ReasonCode, tc.wantReasonCode; got != want {
				t.Fatalf("event.ReasonCode = %q, want %q", got, want)
			}
			if got, want := event.Type, governanceaudit.EventTypeReadAuthorization; got != want {
				t.Fatalf("event.Type = %q, want %q", got, want)
			}
			if got, want := event.Decision, governanceaudit.DecisionDenied; got != want {
				t.Fatalf("event.Decision = %q, want %q", got, want)
			}
			if got, want := event.ActorClass, governanceaudit.ActorClassScopedToken; got != want {
				t.Fatalf("event.ActorClass = %q, want %q", got, want)
			}
			if got, want := event.ActorIDHash, "sha256:abcdef12"; got != want {
				t.Fatalf("event.ActorIDHash = %q, want %q", got, want)
			}
			if got, want := event.PolicyRevisionHash, "sha256:01234567"; got != want {
				t.Fatalf("event.PolicyRevisionHash = %q, want %q", got, want)
			}
			if got, want := event.TenantID, "tenant-a"; got != want {
				t.Fatalf("event.TenantID = %q, want %q -- a tenant admin filters its audit reads by tenant_id, so a denial with no tenant is one it cannot see", got, want)
			}
			if got, want := event.WorkspaceID, "workspace-a"; got != want {
				t.Fatalf("event.WorkspaceID = %q, want %q", got, want)
			}
			if _, err := governanceaudit.NormalizeEvent(event); err != nil {
				t.Fatalf("governanceaudit.NormalizeEvent() error = %v, want nil", err)
			}
		})
	}
}

// TestRecordScopedRouteAuthorizationDeniedBlankReasonFallsBackToUnspecified
// pins the identity of scopedRouteDeniedUnspecifiedReason, the defensive
// fallback recordScopedRouteAuthorizationDeniedWithReason substitutes when a
// caller hands it a blank or whitespace-only reason code.
//
// This one calls the helper directly rather than driving a request through
// the middleware, which is the honest shape here: the production path cannot
// reach the fallback at all. browserSessionRouteDenialReason returns "" only
// for an ADMITTED request, and an admitted request never records a denial, so
// every refusal that reaches the helper today carries one of the two real
// codes. There is no request to construct that would exercise this branch,
// and a test that pretended otherwise would be asserting against a shape the
// product does not have.
//
// The branch is still worth pinning. Its whole value is being distinct: a
// refactor that "simplifies" the fallback back to scopedRouteNotEnabledReason
// would file a future caller's bug under a real code, which is the same
// triage failure #6450 fixed one level up in the admission path, and nothing
// else in the suite would notice. This test notices.
func TestRecordScopedRouteAuthorizationDeniedBlankReasonFallsBackToUnspecified(t *testing.T) {
	t.Parallel()

	auth := AuthContext{
		Mode:               AuthModeBrowserSession,
		TenantID:           "tenant-a",
		WorkspaceID:        "workspace-a",
		SubjectClass:       "local_user",
		SubjectIDHash:      "sha256:abcdef12",
		PolicyRevisionHash: "sha256:01234567",
		AllScopes:          true,
	}

	for _, tc := range []struct {
		name       string
		reasonCode string
	}{
		{name: "empty", reasonCode: ""},
		{name: "whitespace only", reasonCode: "  "},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			audit := &fakeGovernanceAuditAppender{}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
			recordScopedRouteAuthorizationDeniedWithReason(req, audit, auth, tc.reasonCode)

			if len(audit.events) != 1 {
				t.Fatalf("len(audit.events) = %d, want 1: %#v", len(audit.events), audit.events)
			}
			event := audit.events[0]
			if got, want := event.ReasonCode, "scoped_route_denied_unspecified"; got != want {
				t.Fatalf("event.ReasonCode = %q, want %q -- the blank-reason fallback must stay distinct from the two real codes", got, want)
			}
			if got, want := event.Type, governanceaudit.EventTypeReadAuthorization; got != want {
				t.Fatalf("event.Type = %q, want %q", got, want)
			}
			if got, want := event.Decision, governanceaudit.DecisionDenied; got != want {
				t.Fatalf("event.Decision = %q, want %q", got, want)
			}
			if got, want := event.ActorClass, governanceaudit.ActorClassScopedToken; got != want {
				t.Fatalf("event.ActorClass = %q, want %q", got, want)
			}
			if got, want := event.ActorIDHash, "sha256:abcdef12"; got != want {
				t.Fatalf("event.ActorIDHash = %q, want %q", got, want)
			}
			if got, want := event.PolicyRevisionHash, "sha256:01234567"; got != want {
				t.Fatalf("event.PolicyRevisionHash = %q, want %q", got, want)
			}
			if got, want := event.TenantID, "tenant-a"; got != want {
				t.Fatalf("event.TenantID = %q, want %q -- the fallback path carries the caller tenant like every other denial", got, want)
			}
			if got, want := event.WorkspaceID, "workspace-a"; got != want {
				t.Fatalf("event.WorkspaceID = %q, want %q", got, want)
			}
			if _, err := governanceaudit.NormalizeEvent(event); err != nil {
				t.Fatalf("governanceaudit.NormalizeEvent() error = %v, want nil -- a blank code must not produce an event the durable store rejects", err)
			}
		})
	}
}
