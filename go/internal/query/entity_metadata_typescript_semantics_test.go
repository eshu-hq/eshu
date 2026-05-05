package query

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"
)

func TestAttachTypeScriptSemanticsClonesResult(t *testing.T) {
	t.Parallel()

	result := map[string]any{
		"entity_id": "class-ts-1",
		"language":  "typescript",
	}

	got := AttachTypeScriptSemantics(result, map[string]any{
		"decorators":               []any{"@sealed"},
		"type_parameters":          []any{"T"},
		"type_alias_kind":          "mapped_type",
		"declaration_merge_group":  "Service",
		"declaration_merge_count":  2,
		"declaration_merge_kinds":  []any{"class", "namespace"},
		"component_type_assertion": "ComponentType",
		"component_wrapper_kind":   "memo",
		"jsx_fragment_shorthand":   true,
	})

	if _, ok := result["typescript_semantics"]; ok {
		t.Fatal("result was mutated, want original map unchanged")
	}

	semantics, ok := got["typescript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("typescript_semantics type = %T, want map[string]any", got["typescript_semantics"])
	}
	if got, want := semantics["decorators"], []string{"@sealed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := semantics["type_parameters"], []string{"T"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[type_parameters] = %#v, want %#v", got, want)
	}
	if got, want := semantics["type_alias_kind"], "mapped_type"; got != want {
		t.Fatalf("typescript_semantics[type_alias_kind] = %#v, want %#v", got, want)
	}
	if got, want := semantics["declaration_merge_group"], "Service"; got != want {
		t.Fatalf("typescript_semantics[declaration_merge_group] = %#v, want %#v", got, want)
	}
	if got, want := semantics["declaration_merge_count"], 2; got != want {
		t.Fatalf("typescript_semantics[declaration_merge_count] = %#v, want %#v", got, want)
	}
	if got, want := semantics["declaration_merge_kinds"], []string{"class", "namespace"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("typescript_semantics[declaration_merge_kinds] = %#v, want %#v", got, want)
	}
	if got, want := semantics["component_type_assertion"], "ComponentType"; got != want {
		t.Fatalf("typescript_semantics[component_type_assertion] = %#v, want %#v", got, want)
	}
	if got, want := semantics["component_wrapper_kind"], "memo"; got != want {
		t.Fatalf("typescript_semantics[component_wrapper_kind] = %#v, want %#v", got, want)
	}
	if got, want := semantics["jsx_fragment_shorthand"], true; got != want {
		t.Fatalf("typescript_semantics[jsx_fragment_shorthand] = %#v, want %#v", got, want)
	}
}

func TestAttachTypeScriptSemanticsReturnsOriginalWhenEmpty(t *testing.T) {
	t.Parallel()

	result := map[string]any{
		"entity_id": "class-ts-1",
		"language":  "typescript",
	}

	got := AttachTypeScriptSemantics(result, map[string]any{})

	if _, ok := got["typescript_semantics"]; ok {
		t.Fatal("typescript_semantics present, want absent")
	}
	if got["entity_id"] != "class-ts-1" {
		t.Fatalf("entity_id = %#v, want class-ts-1", got["entity_id"])
	}
}

func TestEnrichEntityResultsWithContentMetadataTypeScriptMappedTypeAlias(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/types.ts", "TypeAlias", "ReadonlyMap",
					int64(2), int64(4), "typescript", "type ReadonlyMap<T> = { readonly [K in keyof T]: T[K] }",
					[]byte(`{"type_alias_kind":"mapped_type","type_parameters":["T"]}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "ReadonlyMap",
			"labels":     []string{"TypeAlias"},
			"file_path":  "src/types.ts",
			"repo_id":    "repo-1",
			"language":   "typescript",
			"start_line": 2,
			"end_line":   4,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "ReadonlyMap", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	if gotValue, want := got[0]["semantic_summary"], "TypeAlias ReadonlyMap is a mapped type and declares type parameters T."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}

	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "mapped_type_alias"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := semanticProfile["type_alias_kind"], "mapped_type"; gotValue != want {
		t.Fatalf("semantic_profile[type_alias_kind] = %#v, want %#v", gotValue, want)
	}
}

func TestEnrichEntityResultsWithContentMetadataTypeScriptGenericInterface(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/interfaces.ts", "Interface", "Box",
					int64(2), int64(4), "typescript", "interface Box<T> { value: T }",
					[]byte(`{"type_parameters":["T"]}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "Box",
			"labels":     []string{"Interface"},
			"file_path":  "src/interfaces.ts",
			"repo_id":    "repo-1",
			"language":   "typescript",
			"start_line": 2,
			"end_line":   4,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "Box", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	if gotValue, want := got[0]["semantic_summary"], "Interface Box declares type parameters T."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}

	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "generic_declaration"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := semanticProfile["type_parameters"], []string{"T"}; !reflect.DeepEqual(gotValue, want) {
		t.Fatalf("semantic_profile[type_parameters] = %#v, want %#v", gotValue, want)
	}
}

func TestEnrichEntityResultsWithContentMetadataTypeScriptNamespaceModule(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/types.ts", "Module", "API",
					int64(1), int64(8), "typescript", "namespace API { }",
					[]byte(`{"module_kind":"namespace"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "API",
			"labels":     []string{"Module"},
			"file_path":  "src/types.ts",
			"repo_id":    "repo-1",
			"language":   "typescript",
			"start_line": 1,
			"end_line":   8,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "API", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	if gotValue, want := got[0]["semantic_summary"], "Module API is a namespace."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}

	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "namespace_module"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
}

func TestEnrichEntityResultsWithContentMetadataTypeScriptDeclarationMerging(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/merge.ts", "Class", "Service",
					int64(1), int64(6), "typescript", "class Service {}", []byte(`{"declaration_merge_group":"Service","declaration_merge_count":2,"declaration_merge_kinds":["class","namespace"]}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	results := []map[string]any{
		{
			"id":         "graph-1",
			"name":       "Service",
			"labels":     []string{"Class"},
			"file_path":  "src/merge.ts",
			"repo_id":    "repo-1",
			"language":   "typescript",
			"start_line": 1,
			"end_line":   6,
		},
	}

	got, err := handler.enrichEntityResultsWithContentMetadata(context.Background(), results, "repo-1", "Service", 20)
	if err != nil {
		t.Fatalf("enrichEntityResultsWithContentMetadata() error = %v, want nil", err)
	}

	if gotValue, want := got[0]["semantic_summary"], "Class Service participates in TypeScript declaration merging with namespace Service."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}

	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "declaration_merge"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := semanticProfile["declaration_merge_count"], 2; gotValue != want {
		t.Fatalf("semantic_profile[declaration_merge_count] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := semanticProfile["declaration_merge_kinds"], []string{"class", "namespace"}; !reflect.DeepEqual(gotValue, want) {
		t.Fatalf("semantic_profile[declaration_merge_kinds] = %#v, want %#v", gotValue, want)
	}

	if gotValue, want := got[0]["story"], "Class Service participates in TypeScript declaration merging with namespace Service. Defined in src/merge.ts (typescript)."; gotValue != want {
		t.Fatalf("results[0][story] = %#v, want %#v", gotValue, want)
	}
}
