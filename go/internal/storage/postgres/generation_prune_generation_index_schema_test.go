// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// TestGenerationPruneGenerationIndexMigration pins the #7419 index.
// graph_projection_phase_state buries generation_id fourth in its primary key,
// so the foreign-key cascade that every generation prune fires once per
// deleted generation scanned the whole table. The build must stay a sole
// CONCURRENTLY statement: the migration coordinator runs such a file in
// autocommit without the bootstrap lock_timeout (#7004), and PostgreSQL
// rejects a concurrent build inside a transaction block.
func TestGenerationPruneGenerationIndexMigration(t *testing.T) {
	t.Parallel()

	const (
		name  = "graph_projection_phase_state_generation_idx"
		table = "graph_projection_phase_state"
	)
	migration := MigrationSQL(name)
	if migration == "" {
		t.Fatalf("generation prune generation index migration %q missing", name)
	}
	normalized := strings.Join(strings.Fields(migration), " ")
	for _, fragment := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + name,
		"ON " + table + " (generation_id)",
	} {
		if !strings.Contains(normalized, strings.Join(strings.Fields(fragment), " ")) {
			t.Fatalf("generation prune migration %q missing %q", name, fragment)
		}
	}
	_, statement, found := strings.Cut(migration, "CREATE INDEX")
	if !found {
		t.Fatalf("generation prune migration %q has no CREATE INDEX statement", name)
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent index migration %q has %d SQL statements, want one", name, terminators)
	}
	if !coordination.IsSoleConcurrentIndexStatement(migration) {
		t.Fatalf("generation prune migration %q is not one isolated concurrent-index statement", name)
	}
}
