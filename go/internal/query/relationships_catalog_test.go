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

// fakeRelationshipsGraphReader returns deterministic counts and edges keyed by
// the relationship verb found in the query text.
type fakeRelationshipsGraphReader struct {
	countByVerb map[string]int
	edgesByVerb map[string][]map[string]any
	lastParams  map[string]any
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
	return f.edgesByVerb[verbInCypher(cypher)], nil
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
	edges := []map[string]any{
		{"source_id": "a", "source_name": "fnA", "target_id": "b", "target_name": "fnB", "evidence": "call site"},
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
// carries a bounded LIMIT, and orders by the indexed source-anchor property so
// the LIMIT short-circuits the index-ordered scan instead of materializing and
// sorting the full edge set on a non-indexed coalesce() expression.
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
	}
}
