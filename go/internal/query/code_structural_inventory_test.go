package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCodeHandlerStructuralInventoryReturnsBoundedDataclasses(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: structuralInventoryTestColumns(),
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/models.py", "Class", "Invoice",
					int64(10), int64(24), "python", "class Invoice: ...",
					[]byte(`{"decorators":["@dataclass"],"dead_code_root_kinds":["python.dataclass_model"]}`),
				},
				{
					"entity-2", "repo-1", "src/models.py", "Class", "Payment",
					int64(30), int64(44), "python", "class Payment: ...",
					[]byte(`{"decorators":["@dataclasses.dataclass"],"dead_code_root_kinds":["python.dataclass_model"]}`),
				},
			},
		},
	})
	handler := &CodeHandler{Content: NewContentReader(db), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/structure/inventory",
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"python","inventory_kind":"dataclass","limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(recorder.queries) = %d, want %d", got, want)
	}
	if !strings.Contains(recorder.queries[0], "metadata->'dead_code_root_kinds' ? 'python.dataclass_model'") {
		t.Fatalf("query = %q, want dataclass metadata predicate", recorder.queries[0])
	}
	if !strings.Contains(recorder.queries[0], "ORDER BY repo_id, relative_path, start_line, entity_name, entity_id") {
		t.Fatalf("query = %q, want deterministic ordering", recorder.queries[0])
	}
	if got, want := numericDriverValue(t, recorder.args[0][3]), int64(2); got != want {
		t.Fatalf("limit probe arg = %d, want %d", got, want)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := envelope.Truth.Capability, "code_inventory.structural"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisContentIndex; got != want {
		t.Fatalf("truth basis = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one trimmed result", data["results"])
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("results[0] type = %T, want map", results[0])
	}
	if got, want := first["match_kind"], "dataclass"; got != want {
		t.Fatalf("match_kind = %#v, want %#v", got, want)
	}
	sourceHandle, ok := first["source_handle"].(map[string]any)
	if !ok {
		t.Fatalf("source_handle type = %T, want map", first["source_handle"])
	}
	if got, want := sourceHandle["relative_path"], "src/models.py"; got != want {
		t.Fatalf("source_handle.relative_path = %#v, want %#v", got, want)
	}
}

func TestCodeHandlerStructuralInventoryFindsClassesWithMethod(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: structuralInventoryTestColumns(),
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/dashboard.ts", "Function", "render",
					int64(42), int64(52), "typescript", "render() { return null }",
					[]byte(`{"class_context":"Dashboard"}`),
				},
			},
		},
	})
	handler := &CodeHandler{Content: NewContentReader(db), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/structure/inventory",
		bytes.NewBufferString(`{"repo_id":"repo-1","inventory_kind":"class_with_method","method_name":"render","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	results := data["results"].([]any)
	first := results[0].(map[string]any)
	if got, want := first["class_name"], "Dashboard"; got != want {
		t.Fatalf("class_name = %#v, want %#v", got, want)
	}
	if got, want := first["match_kind"], "class_with_method"; got != want {
		t.Fatalf("match_kind = %#v, want %#v", got, want)
	}
}

func TestCodeHandlerStructuralInventoryCountsFunctionsPerFile(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"repo_id", "relative_path", "language", "function_count"},
			rows: [][]driver.Value{
				{"repo-1", "src/a.py", "python", int64(3)},
				{"repo-1", "src/b.py", "python", int64(1)},
			},
		},
	})
	handler := &CodeHandler{Content: NewContentReader(db), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/structure/inventory",
		bytes.NewBufferString(`{"repo_id":"repo-1","language":"python","inventory_kind":"function_count_by_file","limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(recorder.queries[0], "GROUP BY repo_id, relative_path") {
		t.Fatalf("query = %q, want grouped file-count query", recorder.queries[0])
	}
	if !strings.Contains(recorder.queries[0], "ORDER BY function_count DESC, repo_id, relative_path") {
		t.Fatalf("query = %q, want deterministic count ordering", recorder.queries[0])
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	results := data["results"].([]any)
	first := results[0].(map[string]any)
	if got, want := int(first["function_count"].(float64)), 3; got != want {
		t.Fatalf("function_count = %d, want %d", got, want)
	}
	sourceHandle := first["source_handle"].(map[string]any)
	if got, want := sourceHandle["content_tool"], "get_file_content"; got != want {
		t.Fatalf("source_handle.content_tool = %#v, want %#v", got, want)
	}
}

func TestCodeHandlerStructuralInventoryRejectsInvalidBounds(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: fakePortContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/structure/inventory",
		bytes.NewBufferString(`{"inventory_kind":"entity","offset":10001}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "offset must be <= 10000") {
		t.Fatalf("body = %s, want offset bound error", w.Body.String())
	}
}

func TestStructuralInventoryWhereUsesLanguageVariants(t *testing.T) {
	t.Parallel()

	where, args := structuralInventoryWhere(structuralInventoryRequest{Language: "typescript"})

	joined := strings.Join(where, " AND ")
	if !strings.Contains(joined, "language = $1 OR language = $2") {
		t.Fatalf("where = %q, want TypeScript language variants", joined)
	}
	if got, want := args, []any{"typescript", "tsx"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func structuralInventoryTestColumns() []string {
	return []string{
		"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
		"start_line", "end_line", "language", "source_cache", "metadata",
	}
}
