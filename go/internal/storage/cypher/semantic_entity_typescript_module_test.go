package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSemanticEntityWriterWritesTypeScriptModuleSemanticMetadata(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)

	result, err := writer.WriteSemanticEntities(context.Background(), reducer.SemanticEntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []reducer.SemanticEntityRow{
			{
				RepoID:       "repo-1",
				EntityID:     "module-1",
				EntityType:   "Module",
				EntityName:   "API",
				FilePath:     "/repo/src/types.ts",
				RelativePath: "src/types.ts",
				Language:     "typescript",
				StartLine:    1,
				EndLine:      8,
				Metadata: map[string]any{
					"module_kind": "namespace",
				},
			},
			{
				RepoID:       "repo-1",
				EntityID:     "module-2",
				EntityType:   "Module",
				EntityName:   "Service",
				FilePath:     "/repo/src/merge.ts",
				RelativePath: "src/merge.ts",
				Language:     "typescript",
				StartLine:    1,
				EndLine:      6,
				Metadata: map[string]any{
					"declaration_merge_group": "Service",
					"declaration_merge_count": 2,
					"declaration_merge_kinds": []any{"class", "namespace"},
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
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	moduleRows := executor.calls[1].Parameters["rows"].([]map[string]any)
	if got, want := len(moduleRows), 2; got != want {
		t.Fatalf("module row count = %d, want %d", got, want)
	}
	if got, want := moduleRows[0]["module_kind"], "namespace"; got != want {
		t.Fatalf("module row[0].module_kind = %#v, want %#v", got, want)
	}
	if got, want := moduleRows[1]["declaration_merge_group"], "Service"; got != want {
		t.Fatalf("module row[1].declaration_merge_group = %#v, want %#v", got, want)
	}
	if got, want := moduleRows[1]["declaration_merge_count"], 2; got != want {
		t.Fatalf("module row[1].declaration_merge_count = %#v, want %#v", got, want)
	}
	kinds, ok := moduleRows[1]["declaration_merge_kinds"].([]string)
	if !ok {
		t.Fatalf("module row[1].declaration_merge_kinds type = %T, want []string", moduleRows[1]["declaration_merge_kinds"])
	}
	if got, want := len(kinds), 2; got != want || kinds[0] != "class" || kinds[1] != "namespace" {
		t.Fatalf("module row[1].declaration_merge_kinds = %#v, want [class namespace]", kinds)
	}
}
