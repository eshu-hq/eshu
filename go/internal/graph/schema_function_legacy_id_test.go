// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsForBackendAddsNornicDBFunctionLegacyIDLookup(t *testing.T) {
	t.Parallel()

	const statement = "CREATE INDEX nornicdb_function_legacy_id_lookup IF NOT EXISTS FOR (n:Function) ON (n.id)"
	nornic, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(nornicdb) error = %v", err)
	}
	assertContainsStatement(t, nornic, statement)

	neo4j, err := SchemaStatementsForBackend(SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(neo4j) error = %v", err)
	}
	for _, got := range neo4j {
		if got == statement {
			t.Fatalf("Neo4j schema unexpectedly contains NornicDB-only lookup %q", statement)
		}
	}
}
