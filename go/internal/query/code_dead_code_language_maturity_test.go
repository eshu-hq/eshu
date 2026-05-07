package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestHandleDeadCodeReportsLanguageMaturity(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{
						"entity_id": "go-helper", "name": "goHelper", "labels": []any{"Function"},
						"file_path": "go/internal/query/helper.go", "repo_id": "repo-1", "repo_name": "eshu", "language": "go",
					},
					{
						"entity_id": "rust-helper", "name": "rust_helper", "labels": []any{"Function"},
						"file_path": "crates/eshu/src/lib.rs", "repo_id": "repo-1", "repo_name": "eshu", "language": "rust",
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1"}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp["data"].(map[string]any)
	analysis := data["analysis"].(map[string]any)
	maturity, ok := analysis["dead_code_language_maturity"].(map[string]any)
	if !ok {
		t.Fatalf("analysis[dead_code_language_maturity] type = %T, want map[string]any", analysis["dead_code_language_maturity"])
	}

	for language, want := range map[string]string{
		"go":         "derived",
		"python":     "derived",
		"javascript": "derived",
		"typescript": "derived",
		"tsx":        "derived",
		"rust":       "derived_candidate_only",
		"ruby":       "derived_candidate_only",
	} {
		if got := maturity[language]; got != want {
			t.Fatalf("maturity[%s] = %#v, want %#v", language, got, want)
		}
	}
}

func TestDeadCodeLanguageMaturityCoversParserSourceLanguages(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	for _, definition := range registry.Definitions() {
		key := definition.ParserKey
		if !deadCodeSourceParserKeys[key] {
			if _, ok := deadCodeLanguageMaturity[key]; ok {
				t.Fatalf("deadCodeLanguageMaturity[%q] exists, want non-source parser excluded", key)
			}
			continue
		}
		if _, ok := deadCodeLanguageMaturity[key]; !ok {
			t.Fatalf("deadCodeLanguageMaturity missing source parser key %q", key)
		}
	}
}

var deadCodeSourceParserKeys = map[string]bool{
	"c":          true,
	"c_sharp":    true,
	"cpp":        true,
	"dart":       true,
	"elixir":     true,
	"go":         true,
	"groovy":     true,
	"haskell":    true,
	"java":       true,
	"javascript": true,
	"kotlin":     true,
	"perl":       true,
	"php":        true,
	"python":     true,
	"ruby":       true,
	"rust":       true,
	"scala":      true,
	"swift":      true,
	"tsx":        true,
	"typescript": true,
}
