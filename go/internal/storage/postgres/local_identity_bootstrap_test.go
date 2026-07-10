// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBootstrapLocalIdentityUsesTransactionLockAndHashOnlySecrets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{int64(0)}}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	err := store.BootstrapLocalIdentity(context.Background(), LocalIdentityBootstrapRecord{
		TenantID:               "tenant_local",
		WorkspaceID:            "workspace_local",
		UserID:                 "user_owner",
		SubjectIDHash:          "sha256:owner-subject",
		ProfileHandleHash:      "sha256:owner-handle",
		PasswordHash:           "bcrypt:owner-password-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		MFAFactorID:            "mfa_owner",
		MFAFactorKind:          "recovery_code",
		MFACredentialHandle:    "handle:owner-mfa",
		RecoveryCodeHashes:     []string{"sha256:recovery-a", "sha256:recovery-b"},
		PolicyRevisionHash:     "sha256:policy",
		CreatedAt:              now,
	})
	if err != nil {
		t.Fatalf("BootstrapLocalIdentity() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	if len(db.execs) == 0 || !strings.Contains(db.execs[0].query, "pg_advisory_xact_lock") {
		t.Fatalf("first exec did not acquire advisory transaction lock: %#v", db.execs)
	}
	for _, want := range []string{
		"INSERT INTO tenants",
		"INSERT INTO workspaces",
		"INSERT INTO identity_users",
		"INSERT INTO identity_local_credentials",
		"INSERT INTO identity_mfa_factors",
		"INSERT INTO identity_mfa_recovery_codes",
		"INSERT INTO identity_roles",
		"INSERT INTO identity_tenant_memberships",
		"INSERT INTO identity_membership_roles",
	} {
		if !fakeExecsContainQuery(db.execs, want) {
			t.Fatalf("bootstrap execs missing %q: %#v", want, db.execs)
		}
	}
	for _, exec := range db.execs {
		if fakeExecArgsContain(exec.args, "owner@example.test") || fakeExecArgsContain(exec.args, "plaintext") {
			t.Fatalf("bootstrap args leaked raw setup secret: %#v", exec.args)
		}
	}
}

func TestBootstrapLocalIdentityRejectsMissingAdminMFA(t *testing.T) {
	t.Parallel()

	store := NewIdentitySubjectStore(&fakeBeginnerExecQueryer{})
	err := store.BootstrapLocalIdentity(context.Background(), LocalIdentityBootstrapRecord{
		TenantID:               "tenant_local",
		WorkspaceID:            "workspace_local",
		UserID:                 "user_owner",
		SubjectIDHash:          "sha256:owner-subject",
		PasswordHash:           "bcrypt:owner-password-hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		PolicyRevisionHash:     "sha256:policy",
		CreatedAt:              time.Date(2026, 6, 22, 12, 5, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrLocalIdentityAdminMFARequired) {
		t.Fatalf("BootstrapLocalIdentity() error = %v, want ErrLocalIdentityAdminMFARequired", err)
	}
}

func TestBootstrapLocalIdentityRejectsSecondBootstrap(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{int64(1)}}}},
		},
	}
	store := NewIdentitySubjectStore(db)
	err := store.BootstrapLocalIdentity(context.Background(), completeBootstrapRecord())
	if !errors.Is(err, ErrLocalIdentityBootstrapCompleted) {
		t.Fatalf("BootstrapLocalIdentity() error = %v, want ErrLocalIdentityBootstrapCompleted", err)
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("transaction committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
}

func TestBootstrapLocalIdentityCountsOnlyLocalCredentialUsers(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{int64(0)}}}},
	}
	if _, err := countExistingLocalIdentityUsers(context.Background(), db); err != nil {
		t.Fatalf("countExistingLocalIdentityUsers() error = %v", err)
	}
	if got := db.queries[0].query; !strings.Contains(got, "JOIN identity_local_credentials") {
		t.Fatalf("bootstrap count query = %s, want local credential join", got)
	}
}
