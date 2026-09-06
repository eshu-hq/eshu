// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// assertNoImpactLabelDisjunction fails when a by-id anchor uses the label
// disjunction (`A|B|C`), which matches zero rows on the pinned NornicDB build.
// Mirrors the impacttrace test helper for handler-level Cypher assertions;
// the disjunction constant itself stays home in impacttrace. See #6060.
func assertNoImpactLabelDisjunction(t *testing.T, cypher string) {
	t.Helper()
	if strings.Contains(cypher, impacttrace.ImpactAnchorLabelDisjunction) {
		t.Fatalf("by-id anchor must use per-label inline-property anchors, not the label disjunction: %s", cypher)
	}
}

func TestExplainDependencyPathNullPathRecordOmitsPath(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j: querytestutil.FakeGraphReaderWithSingle{
			RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "shortestPath") {
					// A non-nil record with null path columns (no path found).
					return map[string]any{"depth": nil, "ns": nil, "rels": nil}, nil
				}
				if _, ok := params["source_id"]; ok {
					return map[string]any{"label": "CloudResource", "id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}, nil
				}
				return map[string]any{"label": "Repository", "id": "repo:api", "name": "api", "labels": []any{"Repository"}}, nil
			},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/explain-dependency-path", bytes.NewBufferString(`{"source":"resource:queue","target":"repo:api"}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.explainDependencyPath(rec, req)

	data := querytestutil.DecodeImpactEnvelopeData(t, rec)
	if v, ok := data["path"]; ok && v != nil {
		t.Fatalf("path = %#v, want absent/null for a null shortestPath record", v)
	}
	if src, _ := data["source"].(map[string]any); querycontract.StringVal(src, "id") != "resource:queue" {
		t.Fatalf("source = %#v, want resolved resource:queue", data["source"])
	}
}

// assertNoImpactLabelDisjunction fails when a by-id anchor uses the label
// disjunction (`A|B|C`), which matches zero rows on the pinned NornicDB build.

func TestTraceResourceToCodeAnchorsResolvedLabel(t *testing.T) {
	t.Parallel()

	var resolveCypher, traversalCypher string
	handler := &ImpactHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j: querytestutil.FakeGraphReaderWithSingle{
			RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				resolveCypher = cypher
				return map[string]any{"label": "CloudResource", "id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}, nil
			},
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				traversalCypher = cypher
				return []map[string]any{
					{"repo_id": "repo-a", "repo_name": "api", "depth": int64(1), "rels": []any{
						map[string]any{"type": "DEPENDS_ON", "properties": map[string]any{"confidence": 0.9, "reason": "import"}},
					}},
				}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-resource-to-code",
		bytes.NewBufferString(`{"start":"resource:queue","max_depth":4,"limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.traceResourceToCode(rec, req)

	data := querytestutil.DecodeImpactEnvelopeData(t, rec)
	assertNoImpactLabelDisjunction(t, resolveCypher)
	assertNoImpactLabelDisjunction(t, traversalCypher)
	if !strings.Contains(traversalCypher, "(start:CloudResource {id: $start_id})") {
		t.Fatalf("traversal must anchor the resolved label inline: %s", traversalCypher)
	}
	if strings.Contains(traversalCypher, "MATCH (start) WHERE") {
		t.Fatalf("traversal must not anchor an unlabeled start node: %s", traversalCypher)
	}
	if !strings.Contains(traversalCypher, "(repo:Repository)") {
		t.Fatalf("repo target label must be preserved: %s", traversalCypher)
	}
	if !strings.Contains(traversalCypher, "LIMIT $limit") {
		t.Fatalf("server-side LIMIT must be preserved: %s", traversalCypher)
	}
	// Per-edge hop provenance is built in Go from relationships(path).
	paths, ok := data["paths"].([]any)
	if !ok || len(paths) != 1 {
		t.Fatalf("paths = %#v, want one path", data["paths"])
	}
	first, _ := paths[0].(map[string]any)
	hops, _ := first["hops"].([]any)
	if len(hops) != 1 {
		t.Fatalf("hops = %#v, want one hop from relationships(path)", first["hops"])
	}
	hop0, _ := hops[0].(map[string]any)
	if hop0["type"] != "DEPENDS_ON" || hop0["reason"] != "import" {
		t.Fatalf("hop provenance not decoded from rels: %#v", hop0)
	}
}

func TestTraceResourceToCodeReturnsStartWithoutPaths(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j: querytestutil.FakeGraphReaderWithSingle{
			RunSingleFn: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{"label": "CloudResource", "id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}, nil
			},
			RunFn: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return nil, nil // no Repository paths
			},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-resource-to-code", bytes.NewBufferString(`{"start":"resource:queue"}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.traceResourceToCode(rec, req)

	data := querytestutil.DecodeImpactEnvelopeData(t, rec)
	start, _ := data["start"].(map[string]any)
	if start["id"] != "resource:queue" || start["name"] != "queue" {
		t.Fatalf("start must be hydrated from the resolver even with no paths: %#v", data["start"])
	}
	if got, want := data["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want 0", got)
	}
}

func TestExplainDependencyPathAnchorsResolvedEndpoints(t *testing.T) {
	t.Parallel()

	var resolveCyphers []string
	var pathCypher string
	handler := &ImpactHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j: querytestutil.FakeGraphReaderWithSingle{
			RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "shortestPath") {
					pathCypher = cypher
					return nil, nil // no path found in this shape assertion
				}
				// A per-label CALL{UNION} resolve, one per endpoint.
				resolveCyphers = append(resolveCyphers, cypher)
				if _, ok := params["source_id"]; ok {
					return map[string]any{"label": "CloudResource", "id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}, nil
				}
				return map[string]any{"label": "Repository", "id": "repo:api", "name": "api", "labels": []any{"Repository"}}, nil
			},
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/explain-dependency-path",
		bytes.NewBufferString(`{"source":"resource:queue","target":"repo:api"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.explainDependencyPath(rec, req)

	data := querytestutil.DecodeImpactEnvelopeData(t, rec)
	if len(resolveCyphers) != 2 {
		t.Fatalf("want two per-label resolve queries (source, target), got %d", len(resolveCyphers))
	}
	for _, q := range resolveCyphers {
		assertNoImpactLabelDisjunction(t, q)
		if !strings.Contains(q, "CALL {") {
			t.Fatalf("resolve query must be a CALL{UNION}: %s", q)
		}
	}
	assertNoImpactLabelDisjunction(t, pathCypher)
	if !strings.Contains(pathCypher, "shortestPath((source:CloudResource {id: $source_id})-[*1..8]-(target:Repository {id: $target_id}))") {
		t.Fatalf("shortestPath must anchor both resolved labels inline: %s", pathCypher)
	}
	// The handler intentionally returns "path": null when no path exists.
	if v, ok := data["path"]; ok && v != nil {
		t.Fatalf("path = %#v, want absent/null when no shortest path exists", v)
	}
	// Source and target are still resolved and returned.
	if src, _ := data["source"].(map[string]any); querycontract.StringVal(src, "id") != "resource:queue" {
		t.Fatalf("source = %#v, want resolved resource:queue", data["source"])
	}
}
