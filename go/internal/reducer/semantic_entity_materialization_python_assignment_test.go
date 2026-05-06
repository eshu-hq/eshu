package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractSemanticEntityRowsIncludesPythonAssignmentTypeAnnotationFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/app.py",
			},
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "function-1",
				"relative_path": "src/app.py",
				"entity_type":   "Function",
				"entity_name":   "settings",
				"language":      "python",
				"type_annotations": []any{
					map[string]any{"annotation_kind": "assignment"},
				},
			},
		},
	}

	repoIDs, rows := ExtractSemanticEntityRows(envelopes)
	if got, want := repoIDs, []string{"repo-1"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ExtractSemanticEntityRows() repoIDs = %v, want %v", got, want)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("ExtractSemanticEntityRows() rows = %d, want %d", got, want)
	}

	row := rows[0]
	if got, want := row.Metadata["type_annotation_count"], 1; got != want {
		t.Fatalf("row.Metadata[type_annotation_count] = %#v, want %#v", got, want)
	}
	kinds, ok := row.Metadata["type_annotation_kinds"].([]string)
	if !ok {
		t.Fatalf("row.Metadata[type_annotation_kinds] type = %T, want []string", row.Metadata["type_annotation_kinds"])
	}
	if got, want := len(kinds), 1; got != want || kinds[0] != "assignment" {
		t.Fatalf("row.Metadata[type_annotation_kinds] = %#v, want [assignment]", kinds)
	}
}
