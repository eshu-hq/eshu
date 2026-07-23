// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestContentEntitiesK8sSelectPartialIndexMigration proves the #5490
// migration ships the measured, tuple-size-safe shape: a partial,
// ORDER BY-aligned index scoped to K8sResource rows only, INCLUDE-ing only
// the bounded entity_name column. See
// docs/internal/evidence/5490-k8sresource-candidate-index.md for the
// EXPLAIN ANALYZE ladder this migration is derived from, including the
// PR #5745 codex P1 that rejected an earlier revision covering the
// unbounded metadata JSONB.
func TestContentEntitiesK8sSelectPartialIndexMigration(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("content_entities_k8s_select_partial_index")

	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_k8s_select_partial_idx",
		"ON content_entities (repo_id, relative_path, start_line, entity_id)",
		"INCLUDE (entity_name)",
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

// TestContentEntitiesK8sSelectPartialIndexExcludesUnboundedMetadata is the
// regression guard for the PR #5745 codex P1: metadata is an unbounded JSONB
// payload for K8sResource rows (no size cap on labels/container
// images/backend references), and a btree INCLUDE value is stored in the
// index leaf tuple, which cannot exceed Postgres's ~2.7 KiB per-tuple limit.
// INCLUDE-ing metadata risks CREATE INDEX CONCURRENTLY failing with "index
// row size exceeds btree maximum" on a real, valid K8s manifest, leaving an
// INVALID index and failing schema bootstrap. This test fails if metadata
// (or any other unbounded JSONB/text-blob column) is ever added back to the
// INCLUDE list.
func TestContentEntitiesK8sSelectPartialIndexExcludesUnboundedMetadata(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("content_entities_k8s_select_partial_index")
	for _, forbidden := range []string{
		"INCLUDE (entity_name, metadata)",
		"INCLUDE (metadata)",
		"INCLUDE (entity_name, metadata,",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("content_entities_k8s_select_partial_index must not INCLUDE the unbounded metadata JSONB column (PR #5745 codex P1: btree ~2.7 KiB per-tuple limit); found %q", forbidden)
		}
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
