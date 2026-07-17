// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveByIdImpactAnchorReads is the backend-required proof for #5286. It seeds
// a CloudResource -> Workload -> Repository chain on a live NornicDB, captures the
// OLD label-disjunction/map-comprehension shapes (which corrupt on the pinned
// build), and asserts the shipped trace-resource-to-code and
// explain-dependency-path handlers return the correct paths and hop provenance.
//
//	Run: ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 \
//		go test ./internal/query -run TestLiveByIdImpactAnchorReads -count=1 -v
func TestLiveByIdImpactAnchorReads(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_OCI_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_OCI_PROVE_LIVE=1 to run the live by-id impact-anchor proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required (e.g. bolt://localhost:17687)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	write := func(cypher string, params map[string]any) {
		s := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
		defer func() { _ = s.Close(ctx) }()
		if _, err := s.Run(ctx, cypher, params); err != nil {
			t.Fatalf("seed write failed: %v\ncypher=%s", err, cypher)
		}
	}
	reader := NewNeo4jReader(driver, "nornic")
	handler := &ImpactHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}

	const (
		srcID = "impact5286:src"
		midID = "impact5286:mid"
		tgtID = "impact5286:tgt"
	)
	cleanup := func() {
		for _, id := range []string{srcID, midID, tgtID} {
			write(`MATCH (n {id:$id}) DETACH DELETE n`, map[string]any{"id": id})
		}
	}
	cleanup()
	defer cleanup()
	write(`CREATE (s:CloudResource {id:$s, uid:$s, name:'src'})
	       CREATE (m:Workload {id:$m, name:'mid'})
	       CREATE (t:Repository {id:$t, name:'tgt'})
	       CREATE (s)-[:DEPENDS_ON {confidence:0.9, reason:'a'}]->(m)
	       CREATE (m)-[:DEPENDS_ON {confidence:0.8, reason:'b'}]->(t)`,
		map[string]any{"s": srcID, "m": midID, "t": tgtID})

	// Capture the OLD label-disjunction anchor (matches zero rows) for evidence.
	oldAnchor, _ := reader.Run(ctx, "MATCH (n:"+impactAnchorLabelDisjunction+") WHERE n.id = $id RETURN n.id AS id", map[string]any{"id": srcID})
	t.Logf("OLD label-disjunction anchor rows: %d (want 0 — broken)", len(oldAnchor))

	post := func(path, body string, fn http.HandlerFunc) map[string]any {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Accept", EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		fn(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", path, rec.Code, rec.Body.String())
		}
		var env ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
		data, _ := env.Data.(map[string]any)
		b, _ := json.MarshalIndent(data, "", "  ")
		t.Logf("\n=== %s ===\n%s", path, b)
		return data
	}

	// trace-resource-to-code: src -> tgt (Repository) at depth 2 with 2 hops.
	trace := post("/api/v0/impact/trace-resource-to-code", `{"start":"`+srcID+`","max_depth":8,"limit":50}`, handler.traceResourceToCode)
	if start, _ := trace["start"].(map[string]any); StringVal(start, "id") != srcID {
		t.Errorf("trace start = %#v, want %s", trace["start"], srcID)
	}
	paths, _ := trace["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("trace paths = %#v, want 1 (the tgt Repository)", trace["paths"])
	}
	p0, _ := paths[0].(map[string]any)
	if StringVal(p0, "repo_id") != tgtID || IntVal(p0, "depth") != 2 {
		t.Errorf("trace path = %#v, want repo_id=%s depth=2", p0, tgtID)
	}
	hops, _ := p0["hops"].([]any)
	if len(hops) != 2 {
		t.Fatalf("trace hops = %#v, want 2 (OLD map-comprehension mangled these to empty)", p0["hops"])
	}

	// Name resolution: callers may pass the node name, not the canonical id. The
	// start seed is named "src"; tracing by name must resolve to the same node.
	traceByName := post("/api/v0/impact/trace-resource-to-code", `{"start":"src","limit":50}`, handler.traceResourceToCode)
	if start, _ := traceByName["start"].(map[string]any); StringVal(start, "id") != srcID {
		t.Errorf("trace by name start = %#v, want resolved to %s", traceByName["start"], srcID)
	}
	if c, _ := traceByName["count"].(float64); c != 1 {
		t.Errorf("trace by name count = %v, want 1", traceByName["count"])
	}

	// explain-dependency-path: src -> tgt, shortest path length 2, 2 hops.
	explain := post("/api/v0/impact/explain-dependency-path", `{"source":"`+srcID+`","target":"`+tgtID+`"}`, handler.explainDependencyPath)
	pathInfo, ok := explain["path"].(map[string]any)
	if !ok {
		t.Fatalf("explain missing path: %#v", explain)
	}
	if IntVal(pathInfo, "depth") != 2 {
		t.Errorf("explain depth = %v, want 2", pathInfo["depth"])
	}
	ehops, _ := pathInfo["hops"].([]any)
	if len(ehops) != 2 {
		t.Fatalf("explain hops = %#v, want 2 with from/to endpoints", pathInfo["hops"])
	}
	h0, _ := ehops[0].(map[string]any)
	if StringVal(h0, "from_id") != srcID || StringVal(h0, "to_id") != midID || h0["type"] != "DEPENDS_ON" {
		t.Errorf("explain hop0 = %#v, want src->mid DEPENDS_ON (OLD shape mangled endpoints)", h0)
	}
	if _, ok := explain["confidence"].(float64); !ok {
		t.Errorf("explain missing aggregate confidence: %#v", explain)
	}
}
