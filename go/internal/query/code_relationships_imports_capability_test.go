// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// TestHandleRelationshipsReturnsImportEdgesForImportsRelationshipType proves
// the symbol_graph.imports capability end to end through the real
// /api/v0/code/relationships handler: a request with relationship_type
// "IMPORTS" against a fixture whose graph row mixes an IMPORTS edge with a
// CALLS edge must return only the IMPORTS edge, not the noise. Existing
// coverage in code_relationships_graph_test.go exercises this handler with
// relationship_type "CALLS" only, so the "IMPORTS" filter path itself has no
// committed capability-specific assertion before this test (issue #5681
// cluster A).
func TestHandleRelationshipsReturnsImportEdgesForImportsRelationshipType(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "WHERE e.name = $name") {
					t.Fatalf("cypher = %q, want exact name lookup", cypher)
				}
				if got, want := params["name"], "billing"; got != want {
					t.Fatalf("params[name] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"id":         "module-billing",
						"name":       "billing",
						"labels":     []any{"Module"},
						"file_path":  "src/billing/__init__.py",
						"repo_id":    "repo-1",
						"repo_name":  "payments",
						"language":   "python",
						"start_line": int64(1),
						"end_line":   int64(1),
						"outgoing": []any{
							map[string]any{
								"direction":   "outgoing",
								"type":        "IMPORTS",
								"target_name": "stripe_client",
								"target_id":   "module-stripe-client",
							},
							map[string]any{
								"direction":   "outgoing",
								"type":        "CALLS",
								"target_name": "chargeCard",
								"target_id":   "function-charge-card",
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
		bytes.NewBufferString(`{"name":"billing","direction":"outgoing","relationship_type":"IMPORTS"}`),
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
	if !ok {
		t.Fatalf("resp[outgoing] type = %T, want []any", resp["outgoing"])
	}
	if got, want := len(outgoing), 1; got != want {
		t.Fatalf("len(resp[outgoing]) = %d, want %d (CALLS noise must be filtered out): %#v", got, want, outgoing)
	}
	edge, ok := outgoing[0].(map[string]any)
	if !ok {
		t.Fatalf("resp[outgoing][0] type = %T, want map[string]any", outgoing[0])
	}
	if got, want := edge["type"], "IMPORTS"; got != want {
		t.Fatalf("resp[outgoing][0][type] = %#v, want %#v", got, want)
	}
	if got, want := edge["target_name"], "stripe_client"; got != want {
		t.Fatalf("resp[outgoing][0][target_name] = %#v, want %#v", got, want)
	}
}
