package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleRelationshipsReturnsGraphBackedRustImplBlockOwnership(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{
					"id":         "impl-1",
					"name":       "Point",
					"labels":     []any{"ImplBlock"},
					"file_path":  "src/point.rs",
					"repo_id":    "repo-1",
					"repo_name":  "rust",
					"language":   "rust",
					"start_line": int64(1),
					"end_line":   int64(18),
					"outgoing": []any{
						map[string]any{
							"direction":   "outgoing",
							"type":        "CONTAINS",
							"reason":      "rust_impl_context",
							"target_name": "new",
							"target_id":   "fn-new",
						},
					},
					"incoming": []any{},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"impl-1","direction":"outgoing","relationship_type":"CONTAINS"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	outgoing, ok := resp["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("resp[outgoing] = %#v, want one graph-backed Rust impl relationship", resp["outgoing"])
	}
	relationship, ok := outgoing[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[outgoing][0] type = %T, want map[string]any", outgoing[0])
	}
	if got, want := relationship["type"], "CONTAINS"; got != want {
		t.Fatalf("relationship[type] = %#v, want %#v", got, want)
	}
	if got, want := relationship["reason"], "rust_impl_context"; got != want {
		t.Fatalf("relationship[reason] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_name"], "new"; got != want {
		t.Fatalf("relationship[target_name] = %#v, want %#v", got, want)
	}
	if got, want := relationship["target_id"], "fn-new"; got != want {
		t.Fatalf("relationship[target_id] = %#v, want %#v", got, want)
	}
}
