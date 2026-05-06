package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSemanticEntityWriterWritesElixirProtocolNodes(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "protocol-1",
				EntityType:   "Protocol",
				EntityName:   "Demo.Serializable",
				FilePath:     "/repo/lib/demo/serializable.ex",
				RelativePath: "lib/demo/serializable.ex",
				Language:     "elixir",
				StartLine:    1,
				EndLine:      3,
				Metadata: map[string]any{
					"module_kind": "protocol",
				},
			},
			{
				RepoID:       "repo-1",
				EntityID:     "impl-1",
				EntityType:   "ProtocolImplementation",
				EntityName:   "Demo.Serializable",
				FilePath:     "/repo/lib/demo/serializable.ex",
				RelativePath: "lib/demo/serializable.ex",
				Language:     "elixir",
				StartLine:    5,
				EndLine:      8,
				Metadata: map[string]any{
					"module_kind":     "protocol_implementation",
					"protocol":        "Demo.Serializable",
					"implemented_for": "Demo.Worker",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 3; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	protocolRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := protocolRows[0]["module_kind"], "protocol"; got != want {
		t.Fatalf("protocol module_kind = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "MERGE (n:Protocol {uid: row.entity_id})") {
		t.Fatalf("protocol cypher missing Protocol merge: %s", executor.calls[1].Cypher)
	}

	implementationRows := executor.calls[2].Parameters["rows"].([]map[string]any)
	if got, want := implementationRows[0]["protocol"], "Demo.Serializable"; got != want {
		t.Fatalf("implementation protocol = %#v, want %#v", got, want)
	}
	if got, want := implementationRows[0]["implemented_for"], "Demo.Worker"; got != want {
		t.Fatalf("implementation implemented_for = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[2].Cypher, "MERGE (n:ProtocolImplementation {uid: row.entity_id})") {
		t.Fatalf("implementation cypher missing ProtocolImplementation merge: %s", executor.calls[2].Cypher)
	}
}

func TestSemanticEntityWriterWritesElixirModuleAttributeNodes(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "attr-1",
				EntityType:   "Variable",
				EntityName:   "@timeout",
				FilePath:     "/repo/lib/demo/worker.ex",
				RelativePath: "lib/demo/worker.ex",
				Language:     "elixir",
				StartLine:    2,
				EndLine:      2,
				Metadata: map[string]any{
					"attribute_kind": "module_attribute",
					"value":          "5_000",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	rows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["attribute_kind"], "module_attribute"; got != want {
		t.Fatalf("row[attribute_kind] = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["value"], "5_000"; got != want {
		t.Fatalf("row[value] = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "MERGE (n:Variable {uid: row.entity_id})") {
		t.Fatalf("cypher missing Variable merge: %s", executor.calls[1].Cypher)
	}
}
