// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestGraphSchemaObjectNameReadsEveryNeo4jStatement keeps schema adoption able
// to name every statement the Neo4j dialect applies, including the #7245
// infra_search_key_ft FULLTEXT index, so adoption never refuses the current
// schema.
func TestGraphSchemaObjectNameReadsEveryNeo4jStatement(t *testing.T) {
	t.Parallel()

	stmts, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(neo4j) error = %v", err)
	}
	found := false
	for _, stmt := range stmts {
		if _, dropped := graphSchemaDropObjectName(stmt); dropped {
			continue
		}
		name, err := graphSchemaObjectName(stmt)
		if err != nil {
			t.Errorf("graphSchemaObjectName(%q) error = %v", stmt, err)
		}
		if name == graph.InfraSearchKeyIndexName {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Neo4j schema statement named %s", graph.InfraSearchKeyIndexName)
	}
}
