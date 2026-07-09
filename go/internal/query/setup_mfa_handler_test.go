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
	}
	sessions := &fakeBrowserSessionStore{}
	handler := &SetupHandler{
		Store:     store,
		Sessions:  sessions,
		NewSecret: sequenceSecrets("mfa-factor-id", "session-secret", "csrf-secret"),
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
	if store.completedSubject != "sha256:owner-subject" {
		t.Fatalf("completed subject = %q, want owner subject hash", store.completedSubject)
	}
	if len(store.rotatedMFA.RecoveryCodeHashes) != setupRecoveryCodeCount {
		t.Fatalf("rotated recovery hashes = %d, want %d", len(store.rotatedMFA.RecoveryCodeHashes), setupRecoveryCodeCount)
	}
	for _, hash := range store.rotatedMFA.RecoveryCodeHashes {
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
	if store.completedSubject != "" {
		t.Fatal("CompleteSetup must not run on a rejected credential")
	}
}
