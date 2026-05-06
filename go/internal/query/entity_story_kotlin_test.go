package query

import "testing"

func TestAttachSemanticSummaryAddsKotlinSecondaryConstructorStory(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels":    []string{"Function"},
		"name":      "Widget",
		"language":  "kotlin",
		"file_path": "src/Widget.kt",
		"metadata": map[string]any{
			"constructor_kind": "secondary",
		},
	}

	attachSemanticSummary(entity)

	if got, want := entity["semantic_summary"], "Function Widget is a secondary constructor."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	if got, want := entity["story"], "Function Widget is a secondary constructor. Defined in src/Widget.kt (kotlin)."; got != want {
		t.Fatalf("entity[story] = %#v, want %#v", got, want)
	}
}
