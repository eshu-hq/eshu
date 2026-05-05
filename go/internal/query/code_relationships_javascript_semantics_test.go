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

func TestHandleRelationshipsReturnsGraphBackedJavaScriptSemantics(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "e.docstring as docstring") {
					t.Fatalf("cypher = %q, want graph semantic projection", cypher)
				}
				if got, want := params["name"], "handlePayment"; got != want {
					t.Fatalf("params[name] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"id":          "function-1",
						"name":        "handlePayment",
						"labels":      []any{"Function"},
						"file_path":   "src/payments.ts",
						"repo_id":     "repo-1",
						"repo_name":   "payments",
						"language":    "typescript",
						"start_line":  int64(10),
						"end_line":    int64(32),
						"docstring":   "Handles incoming payment requests.",
						"method_kind": "async",
						"outgoing": []any{
							map[string]any{
								"direction":   "outgoing",
								"type":        "CALLS",
								"target_name": "chargeCard",
								"target_id":   "function-2",
							},
						},
						"incoming": []any{},
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"name":"handlePayment","direction":"outgoing","relationship_type":"CALLS"}`),
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
	if got, want := resp["entity_id"], "function-1"; got != want {
		t.Fatalf("resp[entity_id] = %#v, want %#v", got, want)
	}
	jsSemantics, ok := resp["javascript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("resp[javascript_semantics] type = %T, want map[string]any", resp["javascript_semantics"])
	}
	if got, want := jsSemantics["method_kind"], "async"; got != want {
		t.Fatalf("javascript_semantics[method_kind] = %#v, want %#v", got, want)
	}
	if got, want := jsSemantics["docstring"], "Handles incoming payment requests."; got != want {
		t.Fatalf("javascript_semantics[docstring] = %#v, want %#v", got, want)
	}
	outgoing, ok := resp["outgoing"].([]any)
	if !ok {
		t.Fatalf("resp[outgoing] type = %T, want []any", resp["outgoing"])
	}
	if len(outgoing) != 1 {
		t.Fatalf("len(resp[outgoing]) = %d, want 1", len(outgoing))
	}
}
