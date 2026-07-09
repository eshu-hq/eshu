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

// TestAuthenticateLocalIdentityConsumesBootstrapCredentialOnSuccessfulLogin
// proves the login->consume wiring end to end through
// AuthenticateLocalIdentity, not just ConsumeBootstrapCredential in
// isolation: a successful bootstrap-admin login issues the
// consumeBootstrapCredentialQuery UPDATE with the authenticated subject's
// own tenant_id/workspace_id/subject_id_hash, destroying the retrievable
// envelope on this login.
func TestAuthenticateLocalIdentityConsumesBootstrapCredentialOnSuccessfulLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 8, 0, 0, 0, time.UTC)
	db := localIdentityAuthDB(t, "correct-password", []any{
		"user_owner",
		"tenant_local",
		"workspace_local",
		"sha256:owner-subject",
		"", // filled by localIdentityAuthDB with the bcrypt hash of "correct-password"
		"active",
		sql.NullTime{},
		sql.NullTime{},
		int64(0),
		true, // has_admin_role
		true, // has_active_mfa
		"sha256:policy",
	})
	// Admin login order: consume recovery code, clear failed attempts,
	// consume bootstrap credential. The default fakeResult (RowsAffected=1)
	// satisfies the first two; this test only asserts the third exec's SQL
	// and arguments, not its rows-affected value.
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:       "sha256:owner-subject",
		Password:            "correct-password",
		MFARecoveryCodeHash: "sha256:recovery-a",
		Now:                 now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("auth result = %#v, want authenticated", result)
	}

	var consumeExec *fakeExecCall
	for i := range db.execs {
		if strings.Contains(db.execs[i].query, "sealed_credential = ''") {
			consumeExec = &db.execs[i]
			break
		}
	}
	if consumeExec == nil {
		t.Fatalf("AuthenticateLocalIdentity did not call ConsumeBootstrapCredential's UPDATE: %#v", db.execs)
	}
	wantArgs := []any{"tenant_local", "workspace_local", "sha256:owner-subject"}
	for i, want := range wantArgs {
		if consumeExec.args[i] != want {
			t.Fatalf("consume exec arg[%d] = %v, want %v (%#v)", i, consumeExec.args[i], want, consumeExec.args)
		}
	}
}

// TestAuthenticateLocalIdentityConsumeIsNoOpForNonBootstrapSubject proves the
// unconditional Consume call is a harmless no-op for any subject other than
// the bootstrap admin: the UPDATE still runs (scoped by subject_id_hash), but
// a zero-row result never surfaces as an authentication error.
func TestAuthenticateLocalIdentityConsumeIsNoOpForNonBootstrapSubject(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 8, 5, 0, 0, time.UTC)
	db := localIdentityAuthDB(t, "correct-password", []any{
		"user_member",
		"tenant_local",
		"workspace_local",
		"sha256:member-subject",
		"",
		"active",
		sql.NullTime{},
		sql.NullTime{},
		int64(0),
		false, // has_admin_role: not the bootstrap subject, no MFA required
		false,
		"sha256:policy",
	})
	// Non-admin exec order: clear failed attempts, then consume bootstrap
	// credential. Queue the first with the harmless default and target the
	// second explicitly with rowsAffected=0 (no bootstrap_credentials row
	// exists for this subject).
	db.execResults = []sql.Result{fakeResult{}, fakeConsumeResult{rowsAffected: 0}}
	store := NewIdentitySubjectStore(db)

	result, err := store.AuthenticateLocalIdentity(context.Background(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash: "sha256:member-subject",
		Password:      "correct-password",
		Now:           now,
	})
	if err != nil {
		t.Fatalf("AuthenticateLocalIdentity() error = %v, want nil (consume no-op must not fail login)", err)
	}
	if result.Status != LocalIdentityAuthAuthenticated || !result.Authenticated {
		t.Fatalf("auth result = %#v, want authenticated despite the consume no-op", result)
	}
	if !fakeExecsContainQuery(db.execs, "sealed_credential = ''") {
		t.Fatalf("AuthenticateLocalIdentity did not attempt ConsumeBootstrapCredential's UPDATE: %#v", db.execs)
	}
}

// fakeConsumeResult is a tiny sql.Result used only to control RowsAffected()
// for the consume exec in these two tests.
type fakeConsumeResult struct {
	rowsAffected int64
}

func (r fakeConsumeResult) LastInsertId() (int64, error) { return 0, nil }

func (r fakeConsumeResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }
