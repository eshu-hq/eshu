// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveRouteToCallerReadsAreNornicDBSafe is the backend-required proof for the
// #5287 route-to-caller reads. On the pinned NornicDB build the prior queries
// corrupt: routeToCallerRouteRows (OPTIONAL MATCH computed projection) and
// routeToCallerRelationshipRows (CALL-subquery computed projection) returned
// literal expression text, and routeToCallerImpact's map-valued collect returned
// literal strings. This seeds an endpoint -> handler -> caller/callee graph and
// drives the shipped handler methods.
//
//	Run: ESHU_INFRA_AGG_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687 \
//		go test ./internal/query -run TestLiveRouteToCallerReadsAreNornicDBSafe -count=1 -v
func TestLiveRouteToCallerReadsAreNornicDBSafe(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_INFRA_AGG_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_INFRA_AGG_PROVE_LIVE=1 to run the live route-to-caller proof")
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
	ids := []string{"rc5287:ep", "rc5287:wl", "rc5287:handler", "rc5287:caller", "rc5287:callee", "rc5287:rtwl", "rc5287:repo2"}
	clean := func() {
		for _, id := range ids {
			write(`MATCH (n {id:$id}) DETACH DELETE n`, map[string]any{"id": id})
		}
	}
	clean()
	defer clean()
	write(`CREATE (e:Endpoint {id:'rc5287:ep', path:'/orders', repo_id:'rc5287:repo', framework:'flask'})`, nil)
	write(`CREATE (w:Workload {id:'rc5287:wl', name:'orders-svc'})`, nil)
	write(`CREATE (h:Function {id:'rc5287:handler', name:'handleOrders', file_path:'app.py', language:'python', start_line:10, end_line:20, repo_id:'rc5287:repo'})`, nil)
	write(`CREATE (c:Function {id:'rc5287:caller', name:'main', file_path:'main.py', language:'python', start_line:1, end_line:5, repo_id:'rc5287:repo'})`, nil)
	write(`CREATE (ce:Function {id:'rc5287:callee', name:'queryDB', file_path:'db.py', language:'python', start_line:30, end_line:40, repo_id:'rc5287:repo'})`, nil)
	write(`CREATE (rt:Workload {id:'rc5287:rtwl', name:'orders-runtime'})`, nil)
	write(`CREATE (repo2:Repository {id:'rc5287:repo2', name:'orders-repo'})`, nil)
	write(`MATCH (w:Workload {id:'rc5287:wl'}),(e:Endpoint {id:'rc5287:ep'}) CREATE (w)-[:EXPOSES_ENDPOINT]->(e)`, nil)
	write(`MATCH (h:Function {id:'rc5287:handler'}),(e:Endpoint {id:'rc5287:ep'}) CREATE (h)-[:HANDLES_ROUTE {http_method:'GET', framework:'flask'}]->(e)`, nil)
	write(`MATCH (c:Function {id:'rc5287:caller'}),(h:Function {id:'rc5287:handler'}) CREATE (c)-[:CALLS]->(h)`, nil)
	write(`MATCH (h:Function {id:'rc5287:handler'}),(ce:Function {id:'rc5287:callee'}) CREATE (h)-[:CALLS]->(ce)`, nil)
	write(`MATCH (h:Function {id:'rc5287:handler'}),(rt:Workload {id:'rc5287:rtwl'}) CREATE (h)-[:RUNS_IN]->(rt)`, nil)
	write(`MATCH (repo2:Repository {id:'rc5287:repo2'}),(e:Endpoint {id:'rc5287:ep'}) CREATE (repo2)-[:EXPOSES_ENDPOINT]->(e)`, nil)

	handler := &CodeHandler{Neo4j: NewNeo4jReader(driver, "nornic")}
	r := httptest.NewRequest(http.MethodGet, "/api/v0/code/route-to-caller", nil)
	req := routeToCallerRequest{Path: "/orders", MaxDepth: 5, Limit: 25}

	// Q3: route rows -> select route.
	routeRows, err := handler.routeToCallerRouteRows(r, req)
	if err != nil {
		t.Fatalf("routeToCallerRouteRows: %v", err)
	}
	route, status, ok := selectRouteToCallerRoute(routeRows)
	if !ok {
		t.Fatalf("selectRouteToCallerRoute status=%s rows=%#v", status, routeRows)
	}
	if route.EndpointID != "rc5287:ep" || route.HandlerID != "rc5287:handler" || route.Method != "GET" || route.HandlerName != "handleOrders" {
		t.Errorf("route = %#v, want ep/handler/GET/handleOrders", route)
	}
	if route.Framework != "flask" || route.FilePath != "app.py" {
		t.Errorf("route framework/file = %q/%q, want flask/app.py", route.Framework, route.FilePath)
	}

	// Q4: relationship rows -> callers/callees.
	relRows, err := handler.routeToCallerRelationshipRows(r, route.HandlerID, req)
	if err != nil {
		t.Fatalf("routeToCallerRelationshipRows: %v", err)
	}
	callers, callees, _ := splitRouteToCallerRelationships(relRows, req.Limit)
	if len(callers) != 1 || StringVal(callers[0], "entity_id") != "rc5287:caller" || StringVal(callers[0], "name") != "main" {
		t.Errorf("callers = %#v, want [main/rc5287:caller]", callers)
	}
	if len(callees) != 1 || StringVal(callees[0], "entity_id") != "rc5287:callee" || StringVal(callees[0], "name") != "queryDB" {
		t.Errorf("callees = %#v, want [queryDB/rc5287:callee]", callees)
	}
	if StringVal(callers[0], "file_path") != "main.py" || IntVal(callers[0], "start_line") != 1 {
		t.Errorf("caller detail = %#v, want file main.py start 1", callers[0])
	}

	// Q5: impact.
	impact, err := handler.routeToCallerImpact(r, route, req.Limit)
	if err != nil {
		t.Fatalf("routeToCallerImpact: %v", err)
	}
	workloads, _ := impact["workloads"].([]map[string]any)
	repos, _ := impact["repositories"].([]map[string]any)
	wlIDs := map[string]bool{}
	for _, w := range workloads {
		wlIDs[StringVal(w, "id")] = true
	}
	if !wlIDs["rc5287:wl"] || !wlIDs["rc5287:rtwl"] {
		t.Errorf("impact workloads = %#v, want endpoint+runtime workloads", workloads)
	}
	if len(repos) != 1 || StringVal(repos[0], "id") != "rc5287:repo2" {
		t.Errorf("impact repositories = %#v, want [rc5287:repo2]", repos)
	}
}
