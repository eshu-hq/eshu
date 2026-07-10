// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
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

	// getResult/getErr configure GetSignInPolicy, used by the merged
	// idle/absolute timeout cross-field check (issue #5002 part 2).
	getResult      SignInPolicy
	getErr         error
	getCalledCount int
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

func (s *fakeSignInPolicyMutationStore) GetSignInPolicy(_ context.Context, _ string) (SignInPolicy, error) {
	s.getCalledCount++
	if s.getErr != nil {
		return SignInPolicy{}, s.getErr
	}
	return s.getResult, nil
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

// TestAdminSignInPolicyUpdateRejectsAbsoluteBelowStoredIdle proves issue
// #5002 part 2: a PATCH that sets ONLY absolute_timeout_seconds, which is
// smaller than the tenant's currently STORED idle_timeout_seconds, is
// rejected even though the request body alone (with no idle field present)
// cannot see the conflict.
func TestAdminSignInPolicyUpdateRejectsAbsoluteBelowStoredIdle(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{
		getResult: SignInPolicy{TenantID: "tenant_a", IdleTimeoutSeconds: 3600},
	}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"absolute_timeout_seconds":1800}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if store.getCalledCount != 1 {
		t.Fatalf("GetSignInPolicy calls = %d, want 1", store.getCalledCount)
	}
	if store.upsertCalls != 0 {
		t.Fatalf("UpsertSignInPolicy calls = %d, want 0 (rejected update must not reach the store write)", store.upsertCalls)
	}
}

// TestAdminSignInPolicyUpdateRejectsSecondPATCHConflictingWithFirst drives
// the exact two-PATCH sequence from issue #5002: idle_timeout_seconds=3600
// is persisted by an earlier PATCH, then a later PATCH sets ONLY
// absolute_timeout_seconds=1800. The second PATCH must be rejected once the
// merged (stored+incoming) values are compared.
func TestAdminSignInPolicyUpdateRejectsSecondPATCHConflictingWithFirst(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{
		result: SignInPolicy{TenantID: "tenant_a", IdleTimeoutSeconds: 3600},
	}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// First PATCH: idle_timeout_seconds=3600 only. No stored absolute yet, so
	// the merged check is a no-op and the write succeeds.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"idle_timeout_seconds":3600}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("first PATCH status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// The fake store does not feed UpsertSignInPolicy's return value back into
	// GetSignInPolicy; reflect what the first PATCH persisted, matching what a
	// real GetSignInPolicy read would now return.
	store.getResult = SignInPolicy{TenantID: "tenant_a", IdleTimeoutSeconds: 3600}

	// Second PATCH: absolute_timeout_seconds=1800 only, below the idle stored
	// by the first PATCH. Single-request (body-only) validation cannot see
	// this; only the merged read-then-validate check catches it.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"absolute_timeout_seconds":1800}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second PATCH status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if store.upsertCalls != 1 {
		t.Fatalf("UpsertSignInPolicy calls = %d, want 1 (only the first, accepted PATCH)", store.upsertCalls)
	}
}

// TestAdminSignInPolicyUpdateAllowsIdleBelowStoredAbsolute proves a PATCH
// lowering idle_timeout_seconds below the tenant's stored
// absolute_timeout_seconds is accepted (the merged pair is still valid).
func TestAdminSignInPolicyUpdateAllowsIdleBelowStoredAbsolute(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{
		result:    SignInPolicy{TenantID: "tenant_a"},
		getResult: SignInPolicy{TenantID: "tenant_a", AbsoluteTimeoutSeconds: 7200},
	}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"idle_timeout_seconds":1800}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.upsertCalls != 1 {
		t.Fatalf("UpsertSignInPolicy calls = %d, want 1", store.upsertCalls)
	}
}

// TestAdminSignInPolicyUpdateBothFieldsInOneBodyStillValidated proves the
// same-request (body-only) check still rejects a single PATCH that sets a
// conflicting idle/absolute pair together, independent of the merged check
// and without needing a store read.
func TestAdminSignInPolicyUpdateBothFieldsInOneBodyStillValidated(t *testing.T) {
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
	// The body-only check rejects before ever reading the store.
	if store.getCalledCount != 0 {
		t.Fatalf("GetSignInPolicy calls = %d, want 0 (body-only violation short-circuits)", store.getCalledCount)
	}
}

// TestAdminSignInPolicyUpdateMergedTimeoutStoreReadFailureReturns500 proves a
// GetSignInPolicy failure during the merged-timeout read surfaces as 500, not
// a silent skip of the cross-field check.
func TestAdminSignInPolicyUpdateMergedTimeoutStoreReadFailureReturns500(t *testing.T) {
	t.Parallel()

	store := &fakeSignInPolicyMutationStore{getErr: errors.New("read failed")}
	handler := &SignInPolicyMutationHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, adminSignInPolicyRequest(`{"idle_timeout_seconds":3600}`))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if store.upsertCalls != 0 {
		t.Fatalf("UpsertSignInPolicy calls = %d, want 0", store.upsertCalls)
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
