// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestCodeFingerprintBandEntityIndexMigration pins the #7254 index: the band
// delete statements filter on (repo_id, entity_id), and only the primary key
// carries entity_id (as its fourth column), so a generic plan reads the whole
// repository range. The build must stay a sole CONCURRENTLY statement: the
// migration coordinator runs such a file in autocommit without the bootstrap
// lock_timeout (#7004), and PostgreSQL rejects a concurrent build inside a
// transaction block.
func TestCodeFingerprintBandEntityIndexMigration(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL("code_fingerprint_band_entity_idx")
	if migration == "" {
		t.Fatal("code_fingerprint_band entity index migration missing")
	}
	normalized := strings.Join(strings.Fields(migration), " ")
	for _, fragment := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS code_fingerprint_band_entity_idx",
		"ON code_fingerprint_band (repo_id, entity_id)",
	} {
		if !strings.Contains(normalized, strings.Join(strings.Fields(fragment), " ")) {
			t.Fatalf("code_fingerprint_band entity index migration missing %q", fragment)
		}
	}
	_, statement, found := strings.Cut(migration, "CREATE INDEX")
	if !found {
		t.Fatal("code_fingerprint_band entity index migration has no CREATE INDEX statement")
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent index migration has %d SQL statements, want one", terminators)
	}
}
