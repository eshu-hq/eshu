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

// TestHandleRelationshipsReturnsInheritanceAndOverrideEdgesForClassHierarchy
// proves the symbol_graph.inheritance capability end to end through the real
// /api/v0/code/relationships handler: for a class-hierarchy fixture whose
// graph row mixes INHERITS, OVERRIDES, and CALLS edges, a request with
// relationship_type "INHERITS" returns only the base-class edge and a
// request with relationship_type "OVERRIDES" returns only the overridden
// method edge. Existing coverage in code_relationships_graph_test.go
// exercises this handler with relationship_type "CALLS" only, so neither
// filter value had a committed capability-specific assertion before this
// test (issue #5681 cluster A).
func TestHandleRelationshipsReturnsInheritanceAndOverrideEdgesForClassHierarchy(t *testing.T) {
	t.Parallel()

	classHierarchyRow := []map[string]any{
		{
			"id":         "class-premium-account",
			"name":       "PremiumAccount",
			"labels":     []any{"Class"},
			"file_path":  "src/accounts/premium.py",
			"repo_id":    "repo-1",
			"repo_name":  "accounts",
			"language":   "python",
			"start_line": int64(5),
			"end_line":   int64(40),
			"outgoing": []any{
				map[string]any{
					"direction":   "outgoing",
					"type":        "INHERITS",
					"target_name": "Account",
					"target_id":   "class-account",
				},
				map[string]any{
					"direction":   "outgoing",
					"type":        "OVERRIDES",
					"target_name": "Account.close",
					"target_id":   "function-account-close",
				},
				map[string]any{
					"direction":   "outgoing",
					"type":        "CALLS",
					"target_name": "auditLog",
					"target_id":   "function-audit-log",
				},
			},
			"incoming": []any{},
		},
	}

	tests := []struct {
		name             string
		relationshipType string
		wantTargetName   string
	}{
		{name: "inherits", relationshipType: "INHERITS", wantTargetName: "Account"},
		{name: "overrides", relationshipType: "OVERRIDES", wantTargetName: "Account.close"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := &CodeHandler{
				Neo4j: fakeGraphReader{
					run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
						if !strings.Contains(cypher, "WHERE e.name = $name") {
							t.Fatalf("cypher = %q, want exact name lookup", cypher)
						}
						if got, want := params["name"], "PremiumAccount"; got != want {
							t.Fatalf("params[name] = %#v, want %#v", got, want)
						}
						return classHierarchyRow, nil
					},
				},
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v0/code/relationships",
				bytes.NewBufferString(`{"name":"PremiumAccount","direction":"outgoing","relationship_type":"`+tt.relationshipType+`"}`),
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
				t.Fatalf("len(resp[outgoing]) = %d, want %d (other edge types must be filtered out): %#v", got, want, outgoing)
			}
			edge, ok := outgoing[0].(map[string]any)
			if !ok {
				t.Fatalf("resp[outgoing][0] type = %T, want map[string]any", outgoing[0])
			}
			if got, want := edge["type"], tt.relationshipType; got != want {
				t.Fatalf("resp[outgoing][0][type] = %#v, want %#v", got, want)
			}
			if got, want := edge["target_name"], tt.wantTargetName; got != want {
				t.Fatalf("resp[outgoing][0][target_name] = %#v, want %#v", got, want)
			}
		})
	}
}
