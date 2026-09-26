// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const indexStatusScopedLiveEnv = "ESHU_INDEX_STATUS_NORNICDB_LIVE"

// countingGraph counts RunSingle calls so the live test can prove an empty
// grant never reaches the backend.
type countingGraph struct {
	GraphQuery
	calls atomic.Int64
}

func (g *countingGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	g.calls.Add(1)
	return g.GraphQuery.RunSingle(ctx, cypher, params)
}

// TestIndexStatusScopedRepositoryCountNornicDBLive proves the grant-bound
// count statement getScopedIndexStatus ships is a real row filter on the
// pinned NornicDB, not merely text that mentions the grant. The pinned build
// silently ignores an invalid WHERE and returns every row, so a statement
// assertion proves nothing: this test seeds three Repository nodes and reads
// the count back through the real handler and the real Neo4jReader for each
// grant shape, including a repository named by both grant lists (which must
// not double count) and an empty grant (which must not query at all).
//
// Run against an isolated NornicDB:
//
//	ESHU_INDEX_STATUS_NORNICDB_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17997 \
//	go test ./internal/query -run TestIndexStatusScopedRepositoryCountNornicDBLive -count=1 -v
func TestIndexStatusScopedRepositoryCountNornicDBLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv(indexStatusScopedLiveEnv)) == "" {
		t.Skip("set " + indexStatusScopedLiveEnv + "=1 to run the live NornicDB index-status proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	reader := NewNeo4jReader(driver, "nornic")
	pre, err := reader.RunSingle(ctx, `MATCH (r:Repository) RETURN count(r) AS count`, nil)
	if err != nil {
		t.Fatalf("count existing Repository nodes: %v", err)
	}
	if got := IntVal(pre, "count"); got != 0 {
		t.Fatalf("live proof requires an isolated graph with zero Repository nodes, got %d", got)
	}

	prefix := fmt.Sprintf("idx-status-live-%d-", time.Now().UnixNano())
	repoA, repoB, repoC := prefix+"a", prefix+"b", prefix+"c"
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	seed, err := session.Run(ctx, `UNWIND $ids AS id CREATE (:Repository {id: id, name: id})`, map[string]any{"ids": []string{repoA, repoB, repoC}})
	if err != nil {
		t.Fatalf("seed repositories: %v", err)
	}
	if _, err := seed.Consume(ctx); err != nil {
		t.Fatalf("seed repositories: %v", err)
	}
	defer func() {
		del, delErr := session.Run(ctx, `MATCH (r:Repository) WHERE r.id STARTS WITH $prefix DETACH DELETE r`, map[string]any{"prefix": prefix})
		if delErr == nil {
			_, _ = del.Consume(ctx)
		}
	}()

	for _, tc := range []struct {
		name      string
		auth      AuthContext
		wantCount float64
		wantCalls int64
	}{
		{"one granted repository", AuthContext{AllowedRepositoryIDs: []string{repoA}}, 1, 1},
		{"two granted repositories", AuthContext{AllowedRepositoryIDs: []string{repoA, repoB}}, 2, 1},
		{"scope grant naming a repository", AuthContext{AllowedScopeIDs: []string{repoB}}, 1, 1},
		{"repository and scope grants", AuthContext{AllowedRepositoryIDs: []string{repoA}, AllowedScopeIDs: []string{repoC}}, 2, 1},
		{"same id in both grant lists", AuthContext{AllowedRepositoryIDs: []string{repoA}, AllowedScopeIDs: []string{repoA}}, 1, 1},
		{"grant naming no repository", AuthContext{AllowedRepositoryIDs: []string{prefix + "absent"}}, 0, 1},
		{"empty grant", AuthContext{}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := &countingGraph{GraphQuery: reader}
			handler := &StatusHandler{StatusReader: fakeStatusReader{}, Neo4j: graph}
			mux := http.NewServeMux()
			handler.Mount(mux)
			auth := tc.auth
			auth.Mode, auth.TenantID, auth.WorkspaceID = AuthModeScoped, "tenant-live", "workspace-live"
			authed := AuthMiddlewareWithScopedTokens("", &fakeScopedTokenResolver{context: auth, ok: true}, mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil)
			req.Header.Set("Authorization", "Bearer live-token")
			rec := httptest.NewRecorder()
			authed.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got := payload["repository_count"]; got != tc.wantCount {
				t.Errorf("repository_count = %#v, want %v (3 repositories are seeded)", got, tc.wantCount)
			}
			if got := graph.calls.Load(); got != tc.wantCalls {
				t.Errorf("graph calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}
