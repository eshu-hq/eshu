// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestContentEntitiesK8sSelectPartialIndexMigration proves the #5490
// migration ships the measured-winning shape: a partial, ORDER BY-aligned,
// covering index scoped to K8sResource rows only. See
// docs/internal/evidence/5490-k8sresource-candidate-index.md for the
// EXPLAIN ANALYZE ladder this migration is derived from.
func TestContentEntitiesK8sSelectPartialIndexMigration(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("content_entities_k8s_select_partial_index")

	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_k8s_select_partial_idx",
		"ON content_entities (repo_id, relative_path, start_line, entity_id)",
		"INCLUDE (entity_name, metadata)",
		"WHERE entity_type = 'K8sResource'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("content_entities_k8s_select_partial_index migration missing %q:\n%s", want, sql)
		}
	}

	// The index must stay partial: an unscoped version would pay its write
	// cost on every content_entities insert/update (Function, Variable, and
	// every other entity type), not just the narrow K8sResource slice #5490
	// measured. This is the load-bearing write-amplification guard.
	if strings.Contains(sql, "WHERE entity_type = 'K8sResource'") == false {
		t.Fatal("index must stay partial to K8sResource rows; an unscoped index taxes every content_entities write")
	}
}

// TestContentEntitiesK8sSelectPartialIndexIsSingleConcurrentStatement proves
// the migration is exactly one CREATE INDEX CONCURRENTLY statement so it can
// run outside a transaction block, matching every other CONCURRENTLY
// migration in this package (see
// TestBootstrapDefinitionsDoNotBundleConcurrentIndexStatements for the
// package-wide invariant this migration must not violate).
func TestContentEntitiesK8sSelectPartialIndexIsSingleConcurrentStatement(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("content_entities_k8s_select_partial_index")
	if count := strings.Count(sql, "CREATE INDEX CONCURRENTLY"); count != 1 {
		t.Fatalf("expected exactly 1 CREATE INDEX CONCURRENTLY statement, got %d:\n%s", count, sql)
	}
}
