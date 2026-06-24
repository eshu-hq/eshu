// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsContainCodeTaintEvidenceSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()
	assertContainsStatement(t, stmts, "CREATE CONSTRAINT code_taint_evidence_uid_unique IF NOT EXISTS FOR (n:CodeTaintEvidence) REQUIRE n.uid IS UNIQUE")
}

func TestSchemaStatementsForBackendAddsCodeTaintEvidenceNornicDBUIDLookup(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	assertContainsStatement(t, stmts, "CREATE INDEX nornicdb_code_taint_evidence_uid_lookup IF NOT EXISTS FOR (n:CodeTaintEvidence) ON (n.uid)")
}
