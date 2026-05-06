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

func TestHandleComplexityReturnsGraphBackedJavaScriptSemantics(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if got, want := params["entity_id"], "function-1"; got != want {
					t.Fatalf("params[entity_id] = %#v, want %#v", got, want)
				}
				if !strings.Contains(cypher, "e.docstring as docstring") {
					t.Fatalf("cypher = %q, want graph semantic projection", cypher)
				}
				if !strings.Contains(cypher, "e.method_kind as method_kind") {
					t.Fatalf("cypher = %q, want method_kind projection", cypher)
				}
				return map[string]any{
					"id":                  "function-1",
					"name":                "getTab",
					"labels":              []any{"Function"},
					"file_path":           "src/app.js",
					"repo_id":             "repo-1",
					"repo_name":           "ui",
					"language":            "javascript",
					"start_line":          int64(10),
					"end_line":            int64(24),
					"outgoing_count":      int64(2),
					"incoming_count":      int64(1),
					"total_relationships": int64(3),
					"docstring":           "Returns the active tab.",
					"method_kind":         "getter",
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/complexity",
		bytes.NewBufferString(`{"entity_id":"function-1"}`),
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
	if got, want := resp["semantic_summary"], "Function getTab has JavaScript method kind getter and is documented as \"Returns the active tab.\"."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	jsSemantics, ok := resp["javascript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("resp[javascript_semantics] type = %T, want map[string]any", resp["javascript_semantics"])
	}
	if got, want := jsSemantics["method_kind"], "getter"; got != want {
		t.Fatalf("javascript_semantics[method_kind] = %#v, want %#v", got, want)
	}
	if got, want := jsSemantics["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("javascript_semantics[docstring] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "javascript_method"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}

func TestHandleCallChainReturnsGraphBackedJavaScriptSemanticsOnNodes(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "docstring: node.docstring") {
					t.Fatalf("cypher = %q, want graph semantic projection for call-chain nodes", cypher)
				}
				if !strings.Contains(cypher, "method_kind: node.method_kind") {
					t.Fatalf("cypher = %q, want JavaScript method_kind projection for call-chain nodes", cypher)
				}
				if got, want := params["start"], "wrapper"; got != want {
					t.Fatalf("params[start] = %#v, want %#v", got, want)
				}
				if got, want := params["end"], "helper"; got != want {
					t.Fatalf("params[end] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"chain": []any{
							map[string]any{
								"id":          "function-1",
								"name":        "getTab",
								"labels":      []any{"Function"},
								"language":    "javascript",
								"docstring":   "Returns the active tab.",
								"method_kind": "getter",
							},
						},
						"depth": int64(1),
					},
				}, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start":"wrapper","end":"helper","max_depth":6}`),
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
	chains, ok := resp["chains"].([]any)
	if !ok || len(chains) != 1 {
		t.Fatalf("resp[chains] = %#v, want one graph-backed chain", resp["chains"])
	}
	chain, ok := chains[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[chains][0] type = %T, want map[string]any", chains[0])
	}
	nodes, ok := chain["chain"].([]any)
	if !ok || len(nodes) != 1 {
		t.Fatalf("resp[chains][0][chain] = %#v, want one graph-backed node", chain["chain"])
	}
	node, ok := nodes[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[chains][0][chain][0] type = %T, want map[string]any", nodes[0])
	}
	if got, want := node["semantic_summary"], "Function getTab has JavaScript method kind getter and is documented as \"Returns the active tab.\"."; got != want {
		t.Fatalf("chain node semantic_summary = %#v, want %#v", got, want)
	}
	jsSemantics, ok := node["javascript_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("chain node javascript_semantics type = %T, want map[string]any", node["javascript_semantics"])
	}
	if got, want := jsSemantics["method_kind"], "getter"; got != want {
		t.Fatalf("javascript_semantics[method_kind] = %#v, want %#v", got, want)
	}
	if got, want := jsSemantics["docstring"], "Returns the active tab."; got != want {
		t.Fatalf("javascript_semantics[docstring] = %#v, want %#v", got, want)
	}
	profile, ok := node["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("chain node semantic_profile type = %T, want map[string]any", node["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "javascript_method"; got != want {
		t.Fatalf("semantic_profile[surface_kind] = %#v, want %#v", got, want)
	}
}
