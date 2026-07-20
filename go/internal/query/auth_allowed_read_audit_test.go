// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// TestAuthMiddleware_AllowedScopedReadEmitsExactlyOneEvent proves the F-9
// (#5170) emission contract (design addendum §2/§5): a resolver-success
// scoped-token/OIDC-bearer read — both credential families resolve through
// the same ScopedTokenResolver interface, so this fake stands in for either
// — emits exactly one ALLOWED read_authorization event, with the subject
// hash and policy revision hash carried through from AuthContext untouched,
// before dispatching to next.
func TestAuthMiddleware_AllowedScopedReadEmitsExactlyOneEvent(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:               AuthModeScoped,
			TenantID:           "tenant_a",
			WorkspaceID:        "workspace_a",
			SubjectIDHash:      "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd",
			PolicyRevisionHash: "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba98765432",
			AllScopes:          true,
		},
		ok: true,
	}
	allowedAudit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
		"", resolver, mockHandler(), nil, false, nil, allowedAudit,
	)

	req := httptest.NewRequest("GET", "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.Header.Set("X-Correlation-ID", "corr-allowed-read")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := len(allowedAudit.events), 1; got != want {
		t.Fatalf("len(allowedAudit.events) = %d, want %d: %#v", got, want, allowedAudit.events)
	}
	event := allowedAudit.events[0]
	if got, want := event.Type, governanceaudit.EventTypeReadAuthorization; got != want {
		t.Errorf("event.Type = %q, want %q", got, want)
	}
	if got, want := event.ActorClass, governanceaudit.ActorClassScopedToken; got != want {
		t.Errorf("event.ActorClass = %q, want %q", got, want)
	}
	if got, want := event.ActorIDHash, resolver.context.SubjectIDHash; got != want {
		t.Errorf("event.ActorIDHash = %q, want %q", got, want)
	}
	if got, want := event.Decision, governanceaudit.DecisionAllowed; got != want {
		t.Errorf("event.Decision = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, "scoped_read_allowed"; got != want {
		t.Errorf("event.ReasonCode = %q, want %q", got, want)
	}
	if got, want := event.ScopeClass, governanceaudit.ScopeClassAdmin; got != want {
		t.Errorf("event.ScopeClass = %q, want %q", got, want)
	}
	if got, want := event.PolicyRevisionHash, resolver.context.PolicyRevisionHash; got != want {
		t.Errorf("event.PolicyRevisionHash = %q, want %q", got, want)
	}
	if got, want := event.CorrelationID, "corr-allowed-read"; got != want {
		t.Errorf("event.CorrelationID = %q, want %q", got, want)
	}
	if got, want := event.TenantID, "tenant_a"; got != want {
		t.Errorf("event.TenantID = %q, want %q", got, want)
	}
	if got, want := event.WorkspaceID, "workspace_a"; got != want {
		t.Errorf("event.WorkspaceID = %q, want %q", got, want)
	}
	if _, err := governanceaudit.NormalizeEvent(event); err != nil {
		t.Errorf("NormalizeEvent(event) error = %v, want the emitted event to pass validation", err)
	}
}

// TestAuthMiddleware_DeniedScopedRouteStillOnlyEmitsDenial proves the two
// event streams stay disjoint: a scoped-route denial (auth.Mode ==
// AuthModeScoped but the route does not support the tenant filter) must
// record only the existing DENIED scoped_route_not_enabled event through
// audit, never an ALLOWED event through allowedAudit, even when allowedAudit
// is wired.
func TestAuthMiddleware_DeniedScopedRouteStillOnlyEmitsDenial(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:          AuthModeScoped,
			SubjectIDHash: "sha256:abcdef12abcdef12abcdef12abcdef12abcdef12abcdef12abcdef12abcdef12",
		},
		ok: true,
	}
	audit := &fakeGovernanceAuditAppender{}
	allowedAudit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
		"", resolver, mockHandler(), audit, false, nil, allowedAudit,
	)

	// A route that does not support the tenant filter triggers the
	// scoped-route denial branch, which returns before reaching the ALLOWED
	// emission site.
	req := httptest.NewRequest("GET", "/api/v0/code/search", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 403 {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := len(audit.events), 1; got != want {
		t.Fatalf("len(audit.events) = %d, want %d (denial)", got, want)
	}
	if got, want := audit.events[0].Decision, governanceaudit.DecisionDenied; got != want {
		t.Errorf("audit.events[0].Decision = %q, want %q", got, want)
	}
	if got, want := len(allowedAudit.events), 0; got != want {
		t.Fatalf("len(allowedAudit.events) = %d, want %d (no allowed event on a denied route)", got, want)
	}
}

// TestAuthMiddleware_ResolverErrorEmitsNoAllowedEvent proves a resolver error
// (an unrecognized or infra-failed credential) never reaches the ALLOWED
// emission site, even with allowedAudit wired.
func TestAuthMiddleware_ResolverErrorEmitsNoAllowedEvent(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{err: errors.New("resolver infra error")}
	allowedAudit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
		"", resolver, mockHandler(), nil, false, nil, allowedAudit,
	)

	req := httptest.NewRequest("GET", "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got, want := len(allowedAudit.events), 0; got != want {
		t.Fatalf("len(allowedAudit.events) = %d, want %d", got, want)
	}
}

// TestAuthMiddleware_SharedKeyPathEmitsNoAllowedEvent proves the shared-key
// branch (auth.go:290, sharedAuthContext — no subject) never emits an
// allowed-read event, per the design addendum §2: a shared-key credential has
// no actor identity, and this branch is structurally separate from the
// scoped-resolver success branch that calls recordScopedReadAuthorized.
func TestAuthMiddleware_SharedKeyPathEmitsNoAllowedEvent(t *testing.T) {
	t.Parallel()

	allowedAudit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
		"shared-secret", nil, mockHandler(), nil, true, nil, allowedAudit,
	)

	req := httptest.NewRequest("GET", "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer shared-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := len(allowedAudit.events), 0; got != want {
		t.Fatalf("len(allowedAudit.events) = %d, want %d (shared-key reads must not emit)", got, want)
	}
}

// TestAuthMiddleware_DevOpenHeaderlessEmitsNoAllowedEvent proves the
// dev-mode-open headerless path (no auth source configured) never reaches the
// resolver branch at all, so it cannot emit an allowed-read event.
func TestAuthMiddleware_DevOpenHeaderlessEmitsNoAllowedEvent(t *testing.T) {
	t.Parallel()

	allowedAudit := &fakeGovernanceAuditAppender{}
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
		"", nil, mockHandler(), nil, false, nil, allowedAudit,
	)

	req := httptest.NewRequest("GET", "/api/v0/repositories", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := len(allowedAudit.events), 0; got != want {
		t.Fatalf("len(allowedAudit.events) = %d, want %d", got, want)
	}
}

// TestAuthMiddleware_NilAllowedAuditKeepsBehaviorByteIdentical proves the
// core regression contract: every existing constructor (which defaults
// allowedAudit to nil) behaves exactly as it did before this change — no
// panic, no event of any kind recorded for a successful scoped read through
// audit (the denial-only sink), because recordScopedReadAuthorized is a
// nil-guarded no-op.
func TestAuthMiddleware_NilAllowedAuditKeepsBehaviorByteIdentical(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:          AuthModeScoped,
			SubjectIDHash: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd",
			AllScopes:     true,
		},
		ok: true,
	}
	audit := &fakeGovernanceAuditAppender{}
	// The pre-existing constructor: no allowedAudit parameter exists on it at
	// all, so this exercises the exact call path every caller used before
	// F-9.
	handler := AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge(
		"", resolver, mockHandler(), audit, false, nil,
	)

	req := httptest.NewRequest("GET", "/api/v0/repositories", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := len(audit.events), 0; got != want {
		t.Fatalf("len(audit.events) = %d, want %d (a successful read never touched the denial-only sink before F-9, and still must not)", got, want)
	}
}
