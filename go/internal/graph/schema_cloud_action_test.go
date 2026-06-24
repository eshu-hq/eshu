// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsContainCloudActionIDConstraint(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()
	assertContainsStatement(t, stmts, "CREATE CONSTRAINT cloud_action_id IF NOT EXISTS FOR (a:CloudAction) REQUIRE a.id IS UNIQUE")
}

func TestSchemaStatementsForBackendAddsCloudActionNornicDBIDLookup(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	assertContainsStatement(t, stmts, "CREATE INDEX nornicdb_cloud_action_id_lookup IF NOT EXISTS FOR (a:CloudAction) ON (a.id)")
}
