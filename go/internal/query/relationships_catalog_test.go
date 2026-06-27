// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

// fakeRelationshipsGraphReader returns deterministic counts and edges keyed by
// the relationship verb found in the query text. When breakdownByVerb is set,
// Run returns breakdown rows for queries that contain "source_tool IS NOT NULL".
type fakeRelationshipsGraphReader struct {
	countByVerb     map[string]int
	edgesByVerb     map[string][]map[string]any
	breakdownByVerb map[string][]map[string]any
	lastParams      map[string]any
	lastCypher      string
}

func verbInCypher(cypher string) string {
	for _, entry := range relationshipVerbCatalog {
		if strings.Contains(cypher, "[r:"+entry.verb+"]") {
			return entry.verb
		}
	}
	return ""
}

func (f *fakeRelationshipsGraphReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	verb := verbInCypher(cypher)
	return map[string]any{"count": int64(f.countByVerb[verb])}, nil
}

func (f *fakeRelationshipsGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.lastParams = params
	f.lastCypher = cypher
	verb := verbInCypher(cypher)
	// Breakdown queries contain the IS NOT NULL guard; edge queries do not.
	if strings.Contains(cypher, "source_tool IS NOT NULL") {
		if f.breakdownByVerb != nil {
			return f.breakdownByVerb[verb], nil
		}
		return nil, nil
	}
	return f.edgesByVerb[verb], nil
}

func TestGetRelationshipsCatalogReturnsVerbTiles(t *testing.T) {
	t.Parallel()

	reader := &fakeRelationshipsGraphReader{countByVerb: map[string]int{"CALLS": 932, "IMPORTS": 1840}}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/catalog", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.getRelationshipsCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %T", env.Data)
	}
	verbs, ok := data["verbs"].([]any)
	if !ok {
		t.Fatalf("verbs is not an array: %T", data["verbs"])
	}
	if len(verbs) != len(relationshipVerbCatalog) {
		t.Fatalf("verb tiles = %d, want %d", len(verbs), len(relationshipVerbCatalog))
	}
	if got := data["verb_count"].(float64); int(got) != len(relationshipVerbCatalog) {
		t.Fatalf("verb_count = %v, want %d", got, len(relationshipVerbCatalog))
	}
	if got := data["total_edges"].(float64); int(got) != 932+1840 {
		t.Fatalf("total_edges = %v, want %d", got, 932+1840)
	}
	if got := data["layer_count"].(float64); int(got) != 6 {
		t.Fatalf("layer_count = %v, want 6", got)
	}
	first, _ := verbs[0].(map[string]any)
	if first["verb"] != "CALLS" || first["layer"] != "code" || int(first["count"].(float64)) != 932 {
		t.Fatalf("first tile unexpected: %+v", first)
	}
	if env.Truth == nil || env.Truth.Basis != TruthBasisAuthoritativeGraph {
		t.Fatalf("truth basis = %+v, want authoritative_graph", env.Truth)
	}
}

func TestGetRelationshipEdgesReturnsBoundedSlice(t *testing.T) {
	t.Parallel()

	// Three rows returned for a limit of 2 must truncate to 2 and flag truncated.
	// The first row carries source_tool; the others do not.
	edges := []map[string]any{
		{"source_id": "a", "source_name": "fnA", "target_id": "b", "target_name": "fnB", "evidence": "call site", "source_tool": "terraform"},
		{"source_id": "c", "source_name": "fnC", "target_id": "d", "target_name": "fnD", "evidence": ""},
		{"source_id": "e", "source_name": "fnE", "target_id": "f", "target_name": "fnF", "evidence": ""},
	}
	reader := &fakeRelationshipsGraphReader{edgesByVerb: map[string][]map[string]any{"CALLS": edges}}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	limit := 2
	body, _ := json.Marshal(map[string]any{"verb": "calls", "limit": limit})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := reader.lastParams["limit"]; got != limit+1 {
		t.Fatalf("edge query limit param = %v, want %d (limit+1 over-fetch)", got, limit+1)
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	if data["verb"] != "CALLS" {
		t.Fatalf("verb = %v, want CALLS", data["verb"])
	}
	gotEdges := data["edges"].([]any)
	if len(gotEdges) != limit {
		t.Fatalf("edges = %d, want %d", len(gotEdges), limit)
	}
	if data["truncated"] != true {
		t.Fatalf("truncated = %v, want true", data["truncated"])
	}
	// Assert source_tool is decoded for the first edge.
	first, _ := gotEdges[0].(map[string]any)
	if first["source_tool"] != "terraform" {
		t.Fatalf("first edge source_tool = %v, want terraform", first["source_tool"])
	}
}

func TestGetRelationshipEdgesRejectsUnknownVerb(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Neo4j: &fakeRelationshipsGraphReader{}, Profile: ProfileProduction}
	body, _ := json.Marshal(map[string]any{"verb": "NOT_A_VERB"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetRelationshipEdgesRequiresVerb(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Neo4j: &fakeRelationshipsGraphReader{}, Profile: ProfileProduction}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRelationshipsCatalogUnsupportedOnLightweightProfile(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Neo4j: &fakeRelationshipsGraphReader{}, Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/catalog", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	handler.getRelationshipsCatalog(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetRelationshipEdgesFiltersBySourceTool(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		{"source_id": "r1", "source_name": "repo1", "target_id": "r2", "target_name": "repo2", "evidence": "", "source_tool": "terraform"},
	}
	reader := &fakeRelationshipsGraphReader{edgesByVerb: map[string][]map[string]any{"DEPENDS_ON": edges}}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	body, _ := json.Marshal(map[string]any{"verb": "depends_on", "source_tool": "terraform"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// The filtered Cypher must carry $source_tool in params.
	if got := reader.lastParams["source_tool"]; got != "terraform" {
		t.Fatalf("source_tool param = %v, want terraform", got)
	}
	// The recorded Cypher must contain the WHERE guard.
	if !strings.Contains(reader.lastCypher, "WHERE r.source_tool = $source_tool") {
		t.Fatalf("filtered Cypher missing WHERE guard: %s", reader.lastCypher)
	}
	// The response must echo the source_tool filter.
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	if data["source_tool"] != "terraform" {
		t.Fatalf("response source_tool = %v, want terraform", data["source_tool"])
	}
}

func TestGetRelationshipEdgesRejectsUnknownSourceTool(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Neo4j: &fakeRelationshipsGraphReader{}, Profile: ProfileProduction}
	body, _ := json.Marshal(map[string]any{"verb": "calls", "source_tool": "notatool"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/edges", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.getRelationshipEdges(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetRelationshipsCatalogIncludesSourceToolsBreakdown(t *testing.T) {
	t.Parallel()

	breakdownByVerb := map[string][]map[string]any{
		"DEPENDS_ON": {
			{"source_tool": "ansible", "count": int64(12)},
			{"source_tool": "helm", "count": int64(7)},
		},
	}
	reader := &fakeRelationshipsGraphReader{
		countByVerb:     map[string]int{"DEPENDS_ON": 19},
		breakdownByVerb: breakdownByVerb,
	}
	handler := &InfraHandler{Neo4j: reader, Profile: ProfileProduction}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/relationships/catalog", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.getRelationshipsCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := env.Data.(map[string]any)
	verbs := data["verbs"].([]any)
	// Find the DEPENDS_ON tile.
	var dependsOnTile map[string]any
	for _, v := range verbs {
		tile, _ := v.(map[string]any)
		if tile["verb"] == "DEPENDS_ON" {
			dependsOnTile = tile
			break
		}
	}
	if dependsOnTile == nil {
		t.Fatal("DEPENDS_ON tile missing from catalog response")
	}
	st, ok := dependsOnTile["source_tools"].(map[string]any)
	if !ok {
		t.Fatalf("source_tools not a map on DEPENDS_ON tile: %+v", dependsOnTile)
	}
	if int(st["ansible"].(float64)) != 12 {
		t.Fatalf("source_tools[ansible] = %v, want 12", st["ansible"])
	}
	if int(st["helm"].(float64)) != 7 {
		t.Fatalf("source_tools[helm] = %v, want 7", st["helm"])
	}
}

// TestRelationshipCountCypherIsTypeIndexed guards the catalog count contract:
// every per-verb count must be the bare relationship-type aggregate
// `MATCH ()-[r:VERB]->() RETURN count(r)`. That shape is answered by the
// NornicDB relationship-type index in milliseconds and counts the whole-graph
// edge population for the verb (the documented "bounded whole-graph edge
// count"), instead of scanning the entire source-label population per verb.
// The bare anonymous endpoints `()` are not the gate's unlabeled-bound-node
// pattern `(s)`, so this shape stays within the bounded read contract.
func TestRelationshipCountCypherIsTypeIndexed(t *testing.T) {
	t.Parallel()

	for _, entry := range relationshipVerbCatalog {
		count := relationshipCountCypher(entry)
		typeAnchor := "()-[r:" + entry.verb + "]->()"
		if !strings.Contains(count, typeAnchor) {
			t.Fatalf("count cypher for %s not relationship-type anchored: %s", entry.verb, count)
		}
		if strings.Contains(count, "(s:"+entry.sourceLabel+")") {
			t.Fatalf("count cypher for %s must not scan the source label: %s", entry.verb, count)
		}
		if !strings.Contains(count, "count(r)") {
			t.Fatalf("count cypher for %s missing count(r): %s", entry.verb, count)
		}
	}
}

// TestRelationshipEdgesCypherIsSourceAnchoredAndIndexOrdered guards the edge
// slice contract: every edge query stays anchored on the labeled source node
// (bare-type edges with a bound, unlabeled source are far slower on NornicDB),
// carries a bounded LIMIT, orders by the indexed source-anchor property so
// the LIMIT short-circuits the index-ordered scan instead of materializing and
// sorting the full edge set on a non-indexed coalesce() expression, and carries
// a deterministic tie-breaker so that rows tied on the primary source key are
// ordered consistently across requests (prevents nondeterministic sampling when
// the page boundary falls inside one source node's outgoing edges).
func TestRelationshipEdgesCypherIsSourceAnchoredAndIndexOrdered(t *testing.T) {
	t.Parallel()

	for _, entry := range relationshipVerbCatalog {
		anchor := "(s:" + entry.sourceLabel + ")-[r:" + entry.verb + "]"
		edges := relationshipEdgesCypher(entry)
		if !strings.Contains(edges, anchor) {
			t.Fatalf("edge cypher for %s not source-anchored: %s", entry.verb, edges)
		}
		if !strings.Contains(edges, "LIMIT $limit") {
			t.Fatalf("edge cypher for %s missing bounded LIMIT: %s", entry.verb, edges)
		}
		orderBy := "ORDER BY s." + entry.sourceProperty
		if !strings.Contains(edges, orderBy) {
			t.Fatalf("edge cypher for %s must order by indexed source property %q: %s", entry.verb, orderBy, edges)
		}
		// Tie-breaker: rows with the same source key must have a deterministic
		// secondary sort so page boundaries inside one node's outgoing edges
		// do not produce nondeterministic or repeated rows across requests.
		if !strings.Contains(edges, "coalesce(t.id, t.uid)") {
			t.Fatalf("edge cypher for %s missing deterministic tie-breaker coalesce(t.id, t.uid): %s", entry.verb, edges)
		}
		// source_tool must be projected from the relationship.
		if !strings.Contains(edges, "r.source_tool AS source_tool") {
			t.Fatalf("edge cypher for %s missing r.source_tool projection: %s", entry.verb, edges)
		}
	}
}

// TestRelationshipEdgesFilteredCypherHasWhereGuard guards the filtered-Cypher
// contract: the WHERE clause on r.source_tool must appear after the MATCH line
// and before RETURN, and the same ORDER BY and LIMIT structure as the
// unfiltered variant must be preserved so the index-ordered scan short-circuits.
func TestRelationshipEdgesFilteredCypherHasWhereGuard(t *testing.T) {
	t.Parallel()

	for _, entry := range relationshipVerbCatalog {
		filtered := relationshipEdgesCypherFiltered(entry)
		if !strings.Contains(filtered, "WHERE r.source_tool = $source_tool") {
			t.Fatalf("filtered edge cypher for %s missing WHERE guard: %s", entry.verb, filtered)
		}
		if !strings.Contains(filtered, "LIMIT $limit") {
			t.Fatalf("filtered edge cypher for %s missing LIMIT: %s", entry.verb, filtered)
		}
		orderBy := "ORDER BY s." + entry.sourceProperty
		if !strings.Contains(filtered, orderBy) {
			t.Fatalf("filtered edge cypher for %s must order by indexed source property %q: %s", entry.verb, orderBy, filtered)
		}
		// Unfiltered path must not reference the source_tool param.
		unfiltered := relationshipEdgesCypher(entry)
		if strings.Contains(unfiltered, "$source_tool") {
			t.Fatalf("unfiltered edge cypher for %s must not reference $source_tool: %s", entry.verb, unfiltered)
		}
	}
}

// TestRelationshipSourceToolBreakdownCypherIsTypeIndexed guards that the
// breakdown query uses the type-indexed bare-endpoint shape
// `MATCH ()-[r:VERB]->()`, matching the contract of relationshipCountCypher.
func TestRelationshipSourceToolBreakdownCypherIsTypeIndexed(t *testing.T) {
	t.Parallel()

	for _, entry := range relationshipVerbCatalog {
		breakdown := relationshipSourceToolBreakdownCypher(entry)
		typeAnchor := "()-[r:" + entry.verb + "]->()"
		if !strings.Contains(breakdown, typeAnchor) {
			t.Fatalf("breakdown cypher for %s not relationship-type anchored: %s", entry.verb, breakdown)
		}
		if !strings.Contains(breakdown, "source_tool IS NOT NULL") {
			t.Fatalf("breakdown cypher for %s must filter source_tool IS NOT NULL: %s", entry.verb, breakdown)
		}
		if !strings.Contains(breakdown, "count(r)") {
			t.Fatalf("breakdown cypher for %s missing count(r): %s", entry.verb, breakdown)
		}
	}
}

// digSpec walks a decoded OpenAPI document by successive map keys, failing the
// test if any key is missing or not an object.
func digSpec(t *testing.T, node any, keys ...string) any {
	t.Helper()
	for _, key := range keys {
		obj, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("digSpec: expected object at %q, got %T", key, node)
		}
		node, ok = obj[key]
		if !ok {
			t.Fatalf("digSpec: missing key %q", key)
		}
	}
	return node
}

// TestRelationshipEdgesSourceToolEnumMatchesCanonical guards against drift
// between the OpenAPI source_tool filter enum and the canonical vocabulary: the
// enum is hand-written JSON, so a token added to sourcetool.Canonical without
// updating the spec (or vice versa) must fail here rather than ship a contract
// that rejects a valid tool.
func TestRelationshipEdgesSourceToolEnumMatchesCanonical(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) = %v", err)
	}
	enumRaw := digSpec(t, spec,
		"paths", "/api/v0/relationships/edges", "post", "requestBody",
		"content", "application/json", "schema", "properties", "source_tool", "enum")
	enumList, ok := enumRaw.([]any)
	if !ok {
		t.Fatalf("source_tool enum is %T, want []any", enumRaw)
	}
	got := make([]string, len(enumList))
	for i, v := range enumList {
		got[i], _ = v.(string)
	}
	if !reflect.DeepEqual(got, sourcetool.Canonical) {
		t.Fatalf("source_tool enum drift:\n openapi = %v\n canonical = %v", got, sourcetool.Canonical)
	}
}
