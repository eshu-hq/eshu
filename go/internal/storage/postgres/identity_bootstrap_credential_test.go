// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGenerateBootstrapCredentialUsesAdvisoryLockAndInsertsOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{int64(1)}}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	inserted, err := store.GenerateBootstrapCredential(context.Background(), BootstrapCredentialSeal{
		TenantID:         "tenant_local",
		WorkspaceID:      "workspace_local",
		SubjectIDHash:    "sha256:owner-subject",
		UsernameHash:     "sha256:owner-handle",
		SealedCredential: "ESK1.key1.nonce.ciphertext",
		KeyID:            "key1",
		GeneratedAt:      now,
	})
	if err != nil {
		t.Fatalf("GenerateBootstrapCredential() error = %v", err)
	}
	if !inserted {
		t.Fatal("GenerateBootstrapCredential() inserted = false, want true on genuine insert")
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	if len(db.execs) == 0 || !strings.Contains(db.execs[0].query, "pg_advisory_xact_lock(3456)") {
		t.Fatalf("first exec did not acquire advisory lock 3456: %#v", db.execs)
	}
	if len(db.queries) == 0 || !strings.Contains(db.queries[0].query, "ON CONFLICT (tenant_id, workspace_id) DO NOTHING") {
		t.Fatalf("generate query missing idempotent ON CONFLICT clause: %#v", db.queries)
	}
	for _, exec := range db.execs {
		if fakeExecArgsContain(exec.args, "ESK1.key1.nonce.ciphertext") {
			t.Fatalf("advisory lock exec leaked sealed credential: %#v", exec)
		}
	}
}

// TestGenerateBootstrapCredentialIdempotentOnRestart proves a second Generate
// call for the same (tenant, workspace) — the shape of a process restart
// before first login — reports inserted=false without error when the insert
// conflicts, so a restarting caller must not re-log the one-time banner.
func TestGenerateBootstrapCredentialIdempotentOnRestart(t *testing.T) {
	t.Parallel()

	seal := BootstrapCredentialSeal{
		TenantID:         "tenant_local",
		WorkspaceID:      "workspace_local",
		SubjectIDHash:    "sha256:owner-subject",
		UsernameHash:     "sha256:owner-handle",
		SealedCredential: "ESK1.key1.nonce.ciphertext",
		KeyID:            "key1",
		GeneratedAt:      time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
	}

	// First boot: a true insert (RETURNING one row).
	firstDB := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{int64(1)}}}},
		},
	}
	firstInserted, err := NewIdentitySubjectStore(firstDB).GenerateBootstrapCredential(context.Background(), seal)
	if err != nil {
		t.Fatalf("first GenerateBootstrapCredential() error = %v", err)
	}
	if !firstInserted {
		t.Fatal("first GenerateBootstrapCredential() inserted = false, want true")
	}

	// Restart: the row already exists, so ON CONFLICT DO NOTHING returns zero
	// rows. inserted must be false and no error must surface.
	restartDB := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{}}},
		},
	}
	restartInserted, err := NewIdentitySubjectStore(restartDB).GenerateBootstrapCredential(context.Background(), seal)
	if err != nil {
		t.Fatalf("restart GenerateBootstrapCredential() error = %v", err)
	}
	if restartInserted {
		t.Fatal("restart GenerateBootstrapCredential() inserted = true, want false (idempotent conflict)")
	}
	if !restartDB.committed || restartDB.rolledBack {
		t.Fatalf("restart transaction committed=%t rolledBack=%t, want commit only", restartDB.committed, restartDB.rolledBack)
	}
}

// TestGenerateBootstrapAdminWithCredentialInsertsBothAtomically proves the
// identity insert and the credential seal commit together in one
// transaction when no identity exists yet.
func TestGenerateBootstrapAdminWithCredentialInsertsBothAtomically(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{int64(0)}}}, // countExistingLocalIdentityUsersQuery
				{rows: [][]any{{1}}},        // generateBootstrapCredentialQuery: true insert
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	inserted, err := store.GenerateBootstrapAdminWithCredential(
		context.Background(),
		completeBootstrapRecord(),
		BootstrapCredentialSeal{
			TenantID:         "tenant_local",
			WorkspaceID:      "workspace_local",
			SubjectIDHash:    "sha256:owner-subject",
			UsernameHash:     "sha256:owner-handle",
			SealedCredential: "ESK1.key1.nonce.ciphertext",
			KeyID:            "key1",
			GeneratedAt:      now,
		},
	)
	if err != nil {
		t.Fatalf("GenerateBootstrapAdminWithCredential() error = %v", err)
	}
	if !inserted {
		t.Fatal("GenerateBootstrapAdminWithCredential() inserted = false, want true")
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_users") {
		t.Fatalf("execs missing identity insert: %#v", db.execs)
	}
	if len(db.execs) == 0 || !strings.Contains(db.execs[0].query, "pg_advisory_xact_lock(3455)") {
		t.Fatalf("first exec did not acquire the local-identity advisory lock: %#v", db.execs)
	}
	if !fakeExecsContainQuery(db.execs, "pg_advisory_xact_lock(3456)") {
		t.Fatalf("execs missing the bootstrap-credential advisory lock: %#v", db.execs)
	}
}

// TestGenerateBootstrapAdminWithCredentialReturnsCompletedWhenIdentitiesExist
// proves the atomic path defers to BootstrapLocalIdentity's own
// check-then-insert: when an identity already exists, it rolls back without
// ever attempting the credential insert.
func TestGenerateBootstrapAdminWithCredentialReturnsCompletedWhenIdentitiesExist(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{int64(1)}}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	inserted, err := store.GenerateBootstrapAdminWithCredential(
		context.Background(),
		completeBootstrapRecord(),
		BootstrapCredentialSeal{
			TenantID: "tenant_local", WorkspaceID: "workspace_local",
			SubjectIDHash: "sha256:owner-subject", UsernameHash: "sha256:owner-handle",
			SealedCredential: "ESK1.key1.nonce.ciphertext", KeyID: "key1",
		},
	)
	if !errors.Is(err, ErrLocalIdentityBootstrapCompleted) {
		t.Fatalf("GenerateBootstrapAdminWithCredential() error = %v, want ErrLocalIdentityBootstrapCompleted", err)
	}
	if inserted {
		t.Fatal("GenerateBootstrapAdminWithCredential() inserted = true, want false")
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
	if fakeExecsContainQuery(db.execs, "pg_advisory_xact_lock(3456)") {
		t.Fatal("credential lock/insert attempted even though identities already existed")
	}
}

// TestGenerateBootstrapAdminWithCredentialRollsBackBothOnCredentialFailure is
// the crash-safety regression test: a failure sealing the credential must
// roll back the identity insert too, never leaving an admin identity with no
// retrievable credential. This is what the prior two-transaction design
// (BootstrapLocalIdentity then GenerateBootstrapCredential as separate calls)
// could not guarantee — a crash between them stranded the identity.
func TestGenerateBootstrapAdminWithCredentialRollsBackBothOnCredentialFailure(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{int64(0)}}},                 // countExistingLocalIdentityUsersQuery
				{err: errors.New("credential insert boom")}, // generateBootstrapCredentialQuery fails
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	_, err := store.GenerateBootstrapAdminWithCredential(
		context.Background(),
		completeBootstrapRecord(),
		BootstrapCredentialSeal{
			TenantID: "tenant_local", WorkspaceID: "workspace_local",
			SubjectIDHash: "sha256:owner-subject", UsernameHash: "sha256:owner-handle",
			SealedCredential: "ESK1.key1.nonce.ciphertext", KeyID: "key1",
		},
	)
	if err == nil {
		t.Fatal("GenerateBootstrapAdminWithCredential() error = nil, want the simulated credential-insert failure")
	}
	if db.committed || !db.rolledBack {
		t.Fatalf(
			"transaction committed=%t rolledBack=%t, want rollback only (a credential-seal failure must not leave the identity insert committed)",
			db.committed, db.rolledBack,
		)
	}
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_users") {
		t.Fatalf("identity insert was never attempted before the simulated failure: %#v", db.execs)
	}
}

func TestGenerateBootstrapCredentialRejectsIncompleteSeal(t *testing.T) {
	t.Parallel()

	store := NewIdentitySubjectStore(&fakeBeginnerExecQueryer{})
	_, err := store.GenerateBootstrapCredential(context.Background(), BootstrapCredentialSeal{
		TenantID:    "tenant_local",
		WorkspaceID: "workspace_local",
	})
	if err == nil {
		t.Fatal("GenerateBootstrapCredential() error = nil, want incomplete-seal error")
	}
}

func TestSelectBootstrapCredentialReturnsRetrievableEnvelope(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{"ESK1.key1.nonce.ciphertext", "key1"}}}},
	}
	store := NewIdentitySubjectStore(db)

	got, found, err := store.SelectBootstrapCredential(context.Background(), "tenant_local", "workspace_local")
	if err != nil {
		t.Fatalf("SelectBootstrapCredential() error = %v", err)
	}
	if !found {
		t.Fatal("SelectBootstrapCredential() found = false, want true")
	}
	if got.SealedCredential != "ESK1.key1.nonce.ciphertext" || got.KeyID != "key1" {
		t.Fatalf("SelectBootstrapCredential() = %#v, unexpected", got)
	}
	if !strings.Contains(db.queries[0].query, "consumed_at IS NULL") ||
		!strings.Contains(db.queries[0].query, "sealed_credential <> ''") {
		t.Fatalf("select query missing retrievable predicate:\n%s", db.queries[0].query)
	}
}

// TestOpenAfterConsumeReturnsNotRetrievable proves SelectBootstrapCredential
// reports found=false once the row transitions to consumed (empty rows from
// the fake, matching the real WHERE consumed_at IS NULL AND
// sealed_credential <> ” predicate never matching a consumed row).
func TestOpenAfterConsumeReturnsNotRetrievable(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	store := NewIdentitySubjectStore(db)

	_, found, err := store.SelectBootstrapCredential(context.Background(), "tenant_local", "workspace_local")
	if err != nil {
		t.Fatalf("SelectBootstrapCredential() error = %v", err)
	}
	if found {
		t.Fatal("SelectBootstrapCredential() found = true after consume, want false")
	}
}

func TestConsumeBootstrapCredentialClearsCiphertextAndSetsConsumedAt(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{fakeResultWithRowsAffected{rowsAffected: 1}}}
	store := NewIdentitySubjectStore(db)
	consumedAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

	consumed, err := store.ConsumeBootstrapCredential(context.Background(), "tenant_local", "workspace_local", "sha256:owner-subject", consumedAt)
	if err != nil {
		t.Fatalf("ConsumeBootstrapCredential() error = %v", err)
	}
	if !consumed {
		t.Fatal("ConsumeBootstrapCredential() consumed = false, want true")
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"sealed_credential = ''",
		"consumed_at = $4",
		"subject_id_hash = $3",
		"consumed_at IS NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("consume query missing %q:\n%s", want, query)
		}
	}
}

// TestConsumeBootstrapCredentialNoMatchIsNoOp proves a repeat consume call
// (already consumed, or no row for this tenant/workspace/subject) reports
// consumed=false with no error, matching the "safe to call unconditionally on
// every login" contract.
func TestConsumeBootstrapCredentialNoMatchIsNoOp(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{fakeResultWithRowsAffected{rowsAffected: 0}}}
	store := NewIdentitySubjectStore(db)

	consumed, err := store.ConsumeBootstrapCredential(context.Background(), "tenant_local", "workspace_local", "sha256:non-bootstrap-subject", time.Now())
	if err != nil {
		t.Fatalf("ConsumeBootstrapCredential() error = %v", err)
	}
	if consumed {
		t.Fatal("ConsumeBootstrapCredential() consumed = true, want false for non-matching row")
	}
}

func TestResetBootstrapCredentialRegeneratesAndRotatesAtomically(t *testing.T) {
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
	resetAt := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)

	err := store.ResetBootstrapCredential(context.Background(), ResetBootstrapCredentialInput{
		TenantID:               "tenant_local",
		WorkspaceID:            "workspace_local",
		SealedCredential:       "ESK1.key2.nonce2.ciphertext2",
		KeyID:                  "key2",
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
	if len(db.execs) == 0 || !strings.Contains(db.execs[0].query, "pg_advisory_xact_lock(3456)") {
		t.Fatalf("reset did not acquire advisory lock 3456: %#v", db.execs)
	}
	if !fakeExecsContainQuery(db.execs, "reset_count = reset_count + 1") {
		t.Fatalf("reset execs missing reset_count increment: %#v", db.execs)
	}
	if !fakeExecsContainQuery(db.execs, "UPDATE identity_local_credentials") {
		t.Fatalf("reset execs missing bcrypt rotation: %#v", db.execs)
	}
	for _, exec := range db.execs {
		if fakeExecArgsContain(exec.args, "new-plaintext-password") {
			t.Fatalf("reset execs leaked plaintext: %#v", exec)
		}
	}
}

func TestResetBootstrapCredentialReturnsNotFoundWhenRowMissing(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	err := store.ResetBootstrapCredential(context.Background(), ResetBootstrapCredentialInput{
		TenantID:               "tenant_local",
		WorkspaceID:            "workspace_local",
		SealedCredential:       "ESK1.key2.nonce2.ciphertext2",
		KeyID:                  "key2",
		PasswordHash:           "bcrypt:new-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		MFAFactorID:            "id_new-recovery-factor",
		RecoveryCodeHash:       "sha256:new-recovery-code",
	})
	if !errors.Is(err, ErrBootstrapCredentialNotFound) {
		t.Fatalf("ResetBootstrapCredential() error = %v, want ErrBootstrapCredentialNotFound", err)
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
}

func TestBootstrapCredentialAADBindsTenantAndWorkspace(t *testing.T) {
	t.Parallel()

	aad := BootstrapCredentialAAD("tenant_a", "workspace_a")
	if got := string(aad); got != "eshu:onetime-admin:v1|tenant_a|workspace_a" {
		t.Fatalf("BootstrapCredentialAAD() = %q, unexpected", got)
	}
	if string(BootstrapCredentialAAD("tenant_a", "workspace_b")) == string(aad) {
		t.Fatal("BootstrapCredentialAAD() did not vary with workspace_id")
	}
	if string(BootstrapCredentialAAD("tenant_b", "workspace_a")) == string(aad) {
		t.Fatal("BootstrapCredentialAAD() did not vary with tenant_id")
	}
}

// fakeResultWithRowsAffected is a tiny sql.Result stand-in for tests that only
// need to control RowsAffected().
type fakeResultWithRowsAffected struct {
	rowsAffected int64
}

func (r fakeResultWithRowsAffected) LastInsertId() (int64, error) { return 0, nil }

func (r fakeResultWithRowsAffected) RowsAffected() (int64, error) { return r.rowsAffected, nil }
