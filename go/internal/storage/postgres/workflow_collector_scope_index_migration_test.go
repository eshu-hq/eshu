// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

const (
	// workflowCollectorScopeMigrationName is the migration definition name of
	// 230_workflow_work_items_collector_scope_updated_idx.sql.
	workflowCollectorScopeMigrationName = "workflow_work_items_collector_scope_updated_idx"
	// workflowCollectorScopeIndexName is the index that migration builds; the
	// live plan proof asserts the security-alert latest-work-item read uses it.
	workflowCollectorScopeIndexName = "workflow_work_items_collector_scope_updated_idx"
)

// TestWorkflowCollectorScopeIndexMigration pins the #8840 index that serves
// the security-alert collection-state read's latest-work-item lateral. It must
// be keyed (collector_kind, scope_id, updated_at DESC, work_item_id ASC), the
// read's equality prefix followed by its exact ORDER BY, so the lateral seeks
// one entry per target instead of scanning and sorting the target's whole
// history. It must stay a sole CONCURRENTLY statement and migration-only, for
// the same reasons as migration 211 (see
// TestWorkflowTerminalFailureIndexMigration).
func TestWorkflowCollectorScopeIndexMigration(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL(workflowCollectorScopeMigrationName)
	normalized := strings.Join(strings.Fields(migration), " ")
	for _, fragment := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + workflowCollectorScopeIndexName,
		"ON workflow_work_items (collector_kind, scope_id, updated_at DESC, work_item_id ASC);",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("collector scope index migration missing %q:\n%s", fragment, migration)
		}
	}
	_, statement, found := strings.Cut(migration, "CREATE INDEX")
	if !found {
		t.Fatalf("collector scope index migration has no CREATE INDEX statement")
	}
	if strings.Contains(statement, " WHERE ") {
		t.Fatalf("collector scope index must not be partial: every collector kind's latest-item read uses it:\n%s", statement)
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent index migration has %d SQL statements, want one", terminators)
	}
	if !coordination.IsSoleConcurrentIndexStatement(migration) {
		t.Fatalf("collector scope index migration is not one isolated concurrent-index statement")
	}
	if strings.Contains(workflowControlSchemaSQL, workflowCollectorScopeIndexName) {
		t.Fatalf("workflowControlSchemaSQL carries %s; it must stay migration-only", workflowCollectorScopeIndexName)
	}
}
