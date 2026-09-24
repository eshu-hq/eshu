// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_wrapper_bypass

// Live proof for the #6838 compare-code-paths surface: the diamond's four
// simple paths enumerate through the route against the pinned NornicDB
// build. Run with the same container as wrapper_bypass_live_test.go.
package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/content"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const liveCompareRepo = "repo-cmp-live"

// seedLiveCompareDiamondGraph writes the #6834 diamond under fresh ids:
// a-b-c-d-e, a-b-e, a-x-e, a-y-e. MERGE keeps repeated runs idempotent.
func seedLiveCompareDiamondGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	statements := []string{}
	for _, name := range []string{"a", "b", "c", "d", "e", "x", "y"} {
		uid := "cmp:" + name
		statements = append(statements,
			`MERGE (e:Function {uid:"`+uid+`"}) SET e.id="`+uid+`", e.name="`+name+`", e.repo_id="`+liveCompareRepo+`"`)
	}
	link := func(from, to string, confidence float64) string {
		return `MATCH (a:Function {uid:"cmp:` + from + `"}), (b:Function {uid:"cmp:` + to + `"}) MERGE (a)-[r:CALLS]->(b) SET r.confidence=` + strconv.FormatFloat(confidence, 'f', -1, 64) + `, r.resolution_method="declared"`
	}
	statements = append(statements,
		link("a", "b", 0.9), link("b", "c", 0.8), link("c", "d", 0.7), link("d", "e", 0.6),
		link("b", "e", 0.5), link("a", "x", 0.95), link("x", "e", 0.85),
		link("a", "y", 0.75), link("y", "e", 0.65),
	)
	for _, stmt := range statements {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
}

// TestLiveNornicDBCompareCodePaths runs the diamond through the compare
// route against live NornicDB: four distinct simple paths, shortest first.
func TestLiveNornicDBCompareCodePaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := liveWrapperOpenDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveCompareDiamondGraph(ctx, t, driver)

	handler := &CodeHandler{
		Content:      content.FakePortContentStore{},
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j:        newLiveNornicDBReader(driver, "nornic"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain/compare",
		bytes.NewBufferString(`{"repo_id":"repo-cmp-live","start":"a","end":"e","start_entity_id":"cmp:a","end_entity_id":"cmp:e","max_depth":4,"max_paths":5}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("compare status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Paths []struct {
				Nodes []struct {
					ID string `json:"id"`
				} `json:"nodes"`
				Depth      int     `json:"depth"`
				Confidence float64 `json:"confidence"`
			} `json:"paths"`
			Truncated bool `json:"truncated"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Data.Paths) != 4 {
		t.Fatalf("live paths = %d, want 4 body=%s", len(envelope.Data.Paths), rec.Body.String())
	}
	if envelope.Data.Paths[0].Depth != 2 || envelope.Data.Paths[3].Depth != 4 {
		t.Errorf("live depths = %d..%d, want shortest-first 2..4",
			envelope.Data.Paths[0].Depth, envelope.Data.Paths[3].Depth)
	}
	if envelope.Data.Truncated {
		t.Errorf("live truncated = true, want false")
	}
}
