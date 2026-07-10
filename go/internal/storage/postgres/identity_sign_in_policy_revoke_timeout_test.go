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

// Issue #5002 UpsertSignInPolicy unit tests: part 1 (bulk-revoke local_user
// browser sessions on a require_sso false->true flip) and part 2 (merged
// idle/absolute timeout-ordering check under the FOR UPDATE lock, codex PR
// #5053 review). Split out of identity_sign_in_policy_test.go to keep that
// file under the repository's 500-line cap; shared row-fixture helpers
// (defaultLockedSignInPolicyRow, provenLockedSignInPolicyRow,
// alreadyRequireSSOLockedSignInPolicyRow) and fakeExecsContainQuery stay in
// that file / local_identity_test.go — same package, so they are directly
// callable here.

// TestUpsertSignInPolicyRevokesLocalBrowserSessionsWhenRequireSSOBecomesTrue
// proves issue #5002 part 1: a require_sso false->true flip must bulk-revoke
// every active password-authenticated ("local_user") browser session for the
// tenant, in the SAME transaction as the policy write, so a session issued
// before the flip can never be resolved after it.
func TestUpsertSignInPolicyRevokesLocalBrowserSessionsWhenRequireSSOBecomesTrue(t *testing.T) {
	t.Parallel()

	verifiedAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{provenLockedSignInPolicyRow(verifiedAt)}}, // locked current row: SSO proven, RequireSSO currently false
				{rows: [][]any{{int64(1)}}},                              // one active provider
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	requireSSO := true
	_, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		RequireSSO:         &requireSSO,
		PolicyRevisionHash: "sha256:rev1",
		Now:                now,
	})
	if err != nil {
		t.Fatalf("UpsertSignInPolicy() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}

	upsertIdx, revokeIdx := -1, -1
	for i, exec := range db.execs {
		if strings.Contains(exec.query, "ON CONFLICT (tenant_id) DO UPDATE SET") {
			upsertIdx = i
		}
		if strings.Contains(exec.query, "AND subject_class = 'local_user'") {
			revokeIdx = i
			if len(exec.args) != 2 {
				t.Fatalf("revoke exec args = %#v, want [tenant_id, revoked_at]", exec.args)
			}
			if exec.args[0] != "tenant_a" {
				t.Fatalf("revoke exec tenant_id arg = %v, want tenant_a", exec.args[0])
			}
			if exec.args[1] != now {
				t.Fatalf("revoke exec revoked_at arg = %v, want %v", exec.args[1], now)
			}
		}
	}
	if upsertIdx == -1 {
		t.Fatalf("execs missing policy upsert: %#v", db.execs)
	}
	if revokeIdx == -1 {
		t.Fatalf("execs missing local-user bulk revoke: %#v", db.execs)
	}
	if revokeIdx < upsertIdx {
		t.Fatalf("revoke exec (idx %d) ran before policy upsert (idx %d), want after: %#v", revokeIdx, upsertIdx, db.execs)
	}
}

// TestUpsertSignInPolicyDoesNotRevokeSessionsWhenRequireSSOStaysFalse proves
// an update that never sets require_sso true (e.g. editing an unrelated
// field on a false->false tenant) issues no bulk revoke.
func TestUpsertSignInPolicyDoesNotRevokeSessionsWhenRequireSSOStaysFalse(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{defaultLockedSignInPolicyRow()}}},
		},
	}
	store := NewIdentitySubjectStore(db)

	allowLocal := false
	_, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		AllowLocalUserCreation: &allowLocal,
		PolicyRevisionHash:     "sha256:rev1",
		Now:                    time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSignInPolicy() error = %v", err)
	}
	if fakeExecsContainQuery(db.execs, "AND subject_class = 'local_user'") {
		t.Fatalf("execs unexpectedly contain local-user bulk revoke: %#v", db.execs)
	}
}

// TestUpsertSignInPolicyRevokeIsUnconditionalWhenRequireSSOAlreadyTrue proves
// the bulk revoke runs on the RESULTING require_sso=true regardless of the
// PRIOR value: an update that edits an unrelated field on an already-true
// tenant still issues the (idempotent) revoke, matching the "unconditional on
// resulting value" design so no caller needs to special-case "already true".
func TestUpsertSignInPolicyRevokeIsUnconditionalWhenRequireSSOAlreadyTrue(t *testing.T) {
	t.Parallel()

	verifiedAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{alreadyRequireSSOLockedSignInPolicyRow(verifiedAt)}}, // locked current row: already require_sso=true
				{rows: [][]any{{int64(1)}}},                                         // one active provider
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	requireMFA := true
	_, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		RequireMFAForAllUsers: &requireMFA,
		PolicyRevisionHash:    "sha256:rev2",
		Now:                   time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSignInPolicy() error = %v", err)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
	if !fakeExecsContainQuery(db.execs, "AND subject_class = 'local_user'") {
		t.Fatalf("execs missing local-user bulk revoke on already-true policy: %#v", db.execs)
	}
}

// lockedSignInPolicyRowWithTimeouts returns the fake locked-row shape for a
// tenant whose stored idle/absolute timeouts are already set to the given
// non-zero values (0 means "unset" on either argument), require_sso=false, no
// SSO proof needed. Used to exercise UpsertSignInPolicy's merged (locked
// stored + incoming update) timeout-ordering check (issue #5002 part 2,
// codex PR #5053 review).
func lockedSignInPolicyRowWithTimeouts(idleSeconds, absoluteSeconds int) []any {
	idle := sql.NullInt64{}
	if idleSeconds != 0 {
		idle = sql.NullInt64{Int64: int64(idleSeconds), Valid: true}
	}
	absolute := sql.NullInt64{}
	if absoluteSeconds != 0 {
		absolute = sql.NullInt64{Int64: int64(absoluteSeconds), Valid: true}
	}
	return []any{
		false, true, false,
		idle,
		absolute,
		sql.NullTime{},
		sql.NullString{},
		"sha256:rev0", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestUpsertSignInPolicyRejectsMergedAbsoluteBelowLockedIdle proves issue
// #5002 part 2 (codex PR #5053 review, root-caused): a PATCH setting ONLY
// absolute_timeout_seconds, below the tenant's ALREADY-STORED (locked)
// idle_timeout_seconds, is rejected UNDER THE LOCK — not via a racy
// pre-transaction read — so two concurrent partial PATCHes can never both
// see the same stale stored value and both commit an inconsistent row.
func TestUpsertSignInPolicyRejectsMergedAbsoluteBelowLockedIdle(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{lockedSignInPolicyRowWithTimeouts(3600, 0)}}, // locked current row: stored idle=3600
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	absolute := 1800
	_, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		AbsoluteTimeoutSeconds: &absolute,
		PolicyRevisionHash:     "sha256:rev1",
		Now:                    time.Now(),
	})
	if !errors.Is(err, ErrSignInPolicyTimeoutOrdering) {
		t.Fatalf("UpsertSignInPolicy() error = %v, want ErrSignInPolicyTimeoutOrdering", err)
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
	if fakeExecsContainQuery(db.execs, "ON CONFLICT (tenant_id) DO UPDATE SET") {
		t.Fatalf("execs unexpectedly contain the policy upsert on a rejected timeout ordering: %#v", db.execs)
	}
}

// TestUpsertSignInPolicyAllowsMergedIdleBelowLockedAbsolute proves the
// symmetric valid case: lowering idle_timeout_seconds below the tenant's
// stored absolute_timeout_seconds is accepted (the merged pair stays valid).
func TestUpsertSignInPolicyAllowsMergedIdleBelowLockedAbsolute(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{lockedSignInPolicyRowWithTimeouts(0, 7200)}}, // locked current row: stored absolute=7200
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	idle := 1800
	policy, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		IdleTimeoutSeconds: &idle,
		PolicyRevisionHash: "sha256:rev1",
		Now:                time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSignInPolicy() error = %v", err)
	}
	if policy.IdleTimeoutSeconds != 1800 || policy.AbsoluteTimeoutSeconds != 7200 {
		t.Fatalf("UpsertSignInPolicy() timeouts = %d/%d, want 1800/7200", policy.IdleTimeoutSeconds, policy.AbsoluteTimeoutSeconds)
	}
	if !db.committed || db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want commit only", db.committed, db.rolledBack)
	}
}

// TestUpsertSignInPolicyBothFieldsConflictingInOneUpdateStillRejected proves
// the ordering check still catches a same-request conflict (both fields in
// one update), independent of any stored value (locked row has no timeouts
// set at all).
func TestUpsertSignInPolicyBothFieldsConflictingInOneUpdateStillRejected(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{defaultLockedSignInPolicyRow()}},
			},
		},
	}
	store := NewIdentitySubjectStore(db)

	idle, absolute := 3600, 1800
	_, err := store.UpsertSignInPolicy(context.Background(), "tenant_a", SignInPolicyUpdate{
		IdleTimeoutSeconds:     &idle,
		AbsoluteTimeoutSeconds: &absolute,
		PolicyRevisionHash:     "sha256:rev1",
		Now:                    time.Now(),
	})
	if !errors.Is(err, ErrSignInPolicyTimeoutOrdering) {
		t.Fatalf("UpsertSignInPolicy() error = %v, want ErrSignInPolicyTimeoutOrdering", err)
	}
	if db.committed || !db.rolledBack {
		t.Fatalf("committed=%t rolledBack=%t, want rollback only", db.committed, db.rolledBack)
	}
}
