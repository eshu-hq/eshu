// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesInheritanceDispatchesOverrides(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"child_entity_id":   "entity:class:child",
				"parent_entity_id":  "entity:trait:loggable",
				"repo_id":           "repo-a",
				"relationship_type": "OVERRIDES",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "OVERRIDES") {
		t.Fatalf("cypher missing OVERRIDES: %s", executor.calls[0].Cypher)
	}
	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(batchRows) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	if got, want := batchRows[0]["relationship_type"], "OVERRIDES"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
}

func TestEdgeWriterWriteEdgesInheritanceUsesExactEndpointLabels(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"child_entity_id":    "entity:class:child",
				"child_entity_type":  "Class",
				"parent_entity_id":   "entity:trait:loggable",
				"parent_entity_type": "Trait",
				"repo_id":            "repo-a",
				"relationship_type":  "INHERITS",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (child:Class {uid: row.child_entity_id})") {
		t.Fatalf("cypher = %q, want Class child uid anchor", cypher)
	}
	if !strings.Contains(cypher, "MATCH (parent:Trait {uid: row.parent_entity_id})") {
		t.Fatalf("cypher = %q, want Trait parent uid anchor", cypher)
	}
	if strings.Contains(cypher, "Function|Class|Interface|Trait|Struct|Enum|Protocol") {
		t.Fatalf("cypher = %q, want no multi-label fallback for typed inheritance endpoints", cypher)
	}
	summary, _ := executor.calls[0].Parameters[StatementMetadataSummaryKey].(string)
	for _, want := range []string{
		"domain=inheritance_edges",
		"relationship=INHERITS",
		"child=Class",
		"parent=Trait",
		"rows=1",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("statement summary = %q, want fragment %q", summary, want)
		}
	}
}

func TestEdgeWriterWriteEdgesInheritanceFallsBackWhenEndpointLabelsMissing(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"child_entity_id":   "entity:class:child",
				"parent_entity_id":  "entity:trait:loggable",
				"repo_id":           "repo-a",
				"relationship_type": "INHERITS",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if cypher := executor.calls[0].Cypher; !strings.Contains(cypher, "child:Function|Class|Interface|Trait|Struct|Enum|Protocol") {
		t.Fatalf("cypher = %q, want multi-label fallback for legacy inheritance rows", cypher)
	}
}

func TestEdgeWriterWriteEdgesInheritanceDispatchesAliases(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"child_entity_id":   "entity:class:child",
				"parent_entity_id":  "entity:trait:loggable",
				"repo_id":           "repo-a",
				"relationship_type": "ALIASES",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "ALIASES") {
		t.Fatalf("cypher missing ALIASES: %s", executor.calls[0].Cypher)
	}
	batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(batchRows) != 1 {
		t.Fatalf("expected 1 row in batch, got %v", executor.calls[0].Parameters["rows"])
	}
	if got, want := batchRows[0]["relationship_type"], "ALIASES"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
}

func TestEdgeWriterRetractEdgesInheritanceIncludesOverrides(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainInheritanceEdges, rows, "reducer/inheritance"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	// One statement per child label (#5116/#4367).
	if got, want := len(executor.calls), len(inheritanceRetractChildLabels); got != want {
		t.Fatalf("executor calls = %d, want %d (one per child label)", got, want)
	}
	for _, stmt := range executor.calls {
		if !strings.Contains(stmt.Cypher, "INHERITS|OVERRIDES|ALIASES") {
			t.Fatalf("cypher missing INHERITS|OVERRIDES|ALIASES: %s", stmt.Cypher)
		}
	}
}
