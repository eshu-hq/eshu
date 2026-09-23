// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live answer-truth proof for issue #6786 defect 2: the repo-filtered
// relationship lookup at relationship_handlers.go's name+repo_id branch.
// relationshipGraphRowCypher spliced a backward multi-hop EXISTS
// ("e.name = $name AND EXISTS { MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository) WHERE repo.id = $repo_id }")
// into a bare `MATCH (e) WHERE ...` scan. On NornicDB v1.3.3 that backward
// multi-hop EXISTS is ignored, so a same-named entity in a DIFFERENT
// repository still matches and RunSingle returns whichever row comes back
// first, regardless of the requested repo_id. Neo4j evaluates the EXISTS
// correctly.
//
// This test seeds two repositories that each contain a Function named "Run"
// and asks for "Run" scoped to each repository in turn. The defect's
// signature is that both requests resolve to the SAME entity id on NornicDB
// (the repo_id filter is a no-op), while the fixed anchor-on-repository
// shape (MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(file:File)-[:CONTAINS]->(e))
// must resolve each request to its own repository's entity on both backends.
//
// GraphBackend is left unset on the handler so the request dispatches
// through relationshipsGraphRow's default (Neo4j-labelled) branch even
// though the backing store is the live NornicDB container. #6786 review F2
// established that in production this is NOT the default path: config
// loading (loadGraphBackend -> querycontract.ParseGraphBackend("")) resolves
// an unset ESHU_GRAPH_BACKEND to GraphBackendNornicDB before CodeHandler is
// ever constructed, so both cmd/api and cmd/mcp-server always pass a
// concrete non-empty value; CodeHandler.graphBackend()'s own "" -> Neo4j
// fallback (a *different* default than ParseGraphBackend's) is reachable
// only from code, like this test, that constructs CodeHandler directly
// without going through that config loader. The defect this test proves is
// therefore reachable in production only when an operator explicitly sets
// ESHU_GRAPH_BACKEND=neo4j while the backing store is actually NornicDB (a
// mismatched configuration), not on the default path. The fix is still
// correct and is defense-in-depth hardening for that path. The
// "nornicdb_dispatch" subtest below proves the actual default
// (NornicDB-dispatched, relationships.MetadataRow) path live instead.
//
// Run against isolated containers on the pinned images:
//
//	docker run -d --name eshu-6786-fix2-nornic -p 127.0.0.1:27900:7687 \
//	  -e NORNICDB_NO_AUTH=true -e NORNICDB_ASYNC_WRITES_ENABLED=false \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -e NORNICDB_HEIMDALL_ENABLED=false \
//	  -e NORNICDB_SEARCH_BM25_ENABLED=false -e NORNICDB_SEARCH_VECTOR_ENABLED=false \
//	  -e NORNICDB_QDRANT_GRPC_ENABLED=false -e NORNICDB_PERSIST_SEARCH_INDEXES=false \
//	  ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27900 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
//	  go test ./internal/query/codequery -tags live_nornicdb_answer_truth \
//	  -run TestLiveRelationshipRepoAnchorAnswerTruth -count=1 -v
//
//	docker run -d --name eshu-6786-fix2-neo4j -p 127.0.0.1:27910:7687 \
//	  -e NEO4J_AUTH=none neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27910 ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test ./internal/query/codequery -tags live_nornicdb_answer_truth \
//	  -run TestLiveRelationshipRepoAnchorAnswerTruth -count=1 -v
//
// Call-contract note (eshu-mcp-call-rigor): the request drives the real
// POST /api/v0/code/relationships handler with an explicit repo_id scope (the
// canonical scope for this tool) and reads the entity from the standard
// {data, truth, error} envelope's "data" payload.
package codequery

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

	eshugraph "github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	relAnchorRepoA = "repository:6786-relanchor-repo-a"
	relAnchorRepoB = "repository:6786-relanchor-repo-b"
	relAnchorFnA   = "repository:6786-relanchor-fn-run-a"
	relAnchorFnB   = "repository:6786-relanchor-fn-run-b"
)

// relAnchorSeed builds two repositories that each contain their own File and
// a Function named "Run", so a name-only match without repository anchoring
// is genuinely ambiguous between them.
var relAnchorSeed = []string{
	`CREATE (:Repository {id: '` + relAnchorRepoA + `', name: 'relanchor-repo-a'})`,
	`CREATE (:Repository {id: '` + relAnchorRepoB + `', name: 'relanchor-repo-b'})`,
	`CREATE (:File {id: 'repository:6786-relanchor-file-a', relative_path: 'a.go', language: 'go'})`,
	`CREATE (:File {id: 'repository:6786-relanchor-file-b', relative_path: 'b.go', language: 'go'})`,
	`CREATE (:Function {id: '` + relAnchorFnA + `', name: 'Run', language: 'go'})`,
	`CREATE (:Function {id: '` + relAnchorFnB + `', name: 'Run', language: 'go'})`,
	relAnchorEdge("Repository", relAnchorRepoA, "REPO_CONTAINS", "File", "repository:6786-relanchor-file-a"),
	relAnchorEdge("Repository", relAnchorRepoB, "REPO_CONTAINS", "File", "repository:6786-relanchor-file-b"),
	relAnchorEdge("File", "repository:6786-relanchor-file-a", "CONTAINS", "Function", relAnchorFnA),
	relAnchorEdge("File", "repository:6786-relanchor-file-b", "CONTAINS", "Function", relAnchorFnB),
}

func relAnchorEdge(fromLabel, fromID, relType, toLabel, toID string) string {
	return `MATCH (a:` + fromLabel + ` {id: '` + fromID + `'}) MATCH (b:` + toLabel + ` {id: '` + toID + `'}) CREATE (a)-[:` + relType + `]->(b)`
}

const relAnchorCleanup = `MATCH (n) WHERE n.id STARTS WITH 'repository:6786-relanchor-' DETACH DELETE n`

func TestLiveRelationshipRepoAnchorAnswerTruth(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend := strings.TrimSpace(strings.ToLower(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	if backend == "" {
		t.Fatal("ESHU_LIVE_GRAPH_BACKEND is required (nornicdb|neo4j)")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	if database == "" {
		if backend == "nornicdb" {
			database = "nornic"
		} else {
			database = "neo4j"
		}
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

	reader := codequeryLiveReader{driver: driver, database: database}
	reader.write(ctx, t, relAnchorCleanup)
	defer reader.write(context.Background(), t, relAnchorCleanup)

	schemaBackend := eshugraph.SchemaBackendNeo4j
	if backend == "nornicdb" {
		schemaBackend = eshugraph.SchemaBackendNornicDB
	}
	if err := eshugraph.EnsureSchemaWithBackend(ctx, reader, nil, schemaBackend); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	for _, stmt := range relAnchorSeed {
		reader.write(ctx, t, stmt)
	}

	t.Run("neo4j_dispatch_hardening", func(t *testing.T) {
		// GraphBackend intentionally left unset: this exercises
		// relationshipsGraphRow's default (Neo4j-labelled) branch even
		// though the backing store here is the live NornicDB container --
		// see the file-level comment above for why this is defense-in-depth
		// hardening for a mismatched-config path, not the production
		// default.
		handler := &CodeHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}

		entityA, repoIDA := queryRunRelationshipEntity(t, handler, relAnchorRepoA)
		entityB, repoIDB := queryRunRelationshipEntity(t, handler, relAnchorRepoB)

		if entityA == entityB {
			t.Fatalf("querying \"Run\" scoped to repo A (%s) and repo B (%s) resolved to the SAME entity %q -- repo_id filter is not being applied", relAnchorRepoA, relAnchorRepoB, entityA)
		}
		if entityA != relAnchorFnA {
			t.Errorf("repo A entity_id = %q, want %q", entityA, relAnchorFnA)
		}
		if entityB != relAnchorFnB {
			t.Errorf("repo B entity_id = %q, want %q", entityB, relAnchorFnB)
		}
		if repoIDA != relAnchorRepoA {
			t.Errorf("repo A response repo_id = %q, want %q", repoIDA, relAnchorRepoA)
		}
		if repoIDB != relAnchorRepoB {
			t.Errorf("repo B response repo_id = %q, want %q", repoIDB, relAnchorRepoB)
		}
	})

	// #6786 review F2: prove the ACTUAL production default -- GraphBackend
	// set to whatever loadGraphBackend resolves an unset ESHU_GRAPH_BACKEND
	// to (GraphBackendNornicDB) -- against a real NornicDB store. This
	// dispatches to nornicDBRelationshipsGraphRow -> relationships.MetadataRow
	// (identity.go), a completely different Cypher shape (two separate
	// forward MATCH clauses, not the backward EXISTS defect 2 fixed) that
	// this test file's main defect-2 proof never exercises. Only meaningful
	// against a real NornicDB backend; setting GraphBackendNornicDB while
	// pointed at Neo4j would send NornicDB-dialect Cypher to Neo4j, which
	// proves nothing about either backend.
	if backend == "nornicdb" {
		t.Run("nornicdb_dispatch", func(t *testing.T) {
			handler := &CodeHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative, GraphBackend: GraphBackendNornicDB}

			entityA, repoIDA := queryRunRelationshipEntity(t, handler, relAnchorRepoA)
			entityB, repoIDB := queryRunRelationshipEntity(t, handler, relAnchorRepoB)

			if entityA == entityB {
				t.Fatalf("NornicDB-dispatched: querying \"Run\" scoped to repo A (%s) and repo B (%s) resolved to the SAME entity %q -- repo_id filter is not being applied on the production default path", relAnchorRepoA, relAnchorRepoB, entityA)
			}
			if entityA != relAnchorFnA {
				t.Errorf("NornicDB-dispatched: repo A entity_id = %q, want %q", entityA, relAnchorFnA)
			}
			if entityB != relAnchorFnB {
				t.Errorf("NornicDB-dispatched: repo B entity_id = %q, want %q", entityB, relAnchorFnB)
			}
			if repoIDA != relAnchorRepoA {
				t.Errorf("NornicDB-dispatched: repo A response repo_id = %q, want %q", repoIDA, relAnchorRepoA)
			}
			if repoIDB != relAnchorRepoB {
				t.Errorf("NornicDB-dispatched: repo B response repo_id = %q, want %q", repoIDB, relAnchorRepoB)
			}
		})
	}
}

// queryRunRelationshipEntity drives the real POST /api/v0/code/relationships
// handler for name="Run" scoped to repoID and returns the resolved
// entity_id and repo_id from the response envelope's data payload.
func queryRunRelationshipEntity(t *testing.T, handler *CodeHandler, repoID string) (entityID string, responseRepoID string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"name": "Run", "repo_id": repoID})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.handleRelationships(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("repo_id=%s: status = %d, want 200, body=%s", repoID, rec.Code, rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("repo_id=%s: json.Unmarshal() error = %v", repoID, err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("repo_id=%s: response data is not an object: %#v", repoID, envelope.Data)
	}
	return querycontract.StringVal(data, "entity_id"), querycontract.StringVal(data, "repo_id")
}

// codequeryLiveReader is the test-only live GraphQuery + graph.CypherExecutor
// for this file. The package cannot import root query's Neo4jReader without a
// cycle (same rationale as entity's entityLiveReader).
type codequeryLiveReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (r codequeryLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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

func (r codequeryLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// ExecuteCypher implements graph.CypherExecutor so this reader can also drive
// graph.EnsureSchemaWithBackend.
func (r codequeryLiveReader) ExecuteCypher(ctx context.Context, stmt eshugraph.CypherStatement) error {
	return runLiveWriteWithRetry(ctx, func(ctx context.Context) error {
		session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
		if err != nil {
			return err
		}
		_, err = result.Consume(ctx)
		return err
	})
}

func (r codequeryLiveReader) write(ctx context.Context, t *testing.T, cypher string) {
	t.Helper()
	err := runLiveWriteWithRetry(ctx, func(ctx context.Context) error {
		session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx, cypher, nil)
		if err != nil {
			return err
		}
		_, err = result.Consume(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("write %q: %v", cypher, err)
	}
}

// liveWriteMaxAttempts bounds the retry count for a transient graph-write
// conflict (see isLiveWriteTransientError). Go runs different test packages
// concurrently by default even with no t.Parallel(), so this package's live
// test and a sibling package's live test (e.g. repository's) can race
// writes against the same shared NornicDB/Neo4j instance in CI (#6784).
const liveWriteMaxAttempts = 5

// liveWriteRetryDelay is a small linear backoff for a racing test-seed/
// cleanup/schema write, not a production retry policy: these are one-shot
// DDL/seed statements contending with a sibling test package, not a
// production hot path.
func liveWriteRetryDelay(attempt int) time.Duration {
	return time.Duration(attempt) * 100 * time.Millisecond
}

// isLiveWriteTransientError reports whether err is a transient, safe-to-retry
// write conflict -- observed live as
// "Neo.TransientError.Transaction.Outdated ... Please retry" when two test
// packages' live writes race the same shared NornicDB/Neo4j instance.
func isLiveWriteTransientError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "TransientError")
}

// runLiveWriteWithRetry runs run, retrying up to liveWriteMaxAttempts times
// with a small backoff on a transient write conflict, and returning
// immediately on success or a non-transient error.
func runLiveWriteWithRetry(ctx context.Context, run func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= liveWriteMaxAttempts; attempt++ {
		lastErr = run(ctx)
		if lastErr == nil {
			return nil
		}
		if !isLiveWriteTransientError(lastErr) || attempt == liveWriteMaxAttempts {
			return lastErr
		}
		time.Sleep(liveWriteRetryDelay(attempt))
	}
	return lastErr
}
