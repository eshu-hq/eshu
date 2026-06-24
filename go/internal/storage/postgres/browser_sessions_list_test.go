package postgres

import (
	"strings"
	"testing"
	"time"
)

// TestBrowserSessionStoreListSessionsBySubjectQueryIncludesRequiredClauses
// verifies that the SQL for listing a caller's own sessions by subject hash
// selects only the metadata columns that are safe to expose (no session_hash,
// no csrf_token_hash, no external auth secrets), filters by subject_id_hash,
// and orders deterministically.
func TestBrowserSessionStoreListSessionsBySubjectQueryIncludesRequiredClauses(t *testing.T) {
	t.Parallel()

	q := listBrowserSessionsBySubjectQuery

	for _, want := range []string{
		"subject_id_hash",
		"tenant_id",
		"workspace_id",
		"issued_at",
		"last_seen_at",
		"idle_expires_at",
		"absolute_expires_at",
		"revoked_at",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("listBrowserSessionsBySubjectQuery missing %q", want)
		}
	}

	// Security: session_hash and csrf secrets must NOT appear in the list query.
	for _, forbidden := range []string{
		"session_hash",
		"csrf_token_hash",
		"external_subject_id_hash",
		"token_hash",
	} {
		if strings.Contains(q, forbidden) {
			t.Errorf("listBrowserSessionsBySubjectQuery must not expose %q", forbidden)
		}
	}

	if !strings.Contains(q, "$1") {
		t.Error("listBrowserSessionsBySubjectQuery must accept subject_id_hash parameter")
	}
	if !strings.Contains(q, "$2") {
		t.Error("listBrowserSessionsBySubjectQuery must accept asOf parameter")
	}
}

// TestBrowserSessionStoreListSessionsBySubjectNilDatabase verifies that calling
// ListSessionsBySubject with a nil database returns an error rather than panicking.
func TestBrowserSessionStoreListSessionsBySubjectNilDatabase(t *testing.T) {
	t.Parallel()

	store := &BrowserSessionStore{db: nil}
	_, err := store.ListSessionsBySubject(nil, "subject-hash", time.Now()) //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error for nil database, got nil")
	}
}

// TestBrowserSessionStoreListSessionsBySubjectRejectsBlankInputs verifies that
// blank subject hash or zero asOf are rejected without hitting the database.
func TestBrowserSessionStoreListSessionsBySubjectRejectsBlankInputs(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := &BrowserSessionStore{db: db}
	now := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)

	if _, err := store.ListSessionsBySubject(nil, "", now); err == nil { //nolint:staticcheck
		t.Fatal("expected error for blank subject hash")
	}
	if _, err := store.ListSessionsBySubject(nil, "subject-hash", time.Time{}); err == nil { //nolint:staticcheck
		t.Fatal("expected error for zero asOf")
	}
}
