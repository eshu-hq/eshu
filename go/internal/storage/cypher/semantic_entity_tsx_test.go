package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSemanticEntityWriterWritesTSXFunctionFragmentMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "function-tsx-1",
				EntityType:   "Function",
				EntityName:   "Screen",
				FilePath:     "/repo/src/Screen.tsx",
				RelativePath: "src/Screen.tsx",
				Language:     "tsx",
				StartLine:    7,
				EndLine:      14,
				Metadata: map[string]any{
					"jsx_fragment_shorthand": true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v, want nil", err)
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
	if got, want := functionRows[0]["jsx_fragment_shorthand"], true; got != want {
		t.Fatalf("function jsx_fragment_shorthand = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "n.jsx_fragment_shorthand = row.jsx_fragment_shorthand") {
		t.Fatalf("function cypher missing jsx_fragment_shorthand assignment: %s", executor.calls[1].Cypher)
	}
}

func TestSemanticEntityWriterWritesTSXVariableComponentTypeAssertionMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "variable-tsx-1",
				EntityType:   "Variable",
				EntityName:   "Screen",
				FilePath:     "/repo/src/Screen.tsx",
				RelativePath: "src/Screen.tsx",
				Language:     "tsx",
				StartLine:    6,
				EndLine:      6,
				Metadata: map[string]any{
					"component_type_assertion": "ComponentType",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	variableRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(variableRows), 1; got != want {
		t.Fatalf("variable row count = %d, want %d", got, want)
	}
	if got, want := variableRows[0]["component_type_assertion"], "ComponentType"; got != want {
		t.Fatalf("variable component_type_assertion = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "n.component_type_assertion = row.component_type_assertion") {
		t.Fatalf("variable cypher missing component_type_assertion assignment: %s", executor.calls[1].Cypher)
	}
}

func TestSemanticEntityWriterWritesTSXComponentWrapperMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "component-tsx-1",
				EntityType:   "Component",
				EntityName:   "MemoButton",
				FilePath:     "/repo/src/Screen.tsx",
				RelativePath: "src/Screen.tsx",
				Language:     "tsx",
				StartLine:    3,
				EndLine:      3,
				Metadata: map[string]any{
					"framework":              "react",
					"component_wrapper_kind": "memo",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	componentRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(componentRows), 1; got != want {
		t.Fatalf("component row count = %d, want %d", got, want)
	}
	if got, want := componentRows[0]["component_wrapper_kind"], "memo"; got != want {
		t.Fatalf("component component_wrapper_kind = %#v, want %#v", got, want)
	}
	if !strings.Contains(executor.calls[1].Cypher, "n.component_wrapper_kind = row.component_wrapper_kind") {
		t.Fatalf("component cypher missing component_wrapper_kind assignment: %s", executor.calls[1].Cypher)
	}
}
