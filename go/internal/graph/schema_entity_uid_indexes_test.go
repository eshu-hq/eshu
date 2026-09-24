// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"slices"
	"testing"
)

// TestSchemaIndexesUnconstrainedUIDLabels pins the uid indexes on the two
// uid-keyed labels that carry no uid constraint (#7057). Their uids reach API
// callers as relationship neighbour ids, and the Neo4j entity-id anchor seeks
// them through these indexes instead of scanning every node.
func TestSchemaIndexesUnconstrainedUIDLabels(t *testing.T) {
	t.Parallel()

	want := []string{
		"CREATE INDEX rationale_uid IF NOT EXISTS FOR (r:Rationale) ON (r.uid)",
		"CREATE INDEX documentation_section_uid IF NOT EXISTS FOR (s:DocumentationSection) ON (s.uid)",
	}
	for _, backend := range []SchemaBackend{SchemaBackendNeo4j, SchemaBackendNornicDB} {
		stmts, err := SchemaStatementsForBackend(backend)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%q) error = %v", backend, err)
		}
		for _, stmt := range want {
			if !slices.Contains(stmts, stmt) {
				t.Errorf("%s schema missing %q", backend, stmt)
			}
		}
	}
}
