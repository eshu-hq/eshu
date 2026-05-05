package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSemanticEntityRowsIncludesTypeScriptModuleFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/types.ts",
			},
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "module-1",
				"relative_path": "src/types.ts",
				"entity_type":   "Module",
				"entity_name":   "API",
				"language":      "typescript",
				"module_kind":   "namespace",
			},
		},
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/merge.ts",
			},
			Payload: map[string]any{
				"repo_id":                 "repo-1",
				"entity_id":               "module-2",
				"relative_path":           "src/merge.ts",
				"entity_type":             "Module",
				"entity_name":             "Service",
				"language":                "typescript",
				"declaration_merge_group": "Service",
				"declaration_merge_count": 2,
				"declaration_merge_kinds": []any{"class", "namespace"},
			},
		},
	}

	repoIDs, rows := ExtractSemanticEntityRows(envelopes)
	if got, want := repoIDs, []string{"repo-1"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ExtractSemanticEntityRows() repoIDs = %v, want %v", got, want)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("ExtractSemanticEntityRows() rows = %d, want %d", got, want)
	}

	rowsByID := make(map[string]SemanticEntityRow, len(rows))
	for _, row := range rows {
		rowsByID[row.EntityID] = row
	}

	namespace := rowsByID["module-1"]
	if got, want := namespace.Metadata["module_kind"], "namespace"; got != want {
		t.Fatalf("namespace.Metadata[module_kind] = %#v, want %#v", got, want)
	}

	merged := rowsByID["module-2"]
	if got, want := merged.Metadata["declaration_merge_group"], "Service"; got != want {
		t.Fatalf("merged.Metadata[declaration_merge_group] = %#v, want %#v", got, want)
	}
	if got, want := merged.Metadata["declaration_merge_count"], 2; got != want {
		t.Fatalf("merged.Metadata[declaration_merge_count] = %#v, want %#v", got, want)
	}
	kinds, ok := merged.Metadata["declaration_merge_kinds"].([]string)
	if !ok {
		t.Fatalf("merged.Metadata[declaration_merge_kinds] type = %T, want []string", merged.Metadata["declaration_merge_kinds"])
	}
	if got, want := len(kinds), 2; got != want || kinds[0] != "class" || kinds[1] != "namespace" {
		t.Fatalf("merged.Metadata[declaration_merge_kinds] = %#v, want [class namespace]", kinds)
	}
}
