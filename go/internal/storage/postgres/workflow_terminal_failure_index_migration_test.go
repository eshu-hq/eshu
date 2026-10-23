// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

const (
	// workflowTerminalFailureMigrationName is the migration definition name
	// of 211_workflow_work_items_failed_terminal_instance_idx.sql.
	workflowTerminalFailureMigrationName = "workflow_work_items_failed_terminal_instance_idx"
	// workflowTerminalFailureIndexName is the partial index that migration
	// builds; the live plan proof asserts the status read uses it.
	workflowTerminalFailureIndexName = "workflow_work_items_failed_terminal_instance_updated_idx"
)

// TestWorkflowTerminalFailureIndexMigration pins the #8803 index that serves
// the collector terminal-failure status read. It must be a partial index on
// failed_terminal rows keyed (kind, instance, updated_at DESC, work_item_id)
// so the windowed read seeks the newest rows of one instance in ORDER BY order
// without a sort, and it must stay a sole CONCURRENTLY statement: the
// migration coordinator runs such a file in autocommit without the bootstrap
// lock_timeout (#7004), and PostgreSQL rejects a concurrent build inside a
// transaction block. It is migration-only, like migration 156: a twin in
// workflowControlSchemaSQL would either lock the hot queue table at
// coordinator start or fail inside the bootstrap transaction.
func TestWorkflowTerminalFailureIndexMigration(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL(workflowTerminalFailureMigrationName)
	normalized := strings.Join(strings.Fields(migration), " ")
	for _, fragment := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + workflowTerminalFailureIndexName,
		"ON workflow_work_items (collector_kind, collector_instance_id, updated_at DESC, work_item_id ASC)",
		"WHERE status = 'failed_terminal';",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("terminal failure index migration missing %q:\n%s", fragment, migration)
		}
	}
	_, statement, found := strings.Cut(migration, "CREATE INDEX")
	if !found {
		t.Fatalf("terminal failure index migration has no CREATE INDEX statement")
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent index migration has %d SQL statements, want one", terminators)
	}
	if !coordination.IsSoleConcurrentIndexStatement(migration) {
		t.Fatalf("terminal failure index migration is not one isolated concurrent-index statement")
	}
	if strings.Contains(workflowControlSchemaSQL, workflowTerminalFailureIndexName) {
		t.Fatalf("workflowControlSchemaSQL carries %s; it must stay migration-only", workflowTerminalFailureIndexName)
	}
}
