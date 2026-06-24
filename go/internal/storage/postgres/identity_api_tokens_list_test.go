// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
	"time"
)

// TestIdentitySubjectStoreListAPITokensBySubjectQuerySecurity verifies that the
// SQL for listing a caller's own API tokens by subject hash selects only
// metadata columns (no token_hash, no token value, no password material) and
// that the query is scoped by subject identity.
func TestIdentitySubjectStoreListAPITokensBySubjectQuerySecurity(t *testing.T) {
	t.Parallel()

	q := listLocalIdentityAPITokensBySubjectQuery

	for _, want := range []string{
		"token_id",
		"token_class",
		"issued_at",
		"expires_at",
		"revoked_at",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("listLocalIdentityAPITokensBySubjectQuery missing %q", want)
		}
	}

	// Security: raw token hash and display_handle_hash must NOT appear in the
	// list response query. display_handle_hash is SHA-256(display_label) — a
	// hash rendered as a "label" is misleading and was removed (see #3703).
	for _, forbidden := range []string{
		"token_hash",
		"password_hash",
		"display_handle_hash",
	} {
		if strings.Contains(q, forbidden) {
			t.Errorf("listLocalIdentityAPITokensBySubjectQuery must not expose %q", forbidden)
		}
	}

	// Query must be parameterised by subject.
	if !strings.Contains(q, "$1") {
		t.Error("listLocalIdentityAPITokensBySubjectQuery must accept a subject parameter")
	}
}

// TestIdentitySubjectStoreListAPITokensBySubjectNilDatabase verifies that
// ListAPITokensBySubject with a nil database returns an error.
func TestIdentitySubjectStoreListAPITokensBySubjectNilDatabase(t *testing.T) {
	t.Parallel()

	store := &IdentitySubjectStore{db: nil}
	_, err := store.ListAPITokensBySubject(nil, "", time.Now()) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error for nil database, got nil")
	}
}

// TestIdentitySubjectStoreListAPITokensBySubjectRejectsBlankInputs verifies that
// blank subject hash or zero asOf are rejected before touching the database.
func TestIdentitySubjectStoreListAPITokensBySubjectRejectsBlankInputs(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := &IdentitySubjectStore{db: db}
	now := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)

	if _, err := store.ListAPITokensBySubject(nil, "", now); err == nil { //nolint:staticcheck
		t.Fatal("expected error for blank subject hash")
	}
	if _, err := store.ListAPITokensBySubject(nil, "subject-hash", time.Time{}); err == nil { //nolint:staticcheck
		t.Fatal("expected error for zero asOf")
	}
}
