// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for issue #8795 scenario A2 on a real Neo4j with Eshu's schema
// applied: a pre-#5385 name-only Workload next to its repository-scoped twin
// makes the service story ambiguous, the 409 marks only the leftover
// legacy_id, and once the retirement statement removes the leftover the same
// selector resolves to the scoped workload.
//
// The retirement step runs legacyworkload.Detect and
// legacyworkload.DeleteNeo4jCypher, the detection and delete statement the
// reducer's retirement route executes; this package may not import the
// reducer, so the route itself is proven in
// go/internal/reducer/legacy_workload_retirement_live_test.go.
//
// Neo4j only (owner direction). Run it with, for example:
//
//	docker run -d --name 8795-neo4j -e NEO4J_AUTH=none \
//	  -p 127.0.0.1:<bolt-port>:7687 neo4j:2026-community
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<bolt-port> \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_LIVE_GRAPH_DATABASE=neo4j \
//	  go test ./internal/query/entity -tags live_nornicdb_answer_truth \
//	  -run TestLiveNeo4jServiceStoryLegacyLeftover -count=1 -v
package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func TestLiveNeo4jServiceStoryLegacyLeftover(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend, database := liveGraphBackend()
	if backend != graph.SchemaBackendNeo4j {
		t.Skip("the legacy workload retirement is tested on Neo4j only (#8795)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	if err := graph.EnsureSchemaWithBackendStrict(ctx, liveSchemaExecutor{driver: driver, database: database}, nil, backend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	nonce := fmt.Sprintf("a2-8795-%d", time.Now().UnixNano())
	repoID := "repository:r_" + nonce
	name := "checkout-" + nonce
	handle := "workload:" + name
	scopedID := legacyworkload.TwinID(repoID, name)
	if scopedID == "" || !legacyworkload.IsLegacyID(handle, name) {
		t.Fatalf("fixture ids: scoped %q, handle %q are not the production shapes", scopedID, handle)
	}

	reader := entityLiveReader{driver: driver, database: database}
	write := func(cypher string, params map[string]any) neo4jdriver.Counters {
		t.Helper()
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, cypher, params)
		if err != nil {
			t.Fatalf("write %q: %v", cypher, err)
		}
		summary, err := result.Consume(ctx)
		if err != nil {
			t.Fatalf("write %q: %v", cypher, err)
		}
		return summary.Counters()
	}
	ids := map[string]any{"repo": repoID, "scoped": scopedID, "handle": handle, "name": name}
	cleanup := func() {
		write(`MATCH (n) WHERE n.id IN [$repo, $scoped, $handle] DETACH DELETE n`, ids)
	}
	cleanup()
	defer cleanup()
	// The live workload: scoped id, a generation, defined by its repository.
	// The leftover: name-only handle, no generation, defined by the same
	// repository, no instance.
	write(`CREATE (r:Repository {id: $repo, name: $repo})
CREATE (s:Workload {id: $scoped, name: $name, kind: 'service', repo_id: $repo, generation_id: 'gen-1', scope_id: $repo})
CREATE (l:Workload {id: $handle, name: $name, kind: 'service', repo_id: $repo})
CREATE (r)-[:DEFINES]->(s)
CREATE (r)-[:DEFINES]->(l)`, ids)

	handler := &Handler{GraphBackend: querycontract.GraphBackendNeo4j, Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	story := func(t *testing.T, query string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v0/services/"+name+"/story"+query, nil)
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json.Unmarshal: %v; body = %s", err, rec.Body.String())
		}
		return rec.Code, body
	}

	selectors := []struct {
		label string
		query string
	}{
		{"service_name", ""},
		{"service_name and repo", "?repo=" + repoID},
	}

	for _, sel := range selectors {
		t.Run("ambiguous before retirement/"+sel.label, func(t *testing.T) {
			status, body := story(t, sel.query)
			if status != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body = %v", status, body)
			}
			errBody, _ := body["error"].(map[string]any)
			details, _ := errBody["details"].(map[string]any)
			candidates, _ := details["candidates"].([]any)
			if len(candidates) != 2 {
				t.Fatalf("candidates = %#v, want the leftover and its twin", candidates)
			}
			marks := map[string]any{}
			for _, raw := range candidates {
				candidate, _ := raw.(map[string]any)
				id, _ := candidate["service_id"].(string)
				marks[id] = candidate["legacy_id"]
			}
			if got := marks[handle]; got != true {
				t.Fatalf("leftover legacy_id = %#v, want true; candidates = %#v", got, candidates)
			}
			if got, listed := marks[scopedID]; !listed || got != nil {
				t.Fatalf("scoped legacy_id = %#v (listed %v), want listed and unmarked", got, listed)
			}
		})
	}

	report, err := legacyworkload.Detect(ctx, reader, legacyworkload.Options{IDs: []string{handle, scopedID}})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if got := report.Eligible(); got != 1 {
		t.Fatalf("dry-run eligible = %d, want 1 (the leftover only); report = %#v", got, report)
	}
	counters := write(legacyworkload.DeleteNeo4jCypher, map[string]any{"ids": []any{handle, scopedID}})
	if got := counters.NodesDeleted(); got != 1 {
		t.Fatalf("retirement deleted %d nodes, want 1", got)
	}

	for _, sel := range selectors {
		t.Run("resolves after retirement/"+sel.label, func(t *testing.T) {
			status, body := story(t, sel.query)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %v", status, body)
			}
			encoded, err := json.Marshal(body["data"])
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if !strings.Contains(string(encoded), `"`+scopedID+`"`) {
				t.Fatalf("data does not name the scoped workload %s: %s", scopedID, encoded)
			}
			if strings.Contains(string(encoded), `"`+handle+`"`) {
				t.Fatalf("data still names the retired leftover %s: %s", handle, encoded)
			}
		})
	}
}
