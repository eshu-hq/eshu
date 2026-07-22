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

// TestCompleteSetupMFAHappyPathLocksRotatesAndConsumes proves the SQL text
// and Go control flow: the advisory lock is acquired before the consumed-
// state check, MFA is rotated, and the bootstrap credential is consumed —
// all inside one transaction that commits.
func TestCompleteSetupMFAHappyPathLocksRotatesAndConsumes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			// selectBootstrapCredentialConsumedState: not yet consumed.
			queryResponses: []queueFakeRows{{rows: [][]any{{false}}}},
			// consumeBootstrapCredentialQuery affects one row.
			execResults: []sql.Result{fakeResultWithRowsAffected{rowsAffected: 1}},
		},
	}
	store := NewIdentitySubjectStore(db)

	completed, err := store.CompleteSetupMFA(context.Background(), CompleteSetupMFAInput{
		TenantID:      "default",
		WorkspaceID:   "default",
		SubjectIDHash: "sha256:owner-subject",
		MFA: LocalIdentityMFAReset{
			UserID:             "user-1",
			MFAFactorID:        "mfa-factor-1",
			MFAFactorKind:      "recovery_code",
			RecoveryCodeHashes: []string{"sha256:code-a", "sha256:code-b"},
			ResetAt:            now,
		},
	})
	if err != nil {
		t.Fatalf("CompleteSetupMFA() error = %v", err)
	}
	if !completed {
		t.Fatal("CompleteSetupMFA() completed = false, want true on a clean unconsumed row")
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	if len(db.execs) == 0 || !strings.Contains(db.execs[0].query, "pg_advisory_xact_lock(3456)") {
		t.Fatalf("first exec did not acquire advisory lock 3456: %#v", db.execs)
	}
	// The per-user MFA-reset advisory lock is taken second — after 3456, before
	// any factor mutation — so a concurrent ResetLocalIdentityMFA (which takes
	// only that per-user key) serializes against this rotation.
	if len(db.execs) < 2 || !strings.Contains(db.execs[1].query, "pg_advisory_xact_lock($1") {
		t.Fatalf("second exec did not acquire the per-user MFA-reset advisory lock: %#v", db.execs)
	}
	var sawRevokeRecoveryCodes, sawRevokeFactors, sawInsertFactor, sawConsume bool
	for _, exec := range db.execs {
		switch {
		case strings.Contains(exec.query, "UPDATE identity_mfa_recovery_codes"):
			sawRevokeRecoveryCodes = true
		case strings.Contains(exec.query, "UPDATE identity_mfa_factors"):
			sawRevokeFactors = true
		case strings.Contains(exec.query, "INSERT INTO identity_mfa_factors"):
			sawInsertFactor = true
		case strings.Contains(exec.query, "UPDATE identity_bootstrap_credentials"):
			sawConsume = true
		}
	}
	if !sawRevokeRecoveryCodes || !sawRevokeFactors || !sawInsertFactor || !sawConsume {
		t.Fatalf("missing expected statement(s): revokeCodes=%t revokeFactors=%t insertFactor=%t consume=%t",
			sawRevokeRecoveryCodes, sawRevokeFactors, sawInsertFactor, sawConsume)
	}
}

// TestCompleteSetupMFADetectsConcurrentCompletionAndDoesNotMutateMFA proves
// the P1 fix (#4990): when the consumed-state check (run under the advisory
// lock) finds the credential already consumed by a racing completion,
// CompleteSetupMFA returns completed=false with no error and issues NO MFA
// mutation statements — a losing racer's generated recovery codes are never
// persisted (never orphaned).
func TestCompleteSetupMFADetectsConcurrentCompletionAndDoesNotMutateMFA(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			// selectBootstrapCredentialConsumedState: already consumed by a
			// concurrent winner inside the same critical section.
			queryResponses: []queueFakeRows{{rows: [][]any{{true}}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	completed, err := store.CompleteSetupMFA(context.Background(), CompleteSetupMFAInput{
		TenantID:      "default",
		WorkspaceID:   "default",
		SubjectIDHash: "sha256:owner-subject",
		MFA: LocalIdentityMFAReset{
			UserID:             "user-1",
			MFAFactorID:        "mfa-factor-loser",
			MFAFactorKind:      "recovery_code",
			RecoveryCodeHashes: []string{"sha256:orphaned-code"},
			ResetAt:            time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CompleteSetupMFA() error = %v", err)
	}
	if completed {
		t.Fatal("CompleteSetupMFA() completed = true, want false when the credential was already consumed")
	}
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "identity_mfa_factors") || strings.Contains(exec.query, "identity_mfa_recovery_codes") {
			t.Fatalf("mutated MFA tables after detecting a concurrent completion: %#v", db.execs)
		}
	}
	if db.committed {
		t.Fatal("transaction committed on a losing race — must roll back without mutating anything")
	}
}

// TestCompleteSetupMFAFailsClosedWhenConsumeAffectsNoRows covers the
// defensive belt-and-braces path: even though the consumed-state check
// (under the same lock) should make this unreachable in practice, if the
// final UPDATE somehow affects zero rows, CompleteSetupMFA must not commit
// a half-applied state (MFA rotated but credential not actually consumed).
func TestCompleteSetupMFAFailsClosedWhenConsumeAffectsNoRows(t *testing.T) {
	t.Parallel()

	// The fake's execResults queue is positional (FIFO across every
	// ExecContext call, not routed by query text), so it must be padded to
	// the exact call count preceding the consume UPDATE this test cares
	// about: lock 3456, per-user MFA-reset lock, revoke recovery codes,
	// revoke mfa factors, insert mfa factor, insert one recovery code hash —
	// six calls — before the consume UPDATE (the seventh) returns the
	// zero-rows-affected result.
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{false}}}},
			execResults: []sql.Result{
				fakeResultWithRowsAffected{rowsAffected: 1},
				fakeResultWithRowsAffected{rowsAffected: 1},
				fakeResultWithRowsAffected{rowsAffected: 1},
				fakeResultWithRowsAffected{rowsAffected: 1},
				fakeResultWithRowsAffected{rowsAffected: 1},
				fakeResultWithRowsAffected{rowsAffected: 1},
				fakeResultWithRowsAffected{rowsAffected: 0},
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	completed, err := store.CompleteSetupMFA(context.Background(), CompleteSetupMFAInput{
		TenantID:      "default",
		WorkspaceID:   "default",
		SubjectIDHash: "sha256:owner-subject",
		MFA: LocalIdentityMFAReset{
			UserID:             "user-1",
			MFAFactorID:        "mfa-factor-1",
			MFAFactorKind:      "recovery_code",
			RecoveryCodeHashes: []string{"sha256:code-a"},
			ResetAt:            time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CompleteSetupMFA() error = %v", err)
	}
	if completed {
		t.Fatal("CompleteSetupMFA() completed = true, want false when the consume update affected zero rows")
	}
	if db.committed {
		t.Fatal("transaction committed despite the consume update affecting zero rows")
	}
}

func TestCompleteSetupMFARequiresTenantWorkspaceAndSubject(t *testing.T) {
	t.Parallel()

	store := NewIdentitySubjectStore(&fakeBeginnerExecQueryer{})
	validMFA := LocalIdentityMFAReset{
		UserID:             "user-1",
		MFAFactorID:        "mfa-factor-1",
		MFAFactorKind:      "recovery_code",
		RecoveryCodeHashes: []string{"sha256:code-a"},
		ResetAt:            time.Now().UTC(),
	}
	cases := []CompleteSetupMFAInput{
		{TenantID: "", WorkspaceID: "default", SubjectIDHash: "sha256:s", MFA: validMFA},
		{TenantID: "default", WorkspaceID: "", SubjectIDHash: "sha256:s", MFA: validMFA},
		{TenantID: "default", WorkspaceID: "default", SubjectIDHash: "", MFA: validMFA},
	}
	for i, in := range cases {
		if _, err := store.CompleteSetupMFA(context.Background(), in); err == nil {
			t.Fatalf("case %d: expected a validation error, got nil", i)
		}
	}
}
