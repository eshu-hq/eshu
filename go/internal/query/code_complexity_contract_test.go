package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleComplexityListReturnsTruncationInEnvelope(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "ORDER BY complexity DESC, e.name, e.id") {
					t.Fatalf("cypher = %q, want deterministic complexity order", cypher)
				}
				if got, want := params["limit"], 3; got != want {
					t.Fatalf("params[limit] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{"id": "function:one", "name": "one", "labels": []any{"Function"}, "complexity": int64(13)},
					{"id": "function:two", "name": "two", "labels": []any{"Function"}, "complexity": int64(11)},
					{"id": "function:three", "name": "three", "labels": []any{"Function"}, "complexity": int64(9)},
				}, nil
			},
		},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":2}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleComplexity(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %#v, want two rows", data["results"])
	}
	if got, want := data["limit"], float64(2); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}
