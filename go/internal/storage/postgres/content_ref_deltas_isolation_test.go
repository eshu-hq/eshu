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

// TestRefDeltaGuardCatchesPlantedCanonicalUpsert plants every canonical
// write shape the guard claims (plain, schema-qualified, and ONLY forms) so
// the guard cannot pass on verb coverage it does not have. Each shape gets
// its own guard call: the guard reports only the first match in a slice, so
// sharing one slice would leave every shape after the first unproven.
func TestRefDeltaGuardCatchesPlantedCanonicalUpsert(t *testing.T) {
	t.Parallel()

	planted := map[string]string{
		"plain INSERT":            "INSERT INTO content_files (repo_id) VALUES ('repo-1') ON CONFLICT DO NOTHING",
		"plain UPDATE":            "UPDATE content_files SET content = 'x' WHERE repo_id = 'repo-1'",
		"schema-qualified INSERT": "INSERT INTO public.content_files (repo_id) VALUES ('repo-1')",
		"quoted schema INSERT":    `INSERT INTO "public"."content_files" (repo_id) VALUES ('repo-1')`,
		"UPDATE ONLY":             "UPDATE ONLY content_files SET content = 'x' WHERE repo_id = 'repo-1'",
		"DELETE FROM ONLY":        "DELETE FROM ONLY content_files WHERE repo_id = 'repo-1'",
		"TRUNCATE":                "TRUNCATE content_files",
		"ALTER TABLE ADD COLUMN":  "ALTER TABLE content_files ADD COLUMN ref TEXT",
		"ALTER TABLE schema-qual": "ALTER TABLE public.content_files ADD COLUMN ref TEXT",
		"DROP TABLE":              "DROP TABLE content_files",
		"both-names INSERT":       "INSERT INTO content_files SELECT * FROM content_file_ref_deltas",
		"both-names UPDATE":       "UPDATE content_files SET content = o.content FROM content_file_ref_deltas o WHERE o.repo_id = 'repo-1'",
	}
	for name, shape := range planted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := assertRefDeltaStatementsNeverWriteCanonical([]string{shape})
			if err == nil {
				t.Fatalf("guard = nil, want an error for planted canonical write %q", shape)
			}
			if !strings.Contains(err.Error(), "content_files") {
				t.Fatalf("error %v does not name the canonical table", err)
			}
		})
	}
}

// TestRefDeltaGuardIgnoresOverlayWrites pins the guard's false-positive
// boundary: overlay-table writes in the same shapes the canonical plants
// use (plain, schema-qualified, ONLY) must pass the guard.
func TestRefDeltaGuardIgnoresOverlayWrites(t *testing.T) {
	t.Parallel()

	overlay := map[string]string{
		"plain INSERT":                    "INSERT INTO content_file_ref_deltas (repo_id) VALUES ('repo-1')",
		"schema-qualified INSERT":         "INSERT INTO public.content_file_ref_deltas (repo_id) VALUES ('repo-1')",
		"UPDATE ONLY":                     "UPDATE ONLY content_file_ref_deltas SET content = 'x'",
		"overlay write reading canonical": "INSERT INTO content_file_ref_deltas (repo_id) SELECT repo_id FROM content_files",
	}
	for name, shape := range overlay {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := assertRefDeltaStatementsNeverWriteCanonical([]string{shape}); err != nil {
				t.Fatalf("guard = %v, want nil for overlay-table write %q", err, shape)
			}
		})
	}
}
