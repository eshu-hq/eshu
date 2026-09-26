// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestRepositoryEntryPointsIndexMigration(t *testing.T) {
	t.Parallel()

	migration := MigrationSQL("repository_entry_points_index")
	if migration == "" {
		t.Fatal("repository entry points index migration missing")
	}
	for _, fragment := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS content_entities_repository_entry_point_idx",
		"ON content_entities (repo_id, entity_name, relative_path) INCLUDE (language)",
		"WHERE entity_type = 'Function'",
		"entity_name IN ( 'main', 'handler', 'app', 'create_app', 'lambda_handler',",
		"'Main', 'Handler', 'App', 'CreateApp', 'LambdaHandler' )",
	} {
		if !strings.Contains(strings.Join(strings.Fields(migration), " "), strings.Join(strings.Fields(fragment), " ")) {
			t.Fatalf("repository entry points migration missing %q", fragment)
		}
	}
	_, statement, found := strings.Cut(migration, "CREATE INDEX")
	if !found {
		t.Fatal("repository entry points migration has no CREATE INDEX statement")
	}
	if terminators := strings.Count(statement, ";"); terminators != 1 {
		t.Fatalf("concurrent index migration has %d SQL statements, want one", terminators)
	}
}
