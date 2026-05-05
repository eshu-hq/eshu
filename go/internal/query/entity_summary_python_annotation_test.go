package query

import "testing"

func TestBuildEntitySemanticSummaryPythonAssignmentTypeAnnotation(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"TypeAnnotation"},
		"name":   "timeout",
		"metadata": map[string]any{
			"type":            "int",
			"annotation_kind": "assignment",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "TypeAnnotation timeout is annotated as int."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}
