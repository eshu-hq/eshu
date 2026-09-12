// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// These tests are the security contract for issue #5164's self-service token
// management: any authenticated user may create a token bound to their OWN
// subject and may revoke/rotate ONLY a token they own, while all-scope admins
// keep the unrestricted create-for-others / revoke-any / rotate-any behavior.
// They are written failing-first against the pre-#5164 handlers, which gated
// every mutation behind requireAllScopeAuth and never scoped the store call to
// the caller's subject.

// nonAdminBrowserSession is the AuthContext for a genuine non-admin local user:
// an authenticated browser session with AllScopes=false. This is exactly the
// caller class the pre-#5164 requireAllScopeAuth gate rejected with 403.
func nonAdminBrowserSession(subjectIDHash string) AuthContext {
	return AuthContext{
		Mode:          AuthModeBrowserSession,
		TenantID:      "tenant_local",
		WorkspaceID:   "workspace_local",
		SubjectIDHash: subjectIDHash,
		AllScopes:     false,
	}
}

func selfServiceTokenHandler(store *fakeLocalIdentityStore, now time.Time) (*LocalIdentityHandler, *http.ServeMux) {
	handler := &LocalIdentityHandler{
		Store:     store,
		Audit:     &fakeGovernanceAuditAppender{},
		NewSecret: sequenceSecrets("token-id", "raw-generated-token"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return handler, mux
}

// TestCreatePersonalAPITokenNonAdminBindsOwnSubject proves a non-admin caller
// can create a personal token and that its owner is resolved from the caller's
// own session subject, never a body-supplied user_id the browser cannot know.
func TestCreatePersonalAPITokenNonAdminBindsOwnSubject(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		resolvedUserID:      "user_self",
		resolvedUserIDFound: true,
	}
	_, mux := selfServiceTokenHandler(store, now)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens",
		bytes.NewBufferString(`{"token_class":"personal","display_label":"laptop"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), nonAdminBrowserSession("sha256:self")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if got, want := store.createdAPIToken.UserID, "user_self"; got != want {
		t.Fatalf("created token UserID = %q, want caller's own resolved id %q", got, want)
	}
	if got, want := store.createdAPIToken.TokenClass, "personal"; got != want {
		t.Fatalf("created token class = %q, want %q", got, want)
	}
}

// TestCreateAPITokenNonAdminRejectsForeignUserID proves a non-admin cannot mint
// a token for a DIFFERENT user by naming their user_id: it is a fail-closed 403
// and nothing is written.
func TestCreateAPITokenNonAdminRejectsForeignUserID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 5, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		resolvedUserID:      "user_self",
		resolvedUserIDFound: true,
	}
	_, mux := selfServiceTokenHandler(store, now)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens",
		bytes.NewBufferString(`{"token_class":"personal","user_id":"user_victim","display_label":"laptop"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), nonAdminBrowserSession("sha256:self")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if store.createdAPIToken.TokenID != "" {
		t.Fatalf("a token was created for a foreign user_id: %#v", store.createdAPIToken)
	}
}

// TestCreateAPITokenNonAdminRejectsServicePrincipal proves a non-admin cannot
// mint a service-principal token: a service principal is not the caller's own
// identity, so self-service is limited to personal tokens.
func TestCreateAPITokenNonAdminRejectsServicePrincipal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 10, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		resolvedUserID:      "user_self",
		resolvedUserIDFound: true,
	}
	_, mux := selfServiceTokenHandler(store, now)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens",
		bytes.NewBufferString(`{"token_class":"service_principal","service_principal_id":"sp_1","display_label":"ci"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), nonAdminBrowserSession("sha256:self")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if store.createdAPIToken.TokenID != "" {
		t.Fatalf("a service-principal token was created by a non-admin: %#v", store.createdAPIToken)
	}
}

// TestCreateAPITokenNonAdminFailsClosedWhenSubjectUnresolved proves that when
// the caller's session subject cannot be resolved to a local user, the create
// is denied rather than falling through to a blank/unbound owner.
func TestCreateAPITokenNonAdminFailsClosedWhenSubjectUnresolved(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 15, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		resolvedUserIDFound: false,
	}
	_, mux := selfServiceTokenHandler(store, now)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/api-tokens",
		bytes.NewBufferString(`{"token_class":"personal","display_label":"laptop"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), nonAdminBrowserSession("sha256:ghost")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (fail-closed): %s", rec.Code, rec.Body.String())
	}
	if store.createdAPIToken.TokenID != "" {
		t.Fatalf("a token was created for an unresolved subject: %#v", store.createdAPIToken)
	}
}

// TestRevokeAPITokenNonAdminScopesToOwnSubject proves the handler forwards the
// caller's own subject hash to the store's ownership predicate, so the store
// can atomically refuse a token the caller does not own.
func TestRevokeAPITokenNonAdminScopesToOwnSubject(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 20, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	_, mux := selfServiceTokenHandler(store, now)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/tok_self/revoke", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), nonAdminBrowserSession("sha256:self")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if got, want := store.revokedAPIToken.OwnerSubjectIDHash, "sha256:self"; got != want {
		t.Fatalf("revoke OwnerSubjectIDHash = %q, want caller's own subject %q", got, want)
	}
	if got, want := store.revokedAPIToken.TokenID, "tok_self"; got != want {
		t.Fatalf("revoke TokenID = %q, want %q", got, want)
	}
}

// TestRevokeAPITokenNonAdminNotOwnedReturns404 is the must-pass cross-user
// denial: when the owner-scoped store reports the token is not one the caller
// owns, the handler returns a non-disclosing 404.
func TestRevokeAPITokenNonAdminNotOwnedReturns404(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 25, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		revokeAPITokenError: ErrLocalIdentityAPITokenNotFound,
	}
	_, mux := selfServiceTokenHandler(store, now)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/tok_victim/revoke", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), nonAdminBrowserSession("sha256:attacker")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a token the caller does not own: %s", rec.Code, rec.Body.String())
	}
	// The handler must still have scoped the attempt to the attacker's own
	// subject — never the target's — so the store could refuse it.
	if got, want := store.revokedAPIToken.OwnerSubjectIDHash, "sha256:attacker"; got != want {
		t.Fatalf("revoke OwnerSubjectIDHash = %q, want attacker's own subject %q", got, want)
	}
}

// TestRotateAPITokenNonAdminScopesToOwnSubject proves rotate forwards the
// caller's own subject hash to the store's ownership predicate.
func TestRotateAPITokenNonAdminScopesToOwnSubject(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	_, mux := selfServiceTokenHandler(store, now)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/tok_self/rotate", bytes.NewBufferString("{}"))
	req = req.WithContext(ContextWithAuthContext(req.Context(), nonAdminBrowserSession("sha256:self")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if got, want := store.rotatedAPIToken.OwnerSubjectIDHash, "sha256:self"; got != want {
		t.Fatalf("rotate OwnerSubjectIDHash = %q, want caller's own subject %q", got, want)
	}
	if got, want := store.rotatedAPIToken.OldTokenID, "tok_self"; got != want {
		t.Fatalf("rotate OldTokenID = %q, want %q", got, want)
	}
}

// TestRotateAPITokenNonAdminNotOwnedReturns404 is the rotate half of the
// cross-user denial: a non-owned token rotate returns a non-disclosing 404.
func TestRotateAPITokenNonAdminNotOwnedReturns404(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 35, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		rotateAPITokenError: ErrLocalIdentityAPITokenNotFound,
	}
	_, mux := selfServiceTokenHandler(store, now)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/tok_victim/rotate", bytes.NewBufferString("{}"))
	req = req.WithContext(ContextWithAuthContext(req.Context(), nonAdminBrowserSession("sha256:attacker")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a token the caller does not own: %s", rec.Code, rec.Body.String())
	}
	if got, want := store.rotatedAPIToken.OwnerSubjectIDHash, "sha256:attacker"; got != want {
		t.Fatalf("rotate OwnerSubjectIDHash = %q, want attacker's own subject %q", got, want)
	}
}

// TestRevokeAndRotateAPITokenAdminStayUnrestricted proves the all-scope admin
// path is unchanged: the store call carries NO owner predicate, so an admin can
// still revoke or rotate any token in the tenant.
func TestRevokeAndRotateAPITokenAdminStayUnrestricted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 18, 9, 40, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{}
	_, mux := selfServiceTokenHandler(store, now)

	adminAuth := AuthContext{
		Mode:          AuthModeBrowserSession,
		TenantID:      "tenant_local",
		WorkspaceID:   "workspace_local",
		SubjectIDHash: "sha256:admin",
		AllScopes:     true,
	}

	revokeReq := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/tok_any/revoke", nil)
	revokeReq = revokeReq.WithContext(ContextWithAuthContext(revokeReq.Context(), adminAuth))
	revokeRec := httptest.NewRecorder()
	mux.ServeHTTP(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("admin revoke status = %d, want 204: %s", revokeRec.Code, revokeRec.Body.String())
	}
	if store.revokedAPIToken.OwnerSubjectIDHash != "" {
		t.Fatalf("admin revoke carried an owner predicate %q, want unrestricted", store.revokedAPIToken.OwnerSubjectIDHash)
	}

	rotateReq := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/api-tokens/tok_any/rotate", bytes.NewBufferString("{}"))
	rotateReq = rotateReq.WithContext(ContextWithAuthContext(rotateReq.Context(), adminAuth))
	rotateRec := httptest.NewRecorder()
	mux.ServeHTTP(rotateRec, rotateReq)
	if rotateRec.Code != http.StatusCreated {
		t.Fatalf("admin rotate status = %d, want 201: %s", rotateRec.Code, rotateRec.Body.String())
	}
	if store.rotatedAPIToken.OwnerSubjectIDHash != "" {
		t.Fatalf("admin rotate carried an owner predicate %q, want unrestricted", store.rotatedAPIToken.OwnerSubjectIDHash)
	}
}
