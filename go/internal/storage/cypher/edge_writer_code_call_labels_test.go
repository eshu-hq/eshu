package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesCodeCallsUsesExactEndpointLabels(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-1",
			Payload: map[string]any{
				"caller_entity_id":   "content-entity:caller",
				"caller_entity_type": "Function",
				"callee_entity_id":   "content-entity:callee",
				"callee_entity_type": "Function",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (source:Function {uid: row.caller_entity_id})") {
		t.Fatalf("cypher = %q, want Function source uid anchor", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:Function {uid: row.callee_entity_id})") {
		t.Fatalf("cypher = %q, want Function target uid anchor", cypher)
	}
	if strings.Contains(cypher, "Function|Class|File") {
		t.Fatalf("cypher = %q, want no multi-label fallback for typed endpoints", cypher)
	}
}

func TestEdgeWriterWriteEdgesCodeCallsUsesExactInterfaceEndpointLabels(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-1",
			Payload: map[string]any{
				"caller_entity_id":   "content-entity:caller",
				"caller_entity_type": "Function",
				"callee_entity_id":   "content-entity:callee",
				"callee_entity_type": "Interface",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (source:Function {uid: row.caller_entity_id})") {
		t.Fatalf("cypher = %q, want Function source uid anchor", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:Interface {uid: row.callee_entity_id})") {
		t.Fatalf("cypher = %q, want Interface target uid anchor", cypher)
	}
	if strings.Contains(cypher, "Function|Class|File") {
		t.Fatalf("cypher = %q, want no multi-label fallback for typed interface endpoints", cypher)
	}
}

func TestEdgeWriterWriteEdgesCodeCallsFallsBackWhenEndpointLabelsMissing(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-1",
			Payload: map[string]any{
				"caller_entity_id": "content-entity:caller",
				"callee_entity_id": "content-entity:callee",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if cypher := executor.calls[0].Cypher; !strings.Contains(cypher, "source:Function|Class|File") {
		t.Fatalf("cypher = %q, want multi-label fallback for legacy rows", cypher)
	}
}

func TestEdgeWriterWriteEdgesCodeReferencesUsesExactEndpointLabels(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-1",
			Payload: map[string]any{
				"caller_entity_id":   "content-entity:caller",
				"caller_entity_type": "File",
				"callee_entity_id":   "content-entity:target",
				"callee_entity_type": "TypeAlias",
				"relationship_type":  "REFERENCES",
			},
		},
	}

	if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	cypher := executor.calls[0].Cypher
	if !strings.Contains(cypher, "MATCH (source:File {uid: row.caller_entity_id})") {
		t.Fatalf("cypher = %q, want File source uid anchor", cypher)
	}
	if !strings.Contains(cypher, "MATCH (target:TypeAlias {uid: row.callee_entity_id})") {
		t.Fatalf("cypher = %q, want TypeAlias target uid anchor", cypher)
	}
	if strings.Contains(cypher, "Function|Class|Struct|Interface|TypeAlias|File") {
		t.Fatalf("cypher = %q, want no multi-label fallback for typed reference endpoints", cypher)
	}
}
