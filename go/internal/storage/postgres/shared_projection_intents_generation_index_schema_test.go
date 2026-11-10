// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// TestSharedProjectionIntentsGenerationIndexMigration pins the #10512 index.
// The retention row-count arm and the intents DELETE both probe
// shared_projection_intents by generation_id alone; without a
// generation-leading index each is a sequential scan of the whole table
// (measured 56.9 s isolated on QA, 11.5M rows). The build must stay a sole
// CONCURRENTLY statement: the migration coordinator runs such a file in
// autocommit without the bootstrap lock_timeout (#7004), and PostgreSQL
// rejects a concurrent build inside a transaction block.
func TestSharedProjectionIntentsGenerationIndexMigration(t *testing.T) {
	t.Parallel()

	const (
		name  = "shared_projection_intents_generation_idx"
		table = "shared_projection_intents"
	)
	migration := MigrationSQL(name)
	normalized := strings.Join(strings.Fields(migration), " ")
	for _, fragment := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + name,
		"ON " + table + " (generation_id)",
	} {
		if !strings.Contains(normalized, strings.Join(strings.Fields(fragment), " ")) {
			t.Fatalf("intents generation migration %q missing %q", name, fragment)
		}
	}
	_, statement, found := strings.Cut(migration, "CREATE INDEX")
	if !found {
		t.Fatalf("intents generation migration %q has no CREATE INDEX statement", name)
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent index migration %q has %d SQL statements, want one", name, terminators)
	}
	if !coordination.IsSoleConcurrentIndexStatement(migration) {
		t.Fatalf("intents generation migration %q is not one isolated concurrent-index statement", name)
	}
}
