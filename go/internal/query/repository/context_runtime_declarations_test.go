// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// sectionParamsCapture holds the section read's params for the assertion.
// It is a struct pointer, not a *map, so the reader stays assignable
// without a pointer-to-reference-type parameter.
type sectionParamsCapture struct {
	params map[string]any
}

func runtimeDeclarationsContextReader(sectionRows []map[string]any, captured *sectionParamsCapture) graph.FakeGraphReader {
	return graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repository:repo-a", "name": "repo-a"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "AS lowest_resolved_major") {
				if captured != nil {
					captured.params = params
				}
				return sectionRows, nil
			}
			// The fixtures list every runtime, so the section total is the
			// listed sum here; truncation has its own test in the declaration
			// package.
			if strings.Contains(cypher, "AS declaration_total") {
				var total int64
				for _, row := range sectionRows {
					if count, ok := row["declaration_count"].(int64); ok {
						total += count
					}
				}
				return []map[string]any{{"declaration_total": total}}, nil
			}
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return []map[string]any{}, nil
		},
	}
}

// TestRepositoryContextCarriesTheRuntimeDeclarationsSection pins the #8817
// section: a count and, per runtime, the lowest resolved major, read for the
// resolved repository id only.
func TestRepositoryContextCarriesTheRuntimeDeclarationsSection(t *testing.T) {
	t.Parallel()

	captured := &sectionParamsCapture{}
	reader := runtimeDeclarationsContextReader([]map[string]any{
		{"runtime": "node", "declaration_count": int64(3), "resolved_count": int64(2), "lowest_resolved_major": int64(16)},
		{"runtime": "python", "declaration_count": int64(1), "resolved_count": int64(0), "lowest_resolved_major": nil},
	}, captured)
	handler := &Handler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repository:repo-a/context", nil)
	req.SetPathValue("repo_id", "repository:repo-a")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	section, ok := body["runtime_declarations"].(map[string]any)
	if !ok {
		t.Fatalf("runtime_declarations = %#v, want an object", body["runtime_declarations"])
	}
	if section["count"] != float64(4) {
		t.Errorf("count = %v, want 4", section["count"])
	}
	runtimes, _ := section["runtimes"].([]any)
	if len(runtimes) != 2 {
		t.Fatalf("runtimes = %#v", section["runtimes"])
	}
	node := runtimes[0].(map[string]any)
	if node["runtime"] != "node" || node["lowest_resolved_major"] != float64(16) {
		t.Errorf("node = %v", node)
	}
	if python := runtimes[1].(map[string]any); python["lowest_resolved_major"] != nil {
		t.Errorf("python lowest_resolved_major = %v, want null", python["lowest_resolved_major"])
	}
	if captured.params["repo_id"] != "repository:repo-a" {
		t.Errorf("section read params = %v, want the resolved repository id", captured.params)
	}
	reasons := testutil.RequireStringAnySlice(t, body, "partial_reasons")
	if testutil.AnySliceContains(reasons, runtimeDeclarationsReadDegradedReason) {
		t.Errorf("healthy section read reported a degrade: %v", reasons)
	}
}

// TestRepositoryContextWithoutDeclarationsReportsAnEmptySection: a repository
// that declares nothing says so with count 0, not by omitting the section.
func TestRepositoryContextWithoutDeclarationsReportsAnEmptySection(t *testing.T) {
	t.Parallel()

	handler := &Handler{Neo4j: runtimeDeclarationsContextReader(nil, nil)}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repository:repo-a/context", nil)
	req.SetPathValue("repo_id", "repository:repo-a")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	body := decodeRepositoryAuthzBody(t, rec)
	section, ok := body["runtime_declarations"].(map[string]any)
	if !ok || section["count"] != float64(0) {
		t.Fatalf("runtime_declarations = %#v, want count 0", body["runtime_declarations"])
	}
	if runtimes, ok := section["runtimes"].([]any); !ok || len(runtimes) != 0 {
		t.Errorf("runtimes = %#v, want an empty list", section["runtimes"])
	}
}
