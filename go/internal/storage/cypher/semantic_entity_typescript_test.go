package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSemanticEntityWriterWritesTypeScriptFunctionSemanticMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "function-ts-1",
				EntityType:   "Function",
				EntityName:   "identity",
				FilePath:     "/repo/src/decorators.ts",
				RelativePath: "src/decorators.ts",
				Language:     "typescript",
				StartLine:    4,
				EndLine:      8,
				Metadata: map[string]any{
					"decorators":      []string{"@sealed"},
					"type_parameters": []string{"T"},
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

	functionRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(functionRows), 1; got != want {
		t.Fatalf("function row count = %d, want %d", got, want)
	}
	if got, want := functionRows[0]["decorators"], []string{"@sealed"}; !equalStringSlice(got, want) {
		t.Fatalf("function row decorators = %#v, want %#v", got, want)
	}
	if got, want := functionRows[0]["type_parameters"], []string{"T"}; !equalStringSlice(got, want) {
		t.Fatalf("function row type_parameters = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "MERGE (n:Function {uid: row.entity_id})") {
		t.Fatalf("function cypher missing Function merge: %s", executor.calls[1].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "n.decorators = row.decorators") {
		t.Fatalf("function cypher missing decorators projection: %s", executor.calls[1].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "n.type_parameters = row.type_parameters") {
		t.Fatalf("function cypher missing type_parameters projection: %s", executor.calls[1].Cypher)
	}
}

func equalStringSlice(value any, want []string) bool {
	got, ok := value.([]string)
	if !ok {
		return false
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
