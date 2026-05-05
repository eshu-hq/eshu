package query

import "testing"

func TestBuildLanguageResult_AttachesKotlinSecondaryConstructorMetadata(t *testing.T) {
	row := map[string]any{
		"entity_id":        "func:kotlin:Widget.constructor",
		"name":             "constructor",
		"labels":           []any{"Function"},
		"file_path":        "src/Widget.kt",
		"repo_id":          "repo:kt",
		"repo_name":        "mobile",
		"language":         "kotlin",
		"start_line":       int64(14),
		"end_line":         int64(14),
		"constructor_kind": "secondary",
	}

	result := buildLanguageResult(row, "Function")

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["constructor_kind"], "secondary"; got != want {
		t.Fatalf("metadata[constructor_kind] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Function constructor is a secondary constructor."; got != want {
		t.Fatalf("semantic_summary = %#v, want %#v", result["semantic_summary"], want)
	}
}
