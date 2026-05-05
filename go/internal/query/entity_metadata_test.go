package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestEnrichEntityResultsWithContentMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/decorators.ts", "Class", "Demo",
					int64(5), int64(20), "typescript", "class Demo<T> {}", []byte(`{"decorators":["@sealed"],"type_parameters":["T"]}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "Demo",
			"labels":     []string{"Class"},
			"file_path":  "src/decorators.ts",
			"repo_id":    "repo-1",
			"language":   "typescript",
			"start_line": 5,
			"end_line":   20,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "Demo", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
	}
	decorators, ok := metadata["decorators"].([]any)
	if !ok {
		t.Fatalf("metadata[decorators] type = %T, want []any", metadata["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@sealed" {
		t.Fatalf("metadata[decorators] = %#v, want [@sealed]", decorators)
	}
	params, ok := metadata["type_parameters"].([]any)
	if !ok {
		t.Fatalf("metadata[type_parameters] type = %T, want []any", metadata["type_parameters"])
	}
	if len(params) != 1 || params[0] != "T" {
		t.Fatalf("metadata[type_parameters] = %#v, want [T]", params)
	}
	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "generic_declaration"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	decoratorValues, ok := semanticProfile["decorators"].([]string)
	if !ok {
		t.Fatalf("semantic_profile[decorators] type = %T, want []string", semanticProfile["decorators"])
	}
	if len(decoratorValues) != 1 || decoratorValues[0] != "@sealed" {
		t.Fatalf("semantic_profile[decorators] = %#v, want [@sealed]", decoratorValues)
	}
	typeParameters, ok := semanticProfile["type_parameters"].([]string)
	if !ok {
		t.Fatalf("semantic_profile[type_parameters] type = %T, want []string", semanticProfile["type_parameters"])
	}
	if len(typeParameters) != 1 || typeParameters[0] != "T" {
		t.Fatalf("semantic_profile[type_parameters] = %#v, want [T]", typeParameters)
	}
}

func TestEnrichEntityResultsWithContentMetadataPrefersExistingPythonGraphMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/handler.py", "Function", "handler",
					int64(12), int64(20), "python", "async def handler(): ...", []byte(`{"decorators":["@content"],"async":false}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "handler",
			"labels":     []string{"Function"},
			"file_path":  "src/handler.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 12,
			"end_line":   20,
			"metadata": map[string]any{
				"decorators": []string{"@route"},
				"async":      true,
			},
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "handler", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
	}
	decorators, ok := metadata["decorators"].([]string)
	if !ok {
		t.Fatalf("metadata[decorators] type = %T, want []string", metadata["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@route" {
		t.Fatalf("metadata[decorators] = %#v, want [@route]", decorators)
	}
	if gotValue, want := metadata["async"], true; gotValue != want {
		t.Fatalf("metadata[async] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := got[0]["semantic_summary"], "Function handler is async and uses decorators @route."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "decorated_async_function"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := semanticProfile["async"], true; gotValue != want {
		t.Fatalf("semantic_profile[async] = %#v, want %#v", gotValue, want)
	}
}

func TestEnrichEntityResultsWithContentMetadataSkipsUnmatchedRows(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/other.ts", "Class", "Other",
					int64(1), int64(5), "typescript", "class Other {}", []byte(`{"decorators":["@other"]}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "Demo",
			"labels":     []string{"Class"},
			"file_path":  "src/decorators.ts",
			"repo_id":    "repo-1",
			"language":   "typescript",
			"start_line": 5,
			"end_line":   20,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "Demo", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}
	if _, ok := got[0]["metadata"]; ok {
		t.Fatalf("results[0][metadata] = %#v, want metadata to remain absent", got[0]["metadata"])
	}
	if _, ok := got[0]["semantic_profile"]; ok {
		t.Fatalf("results[0][semantic_profile] = %#v, want semantic profile to remain absent", got[0]["semantic_profile"])
	}
}

func TestEnrichEntityResultsWithContentMetadataRustImplBlock(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"impl-1", "repo-1", "src/point.rs", "ImplBlock", "Point",
					int64(1), int64(18), "rust", "impl Display for Point {}", []byte(`{"kind":"trait_impl","trait":"Display","target":"Point"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "Point",
			"labels":     []string{"ImplBlock"},
			"file_path":  "src/point.rs",
			"repo_id":    "repo-1",
			"language":   "rust",
			"start_line": 1,
			"end_line":   18,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "Point", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
	}
	if gotValue, want := metadata["kind"], "trait_impl"; gotValue != want {
		t.Fatalf("metadata[kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := metadata["trait"], "Display"; gotValue != want {
		t.Fatalf("metadata[trait] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := metadata["target"], "Point"; gotValue != want {
		t.Fatalf("metadata[target] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := got[0]["semantic_summary"], "ImplBlock Point implements Display for Point."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
}
