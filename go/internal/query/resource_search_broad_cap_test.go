// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// broadCapHandlerGraph answers the broad-class probe with a fixed candidate
// count and the main read with fixed rows.
type broadCapHandlerGraph struct {
	calls      []recordedInfraCall
	probeCount int64
	mainRows   []map[string]any
}

func (g *broadCapHandlerGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.calls = append(g.calls, recordedInfraCall{Cypher: cypher, Params: params})
	if strings.Contains(cypher, "candidate_count") {
		return []map[string]any{{"candidate_count": g.probeCount}}, nil
	}
	return g.mainRows, nil
}

func (g *broadCapHandlerGraph) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

func serveBroadCapSearch(t *testing.T, graph *broadCapHandlerGraph, body string) map[string]any {
	t.Helper()
	handler := &InfraHandler{GraphBackend: querycontract.GraphBackendNeo4j, Neo4j: graph}
	handler.SearchKey = readySearchKeyReader(t, true)
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, infraSearchPath, strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d body %.300s", body, rec.Code, rec.Body.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, _ := decoded["data"].(map[string]any)
	if data == nil {
		data = decoded
	}
	return data
}

func broadCapMainRow(name string) map[string]any {
	return map[string]any{
		"id": name, "name": name, "labels": []any{"CloudResource"},
		"kind": "k", "provider": "aws", "environment": "qa",
		"source": "s", "config_path": "c",
	}
}

// A broad query with more than K candidates must return the capped page with
// truncated set, even when the page itself is smaller than the limit.
func TestInfraSearchBroadOverCapMarksTruncated(t *testing.T) {
	t.Parallel()
	graph := &broadCapHandlerGraph{probeCount: fulltext.BroadCandidateCap + 1}
	for _, name := range []string{"a-one", "a-two", "a-three"} {
		graph.mainRows = append(graph.mainRows, broadCapMainRow(name))
	}
	data := serveBroadCapSearch(t, graph, `{"query":"a"}`)
	if data["truncated"] != true {
		t.Fatalf("truncated = %v, want true when the broad cap hits", data["truncated"])
	}
	if got := len(data["results"].([]any)); got != 3 {
		t.Fatalf("results = %d rows, want 3", got)
	}
	if len(graph.calls) != 2 {
		t.Fatalf("statements = %d, want one probe then one capped read", len(graph.calls))
	}
	if !strings.Contains(graph.calls[1].Cypher, "WITH n LIMIT "+strconv.Itoa(fulltext.BroadCandidateCap)) {
		t.Fatalf("capped statement must bound post-index evaluation at K:\n%s", graph.calls[1].Cypher)
	}
}

// At exactly K candidates the broad page must be uncapped with no marker.
func TestInfraSearchBroadAtCapIsUncapped(t *testing.T) {
	t.Parallel()
	graph := &broadCapHandlerGraph{probeCount: fulltext.BroadCandidateCap}
	graph.mainRows = []map[string]any{broadCapMainRow("a-one"), broadCapMainRow("a-two")}
	data := serveBroadCapSearch(t, graph, `{"query":"a"}`)
	if data["truncated"] != false {
		t.Fatalf("truncated = %v, want false when candidates fit in K", data["truncated"])
	}
	if len(graph.calls) != 2 {
		t.Fatalf("statements = %d, want one probe then one uncapped read", len(graph.calls))
	}
	if strings.Contains(graph.calls[1].Cypher, "WITH n LIMIT ") {
		t.Fatalf("at-cap statement must not bound evaluation:\n%s", graph.calls[1].Cypher)
	}
}

// A selective issue term must take one unchanged statement with no probe and
// no marker.
func TestInfraSearchSelectiveHasNoCapEffect(t *testing.T) {
	t.Parallel()
	graph := &broadCapHandlerGraph{probeCount: fulltext.BroadCandidateCap + 1}
	graph.mainRows = []map[string]any{broadCapMainRow("aws_iam_role")}
	data := serveBroadCapSearch(t, graph, `{"query":"aws_iam_role"}`)
	if data["truncated"] != false {
		t.Fatalf("truncated = %v, want false for a selective term", data["truncated"])
	}
	if len(graph.calls) != 1 {
		t.Fatalf("statements = %d, want one uncapped read and no probe", len(graph.calls))
	}
	if strings.Contains(graph.calls[0].Cypher, "candidate_count") || strings.Contains(graph.calls[0].Cypher, "WITH n LIMIT ") {
		t.Fatalf("selective statement must carry no cap machinery:\n%s", graph.calls[0].Cypher)
	}
}
