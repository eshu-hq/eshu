// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestImportDependencyMethodologyMCPTerminalCapLive proves that the real MCP
// JSON-RPC tools/call transport preserves a live bounded cycle terminal page.
func TestImportDependencyMethodologyMCPTerminalCapLive(t *testing.T) {
	if os.Getenv("ESHU_QUERY_METHODOLOGY_LIVE") != "1" {
		t.Skip("set ESHU_QUERY_METHODOLOGY_LIVE=1")
	}
	if os.Getenv("ESHU_QUERYPLAN_PROFILE_ISOLATED") != "1" {
		t.Fatal("isolated Neo4j required")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	auth := neo4j.NoAuth()
	if user := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME")); user != "" {
		auth = neo4j.BasicAuth(user, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	driver, err := neo4j.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatal(err)
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	methodologyMCPSeed(t, ctx, driver, database)
	graph := methodologyMCPGraph{driver: driver, database: database}
	handler := &codequery.CodeHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)
	transport := httptest.NewServer(NewServer(mux, nil).Handler(nil))
	defer transport.Close()
	body := `{"jsonrpc":"2.0","id":7881,"method":"tools/call","params":{"name":"investigate_import_dependencies","arguments":{"query_type":"file_import_cycles","repo_id":"mcp-cycle-cap-repository","offset":999,"limit":200}}}`
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, transport.URL+"/mcp/message", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := transport.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("MCP HTTP %d: %s", response.StatusCode, payload)
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	result, ok := wire["result"].(map[string]any)
	if !ok || result["isError"] == true {
		t.Fatalf("MCP result=%v", wire)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("MCP structuredContent=%T", result["structuredContent"])
	}
	data, ok := structured["data"].(map[string]any)
	if !ok {
		t.Fatalf("MCP data=%T", structured["data"])
	}
	if data["count"] != float64(1) || data["truncated"] != true || data["has_more"] != false || data["next_offset"] != nil {
		t.Fatalf("MCP terminal cycle count=%v truncated=%v has_more=%v next_offset=%v", data["count"], data["truncated"], data["has_more"], data["next_offset"])
	}
	coverage, ok := data["coverage"].(map[string]any)
	if !ok || coverage["cycle_enumeration_stop_reason"] != "cycle_cap" {
		t.Fatalf("MCP coverage=%v", data["coverage"])
	}
	cycles, ok := data["cycles"].([]any)
	if !ok || len(cycles) != 1 {
		t.Fatalf("MCP terminal cycles=%T len=%d", data["cycles"], len(cycles))
	}
	t.Log("MCP tools/call preserved one terminal capped cycle with truncated=true and has_more=false")
}

type methodologyMCPGraph struct {
	driver   neo4j.DriverWithContext
	database string
}

func (g methodologyMCPGraph) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := g.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: g.database, AccessMode: neo4j.AccessModeRead})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (g methodologyMCPGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func methodologyMCPSeed(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string) {
	t.Helper()
	session := driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: database, AccessMode: neo4j.AccessModeWrite})
	defer func() { _ = session.Close(context.Background()) }()
	run := func(statement string) {
		t.Helper()
		result, err := session.Run(ctx, statement, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(`MATCH (n {methodology_proof:'mcp-cycle-cap'}) DETACH DELETE n`)
	run(`CREATE (:Repository {id:'mcp-cycle-cap-repository',name:'cap',methodology_proof:'mcp-cycle-cap'})`)
	run(`MATCH (r:Repository {id:'mcp-cycle-cap-repository'})
 UNWIND range(0,7) AS i
 CREATE (f:File {path:'/mcp-cap/cap'+toString(i)+'.py',relative_path:'cap'+toString(i)+'.py',name:'cap'+toString(i)+'.py',language:'python',methodology_proof:'mcp-cycle-cap'})
 CREATE (m:Module {name:'cap'+toString(i),lang:'python',methodology_proof:'mcp-cycle-cap'})
 CREATE (r)-[:REPO_CONTAINS]->(f)
 CREATE (f)-[:CONTAINS]->(m)`)
	run(`MATCH (r:Repository {id:'mcp-cycle-cap-repository'})-[:REPO_CONTAINS]->(f:File),
 (r)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(m:Module)
 WHERE f.name <> m.name+'.py'
 CREATE (f)-[:IMPORTS {line_number:1}]->(m)`)
}
