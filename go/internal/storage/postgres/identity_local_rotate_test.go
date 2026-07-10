// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// rotationAuthCredentialRow returns the selectLocalIdentityCredentialForUpdateQuery
// row shape for an admin credential, mirroring mustChangePasswordAuthCredentialRow
// but reusable for both must_change_password=true and =false scenarios.
func rotationAuthCredentialRow(t *testing.T, password string, hasActiveMFA, mustChangePassword bool, failedAttempts int64) []any {
	t.Helper()
	return []any{
		"user_owner",
		"tenant_local",
		"workspace_local",
		"sha256:owner-subject",
		mustBcryptHash(t, password),
		"active",
		sql.NullTime{},
		sql.NullTime{},
		failedAttempts,
		true, // has_admin_role
		hasActiveMFA,
		"sha256:policy",
		mustChangePassword,
	}
}

// TestRotateLocalIdentityPasswordLockedReturnsLockedWithoutMutation covers the
// lockout guard in RotateLocalIdentityPassword (self-review P2, PR #5054): a
// credential whose locked_until is still in the future must return
// LocalIdentityAuthLocked — before the password compare and before any
// credential mutation — so a locked-out account cannot rotate its way past the
// lockout window. rotationAuthCredentialRow always zeroes locked_until, so this
// builds the row inline with a future lock.
func TestRotateLocalIdentityPasswordLockedReturnsLockedWithoutMutation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 30, 0, 0, time.UTC)
	lockedUntil := now.Add(15 * time.Minute)
	password := "correct-password"
	row := []any{
		"user_owner",
		"tenant_local",
		"workspace_local",
		"sha256:owner-subject",
		mustBcryptHash(t, password),
		"active",
		sql.NullTime{}, // disabled_at
		sql.NullTime{Time: lockedUntil, Valid: true}, // locked_until (future)
		int64(0), // failed_attempts
		true,     // has_admin_role
		true,     // has_active_mfa
		"sha256:policy",
		true, // must_change_password (irrelevant: lockout fires first)
	}
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: [][]any{row}}, // locked credential select (FOR UPDATE)
		}},
	}
	store := NewIdentitySubjectStore(db)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:owner-subject",
		CurrentPassword:           password,
		NewPasswordHash:           "sha256:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:params",
		CredentialID:              "cred-new",
		Now:                       now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthLocked || result.Authenticated {
		t.Fatalf("auth result = %#v, want locked without a session", result)
	}
	if !result.LockedUntil.Equal(lockedUntil) {
		t.Fatalf("LockedUntil = %v, want %v", result.LockedUntil, lockedUntil)
	}
	if fakeExecsContainQuery(db.execs, "UPDATE identity_local_credentials") ||
		fakeExecsContainQuery(db.execs, "INSERT INTO identity_local_credentials") {
		t.Fatalf("locked rotation must not revoke or insert a credential: %#v", db.execs)
	}
}

func TestRotateLocalIdentityPasswordSucceedsAndClearsMustChangePassword(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	password := "correct-password"
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: [][]any{rotationAuthCredentialRow(t, password, true, true, 0)}}, // locked credential select
			{rows: [][]any{}}, // finishLocalIdentityAuthentication: no second Query for an admin
		}},
	}
	store := NewIdentitySubjectStore(db)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:owner-subject",
		CurrentPassword:           password,
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:user_owner:rotated",
		MFARecoveryCodeHash:       "sha256:recovery-a",
		Now:                       now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("rotate result = %#v, want authenticated", result)
	}
	if !result.Auth.AllScopes || result.Auth.SubjectIDHash != "sha256:owner-subject" {
		t.Fatalf("rotate auth context = %#v, want owner all-scopes subject", result.Auth)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	if !strings.Contains(db.queries[0].query, "FOR UPDATE OF c") {
		t.Fatalf("rotation did not use the row-locked credential select:\n%s", db.queries[0].query)
	}

	var revokeExec, insertExec *fakeExecCall
	for i := range db.execs {
		switch {
		case strings.Contains(db.execs[i].query, "UPDATE identity_local_credentials") && strings.Contains(db.execs[i].query, "status = 'revoked'"):
			revokeExec = &db.execs[i]
		case strings.Contains(db.execs[i].query, "INSERT INTO identity_local_credentials"):
			insertExec = &db.execs[i]
		}
	}
	if revokeExec == nil {
		t.Fatalf("rotation did not revoke the old credential: %#v", db.execs)
	}
	if insertExec == nil {
		t.Fatalf("rotation did not insert the new credential: %#v", db.execs)
	}
	if got := insertExec.args[2]; got != "bcrypt:new-hash" {
		t.Fatalf("inserted credential password_hash = %v, want new hash", got)
	}
	if got := insertExec.args[6]; got != false {
		t.Fatalf("inserted credential must_change_password = %v, want false (rotation always clears it)", got)
	}
	if !fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("rotation did not consume the MFA recovery code: %#v", db.execs)
	}
	if !fakeExecsContainQuery(db.execs, "sealed_credential = ''") {
		t.Fatalf("rotation did not consume the bootstrap credential envelope on first successful proof: %#v", db.execs)
	}
}

// TestRotateLocalIdentityPasswordAlreadyClearedStillSucceeds proves rotation
// is a general self-service capability, not gated on must_change_password
// being true: a credential that already has it false can still rotate
// (idempotent no-op on the flag itself).
func TestRotateLocalIdentityPasswordAlreadyClearedStillSucceeds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 5, 0, 0, time.UTC)
	password := "correct-password"
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: [][]any{rotationAuthCredentialRow(t, password, true, false, 0)}},
			{rows: [][]any{}},
		}},
	}
	store := NewIdentitySubjectStore(db)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:owner-subject",
		CurrentPassword:           password,
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:user_owner:rotated",
		MFARecoveryCodeHash:       "sha256:recovery-a",
		Now:                       now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("rotate result = %#v, want authenticated", result)
	}
}

func TestRotateLocalIdentityPasswordRejectsWrongCurrentPassword(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 10, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: [][]any{rotationAuthCredentialRow(t, "correct-password", true, true, 0)}},
		}},
	}
	store := NewIdentitySubjectStore(db)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:owner-subject",
		CurrentPassword:           "wrong-password",
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:user_owner:rotated",
		Now:                       now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
		t.Fatalf("rotate result = %#v, want invalid", result)
	}
	if !db.rolledBack || db.committed {
		t.Fatalf("transaction committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_local_auth_attempts") {
		t.Fatalf("wrong-password rotation did not record a failed attempt: %#v", db.execs)
	}
	if fakeExecsContainQuery(db.execs, "INSERT INTO identity_local_credentials") {
		t.Fatalf("wrong-password rotation must not insert a new credential: %#v", db.execs)
	}
}

func TestRotateLocalIdentityPasswordRequiresMFAWhenAccountHasActiveFactor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 15, 0, 0, time.UTC)
	password := "correct-password"
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: [][]any{rotationAuthCredentialRow(t, password, true, true, 0)}},
		}},
	}
	store := NewIdentitySubjectStore(db)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:owner-subject",
		CurrentPassword:           password,
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:user_owner:rotated",
		// No MFARecoveryCodeHash.
		Now: now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthMFARequired || result.Authenticated {
		t.Fatalf("rotate result = %#v, want mfa_required without a session", result)
	}
	if !db.rolledBack || db.committed {
		t.Fatalf("transaction committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
	if len(db.execs) != 0 {
		t.Fatalf("missing-recovery-code rotation performed writes = %#v, want none", db.execs)
	}
}

func TestRotateLocalIdentityPasswordRejectsInvalidRecoveryCode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 20, 0, 0, time.UTC)
	password := "correct-password"
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{
			{rows: [][]any{rotationAuthCredentialRow(t, password, true, true, 0)}},
		}},
	}
	db.execResults = []sql.Result{fakeRowsAffected{n: 0}}
	store := NewIdentitySubjectStore(db)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:owner-subject",
		CurrentPassword:           password,
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:user_owner:rotated",
		MFARecoveryCodeHash:       "sha256:wrong-recovery",
		Now:                       now,
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
		t.Fatalf("rotate result = %#v, want invalid", result)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_local_auth_attempts") {
		t.Fatalf("invalid-recovery-code rotation did not record a failed attempt: %#v", db.execs)
	}
}

func TestRotateLocalIdentityPasswordUnknownSubjectReturnsInvalid(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}},
	}
	store := NewIdentitySubjectStore(db)

	result, err := store.RotateLocalIdentityPassword(context.Background(), LocalIdentityPasswordRotation{
		SubjectIDHash:             "sha256:unknown-subject",
		CurrentPassword:           "whatever",
		NewPasswordHash:           "bcrypt:new-hash",
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: "sha256:bcrypt-cost",
		CredentialID:              "credential:x:rotated",
		Now:                       time.Date(2026, 7, 10, 12, 25, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RotateLocalIdentityPassword() error = %v", err)
	}
	if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
		t.Fatalf("rotate result = %#v, want invalid", result)
	}
	if !db.rolledBack || db.committed {
		t.Fatalf("transaction committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
}
