// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

const (
	// workflowSecurityAlertScopeMigrationName is the migration definition name
	// of 231_workflow_work_items_security_alert_scope_updated_idx.sql.
	workflowSecurityAlertScopeMigrationName = "workflow_work_items_security_alert_scope_updated_idx"
	// workflowSecurityAlertScopeIndexName is the index that migration builds;
	// the live plan proofs assert the security-alert latest-work-item read
	// uses it and the work-item retention prune does not.
	workflowSecurityAlertScopeIndexName = "workflow_work_items_security_alert_scope_updated_idx"
)

// TestWorkflowSecurityAlertScopeIndexMigration pins the #8840 index that
// serves the security-alert collection-state read's latest-work-item lateral.
// It must be keyed (scope_id, updated_at DESC, work_item_id ASC), the read's
// equality column followed by its exact ORDER BY, so the lateral seeks one
// entry per target instead of scanning and sorting the target's whole history.
//
// It must be partial on collector_kind = 'security_alert'. The lateral's
// literal predicate proves that at plan time. The work-item retention prune's
// "newer row" probe (retention/work/items) matches collector_kind against the
// outer row, which the planner cannot prove, so the prune keeps its
// generation-index plan. A full (collector_kind, scope_id, updated_at DESC)
// key let the prune walk every newer row whose status differs (#8840 review:
// 274 ms to 2,077 ms per 2,000-row batch on a recovered-target seed).
//
// It must stay a sole CONCURRENTLY statement and migration-only, for the same
// reasons as migration 211 (see TestWorkflowTerminalFailureIndexMigration).
func TestWorkflowSecurityAlertScopeIndexMigration(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL(workflowSecurityAlertScopeMigrationName)
	normalized := strings.Join(strings.Fields(migration), " ")
	for _, fragment := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + workflowSecurityAlertScopeIndexName,
		"ON workflow_work_items (scope_id, updated_at DESC, work_item_id ASC) WHERE collector_kind = 'security_alert';",
	} {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("security-alert scope index migration missing %q:\n%s", fragment, migration)
		}
	}
	_, statement, found := strings.Cut(migration, "CREATE INDEX")
	if !found {
		t.Fatalf("security-alert scope index migration has no CREATE INDEX statement")
	}
	key, _, _ := strings.Cut(statement, "WHERE")
	if strings.Contains(key, "collector_kind") {
		t.Fatalf("security-alert scope index key must not carry collector_kind; the retention prune's correlated probe could then use it:\n%s", statement)
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent index migration has %d SQL statements, want one", terminators)
	}
	if !coordination.IsSoleConcurrentIndexStatement(migration) {
		t.Fatalf("security-alert scope index migration is not one isolated concurrent-index statement")
	}
	if strings.Contains(workflowControlSchemaSQL, workflowSecurityAlertScopeIndexName) {
		t.Fatalf("workflowControlSchemaSQL carries %s; it must stay migration-only", workflowSecurityAlertScopeIndexName)
	}
}
