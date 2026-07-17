// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestImpactPathDecodersDecodeBothBackendShapes guards the cross-backend decode:
// relationships(path) is a neo4j.Relationship on Neo4j but a map[string]any on
// NornicDB, and nodes(path) is a neo4j.Node on both. Both rel shapes and the node
// shape must decode to the same provenance/identity, or a hop is silently dropped
// on one backend.
func TestImpactPathDecodersDecodeBothBackendShapes(t *testing.T) {
	t.Parallel()

	relCases := map[string]any{
		"neo4j.Relationship": []any{
			neo4jdriver.Relationship{Type: "DEPENDS_ON", Props: map[string]any{"confidence": 0.9, "reason": "import"}},
		},
		"nornicdb map": []any{
			map[string]any{"type": "DEPENDS_ON", "properties": map[string]any{"confidence": 0.9, "reason": "import"}},
		},
	}
	for name, raw := range relCases {
		got := impactRelProvenanceList(raw)
		if len(got) != 1 {
			t.Fatalf("%s: got %d rels, want 1", name, len(got))
		}
		if got[0].relType != "DEPENDS_ON" || !got[0].hasConf || got[0].confidence != 0.9 || got[0].reason != "import" {
			t.Errorf("%s: decoded %#v, want DEPENDS_ON/0.9/import", name, got[0])
		}
	}

	nodeCases := map[string]any{
		"neo4j.Node": []any{neo4jdriver.Node{Props: map[string]any{"id": "z:1", "name": "one"}}, neo4jdriver.Node{Props: map[string]any{"id": "z:2", "name": "two"}}},
		"map":        []any{map[string]any{"properties": map[string]any{"id": "z:1", "name": "one"}}, map[string]any{"properties": map[string]any{"id": "z:2", "name": "two"}}},
	}
	for name, raw := range nodeCases {
		got := impactNodeIdentityList(raw)
		if len(got) != 2 || got[0].id != "z:1" || got[0].name != "one" || got[1].id != "z:2" {
			t.Errorf("%s: decoded %#v, want [z:1/one, z:2/two]", name, got)
		}
	}

	// A zipped hop uses path-order endpoints (nodes[i] -> nodes[i+1]).
	hops := impactDependencyHops(nodeCases["neo4j.Node"], relCases["neo4j.Relationship"])
	if len(hops) != 1 {
		t.Fatalf("hops = %#v, want 1", hops)
	}
	if hops[0]["from_id"] != "z:1" || hops[0]["to_id"] != "z:2" || hops[0]["type"] != "DEPENDS_ON" {
		t.Errorf("zipped hop = %#v, want z:1->z:2 DEPENDS_ON", hops[0])
	}
}

// TestExplainDependencyPathNullPathRecordOmitsPath proves the no-path guard is
// robust when shortestPath returns a single null-valued record (nodes(path) IS
// NULL) instead of zero rows: the handler must omit `path` and still return the
// resolved source/target, not report a bogus `path: {depth: 0, hops: []}`.
func TestExplainDependencyPathNullPathRecordOmitsPath(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReaderWithSingle{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
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
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.explainDependencyPath(rec, req)

	data := decodeImpactEnvelopeData(t, rec)
	if v, ok := data["path"]; ok && v != nil {
		t.Fatalf("path = %#v, want absent/null for a null shortestPath record", v)
	}
	if src, _ := data["source"].(map[string]any); StringVal(src, "id") != "resource:queue" {
		t.Fatalf("source = %#v, want resolved resource:queue", data["source"])
	}
}

// assertNoImpactLabelDisjunction fails when a by-id anchor uses the label
// disjunction (`A|B|C`), which matches zero rows on the pinned NornicDB build.
func assertNoImpactLabelDisjunction(t *testing.T, cypher string) {
	t.Helper()
	if strings.Contains(cypher, impactAnchorLabelDisjunction) {
		t.Fatalf("by-id anchor must use per-label inline-property anchors, not the label disjunction: %s", cypher)
	}
}

// TestImpactAnchorResolveCypherIsPerLabelUnion guards the #5286 fix: the by-id
// label resolver must be a CALL{UNION} of per-label inline-property anchors, not
// a label-disjunction anchor (which matches zero rows on the pinned NornicDB).
func TestImpactAnchorResolveCypherIsPerLabelUnion(t *testing.T) {
	t.Parallel()

	// The resolver must be a CALL{UNION} of per-label inline-property anchors,
	// never the label disjunction, and never a `WHERE n.id` predicate that would
	// reintroduce the disjunction-shaped scan.
	resolve := impactAnchorResolveCypher("start_id")
	assertNoImpactLabelDisjunction(t, resolve)
	if !strings.Contains(resolve, "CALL {") {
		t.Errorf("resolve must wrap the per-label union in CALL {}: %s", resolve)
	}
	if strings.Contains(resolve, "WHERE") && strings.Contains(resolve, ".id =") {
		t.Errorf("resolve must anchor by inline property, not a WHERE id predicate: %s", resolve)
	}
	// It must anchor every label on BOTH id and name (callers pass human names).
	for _, label := range []string{"CloudResource", "Repository", "TerraformResource", "KubernetesWorkload"} {
		if !strings.Contains(resolve, "MATCH (n:"+label+" {id: $start_id})") {
			t.Errorf("resolve must anchor %s by id: %s", label, resolve)
		}
		if !strings.Contains(resolve, "MATCH (n:"+label+" {name: $start_id})") {
			t.Errorf("resolve must anchor %s by name: %s", label, resolve)
		}
	}

	// The traversal anchors a single resolved label inline (no disjunction) and
	// projects the raw relationships(path) list to a Repository target.
	traversal := fmt.Sprintf(impactRepoPathCypher, "(start:Repository {id: $start_id})", 8)
	assertNoImpactLabelDisjunction(t, traversal)
	if strings.Count(traversal, "MATCH") != 1 {
		t.Errorf("traversal must be a single anchoring MATCH: %s", traversal)
	}
	if !strings.Contains(traversal, "relationships(path) AS rels") || !strings.Contains(traversal, "(repo:Repository)") {
		t.Errorf("traversal must project relationships(path) to a Repository target: %s", traversal)
	}
}

// TestTraceResourceToCodeAnchorsResolvedLabel proves the resource-to-code start
// anchor is resolved to a single label and folded into the traversal inline. The
// pinned NornicDB build matches zero rows for a label-disjunction by-id anchor
// (#5286), so the start is resolved with a per-label CALL{UNION} first, then the
// traversal anchors the resolved label with the exact id predicate, repo target,
// and server-side LIMIT.
func TestTraceResourceToCodeAnchorsResolvedLabel(t *testing.T) {
	t.Parallel()

	var resolveCypher, traversalCypher string
	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReaderWithSingle{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				resolveCypher = cypher
				return map[string]any{"label": "CloudResource", "id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}, nil
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
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
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.traceResourceToCode(rec, req)

	data := decodeImpactEnvelopeData(t, rec)
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

// TestTraceResourceToCodeReturnsStartWithoutPaths proves that when the start
// resolves but the traversal finds no Repository paths, the start is still
// hydrated (from the resolver) with an empty paths list.
func TestTraceResourceToCodeReturnsStartWithoutPaths(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReaderWithSingle{
			runSingle: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
				return map[string]any{"label": "CloudResource", "id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}, nil
			},
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return nil, nil // no Repository paths
			},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-resource-to-code", bytes.NewBufferString(`{"start":"resource:queue"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.traceResourceToCode(rec, req)

	data := decodeImpactEnvelopeData(t, rec)
	start, _ := data["start"].(map[string]any)
	if start["id"] != "resource:queue" || start["name"] != "queue" {
		t.Fatalf("start must be hydrated from the resolver even with no paths: %#v", data["start"])
	}
	if got, want := data["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want 0", got)
	}
}

// TestExplainDependencyPathAnchorsResolvedEndpoints proves the dependency-path
// source and target are resolved to single labels and the shortestPath anchors
// both inline. The pinned NornicDB build matches zero rows for a label-disjunction
// by-id anchor (#5286).
func TestExplainDependencyPathAnchorsResolvedEndpoints(t *testing.T) {
	t.Parallel()

	var resolveCyphers []string
	var pathCypher string
	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReaderWithSingle{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
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
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.explainDependencyPath(rec, req)

	data := decodeImpactEnvelopeData(t, rec)
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
	if src, _ := data["source"].(map[string]any); StringVal(src, "id") != "resource:queue" {
		t.Fatalf("source = %#v, want resolved resource:queue", data["source"])
	}
}

// TestImpactAnchorLabelDisjunctionIncludesTerraformResource proves that
// TerraformResource is present in the impact anchor label disjunction.
// TerraformResource nodes are written with SET r.id = row.uid
// (go/internal/storage/cypher/tfstate_canonical_writer.go:13) so callers that
// pass a TerraformResource uid as start_id must resolve to a non-empty anchor.
// The prior unlabeled MATCH found them; the labeled disjunction must too.
func TestImpactAnchorLabelDisjunctionIncludesTerraformResource(t *testing.T) {
	t.Parallel()
	if !strings.Contains(impactAnchorLabelDisjunction, "TerraformResource") {
		t.Fatalf("impactAnchorLabelDisjunction must include TerraformResource (its .id is set to row.uid by tfstate_canonical_writer); got: %s", impactAnchorLabelDisjunction)
	}
}

// TestImpactAnchorLabelDisjunctionIncludesTerraformOutput proves TerraformOutput
// is present. TerraformOutput nodes are written with SET o.id = row.uid
// (go/internal/storage/cypher/tfstate_canonical_writer.go:62) so they share the
// same id-via-uid pattern as TerraformResource.
func TestImpactAnchorLabelDisjunctionIncludesTerraformOutput(t *testing.T) {
	t.Parallel()
	if !strings.Contains(impactAnchorLabelDisjunction, "TerraformOutput") {
		t.Fatalf("impactAnchorLabelDisjunction must include TerraformOutput (its .id is set to row.uid by tfstate_canonical_writer); got: %s", impactAnchorLabelDisjunction)
	}
}

// TestImpactAnchorLabelDisjunctionIncludesKubernetesWorkload proves
// KubernetesWorkload is present. KubernetesWorkload nodes are written with
// SET w.id = row.uid (go/internal/storage/cypher/kubernetes_workload_node_writer.go)
// so callers that pass a KubernetesWorkload uid as start_id must resolve.
func TestImpactAnchorLabelDisjunctionIncludesKubernetesWorkload(t *testing.T) {
	t.Parallel()
	if !strings.Contains(impactAnchorLabelDisjunction, "KubernetesWorkload") {
		t.Fatalf("impactAnchorLabelDisjunction must include KubernetesWorkload (its .id is set to row.uid by kubernetes_workload_node_writer); got: %s", impactAnchorLabelDisjunction)
	}
}

// fakeGraphReaderWithSingle is a GraphQuery test double that scripts both Run
// and RunSingle so the dependency-path and resource-to-code fallback paths can
// be exercised independently.
type fakeGraphReaderWithSingle struct {
	run       func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle func(context.Context, string, map[string]any) (map[string]any, error)
}

func (f fakeGraphReaderWithSingle) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.run == nil {
		return nil, nil
	}
	return f.run(ctx, cypher, params)
}

func (f fakeGraphReaderWithSingle) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.runSingle == nil {
		return nil, nil
	}
	return f.runSingle(ctx, cypher, params)
}
