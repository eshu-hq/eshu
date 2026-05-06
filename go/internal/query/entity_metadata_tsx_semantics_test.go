package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestEnrichEntityResultsWithContentMetadataTSXFragmentComponent(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"component-1", "repo-1", "src/Screen.tsx", "Component", "Screen",
					int64(7), int64(14), "tsx", "export function Screen() { return <>...</> }",
					[]byte(`{"framework":"react","jsx_fragment_shorthand":true}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "Screen",
			"labels":     []string{"Component"},
			"file_path":  "src/Screen.tsx",
			"repo_id":    "repo-1",
			"language":   "tsx",
			"start_line": 7,
			"end_line":   14,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "Screen", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	if gotValue, want := got[0]["semantic_summary"], "Component Screen is associated with the react framework and uses JSX fragment shorthand."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}

	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["jsx_fragment_shorthand"], true; gotValue != want {
		t.Fatalf("semantic_profile[jsx_fragment_shorthand] = %#v, want %#v", gotValue, want)
	}
}

func TestEnrichEntityResultsWithContentMetadataTSXComponentTypeAssertion(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"variable-1", "repo-1", "src/Screen.tsx", "Variable", "Dynamic",
					int64(6), int64(6), "tsx", "const Dynamic = component as ComponentType<Props>",
					[]byte(`{"component_type_assertion":"ComponentType"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "Dynamic",
			"labels":     []string{"Variable"},
			"file_path":  "src/Screen.tsx",
			"repo_id":    "repo-1",
			"language":   "tsx",
			"start_line": 6,
			"end_line":   6,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "Dynamic", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	if gotValue, want := got[0]["semantic_summary"], "Variable Dynamic narrows to ComponentType."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}

	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "component_type_assertion"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
}

func TestEnrichEntityResultsWithContentMetadataTSXComponentWrapper(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"component-1", "repo-1", "src/Screen.tsx", "Component", "MemoButton",
					int64(3), int64(3), "tsx", "const MemoButton = memo(() => <button />)",
					[]byte(`{"framework":"react","component_wrapper_kind":"memo"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "MemoButton",
			"labels":     []string{"Component"},
			"file_path":  "src/Screen.tsx",
			"repo_id":    "repo-1",
			"language":   "tsx",
			"start_line": 3,
			"end_line":   3,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "MemoButton", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	if gotValue, want := got[0]["semantic_summary"], "Component MemoButton is associated with the react framework and is wrapped by memo."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}

	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "component_wrapper"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := semanticProfile["component_wrapper_kind"], "memo"; gotValue != want {
		t.Fatalf("semantic_profile[component_wrapper_kind] = %#v, want %#v", gotValue, want)
	}
}
