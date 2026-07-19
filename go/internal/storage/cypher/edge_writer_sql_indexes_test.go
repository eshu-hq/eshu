// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestEdgeWriterWriteEdgesSQLRelationshipIndexes proves the edge writer
// accepts an INDEXES row (SqlIndex -> SqlTable) through the label-scoped MERGE
// template — the same idempotent write shape every other SQL relationship
// type uses — now that the reducer emits INDEXES rows (#5330 Task 3).
func TestEdgeWriterWriteEdgesSQLRelationshipIndexes(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"source_entity_id":   "content-entity:users_email_idx",
				"target_entity_id":   "content-entity:users",
				"source_entity_type": "SqlIndex",
				"target_entity_type": "SqlTable",
				"repo_id":            "repo-a",
				"relationship_type":  "INDEXES",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainSQLRelationships, rows, "reducer/sql-relationships"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d (INDEXES row must not be silently skipped)", got, want)
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (source:SqlIndex {uid: row.source_entity_id})",
		"MATCH (target:SqlTable {uid: row.target_entity_id})",
		"MERGE (source)-[rel:INDEXES]->(target)",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q: %s", want, cypher)
		}
	}
}
