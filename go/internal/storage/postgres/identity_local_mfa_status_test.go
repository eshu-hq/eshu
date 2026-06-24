// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
	"time"
)

// TestGetLocalIdentityMFAStatusQueryUsesConsistentAsOfBoundary verifies that
// both the has_active_mfa EXISTS subquery and the factor_kind subquery apply
// the same created_at <= $2 boundary so the two values are always consistent.
func TestGetLocalIdentityMFAStatusQueryUsesConsistentAsOfBoundary(t *testing.T) {
	t.Parallel()

	q := getLocalIdentityMFAStatusQuery

	// Both branches must reference the asOf parameter ($2).
	count := strings.Count(q, "created_at <= $2")
	if count != 2 {
		t.Errorf("expected 2 occurrences of 'created_at <= $2' (one per branch), got %d", count)
	}

	// Security: no credential handles or recovery hashes in the query.
	for _, forbidden := range []string{
		"credential_handle",
		"recovery_code_hash",
		"password_hash",
		"token_hash",
	} {
		if strings.Contains(q, forbidden) {
			t.Errorf("getLocalIdentityMFAStatusQuery must not expose %q", forbidden)
		}
	}

	// factor_kind is the only non-boolean output — must be present.
	if !strings.Contains(q, "factor_kind") {
		t.Error("getLocalIdentityMFAStatusQuery must select factor_kind")
	}
}

// TestIdentitySubjectStoreGetLocalIdentityMFAStatusNilDatabase verifies that
// GetLocalIdentityMFAStatus with a nil database returns an error.
func TestIdentitySubjectStoreGetLocalIdentityMFAStatusNilDatabase(t *testing.T) {
	t.Parallel()

	store := &IdentitySubjectStore{db: nil}
	_, err := store.GetLocalIdentityMFAStatus(nil, "subject-hash", time.Now()) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error for nil database, got nil")
	}
}

// TestIdentitySubjectStoreGetLocalIdentityMFAStatusRejectsBlankInputs verifies
// that blank subject hash and zero asOf are both rejected without touching the
// database — consistent with the sessions and tokens store contract.
func TestIdentitySubjectStoreGetLocalIdentityMFAStatusRejectsBlankInputs(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := &IdentitySubjectStore{db: db}
	now := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)

	if _, err := store.GetLocalIdentityMFAStatus(nil, "", now); err == nil { //nolint:staticcheck
		t.Fatal("expected error for blank subject hash")
	}
	if _, err := store.GetLocalIdentityMFAStatus(nil, "subject-hash", time.Time{}); err == nil { //nolint:staticcheck
		t.Fatal("expected error for zero asOf")
	}
}
