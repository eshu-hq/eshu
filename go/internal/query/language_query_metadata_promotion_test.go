// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"reflect"
	"testing"
)

// TestEnrichLanguageResultsWithContentMetadataPromotesExistingPythonSemanticsWithoutContent
// proves python_semantics is promoted from the graph row's own metadata even
// when the content store contributes zero matching rows.
//
// Driven through the mounted route (#6642): the graph row here carries the
// raw columns (decorators, async, type_annotation_count,
// type_annotation_kinds) buildLanguageResult's graphResultMetadata reads,
// rather than a pre-built "metadata" map and "semantic_summary" spliced
// directly into the row -- buildLanguageResult and attachSemanticSummary
// compute those from the raw columns exactly as production traffic does, so
// this is the same "existing" (graph-derived, not content-derived) metadata
// the original direct-call test fed in by hand.
func TestEnrichLanguageResultsWithContentMetadataPromotesExistingPythonSemanticsWithoutContent(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: nil,
		},
	})
	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":             "graph-1",
				"name":                  "handler",
				"labels":                []string{"Function"},
				"file_path":             "src/handler.py",
				"repo_id":               "repo-1",
				"language":              "python",
				"start_line":            int64(12),
				"end_line":              int64(24),
				"decorators":            []string{"@route"},
				"async":                 true,
				"type_annotation_count": int64(2),
				"type_annotation_kinds": []string{"parameter", "return"},
			},
		}},
		Content: NewContentReader(db),
	}
	result := languageQueryMetadataResult(t, handler,
		`{"language":"python","entity_type":"function","query":"handler","repo_id":"repo-1"}`)

	pythonSemantics, ok := result["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][python_semantics] type = %T, want map[string]any", result["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := asStringSlice(t, pythonSemantics["decorators"]), []string{"@route"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("python_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["async"], true; got != want {
		t.Fatalf("python_semantics[async] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["type_annotation_count"], float64(2); got != want {
		t.Fatalf("python_semantics[type_annotation_count] = %#v, want %#v", got, want)
	}
	if got, want := asStringSlice(t, pythonSemantics["type_annotation_kinds"]), []string{"parameter", "return"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("python_semantics[type_annotation_kinds] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Function handler is async, uses decorators @route, and has parameter and return type annotations."; got != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", got, want)
	}
}

// asStringSlice converts a JSON-decoded []any of strings back to []string so
// assertions can compare against a plain string-slice literal, matching how
// the pre-route direct-call tests in this family compared their in-process
// []string values.
func asStringSlice(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("value type = %T, want []any", value)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("value entry type = %T, want string", item)
		}
		out = append(out, s)
	}
	return out
}

func TestEnrichLanguageResultsWithContentMetadataRustImplBlock(t *testing.T) {
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

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "Point",
				"labels":     []string{"ImplBlock"},
				"file_path":  "src/point.rs",
				"repo_id":    "repo-1",
				"language":   "rust",
				"start_line": int64(1),
				"end_line":   int64(18),
			},
		}},
		Content: NewContentReader(db),
	}
	result := languageQueryMetadataResult(t, handler,
		`{"language":"rust","entity_type":"impl_block","query":"Point","repo_id":"repo-1"}`)

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", result["metadata"])
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
	if gotValue, want := result["semantic_summary"], "ImplBlock Point implements Display for Point."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
}

func TestEnrichLanguageResultsWithContentMetadataPreservesPythonGraphMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/models.py", "Class", "Logged",
					int64(4), int64(8), "python", "class Logged(metaclass=FallbackMeta): pass", []byte(`{"decorators":["@tracked"],"metaclass":"FallbackMeta"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "Logged",
				"labels":     []string{"Class"},
				"file_path":  "src/models.py",
				"repo_id":    "repo-1",
				"language":   "python",
				"start_line": int64(4),
				"end_line":   int64(8),
				"metaclass":  "MetaLogger",
			},
		}},
		Content: NewContentReader(db),
	}
	result := languageQueryMetadataResult(t, handler,
		`{"language":"python","entity_type":"class","query":"Logged","repo_id":"repo-1"}`)

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", result["metadata"])
	}
	if gotValue, want := metadata["metaclass"], "MetaLogger"; gotValue != want {
		t.Fatalf("metadata[metaclass] = %#v, want %#v", gotValue, want)
	}
	decorators, ok := metadata["decorators"].([]any)
	if !ok {
		t.Fatalf("metadata[decorators] type = %T, want []any", metadata["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@tracked" {
		t.Fatalf("metadata[decorators] = %#v, want [@tracked]", decorators)
	}
	if gotValue, want := result["semantic_summary"], "Class Logged uses decorators @tracked and uses metaclass MetaLogger."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if gotValue, want := profile["surface_kind"], "decorated_class"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := profile["metaclass"], "MetaLogger"; gotValue != want {
		t.Fatalf("semantic_profile[metaclass] = %#v, want %#v", gotValue, want)
	}
}
