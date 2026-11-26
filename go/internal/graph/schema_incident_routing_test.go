// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import "testing"

func TestSchemaStatementsContainIncidentRoutingEvidenceSchema(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()
	assertContainsStatement(t, stmts, "CREATE CONSTRAINT incident_routing_evidence_uid_unique IF NOT EXISTS FOR (n:IncidentRoutingEvidence) REQUIRE n.uid IS UNIQUE")
}

func TestSchemaStatementsForBackendDropsIncidentRoutingEvidenceNornicDBUIDLookupIndex(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend returned error: %v", err)
	}
	assertContainsStatement(t, stmts, "DROP INDEX nornicdb_incident_routing_evidence_uid_lookup IF EXISTS")
}
