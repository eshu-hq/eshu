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

func TestGetSignInPolicyReturnsDefaultsWhenNoRowExists(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
	store := NewIdentitySubjectStore(db)

	policy, err := store.GetSignInPolicy(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("GetSignInPolicy() error = %v", err)
	}
	want := defaultSignInPolicy("tenant_a")
	if policy != want {
		t.Fatalf("GetSignInPolicy() = %+v, want %+v", policy, want)
	}
}

func TestGetSignInPolicyScansExistingRow(t *testing.T) {
	t.Parallel()

	verifiedAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{
			true, false, true,
			sql.NullInt64{Int64: 900, Valid: true},
			sql.NullInt64{Int64: 43200, Valid: true},
			sql.NullTime{Time: verifiedAt, Valid: true},
			sql.NullString{String: "pc_abc", Valid: true},
			"sha256:rev1",
			updatedAt,
		}}}},
	}
	store := NewIdentitySubjectStore(db)

	policy, err := store.GetSignInPolicy(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("GetSignInPolicy() error = %v", err)
	}
	if !policy.RequireSSO || policy.AllowLocalUserCreation || !policy.RequireMFAForAllUsers {
		t.Fatalf("GetSignInPolicy() boolean fields = %+v", policy)
	}
	if policy.IdleTimeoutSeconds != 900 || policy.AbsoluteTimeoutSeconds != 43200 {
		t.Fatalf("GetSignInPolicy() timeouts = %d/%d, want 900/43200", policy.IdleTimeoutSeconds, policy.AbsoluteTimeoutSeconds)
	}
	if !policy.SSOAdminVerifiedAt.Equal(verifiedAt) || policy.SSOAdminVerifiedProviderConfigID != "pc_abc" {
		t.Fatalf("GetSignInPolicy() sso proof = %+v", policy)
	}
	if policy.PolicyRevisionHash != "sha256:rev1" {
		t.Fatalf("GetSignInPolicy() policy_revision_hash = %q", policy.PolicyRevisionHash)
	}
}

// defaultLockedSignInPolicyRow returns the fake row shape for a
// never-configured tenant (all defaults, no SSO admin proof), as
// selectSignInPolicyForUpdateQuery would return it after
// ensureSignInPolicyRowQuery lazily materializes the row.
func defaultLockedSignInPolicyRow() []any {
	return []any{
		false, true, false,
		sql.NullInt64{},
		sql.NullInt64{},
		sql.NullTime{},
		sql.NullString{},
		"",
		time.Time{},
	}
}

func provenLockedSignInPolicyRow(verifiedAt time.Time) []any {
	return []any{
		false, true, false,
		sql.NullInt64{},
		sql.NullInt64{},
		sql.NullTime{Time: verifiedAt, Valid: true},
		sql.NullString{String: "pc_proven", Valid: true},
		"sha256:rev0", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
}

// alreadyRequireSSOLockedSignInPolicyRow returns the fake row shape for a
// tenant that already has require_sso=true persisted (both guardrails
// already proven), used to exercise an UpsertSignInPolicy call that edits an
// unrelated field without touching RequireSSO.
func alreadyRequireSSOLockedSignInPolicyRow(verifiedAt time.Time) []any {
	return []any{
		true, true, false,
		sql.NullInt64{},
		sql.NullInt64{},
		sql.NullTime{Time: verifiedAt, Valid: true},
		sql.NullString{String: "pc_proven", Valid: true},
		"sha256:rev0", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestUpsertSignInPolicyLocksAndCommitsSimpleUpdate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{defaultLockedSignInPolicyRow()}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	allowLocal := false
	policy, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		AllowLocalUserCreation: &allowLocal,
		PolicyRevisionHash:     "sha256:rev1",
		Now:                    now,
	})
	if err != nil {
		t.Fatalf("UpsertSignInPolicy() error = %v", err)
	}
	if policy.AllowLocalUserCreation {
		t.Fatalf("UpsertSignInPolicy() AllowLocalUserCreation = true, want false")
	}
	if policy.RequireSSO {
		t.Fatalf("UpsertSignInPolicy() RequireSSO = true, want unchanged false")
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	// selectSignInPolicyForUpdateQuery is a QueryContext call, not an exec;
	// assert the exec list instead contains the ensure + upsert statements.
	if !fakeExecsContainQuery(db.execs, "INSERT INTO identity_sign_in_policies (tenant_id)") {
		t.Fatalf("execs missing ensure-row insert: %#v", db.execs)
	}
	if !fakeExecsContainQuery(db.execs, "ON CONFLICT (tenant_id) DO UPDATE SET") {
		t.Fatalf("execs missing upsert: %#v", db.execs)
	}
}

func TestUpsertSignInPolicyRejectsRequireSSOWithoutActiveProvider(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{defaultLockedSignInPolicyRow()}}, // locked current row: no SSO proof
				{rows: [][]any{{int64(0)}}},                     // zero active providers
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	requireSSO := true
	_, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		RequireSSO:         &requireSSO,
		PolicyRevisionHash: "sha256:rev1",
		Now:                time.Now(),
	})
	if !errors.Is(err, ErrSignInPolicyGuardrailNoProvenProvider) {
		t.Fatalf("UpsertSignInPolicy() error = %v, want ErrSignInPolicyGuardrailNoProvenProvider", err)
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
}

func TestUpsertSignInPolicyRejectsRequireSSOWithoutSSOAdminProof(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{defaultLockedSignInPolicyRow()}}, // locked current row: no SSO proof
				{rows: [][]any{{int64(1)}}},                     // one active provider (proven)
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	requireSSO := true
	_, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		RequireSSO:         &requireSSO,
		PolicyRevisionHash: "sha256:rev1",
		Now:                time.Now(),
	})
	if !errors.Is(err, ErrSignInPolicyGuardrailNoSSOAdminProof) {
		t.Fatalf("UpsertSignInPolicy() error = %v, want ErrSignInPolicyGuardrailNoSSOAdminProof", err)
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
}

func TestUpsertSignInPolicyAllowsRequireSSOWhenBothGuardrailsProven(t *testing.T) {
	t.Parallel()

	verifiedAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{provenLockedSignInPolicyRow(verifiedAt)}}, // locked current row: SSO proven
				{rows: [][]any{{int64(1)}}},                              // one active provider
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	requireSSO := true
	policy, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		RequireSSO:         &requireSSO,
		PolicyRevisionHash: "sha256:rev1",
		Now:                time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSignInPolicy() error = %v", err)
	}
	if !policy.RequireSSO {
		t.Fatalf("UpsertSignInPolicy() RequireSSO = false, want true")
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
}

// Issue #5002 UpsertSignInPolicy unit tests (part 1 bulk-revoke, part 2
// merged timeout-ordering under the lock) live in
// identity_sign_in_policy_revoke_timeout_test.go, split out to keep this
// file under the repository's 500-line cap.

func TestRecordSSOAdminVerificationIssuesStickyUpsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)

	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := store.RecordSSOAdminVerification(context.Background(), "tenant_a", "pc_abc", at); err != nil {
		t.Fatalf("RecordSSOAdminVerification() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "COALESCE(identity_sign_in_policies.sso_admin_verified_at") {
		t.Fatalf("exec query missing sticky COALESCE: %s", db.execs[0].query)
	}
}

func TestRecordSSOAdminVerificationRejectsMissingFields(t *testing.T) {
	t.Parallel()

	store := NewIdentitySubjectStore(&fakeExecQueryer{})
	if err := store.RecordSSOAdminVerification(context.Background(), "", "pc_abc", time.Now()); err == nil {
		t.Fatal("RecordSSOAdminVerification() with empty tenant_id error = nil, want error")
	}
}

func TestAcceptLocalIdentityInvitationRejectsMissingMFAWhenPolicyRequiresIt(t *testing.T) {
	t.Parallel()

	acceptedAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{
					"invite_1", "tenant_a", "workspace_a", "developer", "sha256:policy",
				}}}, // selectLocalIdentityInvitation
				{rows: [][]any{{true}}}, // signInPolicyRequiresMFAForUsers: required
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	err := store.AcceptLocalIdentityInvitation(context.Background(), LocalIdentityInvitationAcceptance{
		InviteCodeHash:         "sha256:invite-code",
		UserID:                 "user_1",
		SubjectIDHash:          "sha256:subject",
		ProfileHandleHash:      "sha256:handle",
		PasswordHash:           "bcrypt:hash",
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: "sha256:bcrypt-cost",
		AcceptedAt:             acceptedAt,
	})
	if !errors.Is(err, ErrLocalIdentityMFARequiredByPolicy) {
		t.Fatalf("AcceptLocalIdentityInvitation() error = %v, want ErrLocalIdentityMFARequiredByPolicy", err)
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
}
