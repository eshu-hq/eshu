package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type mockLanguageQueryGraphReader struct {
	rows []map[string]any
}

func (m *mockLanguageQueryGraphReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return m.rows, nil
}

func (m *mockLanguageQueryGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if len(m.rows) == 0 {
		return nil, nil
	}
	return m.rows[0], nil
}

func TestEnrichLanguageResultsWithContentMetadata(t *testing.T) {
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
					int64(12), int64(20), "python", "async def handler(): ...", []byte(`{"decorators":["@route"],"async":true}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "handler",
			"labels":     []string{"Function"},
			"file_path":  "src/handler.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 12,
			"end_line":   20,
		},
	}

	got, err := handler.enrichLanguageResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"python",
		"Function",
		"handler",
		"repo-1",
		10,
	)
	if err != nil {
		t.Fatalf("enrichLanguageResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
	}
	if gotValue, want := metadata["async"], true; gotValue != want {
		t.Fatalf("metadata[async] = %#v, want %#v", gotValue, want)
	}
	decorators, ok := metadata["decorators"].([]any)
	if !ok {
		t.Fatalf("metadata[decorators] type = %T, want []any", metadata["decorators"])
	}
	if len(decorators) != 1 || decorators[0] != "@route" {
		t.Fatalf("metadata[decorators] = %#v, want [@route]", decorators)
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
	decoratorValues, ok := semanticProfile["decorators"].([]string)
	if !ok {
		t.Fatalf("semantic_profile[decorators] type = %T, want []string", semanticProfile["decorators"])
	}
	if len(decoratorValues) != 1 || decoratorValues[0] != "@route" {
		t.Fatalf("semantic_profile[decorators] = %#v, want [@route]", decoratorValues)
	}
}

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
	handler := &LanguageQueryHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "handler",
			"labels":     []string{"Function"},
			"file_path":  "src/handler.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 12,
			"end_line":   24,
			"metadata": map[string]any{
				"decorators":            []any{"@route"},
				"async":                 true,
				"type_annotation_count": 2,
				"type_annotation_kinds": []any{"parameter", "return"},
			},
			"semantic_summary": "Function handler is async, uses decorators @route, and has parameter and return type annotations.",
		},
	}

	got, err := handler.enrichLanguageResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"python",
		"Function",
		"handler",
		"repo-1",
		10,
	)
	if err != nil {
		t.Fatalf("enrichLanguageResultsWithContentMetadata() error = %v, want nil", err)
	}

	pythonSemantics, ok := got[0]["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][python_semantics] type = %T, want map[string]any", got[0]["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["decorators"], []string{"@route"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("python_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["async"], true; got != want {
		t.Fatalf("python_semantics[async] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["type_annotation_count"], 2; got != want {
		t.Fatalf("python_semantics[type_annotation_count] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["type_annotation_kinds"], []string{"parameter", "return"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("python_semantics[type_annotation_kinds] = %#v, want %#v", got, want)
	}
	if got, want := got[0]["semantic_summary"], "Function handler is async, uses decorators @route, and has parameter and return type annotations."; got != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", got, want)
	}
}

func TestEnrichLanguageResultsWithContentMetadataSkipsUnmatchedRows(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/other.py", "Function", "other",
					int64(1), int64(5), "python", "def other(): pass", []byte(`{"decorators":["@cached"]}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "handler",
			"labels":     []string{"Function"},
			"file_path":  "src/handler.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 12,
			"end_line":   20,
		},
	}

	got, err := handler.enrichLanguageResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"python",
		"Function",
		"handler",
		"repo-1",
		10,
	)
	if err != nil {
		t.Fatalf("enrichLanguageResultsWithContentMetadata() error = %v, want nil", err)
	}
	if _, ok := got[0]["metadata"]; ok {
		t.Fatalf("results[0][metadata] = %#v, want metadata to remain absent", got[0]["metadata"])
	}
	if _, ok := got[0]["semantic_summary"]; ok {
		t.Fatalf("results[0][semantic_summary] = %#v, want semantic summary to remain absent", got[0]["semantic_summary"])
	}
	if _, ok := got[0]["semantic_profile"]; ok {
		t.Fatalf("results[0][semantic_profile] = %#v, want semantic profile to remain absent", got[0]["semantic_profile"])
	}
}

func TestEnrichLanguageResultsWithContentMetadataAnnotation(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/Logged.java", "Annotation", "Logged",
					int64(2), int64(2), "java", "@Logged", []byte(`{"kind":"applied","target_kind":"method_declaration"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "Logged",
			"labels":     []string{"Annotation"},
			"file_path":  "src/Logged.java",
			"repo_id":    "repo-1",
			"language":   "java",
			"start_line": 2,
			"end_line":   2,
		},
	}

	got, err := handler.enrichLanguageResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"java",
		"Annotation",
		"Logged",
		"repo-1",
		10,
	)
	if err != nil {
		t.Fatalf("enrichLanguageResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
	}
	if gotValue, want := metadata["kind"], "applied"; gotValue != want {
		t.Fatalf("metadata[kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := got[0]["semantic_summary"], "Annotation Logged is applied to a method declaration."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	semanticProfile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := semanticProfile["surface_kind"], "applied_annotation"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
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

	handler := &LanguageQueryHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "Point",
			"labels":     []string{"ImplBlock"},
			"file_path":  "src/point.rs",
			"repo_id":    "repo-1",
			"language":   "rust",
			"start_line": 1,
			"end_line":   18,
		},
	}

	got, err := handler.enrichLanguageResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"rust",
		"ImplBlock",
		"Point",
		"repo-1",
		10,
	)
	if err != nil {
		t.Fatalf("enrichLanguageResultsWithContentMetadata() error = %v, want nil", err)
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

func TestHandleLanguageQuery_RustImplBlockPrefersGraphPathAndEnrichesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"content-1", "repo-1", "src/point.rs", "ImplBlock", "Point",
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
				"repo_name":  "repo-1",
				"language":   "rust",
				"start_line": int64(1),
				"end_line":   int64(18),
			},
		}},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"rust","entity_type":"impl_block","query":"Point","repo_id":"repo-1"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed impl block", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "graph-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "ImplBlock Point implements Display for Point."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("result[metadata] type = %T, want map[string]any", result["metadata"])
	}
	if got, want := metadata["kind"], "trait_impl"; got != want {
		t.Fatalf("metadata[kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_AnnotationPrefersGraphPathAndEnrichesMetadata(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/Logged.java", "Annotation", "Logged",
					int64(2), int64(2), "java", "@Logged", []byte(`{"kind":"applied","target_kind":"method_declaration"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "Logged",
				"labels":     []string{"Annotation"},
				"file_path":  "src/Logged.java",
				"repo_id":    "repo-1",
				"repo_name":  "repo-1",
				"language":   "java",
				"start_line": int64(2),
				"end_line":   int64(2),
			},
		}},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"java","entity_type":"annotation","query":"Logged","repo_id":"repo-1"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed annotation", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "graph-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Annotation Logged is applied to a method declaration."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "applied_annotation"; got != want {
		t.Fatalf("result[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_AnnotationFallsBackToContentWhenGraphMissing(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/Logged.java", "Annotation", "Logged",
					int64(2), int64(2), "java", "@Logged", []byte(`{"kind":"applied","target_kind":"method_declaration"}`),
				},
			},
		},
	})

	handler := &LanguageQueryHandler{
		Neo4j:   &mockLanguageQueryGraphReader{},
		Content: NewContentReader(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"java","entity_type":"annotation","query":"Logged","repo_id":"repo-1"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one content-backed annotation", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["entity_id"], "annotation-1"; got != want {
		t.Fatalf("result[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := result["semantic_summary"], "Annotation Logged is applied to a method declaration."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
}

func TestHandleLanguageQuery_AnnotationUsesGraphMetadataWithoutContent(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":   "graph-annotation-1",
				"name":        "Logged",
				"labels":      []string{"Annotation"},
				"file_path":   "src/Logged.java",
				"repo_id":     "repo-1",
				"repo_name":   "repo-1",
				"language":    "java",
				"start_line":  int64(2),
				"end_line":    int64(2),
				"kind":        "applied",
				"target_kind": "method_declaration",
			},
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"java","entity_type":"annotation","query":"Logged","repo_id":"repo-1"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	results, ok := resp["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one graph-backed annotation", resp["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", results[0])
	}
	if got, want := result["semantic_summary"], "Annotation Logged is applied to a method declaration."; got != want {
		t.Fatalf("result[semantic_summary] = %#v, want %#v", got, want)
	}
	profile, ok := result["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("result[semantic_profile] type = %T, want map[string]any", result["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "applied_annotation"; got != want {
		t.Fatalf("result[semantic_profile][surface_kind] = %#v, want %#v", got, want)
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

	handler := &LanguageQueryHandler{Content: NewContentReader(db)}
	graphResults := []map[string]any{
		{
			"entity_id":  "graph-1",
			"name":       "Logged",
			"labels":     []string{"Class"},
			"file_path":  "src/models.py",
			"repo_id":    "repo-1",
			"language":   "python",
			"start_line": 4,
			"end_line":   8,
			"metadata": map[string]any{
				"metaclass": "MetaLogger",
			},
		},
	}

	got, err := handler.enrichLanguageResultsWithContentMetadata(
		context.Background(),
		graphResults,
		"python",
		"Class",
		"Logged",
		"repo-1",
		10,
	)
	if err != nil {
		t.Fatalf("enrichLanguageResultsWithContentMetadata() error = %v, want nil", err)
	}

	metadata, ok := got[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][metadata] type = %T, want map[string]any", got[0]["metadata"])
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
	if gotValue, want := got[0]["semantic_summary"], "Class Logged uses decorators @tracked and uses metaclass MetaLogger."; gotValue != want {
		t.Fatalf("results[0][semantic_summary] = %#v, want %#v", gotValue, want)
	}
	profile, ok := got[0]["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("results[0][semantic_profile] type = %T, want map[string]any", got[0]["semantic_profile"])
	}
	if gotValue, want := profile["surface_kind"], "decorated_class"; gotValue != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := profile["metaclass"], "MetaLogger"; gotValue != want {
		t.Fatalf("semantic_profile[metaclass] = %#v, want %#v", gotValue, want)
	}
}
