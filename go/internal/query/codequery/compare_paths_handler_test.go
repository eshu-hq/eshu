// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/content"
)

// compareGraphFixture serves diamond outgoing edges by source entity id,
// the same shape the hop builder queries.
type compareGraphFixture struct {
	edges map[string][]map[string]any
}

func diamondCompareGraph() compareGraphFixture {
	edge := func(id, name string, confidence float64) map[string]any {
		return map[string]any{"id": id, "name": name, "repo_id": "repo-x", "edge_confidence": confidence}
	}
	return compareGraphFixture{edges: map[string][]map[string]any{
		"a": {edge("b", "b", 0.9), edge("x", "x", 0.95), edge("y", "y", 0.75)},
		"b": {edge("c", "c", 0.8), edge("e", "e", 0.5)},
		"c": {edge("d", "d", 0.7)},
		"d": {edge("e", "e", 0.6)},
		"x": {edge("e", "e", 0.85)},
		"y": {edge("e", "e", 0.65)},
	}}
}

func (f compareGraphFixture) run(_ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	_ = cypher
	id, _ := params["source_entity_id"].(string)
	return f.edges[id], nil
}

type comparePathsEnvelope struct {
	Data struct {
		Paths []struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"nodes"`
			Depth      int     `json:"depth"`
			Confidence float64 `json:"confidence"`
		} `json:"paths"`
		Truncated bool `json:"truncated"`
		MaxDepth  int  `json:"max_depth"`
		MaxPaths  int  `json:"max_paths"`
		Visited   int  `json:"visited"`
	} `json:"data"`
	Truth struct {
		Level string `json:"level"`
	} `json:"truth"`
}

func postComparePaths(t *testing.T, handler *CodeHandler, body string) (int, comparePathsEnvelope) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain/compare",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var envelope comparePathsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, rec.Body.String())
	}
	return rec.Code, envelope
}

func compareHandler(graph compareGraphFixture, backend GraphBackend) *CodeHandler {
	return &CodeHandler{
		Content:      content.FakePortContentStore{},
		Neo4j:        fakeGraphReader{run: graph.run},
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: backend,
	}
}

// TestCodeHandlerCompareCodePathsDiamond pins the route contract: the
// diamond yields four shortest-first simple paths with weakest-edge
// confidence, identical on both backends.
func TestCodeHandlerCompareCodePathsDiamond(t *testing.T) {
	t.Parallel()

	for _, backend := range []GraphBackend{GraphBackendNornicDB, GraphBackendNeo4j} {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			handler := compareHandler(diamondCompareGraph(), backend)
			code, envelope := postComparePaths(t, handler,
				`{"repo_id":"repo-x","start_entity_id":"a","end_entity_id":"e","max_depth":4,"max_paths":5}`)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want %d", code, http.StatusOK)
			}
			if len(envelope.Data.Paths) != 4 {
				t.Fatalf("paths = %d, want 4", len(envelope.Data.Paths))
			}
			if envelope.Data.Paths[0].Depth != 2 {
				t.Errorf("first path depth = %d, want 2 (shortest first)", envelope.Data.Paths[0].Depth)
			}
			if envelope.Data.Truncated {
				t.Errorf("truncated = true, want false")
			}
			if envelope.Data.Visited <= 0 {
				t.Errorf("visited = %d, want the traversal budget signal", envelope.Data.Visited)
			}
			// Exact like the sibling call-chain route: the paths are a
			// direct authoritative-graph read, not an assembled finding.
			if envelope.Truth.Level != "exact" {
				t.Errorf("truth level = %q, want exact (call-chain family)", envelope.Truth.Level)
			}
			// a-x-e confidence is min(0.95, 0.85).
			found := false
			for _, path := range envelope.Data.Paths {
				ids := make([]string, 0, len(path.Nodes))
				for _, node := range path.Nodes {
					ids = append(ids, node.ID)
				}
				if strings.Join(ids, ",") == "a,x,e" {
					found = true
					if path.Confidence != 0.85 {
						t.Errorf("a-x-e confidence = %v, want 0.85", path.Confidence)
					}
				}
			}
			if !found {
				t.Errorf("a-x-e path missing from %+v", envelope.Data.Paths)
			}
		})
	}
}

// TestCodeHandlerCompareCodePathsValidation pins the v1 scope: repo_id is
// required and cross_repo is refused.
func TestCodeHandlerCompareCodePathsValidation(t *testing.T) {
	t.Parallel()

	handler := compareHandler(diamondCompareGraph(), GraphBackendNornicDB)
	for _, body := range []string{
		`{"start_entity_id":"a","end_entity_id":"e"}`,
		`{"repo_id":"repo-x","start_entity_id":"a","end_entity_id":"e","cross_repo":true}`,
	} {
		code, _ := postComparePaths(t, handler, body)
		if code != http.StatusBadRequest {
			t.Errorf("body %s status = %d, want 400", body, code)
		}
	}
}
