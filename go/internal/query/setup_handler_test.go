// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

type fakeSetupStore struct {
	needsSetup        bool
	needsSetupErr     error
	validUsername     string
	validPassword     string
	verifyErr         error
	owner             SetupOwner
	resolveOwnerErr   error
	rotatedPassword   LocalIdentityPasswordReset
	rotatePasswordErr error
	rotatedMFA        LocalIdentityMFAReset
	rotateMFAErr      error
	completedSubject  string
	completedAt       time.Time
	completeErr       error
}

func (s *fakeSetupStore) SetupNeeded(_ context.Context) (bool, error) {
	return s.needsSetup, s.needsSetupErr
}

func (s *fakeSetupStore) VerifyBootstrapCredential(_ context.Context, username, password string) (bool, error) {
	if s.verifyErr != nil {
		return false, s.verifyErr
	}
	return username == s.validUsername && password == s.validPassword, nil
}

func (s *fakeSetupStore) ResolveSetupOwner(_ context.Context) (SetupOwner, error) {
	return s.owner, s.resolveOwnerErr
}

func (s *fakeSetupStore) RotateSetupPassword(_ context.Context, reset LocalIdentityPasswordReset) error {
	s.rotatedPassword = reset
	return s.rotatePasswordErr
}

func (s *fakeSetupStore) RotateSetupMFA(_ context.Context, reset LocalIdentityMFAReset) error {
	s.rotatedMFA = reset
	return s.rotateMFAErr
}

func (s *fakeSetupStore) CompleteSetup(_ context.Context, subjectIDHash string, now time.Time) error {
	s.completedSubject = subjectIDHash
	s.completedAt = now
	return s.completeErr
}

func TestSetupStateReportsNeedsSetupAndBootstrapMode(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{needsSetup: true}
	handler := &SetupHandler{Store: store, BootstrapMode: "generated"}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/setup-state", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"needs_setup":true`) {
		t.Fatalf("body = %s, want needs_setup=true", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"bootstrap_mode":"generated"`) {
		t.Fatalf("body = %s, want bootstrap_mode=generated", rec.Body.String())
	}
}

func TestSetupClaimAcceptsValidCredentialAndAudits(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{needsSetup: true, validUsername: "admin", validPassword: "generated-pw"}
	audit := &fakeGovernanceAuditAppender{}
	handler := &SetupHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/setup/claim",
		bytes.NewBufferString(`{"username":"admin","password":"generated-pw"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionAllowed {
		t.Fatalf("audit events = %#v, want one allowed event", audit.events)
	}
}

func TestSetupClaimRejectsWrongCredentialWithoutLeakingWhy(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{needsSetup: true, validUsername: "admin", validPassword: "generated-pw"}
	audit := &fakeGovernanceAuditAppender{}
	handler := &SetupHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/setup/claim",
		bytes.NewBufferString(`{"username":"admin","password":"wrong-password"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "initial-credential") {
		t.Fatalf("body = %s, want a pointer to the recovery CLI", rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied {
		t.Fatalf("audit events = %#v, want one denied event", audit.events)
	}
}

func TestSetupRoutesReturnGoneOnceSetupIsNoLongerNeeded(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{needsSetup: false}
	handler := &SetupHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v0/auth/setup/claim", `{"username":"admin","password":"x"}`},
		{http.MethodPost, "/api/v0/auth/setup/admin", `{"username":"admin","password":"x","new_password":"y"}`},
		{http.MethodPost, "/api/v0/auth/setup/mfa", `{"username":"admin","password":"x"}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusGone {
			t.Fatalf("%s status = %d, want %d: %s", tc.path, rec.Code, http.StatusGone, rec.Body.String())
		}
	}

	// The read-only state check must still answer normally, never 410 — the
	// console needs it to route away from the wizard.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/auth/setup-state", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup-state status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSetupCreateAdminRotatesPasswordForResolvedOwner(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{
		needsSetup:    true,
		validUsername: "admin",
		validPassword: "generated-pw",
		owner: SetupOwner{
			UserID:        "user-1",
			SubjectIDHash: "sha256:owner-subject",
			TenantID:      "default",
			WorkspaceID:   "default",
		},
	}
	handler := &SetupHandler{
		Store:        store,
		NewSecret:    sequenceSecrets("credential-id"),
		PasswordCost: 4,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/setup/admin",
		bytes.NewBufferString(`{"username":"admin","password":"generated-pw","new_password":"operator-chosen-password"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.rotatedPassword.UserID != "user-1" {
		t.Fatalf("rotated password userID = %q, want %q", store.rotatedPassword.UserID, "user-1")
	}
	if store.rotatedPassword.PasswordHash == "" || store.rotatedPassword.PasswordHash == "operator-chosen-password" {
		t.Fatalf("rotated password hash = %q, want a bcrypt hash, not plaintext", store.rotatedPassword.PasswordHash)
	}
	if strings.Contains(rec.Body.String(), "operator-chosen-password") {
		t.Fatalf("response leaked the new plaintext password: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tenant_id":"default"`) {
		t.Fatalf("body = %s, want tenant_id echoed", rec.Body.String())
	}
}

func TestSetupCreateAdminRequiresNewPassword(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{needsSetup: true, validUsername: "admin", validPassword: "generated-pw"}
	handler := &SetupHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/setup/admin",
		bytes.NewBufferString(`{"username":"admin","password":"generated-pw"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
