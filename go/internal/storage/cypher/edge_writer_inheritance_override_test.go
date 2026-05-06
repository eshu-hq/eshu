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
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "INHERITS|OVERRIDES|ALIASES") {
		t.Fatalf("cypher missing INHERITS|OVERRIDES|ALIASES: %s", executor.calls[0].Cypher)
	}
}
