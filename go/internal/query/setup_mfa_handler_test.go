// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetupCompleteMFAGeneratesCodesAndSealsSetup(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{
		needsSetup:    true,
		validUsername: "admin",
		validPassword: "operator-chosen-password",
		owner: SetupOwner{
			UserID:        "user-1",
			SubjectIDHash: "sha256:owner-subject",
			TenantID:      "default",
			WorkspaceID:   "default",
		},
		completeMFAResult: true,
	}
	sessions := &fakeBrowserSessionStore{}
	handler := &SetupHandler{
		Store:    store,
		Sessions: sessions,
		// Session issuance now runs BEFORE the atomic MFA-rotate/consume
		// call (#4990 P2), so the secret sequence is session, csrf, then
		// the MFA factor id.
		NewSecret: sequenceSecrets("session-secret", "csrf-secret", "mfa-factor-id"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/setup/mfa",
		bytes.NewBufferString(`{"username":"admin","password":"operator-chosen-password"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.completeMFACalls != 1 {
		t.Fatalf("CompleteSetupMFA calls = %d, want 1", store.completeMFACalls)
	}
	if store.completeMFAInput.SubjectIDHash != "sha256:owner-subject" {
		t.Fatalf("completed subject = %q, want owner subject hash", store.completeMFAInput.SubjectIDHash)
	}
	if store.completeMFAInput.MFAFactorID != "mfa-factor-id" {
		t.Fatalf("mfa factor id = %q, want %q", store.completeMFAInput.MFAFactorID, "mfa-factor-id")
	}
	if len(store.completeMFAInput.RecoveryCodeHashes) != setupRecoveryCodeCount {
		t.Fatalf("recovery hashes = %d, want %d", len(store.completeMFAInput.RecoveryCodeHashes), setupRecoveryCodeCount)
	}
	for _, hash := range store.completeMFAInput.RecoveryCodeHashes {
		if !strings.HasPrefix(hash, "sha256:") {
			t.Fatalf("recovery code hash %q is not hash-only", hash)
		}
	}
	if len(sessions.created) != 1 {
		t.Fatalf("created sessions = %d, want 1 (wizard completion logs the operator in)", len(sessions.created))
	}
	if sessions.created[0].AllScopes != true {
		t.Fatalf("created session AllScopes = %t, want true (owner)", sessions.created[0].AllScopes)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookies were set on wizard completion")
	}
}

func TestSetupCompleteMFARejectsWrongCredentialAndDoesNotSeal(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{needsSetup: true, validUsername: "admin", validPassword: "correct-password"}
	handler := &SetupHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/setup/mfa",
		bytes.NewBufferString(`{"username":"admin","password":"wrong"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if store.completeMFACalls != 0 {
		t.Fatal("CompleteSetupMFA must not run on a rejected credential")
	}
}

// TestSetupCompleteMFAReturnsConflictWhenConcurrentCompletionWon proves the
// P1 fix (#4990): when the store reports completed=false (a concurrent
// /setup/mfa call already won the advisory-locked race), the handler MUST
// fail closed with 409 instead of returning the generated recovery codes as
// if they were persisted. The operator already reproved ownership, so a
// session is issued regardless (see handleCompleteMFA's doc comment) — but
// the response body must never claim "completed" for codes nobody can use.
func TestSetupCompleteMFAReturnsConflictWhenConcurrentCompletionWon(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{
		needsSetup:    true,
		validUsername: "admin",
		validPassword: "operator-chosen-password",
		owner: SetupOwner{
			UserID:        "user-1",
			SubjectIDHash: "sha256:owner-subject",
			TenantID:      "default",
			WorkspaceID:   "default",
		},
		completeMFAResult: false, // a concurrent caller already won.
	}
	audit := &fakeGovernanceAuditAppender{}
	sessions := &fakeBrowserSessionStore{}
	handler := &SetupHandler{
		Store:     store,
		Sessions:  sessions,
		Audit:     audit,
		NewSecret: sequenceSecrets("session-secret", "csrf-secret", "mfa-factor-id"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/setup/mfa",
		bytes.NewBufferString(`{"username":"admin","password":"operator-chosen-password"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"status":"completed"`) {
		t.Fatalf("response falsely claimed completion for a losing race: %s", rec.Body.String())
	}
	if store.completeMFACalls != 1 {
		t.Fatalf("CompleteSetupMFA calls = %d, want 1", store.completeMFACalls)
	}
	found := false
	for _, event := range audit.events {
		if event.ReasonCode == "setup_mfa_concurrent_completion" {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit events = %#v, want a setup_mfa_concurrent_completion denial", audit.events)
	}
}
