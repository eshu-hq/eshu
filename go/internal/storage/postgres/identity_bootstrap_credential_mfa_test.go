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

// TestResetBootstrapCredentialReenrollsRecoveryCodeMFAAtomically proves issue
// #5602's fix: a reset must not only rotate the password and envelope, it
// must also revoke the owner's stale recovery-code factor/codes and insert a
// fresh factor + recovery-code hash, all inside the SAME committed
// transaction as the password rotation — otherwise the printed recovery code
// can never authenticate.
func TestResetBootstrapCredentialReenrollsRecoveryCodeMFAAtomically(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{"sha256:owner-subject"}}}, // selectBootstrapCredentialSubjectQuery
				{rows: [][]any{{"user_owner"}}},           // selectBootstrapCredentialOwnerUserIDQuery
			},
			execResults: []sql.Result{fakeResultWithRowsAffected{rowsAffected: 1}},
		},
	}
	store := NewIdentitySubjectStore(db)
	resetAt := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)

	err := store.ResetBootstrapCredential(context.Background(), ResetBootstrapCredentialInput{
		TenantID:               "tenant_local",
		WorkspaceID:            "workspace_local",
		SealedCredential:       "ESK1.key3.nonce3.ciphertext3",
		KeyID:                  "key3",
		PasswordHash:           "bcrypt:new-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		MFAFactorID:            "id_new-recovery-factor",
		RecoveryCodeHash:       "sha256:new-recovery-code",
		ResetAt:                resetAt,
	})
	if err != nil {
		t.Fatalf("ResetBootstrapCredential() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}

	if !fakeExecsContainQuery(db.execs, "UPDATE identity_mfa_recovery_codes") {
		t.Fatalf("reset execs missing recovery code revocation: %#v", db.execs)
	}
	revokeFactorIdx := -1
	for i, exec := range db.execs {
		if strings.Contains(exec.query, "UPDATE identity_mfa_factors") {
			revokeFactorIdx = i
			if !fakeExecArgsContain(exec.args, "recovery_code") {
				t.Fatalf("factor revocation did not scope to recovery_code kind: %#v", exec)
			}
			if fakeExecArgsContain(exec.args, "totp") {
				t.Fatalf("factor revocation must never reference totp: %#v", exec)
			}
		}
	}
	if revokeFactorIdx == -1 {
		t.Fatalf("reset execs missing recovery-code factor revocation: %#v", db.execs)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_mfa_factors") {
		t.Fatalf("reset execs missing fresh factor insert: %#v", db.execs)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_mfa_recovery_codes") {
		t.Fatalf("reset execs missing fresh recovery code insert: %#v", db.execs)
	}
	insertCodeIdx := -1
	for i, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO identity_mfa_recovery_codes") {
			insertCodeIdx = i
			if !fakeExecArgsContain(exec.args, "sha256:new-recovery-code") {
				t.Fatalf("fresh recovery code insert missing the new hash: %#v", exec)
			}
			if !fakeExecArgsContain(exec.args, "id_new-recovery-factor") {
				t.Fatalf("fresh recovery code insert not tied to the fresh factor id: %#v", exec)
			}
		}
	}
	if insertCodeIdx == -1 || insertCodeIdx < revokeFactorIdx {
		t.Fatalf("recovery code insert must run after the stale factor revocation: execs=%#v", db.execs)
	}

	// db.committed is set by the single fakeTransaction this fake ever
	// constructs (see fakeBeginnerExecQueryer.Begin): every exec above landed
	// on that one shared handle, so a single committed=true here proves the
	// password rotation, envelope reseal, and MFA re-enrollment all ran
	// inside the SAME transaction rather than a second, separately committed
	// one.
	if !db.committed {
		t.Fatalf("ResetBootstrapCredential() did not commit the shared reset transaction")
	}
}
