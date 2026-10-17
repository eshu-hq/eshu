// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestRefDeltaStatementsNeverWriteCanonical is the GREEN half of the #5393
// Phase 1 key-invariant guard: a delta row must never shadow canonical
// content_files, and canonical content stays default-branch-only
// byte-identical. It runs the guard over every SQL statement the overlay
// store can emit plus the migration that creates its tables.
func TestRefDeltaStatementsNeverWriteCanonical(t *testing.T) {
	t.Parallel()

	statements := append([]string{MigrationSQL("content_file_ref_deltas")}, refDeltaStatementInventory()...)
	if len(statements) == 0 {
		t.Fatal("ref-delta statement inventory is empty; the guard would pass vacuously")
	}
	if err := assertRefDeltaStatementsNeverWriteCanonical(statements); err != nil {
		t.Fatalf("ref-delta SQL writes canonical content: %v", err)
	}
}

// TestRefDeltaGuardCatchesPlantedCanonicalWrite is the seeded-violation RED
// half of the guard: a planted write to content_files must fail the guard
// and name the table. If this test ever goes green on a planted violation,
// the guard in assertRefDeltaStatementsNeverWriteCanonical is broken.
func TestRefDeltaGuardCatchesPlantedCanonicalWrite(t *testing.T) {
	t.Parallel()

	planted := []string{
		"DELETE FROM content_files WHERE repo_id = 'repo-1'",
	}
	err := assertRefDeltaStatementsNeverWriteCanonical(planted)
	if err == nil {
		t.Fatal("guard = nil, want an error for a planted canonical write")
	}
	if !strings.Contains(err.Error(), "content_files") {
		t.Fatalf("error %v does not name the canonical table", err)
	}
}

// TestRefDeltaGuardCatchesPlantedCanonicalUpsert plants the other write
// shapes (INSERT, UPDATE) so the guard cannot pass on verb coverage it does
// not have.
func TestRefDeltaGuardCatchesPlantedCanonicalUpsert(t *testing.T) {
	t.Parallel()

	planted := []string{
		"INSERT INTO content_files (repo_id) VALUES ('repo-1') ON CONFLICT DO NOTHING",
		"UPDATE content_files SET content = 'x' WHERE repo_id = 'repo-1'",
	}
	err := assertRefDeltaStatementsNeverWriteCanonical(planted)
	if err == nil {
		t.Fatal("guard = nil, want an error for planted canonical writes")
	}
	if !strings.Contains(err.Error(), "content_files") {
		t.Fatalf("error %v does not name the canonical table", err)
	}
}
