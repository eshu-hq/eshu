// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"context"
	"slices"
	"testing"
)

// unconstrainedUIDIndexStatements are the #7057 uid indexes on the two
// uid-keyed labels that carry no uid constraint. Their uids reach API callers
// as relationship neighbour ids, and the Neo4j entity-id anchor seeks them
// through these indexes instead of scanning every node.
var unconstrainedUIDIndexStatements = []string{
	"CREATE INDEX rationale_uid IF NOT EXISTS FOR (r:Rationale) ON (r.uid)",
	"CREATE INDEX documentation_section_uid IF NOT EXISTS FOR (s:DocumentationSection) ON (s.uid)",
}

// TestSchemaUnconstrainedUIDIndexesAreNeo4jOnly pins the indexes to the Neo4j
// dialect. NornicDB readers never use the Neo4j anchor, and a NornicDB
// fingerprint bump forces a schema re-apply on every existing store
// (nornicdb-pitfalls.md), so a NornicDB statement needs a reason of its own.
// The NornicDB fingerprint did move once since, for the #7097 constraint
// retirement (TestSchemaApplicationsDeclareCompatibilityDecision pins it), but
// that adds no statement listed here.
func TestSchemaUnconstrainedUIDIndexesAreNeo4jOnly(t *testing.T) {
	t.Parallel()

	neo4j, err := SchemaStatementsForBackend(SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(neo4j) error = %v", err)
	}
	nornic, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(nornicdb) error = %v", err)
	}
	for _, stmt := range unconstrainedUIDIndexStatements {
		if !slices.Contains(neo4j, stmt) {
			t.Errorf("neo4j schema missing %q", stmt)
		}
		if slices.Contains(nornic, stmt) {
			t.Errorf("nornicdb schema must not carry %q", stmt)
		}
	}
}

// TestEnsureSchemaAppliesUnconstrainedUIDIndexesOnNeo4jOnly drives the real
// execution path, which walks the DDL tables separately from
// SchemaStatementsForBackend, so a dialect gate on one and not the other fails.
func TestEnsureSchemaAppliesUnconstrainedUIDIndexesOnNeo4jOnly(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		backend SchemaBackend
		want    bool
	}{
		{SchemaBackendNeo4j, true},
		{SchemaBackendNornicDB, false},
	} {
		executor := &schemaRecordingExecutor{}
		if err := EnsureSchemaWithBackend(context.Background(), executor, nil, tc.backend); err != nil {
			t.Fatalf("EnsureSchemaWithBackend(%s) error = %v", tc.backend, err)
		}
		executed := make([]string, 0, len(executor.calls))
		for _, call := range executor.calls {
			executed = append(executed, call.Cypher)
		}
		for _, stmt := range unconstrainedUIDIndexStatements {
			if got := slices.Contains(executed, stmt); got != tc.want {
				t.Errorf("%s executed %q = %v, want %v", tc.backend, stmt, got, tc.want)
			}
		}
		want, err := SchemaStatementsForBackend(tc.backend)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%s) error = %v", tc.backend, err)
		}
		for _, stmt := range want {
			if !slices.Contains(executed, stmt) {
				t.Errorf("%s: EnsureSchema never executed listed statement %q", tc.backend, stmt)
			}
		}
	}
}
