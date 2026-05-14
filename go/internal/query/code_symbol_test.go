package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodeHandlerFindSymbolReturnsBoundedContentDefinitions(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"entity-1", "repo-1", "src/app.ts", "Function", "renderApp",
					int64(10), int64(20), "typescript", "function renderApp() {}", []byte(`{"kind":"handler"}`),
				},
				{
					"entity-2", "repo-1", "src/other.ts", "Function", "renderApp",
					int64(30), int64(38), "typescript", "function renderApp() {}", []byte(`{"kind":"helper"}`),
				},
			},
		},
	})
	handler := &CodeHandler{Content: NewContentReader(db), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/symbols/search",
		bytes.NewBufferString(`{"symbol":"renderApp","repo_id":"repo-1","match_mode":"exact","limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got, want := len(recorder.args), 1; got != want {
		t.Fatalf("len(recorder.args) = %d, want %d", got, want)
	}
	if !strings.Contains(recorder.queries[0], "entity_name = $1") {
		t.Fatalf("query = %q, want exact indexed symbol predicate", recorder.queries[0])
	}
	if got, want := recorder.args[0][0], "renderApp"; got != want {
		t.Fatalf("symbol arg = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], "repo-1"; got != want {
		t.Fatalf("repo arg = %#v, want %#v", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][2]), int64(2); got != want {
		t.Fatalf("limit probe arg = %d, want %d", got, want)
	}
	if got, want := numericDriverValue(t, recorder.args[0][3]), int64(0); got != want {
		t.Fatalf("offset arg = %d, want %d", got, want)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := envelope.Truth.Capability, "code_search.symbol_lookup"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := int(data["count"].(float64)), 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	ambiguity, ok := data["ambiguity"].(map[string]any)
	if !ok {
		t.Fatalf("ambiguity type = %T, want map", data["ambiguity"])
	}
	if got, want := ambiguity["ambiguous"], true; got != want {
		t.Fatalf("ambiguity.ambiguous = %#v, want %#v", got, want)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v, want one trimmed result", data["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("results[0] type = %T, want map", results[0])
	}
	if got, want := result["classification"], "definition"; got != want {
		t.Fatalf("classification = %#v, want %#v", got, want)
	}
	if got, want := result["match_kind"], "exact"; got != want {
		t.Fatalf("match_kind = %#v, want %#v", got, want)
	}
	sourceHandle, ok := result["source_handle"].(map[string]any)
	if !ok {
		t.Fatalf("source_handle type = %T, want map", result["source_handle"])
	}
	if got, want := sourceHandle["repo_id"], "repo-1"; got != want {
		t.Fatalf("source_handle.repo_id = %#v, want %#v", got, want)
	}
}

func TestCodeHandlerFindSymbolRejectsHugeOffset(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: fakePortContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/symbols/search",
		bytes.NewBufferString(`{"symbol":"renderApp","offset":10001}`),
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

func TestCodeHandlerFindSymbolRejectsGraphOnlyOffset(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Neo4j: &stubGraphReader{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/symbols/search",
		bytes.NewBufferString(`{"symbol":"renderApp","offset":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "requires content-index search") {
		t.Fatalf("body = %s, want content-index offset error", w.Body.String())
	}
}
