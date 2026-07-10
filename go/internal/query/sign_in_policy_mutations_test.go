// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

type fakeSignInPolicyMutationStore struct {
	result SignInPolicy
	err    error

	calledTenantID string
	calledUpdate   SignInPolicyUpdateRequest
	upsertCalls    int
}

func (s *fakeSignInPolicyMutationStore) UpsertSignInPolicy(
	_ context.Context,
	tenantID string,
	update SignInPolicyUpdateRequest,
	_ string,
	_ time.Time,
) (SignInPolicy, error) {
	s.upsertCalls++
	s.calledTenantID = tenantID
	s.calledUpdate = update
	if s.err != nil {
		return SignInPolicy{}, s.err
	}
	return s.result, nil
}

type fakeSignInPolicyAudit struct {
	events []governanceaudit.Event
}

func (a *fakeSignInPolicyAudit) Append(_ context.Context, events []governanceaudit.Event) error {
	a.events = append(a.events, events...)
	return nil
}

func adminSignInPolicyRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPatch, "/api/v0/auth/admin/sign-in-policy", bytes.NewBufferString(body))
	return req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: true, TenantID: "tenant_a",
	}))
}

func TestAdminSignInPolicyUpdateRequiresAllScopeAdmin(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v0/auth/admin/sign-in-policy", bytes.NewBufferString(`{}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: false, TenantID: "tenant_a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminSignInPolicyUpdateSuccessAuditsAllowed(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{result: SignInPolicy{TenantID: "tenant_a", AllowLocalUserCreation: false}}
	audit := &fakeSignInPolicyAudit{}
	handler := &SignInPolicyMutationHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"allow_local_user_creation":false}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.calledTenantID != "tenant_a" {
		t.Fatalf("store called with tenant_id = %q, want tenant_a", store.calledTenantID)
	}
	if store.calledUpdate.AllowLocalUserCreation == nil || *store.calledUpdate.AllowLocalUserCreation {
		t.Fatalf("update.AllowLocalUserCreation = %#v, want pointer to false", store.calledUpdate.AllowLocalUserCreation)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionAllowed {
		t.Fatalf("audit events = %#v, want exactly one allowed event", audit.events)
	}
}

func TestAdminSignInPolicyUpdateGuardrailNoProviderReturns400AndAudits(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{err: ErrSignInPolicyGuardrailNoProvenProvider}
	audit := &fakeSignInPolicyAudit{}
	handler := &SignInPolicyMutationHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"require_sso":true}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
		audit.events[0].ReasonCode != "sign_in_policy_guardrail_no_provider" {
		t.Fatalf("audit events = %#v, want exactly one denied guardrail event", audit.events)
	}
}

func TestAdminSignInPolicyUpdateGuardrailNoSSOAdminProofReturns400AndAudits(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{err: ErrSignInPolicyGuardrailNoSSOAdminProof}
	audit := &fakeSignInPolicyAudit{}
	handler := &SignInPolicyMutationHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"require_sso":true}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
		audit.events[0].ReasonCode != "sign_in_policy_guardrail_no_sso_proof" {
		t.Fatalf("audit events = %#v, want exactly one denied guardrail event", audit.events)
	}
}

func TestAdminSignInPolicyUpdateRejectsWorkspaceScopedCaller(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v0/auth/admin/sign-in-policy", bytes.NewBufferString(`{}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode: AuthModeBrowserSession, AllScopes: true, TenantID: "",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminSignInPolicyUpdateRejectsNegativeIdleTimeout(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"idle_timeout_seconds":-1}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if store.calledTenantID != "" {
		t.Fatalf("store was called with a rejected update: tenant_id = %q", store.calledTenantID)
	}
}

func TestAdminSignInPolicyUpdateRejectsNegativeAbsoluteTimeout(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"absolute_timeout_seconds":-1}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if store.calledTenantID != "" {
		t.Fatalf("store was called with a rejected update: tenant_id = %q", store.calledTenantID)
	}
}

func TestAdminSignInPolicyUpdateRejectsIdleBelowMinimumFloor(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"idle_timeout_seconds":59}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAdminSignInPolicyUpdateRejectsAbsoluteBelowMinimumFloor(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"absolute_timeout_seconds":59}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAdminSignInPolicyUpdateRejectsAbsoluteLessThanIdle(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"idle_timeout_seconds":3600,"absolute_timeout_seconds":1800}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if store.calledTenantID != "" {
		t.Fatalf("store was called with a rejected update: tenant_id = %q", store.calledTenantID)
	}
}

// TestAdminSignInPolicyUpdateTimeoutOrderingErrorReturns400AndAudits proves
// issue #5002 part 2 (codex PR #5053 review, root-caused): the handler no
// longer pre-reads the stored policy to validate a merged idle/absolute
// pair (that read-then-validate was racy under concurrent PATCHes — see
// storage/postgres.IdentitySubjectStore.UpsertSignInPolicy's doc comment).
// Instead the STORE enforces the merged check under its row lock and
// returns ErrSignInPolicyTimeoutOrdering, which this handler maps to 400,
// exactly like the require_sso guardrail sentinels above.
func TestAdminSignInPolicyUpdateTimeoutOrderingErrorReturns400AndAudits(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{err: ErrSignInPolicyTimeoutOrdering}
	audit := &fakeSignInPolicyAudit{}
	handler := &SignInPolicyMutationHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"absolute_timeout_seconds":1800}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	// The single-field body reaches the store (no pre-read short-circuit) —
	// the store is the one that rejects it.
	if store.upsertCalls != 1 {
		t.Fatalf("UpsertSignInPolicy calls = %d, want 1", store.upsertCalls)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
		audit.events[0].ReasonCode != "sign_in_policy_invalid_timeout" {
		t.Fatalf("audit events = %#v, want exactly one denied invalid-timeout event", audit.events)
	}
}

func TestAdminSignInPolicyUpdateAllowsZeroAndMinimumFloorTimeouts(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{result: SignInPolicy{TenantID: "tenant_a"}}
	audit := &fakeSignInPolicyAudit{}
	handler := &SignInPolicyMutationHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"idle_timeout_seconds":0,"absolute_timeout_seconds":60}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.calledTenantID != "tenant_a" {
		t.Fatalf("store was not called: tenant_id = %q", store.calledTenantID)
	}
}
