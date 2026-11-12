// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// TestFactWorkItemsGenerationIndexMigration pins the #10525 index. The
// retention row-count fact_work_items arm and the pre-delete work-item
// lock both probe fact_work_items by generation_id alone; the only
// existing candidate index leads with scope_id, so each probe scans the
// whole table (measured 158 ms and 33,976 buffers for a 100-generation
// QA batch generic plan, against 1.3 ms and 316 buffers behind this
// index; docs/internal/evidence/10525-retention-planning-count.md). The
// build must stay a sole CONCURRENTLY statement: the migration
// coordinator runs such a file in autocommit without the bootstrap
// lock_timeout (#7004), and PostgreSQL rejects a concurrent build inside
// a transaction block.
func TestFactWorkItemsGenerationIndexMigration(t *testing.T) {
	t.Parallel()

	const (
		name  = "fact_work_items_generation_idx"
		table = "fact_work_items"
	)
	migration := MigrationSQL(name)
	normalized := strings.Join(strings.Fields(migration), " ")
	for _, fragment := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + name,
		"ON " + table + " (generation_id)",
	} {
		if !strings.Contains(normalized, strings.Join(strings.Fields(fragment), " ")) {
			t.Fatalf("work items generation migration %q missing %q", name, fragment)
		}
	}
	_, statement, found := strings.Cut(migration, "CREATE INDEX")
	if !found {
		t.Fatalf("work items generation migration %q has no CREATE INDEX statement", name)
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent index migration %q has %d SQL statements, want one", name, terminators)
	}
	if !coordination.IsSoleConcurrentIndexStatement(migration) {
		t.Fatalf("work items generation migration %q is not one isolated concurrent-index statement", name)
	}
}

// TestRetentionWorkArmDerivedFromShippedConstant is the hermetic guard
// for the #10525 exactness differential's oracle: the derived statement
// must be built from the shipped constant itself — its CTE head a
// byte-prefix of production, its arm a byte-substring after that head —
// never a hand-copied frozen query that could drift false-green.
func TestRetentionWorkArmDerivedFromShippedConstant(t *testing.T) {
	t.Parallel()

	prefix, arm, ok := derivedRetentionWorkArmParts()
	if !ok {
		t.Fatal("derive work arm from shipped constant: markers not found")
	}
	if !strings.HasPrefix(generationRetentionRowCountsQuery, prefix) {
		t.Fatal("derived CTE head is not a byte-prefix of the shipped constant")
	}
	if !strings.Contains(generationRetentionRowCountsQuery[len(prefix):], arm) {
		t.Fatal("derived arm is not a byte-substring of the shipped constant after its head")
	}
	if !strings.HasPrefix(arm, retentionWorkArmMarker) {
		t.Fatalf("derived arm does not start at the work arm marker:\n%s", arm)
	}
}
