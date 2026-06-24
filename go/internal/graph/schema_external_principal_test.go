// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsContainExternalPrincipalSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()
	assertContainsStatement(t, stmts, "CREATE CONSTRAINT external_principal_uid_unique IF NOT EXISTS FOR (n:ExternalPrincipal) REQUIRE n.uid IS UNIQUE")
}

func TestSchemaStatementsForBackendAddsExternalPrincipalNornicDBUIDLookup(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	assertContainsStatement(t, stmts, "CREATE INDEX nornicdb_external_principal_uid_lookup IF NOT EXISTS FOR (n:ExternalPrincipal) ON (n.uid)")
}
