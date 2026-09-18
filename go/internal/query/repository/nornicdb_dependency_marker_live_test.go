// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live answer-truth proof for issue #6786 defect 1: the repository
// dependency marker (is_dependency).
// querycontract.RepositoryDependencyMarkerProjection rendered
// `EXISTS { MATCH (r)<-[:DEPENDS_ON]-(dep:Repository)... } as is_dependency`
// as a RETURN expression. On NornicDB v1.3.3 an EXISTS used as a RETURN
// expression is always false, so every repository row reported
// is_dependency=false regardless of ground truth. In scoped mode the
// caller's grant predicate was spliced directly after the MATCH pattern
// with no WHERE keyword, which is invalid Cypher: Neo4j fails the whole
// scoped repository list with a SyntaxError, while NornicDB silently
// accepts it and still returns false.
//
// The fix derives is_dependency in Go from the same bounded, correctly
// scoped (:Repository)-[:DEPENDS_ON]->(:Repository) edge pre-pass the
// handler already runs for dependency-cluster grouping
// (loadRepositoryDependencyEdges / repositoryDependencyClusterEdgeCypher),
// instead of a second per-row graph expression.
//
// Run against isolated containers on the pinned images:
//
//	docker run -d --name eshu-6786-fix2-nornic -p 127.0.0.1:27900:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_ASYNC_WRITES_ENABLED=false \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -e NORNICDB_HEIMDALL_ENABLED=false \
//	  -e NORNICDB_SEARCH_BM25_ENABLED=false -e NORNICDB_SEARCH_VECTOR_ENABLED=false \
//	  -e NORNICDB_QDRANT_GRPC_ENABLED=false -e NORNICDB_PERSIST_SEARCH_INDEXES=false \
//	  timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27900 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query/repository -tags live_nornicdb_answer_truth \
//	  -run TestLiveRepositoryDependencyMarkerAnswerTruth -count=1 -v
//
//	docker run -d --name eshu-6786-fix2-neo4j -p 127.0.0.1:27910:7687 \
//	  -e NEO4J_AUTH=none neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27910 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/query/repository -tags live_nornicdb_answer_truth \
//	  -run TestLiveRepositoryDependencyMarkerAnswerTruth -count=1 -v
//
// Call-contract note (eshu-mcp-call-rigor): both requests this test drives
// are bounded (LIMIT via a small explicit page limit), the unscoped case
// carries no scope by design (shared/admin/local), the scoped case supplies
// AllowedRepositoryIDs as its canonical scope, and both read the "data"
// payload from the standard {data, truth, error} envelope rather than
// treating "repositories" as a top-level field.
package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	eshugraph "github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	depMarkerR1 = "repository:6786-depmarker-r1"
	depMarkerR2 = "repository:6786-depmarker-r2"
	depMarkerR3 = "repository:6786-depmarker-r3"
	depMarkerR4 = "repository:6786-depmarker-r4"
)

// depMarkerSeed builds four repositories with two independent DEPENDS_ON
// edges: r2 -> r1 and r3 -> r4. Ground truth: r1 and r4 are depended upon
// (is_dependency=true); r2 and r3 depend on something else and are not
// themselves depended upon (is_dependency=false).
var depMarkerSeed = []string{
	`CREATE (:Repository {id: '` + depMarkerR1 + `', name: 'depmarker-r1'})`,
	`CREATE (:Repository {id: '` + depMarkerR2 + `', name: 'depmarker-r2'})`,
	`CREATE (:Repository {id: '` + depMarkerR3 + `', name: 'depmarker-r3'})`,
	`CREATE (:Repository {id: '` + depMarkerR4 + `', name: 'depmarker-r4'})`,
	depMarkerEdge(depMarkerR2, depMarkerR1),
	depMarkerEdge(depMarkerR3, depMarkerR4),
}

func depMarkerEdge(fromID, toID string) string {
	return `MATCH (a:Repository {id: '` + fromID + `'}) MATCH (b:Repository {id: '` + toID + `'}) CREATE (a)-[:DEPENDS_ON]->(b)`
}

const depMarkerCleanup = `MATCH (n) WHERE n.id STARTS WITH 'repository:6786-depmarker-' DETACH DELETE n`

// TestLiveRepositoryDependencyMarkerAnswerTruth drives the real
// GET /api/v0/repositories (unscoped and scoped) and GET /api/v0/catalog
// handlers against a live graph backend and proves the is_dependency
// marker matches ground truth in every mode.
func TestLiveRepositoryDependencyMarkerAnswerTruth(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend := strings.TrimSpace(strings.ToLower(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	if backend == "" {
		t.Fatal("ESHU_LIVE_GRAPH_BACKEND is required (nornicdb|neo4j)")
	}
	database := liveGraphDatabaseName(backend)

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

	reader := repositoryLiveReader{driver: driver, database: database}
	reader.write(ctx, t, depMarkerCleanup)
	defer reader.write(context.Background(), t, depMarkerCleanup)

	schemaBackend := eshugraph.SchemaBackendNeo4j
	if backend == "nornicdb" {
		schemaBackend = eshugraph.SchemaBackendNornicDB
	}
	if err := eshugraph.EnsureSchemaWithBackend(ctx, reader, nil, schemaBackend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	for _, stmt := range depMarkerSeed {
		reader.write(ctx, t, stmt)
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}

	t.Run("unscoped", func(t *testing.T) {
		got := listRepositoriesIsDependency(t, handler, nil)
		want := map[string]bool{
			depMarkerR1: true,
			depMarkerR2: false,
			depMarkerR3: false,
			depMarkerR4: true,
		}
		for id, wantDep := range want {
			if got[id] != wantDep {
				t.Errorf("unscoped is_dependency[%s] = %v, want %v (all: %v)", id, got[id], wantDep, got)
			}
		}
	})

	t.Run("scoped", func(t *testing.T) {
		authCtx := queryauth.AuthContext{
			Mode:                 queryauth.AuthModeScoped,
			TenantID:             "tenant-6786",
			WorkspaceID:          "workspace-6786",
			SubjectClass:         "team",
			SubjectIDHash:        "sha256:team-6786",
			PolicyRevisionHash:   "sha256:policy-6786",
			AllowedRepositoryIDs: []string{depMarkerR1, depMarkerR2},
		}
		got := listRepositoriesIsDependency(t, handler, &authCtx)
		want := map[string]bool{
			depMarkerR1: true,
			depMarkerR2: false,
		}
		if len(got) != len(want) {
			t.Fatalf("scoped result set = %v, want exactly %v", got, want)
		}
		for id, wantDep := range want {
			if got[id] != wantDep {
				t.Errorf("scoped is_dependency[%s] = %v, want %v (all: %v)", id, got[id], wantDep, got)
			}
		}
	})

	t.Run("catalog_unscoped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=100", nil)
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		handler.listCatalog(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
		}
		var envelope querycontract.ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		data := envelope.Data.(map[string]any)
		repos := data["repositories"].([]any)
		got := map[string]bool{}
		for _, raw := range repos {
			repo := raw.(map[string]any)
			id := querycontract.StringVal(repo, "id")
			if !strings.HasPrefix(id, "repository:6786-depmarker-") {
				continue
			}
			got[id] = querycontract.BoolVal(repo, "is_dependency")
		}
		want := map[string]bool{
			depMarkerR1: true,
			depMarkerR2: false,
			depMarkerR3: false,
			depMarkerR4: true,
		}
		for id, wantDep := range want {
			if got[id] != wantDep {
				t.Errorf("catalog is_dependency[%s] = %v, want %v (all: %v)", id, got[id], wantDep, got)
			}
		}
	})
}

func listRepositoriesIsDependency(t *testing.T, handler *Handler, authCtx *queryauth.AuthContext) map[string]bool {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=100", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	if authCtx != nil {
		req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), *authCtx))
	}
	rec := httptest.NewRecorder()
	handler.listRepositories(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data := envelope.Data.(map[string]any)
	repos := data["repositories"].([]any)
	got := map[string]bool{}
	for _, raw := range repos {
		repo := raw.(map[string]any)
		id := querycontract.StringVal(repo, "id")
		if !strings.HasPrefix(id, "repository:6786-depmarker-") {
			continue
		}
		got[id] = querycontract.BoolVal(repo, "is_dependency")
	}
	return got
}

// liveGraphDatabaseName returns ESHU_LIVE_GRAPH_DATABASE when set, or the
// pinned image's default database name for backend ("nornic" for NornicDB,
// "neo4j" for Neo4j).
func liveGraphDatabaseName(backend string) string {
	if db := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE")); db != "" {
		return db
	}
	if backend == "nornicdb" {
		return "nornic"
	}
	return "neo4j"
}

// repositoryLiveReader is the test-only live GraphQuery + graph.CypherExecutor
// for this file. The package cannot import root query's Neo4jReader without a
// cycle (same rationale as entity's entityLiveReader).
type repositoryLiveReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (r repositoryLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
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

func (r repositoryLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// ExecuteCypher implements graph.CypherExecutor so this reader can also drive
// graph.EnsureSchemaWithBackend.
func (r repositoryLiveReader) ExecuteCypher(ctx context.Context, stmt eshugraph.CypherStatement) error {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func (r repositoryLiveReader) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("consume %q: %v", cypher, err)
	}
}
