// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"
	"time"
)

// Live proof for issue #7498: recording a failed local-identity attempt must
// prepare against real Postgres (the uncast CASE resolved $3 to text and was
// rejected with SQLSTATE 42804), count failures, and lock at the threshold.
func TestRecordFailedLocalIdentityAttemptLiveCountsAndLocks(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "local_identity_failed_attempt", "set ESHU_POSTGRES_DSN to run the local-identity failed-attempt #7498 proof")

	base := time.Now().UTC().Truncate(time.Millisecond)
	userID := "user-live-failed-attempt"
	if _, err := database.ExecContext(ctx, `
INSERT INTO identity_users (user_id, subject_id_hash, status, profile_handle_hash, created_at, updated_at, disabled_at, tombstoned_at)
VALUES ($1, $2, 'active', '', $3, $3, NULL, NULL)
`, userID, "sha256:live-failed-attempt", base); err != nil {
		t.Fatalf("seed identity user: %v", err)
	}

	store := NewIdentitySubjectStore(SQLDB{DB: database})
	var lockedAt time.Time
	for i := 0; i < defaultLocalIdentityLockoutThreshold; i++ {
		attemptAt := base.Add(time.Duration(i) * time.Second)
		result, err := store.recordFailedLocalIdentityAttempt(ctx, localIdentityCredentialRow{
			UserID:         userID,
			FailedAttempts: int64(i),
		}, attemptAt)
		if err != nil {
			t.Fatalf("recordFailedLocalIdentityAttempt() attempt %d error = %v", i+1, err)
		}
		if i < defaultLocalIdentityLockoutThreshold-1 {
			if result.Status != LocalIdentityAuthInvalid || result.Authenticated {
				t.Fatalf("attempt %d result = %#v, want invalid without a session", i+1, result)
			}
			if !result.LockedUntil.IsZero() {
				t.Fatalf("attempt %d LockedUntil = %v, want zero before the threshold", i+1, result.LockedUntil)
			}
			continue
		}
		if result.Status != LocalIdentityAuthLocked || result.Authenticated {
			t.Fatalf("attempt %d result = %#v, want locked without a session", i+1, result)
		}
		if result.LockedUntil.IsZero() {
			t.Fatalf("attempt %d LockedUntil is zero, want lockout window start", i+1)
		}
		lockedAt = result.LockedUntil
	}

	var failedAttempts int64
	var lockedUntil time.Time
	if err := database.QueryRowContext(ctx, `
SELECT failed_attempts, COALESCE(locked_until, 'epoch'::timestamptz)
FROM identity_local_auth_attempts
WHERE user_id = $1
`, userID).Scan(&failedAttempts, &lockedUntil); err != nil {
		t.Fatalf("read failed attempts: %v", err)
	}
	if failedAttempts != int64(defaultLocalIdentityLockoutThreshold) {
		t.Fatalf("failed_attempts = %d, want %d", failedAttempts, defaultLocalIdentityLockoutThreshold)
	}
	if lockedUntil.IsZero() || lockedUntil.Before(base) {
		t.Fatalf("locked_until = %v, want a timestamp at or after the attempts", lockedUntil)
	}
	if !lockedUntil.Equal(lockedAt) {
		t.Fatalf("locked_until = %v, want the locked result's %v", lockedUntil, lockedAt)
	}
}
