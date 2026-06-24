// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsContainsCloudResourceSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	expected := []string{
		"CREATE CONSTRAINT cloud_resource_uid_unique IF NOT EXISTS FOR (n:CloudResource) REQUIRE n.uid IS UNIQUE",
		"CREATE INDEX cloud_resource_arn IF NOT EXISTS FOR (r:CloudResource) ON (r.arn)",
		"CREATE INDEX cloud_resource_resource_id IF NOT EXISTS FOR (r:CloudResource) ON (r.resource_id)",
		"CREATE INDEX cloud_resource_type IF NOT EXISTS FOR (r:CloudResource) ON (r.resource_type)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

func TestSchemaStatementsForBackendAddsCloudResourceNornicDBUIDLookup(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	assertContainsStatement(
		t,
		stmts,
		"CREATE INDEX nornicdb_cloud_resource_uid_lookup IF NOT EXISTS FOR (n:CloudResource) ON (n.uid)",
	)
}
