// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetRepositoryContextIncludesLanguageBreakdown asserts that language_breakdown
// is present in the repository context response as a map[string]int when the
// graph returns language rows.
func TestGetRepositoryContextIncludesLanguageBreakdown(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": {
					"id":   "repo-fp",
					"name": "fingerprint-svc",
					"path": "/repos/fingerprint-svc",
				},
			},
			runByMatch: map[string][]map[string]any{
				"RETURN count(DISTINCT f) AS count":   {{"count": int64(20)}},
				"RETURN count(DISTINCT w) AS count":   {{"count": int64(1)}},
				"RETURN count(DISTINCT p) AS count":   {{"count": int64(1)}},
				"RETURN count(DISTINCT dep) AS count": {{"count": int64(0)}},
				"fn.name IN":                          {},
				"K8sResource OR":                      {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				// language distribution query
				"f.language IS NOT NULL": {
					{"language": "go", "file_count": int64(15)},
					{"language": "yaml", "file_count": int64(5)},
				},
				// source_tool breakdown query — repo-anchored outgoing edges
				"rel.source_tool IS NOT NULL": {
					{"source_tool": "terraform", "edge_count": int64(3)},
					{"source_tool": "helm", "edge_count": int64(1)},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-fp/context", nil)
	req.SetPathValue("repo_id", "repo-fp")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// language_breakdown must be a map with the expected counts.
	langBreakdown, ok := resp["language_breakdown"].(map[string]any)
	if !ok || langBreakdown == nil {
		t.Fatalf("language_breakdown = %#v, want map[string]any", resp["language_breakdown"])
	}
	if got, want := langBreakdown["go"], float64(15); got != want {
		t.Errorf("language_breakdown[go] = %v, want %v", got, want)
	}
	if got, want := langBreakdown["yaml"], float64(5); got != want {
		t.Errorf("language_breakdown[yaml] = %v, want %v", got, want)
	}

	// source_tool_breakdown must be present and correct.
	toolBreakdown, ok := resp["source_tool_breakdown"].(map[string]any)
	if !ok || toolBreakdown == nil {
		t.Fatalf("source_tool_breakdown = %#v, want map[string]any", resp["source_tool_breakdown"])
	}
	if got, want := toolBreakdown["terraform"], float64(3); got != want {
		t.Errorf("source_tool_breakdown[terraform] = %v, want %v", got, want)
	}
	if got, want := toolBreakdown["helm"], float64(1); got != want {
		t.Errorf("source_tool_breakdown[helm] = %v, want %v", got, want)
	}
}

// TestGetRepositoryContextOmitsBreakdownsWhenEmpty asserts that language_breakdown
// and source_tool_breakdown are absent from the response when the graph returns no
// rows for either aggregate, keeping the schema sparse.
func TestGetRepositoryContextOmitsBreakdownsWhenEmpty(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (r:Repository {id: $repo_id})": {
					"id":   "repo-empty",
					"name": "empty-svc",
					"path": "/repos/empty-svc",
				},
			},
			runByMatch: map[string][]map[string]any{
				"RETURN count(DISTINCT f) AS count":   {{"count": int64(0)}},
				"RETURN count(DISTINCT w) AS count":   {{"count": int64(0)}},
				"RETURN count(DISTINCT p) AS count":   {{"count": int64(0)}},
				"RETURN count(DISTINCT dep) AS count": {{"count": int64(0)}},
				"fn.name IN":                          {},
				"K8sResource OR":                      {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"f.language IS NOT NULL":              {},
				"rel.source_tool IS NOT NULL":         {},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-empty/context", nil)
	req.SetPathValue("repo_id", "repo-empty")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if _, present := resp["language_breakdown"]; present {
		t.Errorf("language_breakdown present when no language data, want absent")
	}
	if _, present := resp["source_tool_breakdown"]; present {
		t.Errorf("source_tool_breakdown present when no source_tool data, want absent")
	}
}

// TestQueryRepoSourceToolBreakdownCypherIsAnchored asserts that the Cypher
// emitted by queryRepoSourceToolBreakdown is anchored on the repository node
// (i.e. contains `{id: $repo_id}`) and does not use an all-node scan.
func TestQueryRepoSourceToolBreakdownCypherIsAnchored(t *testing.T) {
	t.Parallel()

	var capturedCypher string
	reader := &captureGraphQuery{
		runFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "source_tool IS NOT NULL") {
				capturedCypher = cypher
			}
			return nil, nil
		},
	}

	queryRepoSourceToolBreakdown(context.Background(), reader, map[string]any{"repo_id": "repo-test"})

	if capturedCypher == "" {
		t.Fatal("queryRepoSourceToolBreakdown did not issue a source_tool query")
	}
	if !strings.Contains(capturedCypher, "Repository {id: $repo_id}") {
		t.Errorf("source_tool breakdown query is not anchored on repo id:\n%s", capturedCypher)
	}
	// Must NOT be an all-node scan: no bare `MATCH ()-[rel]->()` pattern.
	if strings.Contains(capturedCypher, "MATCH ()-[") {
		t.Errorf("source_tool breakdown query uses all-node scan pattern:\n%s", capturedCypher)
	}
	// Must type the expansion to the source_tool-bearing Tier-2 verbs so it does
	// not fan out across REPO_CONTAINS (every File) before filtering. A regression
	// to an untyped `-[rel]->()` would drop the type list and fail this.
	if !strings.Contains(capturedCypher, "DEPENDS_ON|") {
		t.Errorf("source_tool breakdown query must restrict to typed Tier-2 verbs:\n%s", capturedCypher)
	}
}

// TestBuildLanguageBreakdownFromRows covers the helper that converts the
// language distribution row slice into a map.
func TestBuildLanguageBreakdownFromRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"language": "go", "file_count": 12},
		{"language": "yaml", "file_count": 3},
		{"language": "", "file_count": 1}, // should be skipped
	}
	got := buildLanguageBreakdownFromRows(rows)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got = %#v", len(got), got)
	}
	if got["go"] != 12 {
		t.Errorf("go = %d, want 12", got["go"])
	}
	if got["yaml"] != 3 {
		t.Errorf("yaml = %d, want 3", got["yaml"])
	}
	if _, hasEmpty := got[""]; hasEmpty {
		t.Error("empty language key present, want skipped")
	}
}

// TestBuildSourceToolBreakdownFromRows covers the helper that converts the
// source_tool edge row slice into a map.
func TestBuildSourceToolBreakdownFromRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"source_tool": "terraform", "edge_count": 7},
		{"source_tool": "ansible", "edge_count": 2},
		{"source_tool": "", "edge_count": 5}, // should be skipped
	}
	got := buildSourceToolBreakdownFromRows(rows)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; got = %#v", len(got), got)
	}
	if got["terraform"] != 7 {
		t.Errorf("terraform = %d, want 7", got["terraform"])
	}
	if got["ansible"] != 2 {
		t.Errorf("ansible = %d, want 2", got["ansible"])
	}
}

// captureGraphQuery is a minimal GraphQuery stub that records the Cypher strings
// it receives for assertion in bounded-query guard tests.
type captureGraphQuery struct {
	runFn func(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

func (c *captureGraphQuery) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if c.runFn != nil {
		return c.runFn(ctx, cypher, params)
	}
	return nil, nil
}

func (c *captureGraphQuery) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return nil, nil
}
