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
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-499-6ac958a9@sha256:fc90a2c3115d5dc0fe9a69ac676e5c77428bcfdcadc3320f2e99f887bea22f26
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
	"reflect"
	"sort"
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

const (
	depMarkerW1 = "workload:6786-depmarker-w1"
	depMarkerW2 = "workload:6786-depmarker-w2"
)

// depMarkerSeed builds four repositories with two independent
// Repository-to-Repository DEPENDS_ON edges: r2 -> r1 and r3 -> r4. Ground
// truth: r1 and r4 are depended upon (is_dependency=true); r2 and r3 depend
// on something else and are not themselves depended upon
// (is_dependency=false). The dependency clusters are {r1, r2} and {r3, r4}.
//
// It also seeds DEPENDS_ON edges with a non-Repository endpoint: w1 -> w2
// (Workload to Workload), r1 -> w1 (Repository to Workload) and w2 -> r3
// (Workload to Repository). Ground truth ignores them. On NornicDB v1.3.3 the
// unscoped grouped reads rely on the relationship-aggregation fast path
// checking both endpoint labels; the same pin ignores a label predicate in
// WHERE (2,317 rows against a true 300, #6786 review R3-F3). If the fast path
// ever stopped checking labels, these edges would join r1, w1, w2 and r3 into
// one cluster and mark r3 as a dependency, and this test would fail.
var depMarkerSeed = []string{
	`CREATE (:Repository {id: '` + depMarkerR1 + `', name: 'depmarker-r1'})`,
	`CREATE (:Repository {id: '` + depMarkerR2 + `', name: 'depmarker-r2'})`,
	`CREATE (:Repository {id: '` + depMarkerR3 + `', name: 'depmarker-r3'})`,
	`CREATE (:Repository {id: '` + depMarkerR4 + `', name: 'depmarker-r4'})`,
	`CREATE (:Workload {id: '` + depMarkerW1 + `', name: 'depmarker-w1'})`,
	`CREATE (:Workload {id: '` + depMarkerW2 + `', name: 'depmarker-w2'})`,
	depMarkerEdge("Repository", depMarkerR2, "Repository", depMarkerR1),
	depMarkerEdge("Repository", depMarkerR3, "Repository", depMarkerR4),
	depMarkerEdge("Workload", depMarkerW1, "Workload", depMarkerW2),
	depMarkerEdge("Repository", depMarkerR1, "Workload", depMarkerW1),
	depMarkerEdge("Workload", depMarkerW2, "Repository", depMarkerR3),
}

func depMarkerEdge(fromLabel, fromID, toLabel, toID string) string {
	return `MATCH (a:` + fromLabel + ` {id: '` + fromID + `'}) MATCH (b:` + toLabel + ` {id: '` + toID + `'}) CREATE (a)-[:DEPENDS_ON]->(b)`
}

// depMarkerCleanup removes every node this test seeds, one prefix per
// statement.
var depMarkerCleanup = []string{
	`MATCH (n) WHERE n.id STARTS WITH 'repository:6786-depmarker-' DETACH DELETE n`,
	`MATCH (n) WHERE n.id STARTS WITH 'workload:6786-depmarker-' DETACH DELETE n`,
}

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
	for _, stmt := range depMarkerCleanup {
		reader.write(ctx, t, stmt)
	}
	defer func() {
		for _, stmt := range depMarkerCleanup {
			reader.write(context.Background(), t, stmt)
		}
	}()

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
		got, clusters := listRepositoriesDependencyEvidence(t, handler, nil)
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
		assertDepMarkerClusters(t, clusters, map[string]string{
			depMarkerR1: depMarkerR1,
			depMarkerR2: depMarkerR1,
			depMarkerR3: depMarkerR3,
			depMarkerR4: depMarkerR3,
		})
	})

	t.Run("unscoped_grouped_reads_check_endpoint_labels", func(t *testing.T) {
		assertGroupedReadsExcludeNonRepositoryEdges(ctx, t, reader)
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
		got, clusters := listRepositoriesDependencyEvidence(t, handler, &authCtx)
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
		assertDepMarkerClusters(t, clusters, map[string]string{
			depMarkerR1: depMarkerR1,
			depMarkerR2: depMarkerR1,
		})
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

// listRepositoriesDependencyEvidence drives GET /api/v0/repositories and
// returns, for this test's repositories, the is_dependency marker and the
// group_key of every row grouped as a dependency cluster.
func listRepositoriesDependencyEvidence(t *testing.T, handler *Handler, authCtx *queryauth.AuthContext) (map[string]bool, map[string]string) {
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
	clusters := map[string]string{}
	for _, raw := range repos {
		repo := raw.(map[string]any)
		id := querycontract.StringVal(repo, "id")
		if !strings.HasPrefix(id, "repository:6786-depmarker-") {
			continue
		}
		got[id] = querycontract.BoolVal(repo, "is_dependency")
		if querycontract.StringVal(repo, "group_source") == repositoryGroupSourceDependencyCluster {
			clusters[id] = querycontract.StringVal(repo, "group_key")
		}
	}
	return got, clusters
}

// assertDepMarkerClusters fails unless exactly want's repositories are
// grouped as dependency clusters with want's cluster keys.
func assertDepMarkerClusters(t *testing.T, got, want map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dependency clusters = %v, want %v", got, want)
	}
}

// assertGroupedReadsExcludeNonRepositoryEdges runs the two unscoped grouped
// statements production uses and fails if any row for this test's seed
// carries a Workload endpoint. It checks the rows for this seed's ids only,
// so other data on a shared backend cannot affect it. The group-size read is
// the statement the capped path runs when the DEPENDS_ON count exceeds the
// bound, which a small live seed cannot reach through the handler.
func assertGroupedReadsExcludeNonRepositoryEdges(ctx context.Context, t *testing.T, reader repositoryLiveReader) {
	t.Helper()
	seeded := func(id string) bool {
		return strings.HasPrefix(id, "repository:6786-depmarker-") || strings.HasPrefix(id, "workload:6786-depmarker-")
	}

	rows, err := readGroupedRepositoryDependencyEdges(ctx, reader, repositoryDependencyClusterEdgeFetchLimit)
	if err != nil {
		t.Fatalf("grouped read: %v", err)
	}
	groups := map[string][]string{}
	for _, row := range rows {
		source := querycontract.StringVal(row, "source_id")
		if !seeded(source) {
			continue
		}
		targets := groupedTargetIDs(row["target_ids"])
		sort.Strings(targets)
		groups[source] = targets
	}
	wantGroups := map[string][]string{depMarkerR2: {depMarkerR1}, depMarkerR3: {depMarkerR4}}
	if !reflect.DeepEqual(groups, wantGroups) {
		t.Errorf("grouped read rows for the seed = %v, want %v (a Workload id means the endpoint label check was skipped)", groups, wantGroups)
	}

	rows, err = readRepositoryDependencyGroupSizes(ctx, reader)
	if err != nil {
		t.Fatalf("group-size read: %v", err)
	}
	sizes := map[string]int64{}
	for _, row := range rows {
		source := querycontract.StringVal(row, "source_id")
		if seeded(source) {
			sizes[source] = groupTargetCount(row["target_count"])
		}
	}
	wantSizes := map[string]int64{depMarkerR2: 1, depMarkerR3: 1}
	if !reflect.DeepEqual(sizes, wantSizes) {
		t.Errorf("group-size rows for the seed = %v, want %v (a Workload id or a size above 1 means the endpoint label check was skipped)", sizes, wantSizes)
	}
}
