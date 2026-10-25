// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

// TestSchemaStatementsIncludeRuntimeDeclarationIdentity pins the #8816
// RuntimeDeclaration schema: the five-part identity the reducer MERGEs on
// (repo_id, relative_path, line_number, runtime, order_index) and the
// (runtime, version_major) range index the version-threshold read seeks.
func TestSchemaStatementsIncludeRuntimeDeclarationIdentity(t *testing.T) {
	t.Parallel()

	neo4j, err := SchemaStatementsForBackend(SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(neo4j) error = %v", err)
	}
	assertContainsStatement(t, neo4j,
		"CREATE CONSTRAINT runtime_declaration_identity IF NOT EXISTS FOR (d:RuntimeDeclaration) REQUIRE (d.repo_id, d.relative_path, d.line_number, d.runtime, d.order_index) IS UNIQUE")
	assertContainsStatement(t, neo4j,
		"CREATE INDEX runtime_declaration_runtime_major IF NOT EXISTS FOR (d:RuntimeDeclaration) ON (d.runtime, d.version_major)")
	assertNoStatementContains(t, neo4j, "nornicdb_runtime_declaration_repo_id_lookup")

	nornic, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(nornicdb) error = %v", err)
	}
	// NornicDB drops composite constraints (nornicDBSchemaConstraint), so the
	// MERGE needs a single-property lookup index or it scans the label per row.
	assertContainsStatement(t, nornic,
		"CREATE INDEX nornicdb_runtime_declaration_repo_id_lookup IF NOT EXISTS FOR (d:RuntimeDeclaration) ON (d.repo_id)")
	assertContainsStatement(t, nornic,
		"CREATE INDEX runtime_declaration_runtime_major IF NOT EXISTS FOR (d:RuntimeDeclaration) ON (d.runtime, d.version_major)")
}
