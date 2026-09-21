// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestListMostComplexFunctionsResortsBackendDelivery is the #6915 Eshu-half
// regression: the backend may deliver rows in an order that does not match
// ORDER BY complexity DESC, e.name, e.id (proven NornicDB multi-key mis-sort
// on the capture legs). The API contract is top-N by those keys, so the
// handler must re-sort in Go before truncating instead of trusting delivery
// order. The fake returns three rows backend-misordered with limit 2: without
// a Go re-sort the wrong member survives truncation.
func TestListMostComplexFunctionsResortsBackendDelivery(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				row := func(id, name string, complexity int64) map[string]any {
					return map[string]any{
						"id":         id,
						"name":       name,
						"labels":     []any{"Function"},
						"file_path":  "src/" + name + ".go",
						"repo_id":    "repo-1",
						"repo_name":  "catalog",
						"language":   "go",
						"start_line": int64(1),
						"end_line":   int64(10),
						"complexity": complexity,
					}
				}
				return []map[string]any{
					row("function-low", "zebra", 3),
					row("function-top", "apple", 25),
					row("function-mid", "mango", 9),
				}, nil
			},
		},
	}

	results, limit, truncated, err := handler.listMostComplexFunctions(
		context.Background(), "repo-1", 2,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("listMostComplexFunctions() error = %v, want nil", err)
	}
	if limit != 2 {
		t.Fatalf("limit = %d, want 2", limit)
	}
	if !truncated {
		t.Fatal("truncated = false, want true for 3 rows at limit 2")
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	wantIDs := []string{"function-top", "function-mid"}
	for i, want := range wantIDs {
		if got := StringVal(results[i], "entity_id"); got != want {
			t.Fatalf("results[%d].entity_id = %q, want %q (full order: %v)", i, got, want, results)
		}
	}
}

// TestListMostComplexFunctionsBreaksNameTiesByEntityID pins the deterministic
// tiebreak: rows tied on complexity and name order by entity_id, so the API
// answer is fully deterministic (entity ids are unique) regardless of backend
// delivery order.
func TestListMostComplexFunctionsBreaksNameTiesByEntityID(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				row := func(id string) map[string]any {
					return map[string]any{
						"id":         id,
						"name":       "same",
						"labels":     []any{"Function"},
						"file_path":  "src/same.go",
						"repo_id":    "repo-1",
						"repo_name":  "catalog",
						"language":   "go",
						"start_line": int64(1),
						"end_line":   int64(10),
						"complexity": int64(7),
					}
				}
				return []map[string]any{row("function-b"), row("function-a")}, nil
			},
		},
	}

	results, _, _, err := handler.listMostComplexFunctions(
		context.Background(), "repo-1", 10,
		querycontract.RepositoryAccessFilter{AllScopes: true},
	)
	if err != nil {
		t.Fatalf("listMostComplexFunctions() error = %v, want nil", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if got := StringVal(results[0], "entity_id"); got != "function-a" {
		t.Fatalf("results[0].entity_id = %q, want %q (name ties break by entity_id)", got, "function-a")
	}
}
