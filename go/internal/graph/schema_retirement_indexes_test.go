// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"testing"
)

// TestSchemaStatementsIncludeRetirementRetractLookupIndexes pins the two
// indexes the #8659 graph retraction seeks (#8659 prove-first: live Neo4j
// EXPLAIN moved both statements from NodeByLabelScan to an index seek):
//   - file_repo_id backs the G5f repo-anchored File retract on both
//     backends, the same shape as the function_repo_id retract index;
//   - parameter_path backs the G5p root-prefix Parameter retract on Neo4j
//     only. NornicDB keeps its existing nornicdb_parameter_path_lookup
//     for the same label and property, which already seeks that shape
//     there; a NornicDB parameter_path entry would re-issue a second
//     property index on Parameter.path and re-backfill every existing
//     store (nornicdb-pitfalls.md), so no backend ever holds both.
func TestSchemaStatementsIncludeRetirementRetractLookupIndexes(t *testing.T) {
	t.Parallel()

	for _, backend := range []SchemaBackend{SchemaBackendNeo4j, SchemaBackendNornicDB} {
		stmts, err := SchemaStatementsForBackend(backend)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", backend, err)
		}
		assertContainsStatement(t, stmts,
			"CREATE INDEX file_repo_id IF NOT EXISTS FOR (f:File) ON (f.repo_id)")
	}
	neo4jStmts, err := SchemaStatementsForBackend(SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(neo4j) error = %v, want nil", err)
	}
	assertContainsStatement(t, neo4jStmts,
		"CREATE INDEX parameter_path IF NOT EXISTS FOR (p:Parameter) ON (p.path)")
	assertNoStatementContains(t, neo4jStmts, "nornicdb_parameter_path_lookup")

	nornicStmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(nornicdb) error = %v, want nil", err)
	}
	assertContainsStatement(t, nornicStmts,
		"CREATE INDEX nornicdb_parameter_path_lookup IF NOT EXISTS FOR (n:Parameter) ON (n.path)")
	assertNoStatementContains(t, nornicStmts, "CREATE INDEX parameter_path ")
}
