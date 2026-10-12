// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// TestContentEntitiesRepoLanguageTypePathIndexMigration proves the #7735
// migration ships the measured page-read shape: the three equality columns of
// the language-query filter first, then exactly the statement's ORDER BY key,
// so the ordered page is a btree descent that stops at LIMIT instead of an
// ordered walk that heap-filters tens of thousands of other repositories'
// rows. See docs/internal/evidence/7735-content-entities-page-read-index.md
// for the EXPLAIN (ANALYZE, BUFFERS) ladder this migration is derived from
// (80,234 page buffers become 13, 65 ms become 0.14 ms warm).
func TestContentEntitiesRepoLanguageTypePathIndexMigration(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("content_entities_repo_language_type_path_idx")

	for _, want := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_repo_language_type_path_idx",
		"ON content_entities (repo_id, language, entity_type, relative_path, start_line, entity_name)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("content_entities page-read migration missing %q:\n%s", want, sql)
		}
	}

	// The page read keeps its ORDER BY, so a truncated key (the 164 gate
	// key without the ordering tail) restores the defect while keeping the
	// name above intact: the planner can no longer serve the ORDER BY from
	// the index and falls back to the corpus-wide ordered walk with repo_id
	// as a heap filter.
	if !strings.Contains(sql, "(repo_id, language, entity_type, relative_path, start_line, entity_name)") {
		t.Fatalf("page-read index must carry the full ORDER BY tail after the equality columns:\n%s", sql)
	}

	// The index must not swallow the SELECT payload: source_cache and
	// metadata are unbounded text/JSONB blobs, and a btree leaf tuple cannot
	// exceed ~2.7 KiB. Covering them risks CREATE INDEX failing on a real,
	// valid row, leaving an INVALID index and failing schema bootstrap.
	// The header comment above names those columns when explaining why, so
	// the check runs on the statement body with line comments stripped.
	statement := stripSQLLineComments(sql)
	for _, forbidden := range []string{"source_cache", "metadata", "INCLUDE"} {
		if strings.Contains(statement, forbidden) {
			t.Fatalf("page-read index must not cover the unbounded payload column %q:\n%s", forbidden, sql)
		}
	}

	// #7759 owns any drop review on this table's indexes: this migration
	// must create only, and must leave content_entities_repo_idx alone.
	// The header comment above says so in prose, so this checks the
	// statement body, not the comment.
	if strings.Contains(statement, "DROP") {
		t.Fatalf("page-read migration must not drop any index (owner review #7759):\n%s", sql)
	}
}

// TestContentEntitiesRepoLanguageTypePathIndexIsSingleConcurrentStatement
// proves the migration is exactly one CREATE INDEX CONCURRENTLY statement so
// it can run outside a transaction block, matching every other CONCURRENTLY
// migration in this package.
func TestContentEntitiesRepoLanguageTypePathIndexIsSingleConcurrentStatement(t *testing.T) {
	t.Parallel()

	sql := MigrationSQL("content_entities_repo_language_type_path_idx")
	if count := strings.Count(sql, "CREATE INDEX CONCURRENTLY"); count != 1 {
		t.Fatalf("expected exactly 1 CREATE INDEX CONCURRENTLY statement, got %d:\n%s", count, sql)
	}
	if !coordination.IsSoleConcurrentIndexStatement(sql) {
		t.Fatalf("migration is not one isolated concurrent-index statement:\n%s", sql)
	}
}
