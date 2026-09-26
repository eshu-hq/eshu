// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Live proof for issue #7089 on a real graph backend with Eshu's schema
// applied: GetEntityContext's per-label reads of uid-constrained labels anchor
// on `e.uid = $entity_id AND e.id = $entity_id`, which must (1) resolve a
// canonical id == uid entity on its first read, (2) never resolve a node by
// uid alone (a File carries a uid but no id), and (3) still resolve an entity
// that carries an id but no uid through the unlabeled fallback. On Neo4j it
// also checks the plan: the uid form is a unique index seek, the old id form
// a label scan. Run it on Neo4j with, for example:
//
//	docker run -d --name eshu-7089-neo4j -e NEO4J_AUTH=none \
//	  -p 127.0.0.1:<bolt-port>:7687 neo4j:2026-community
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<bolt-port> \
//	  ESHU_LIVE_GRAPH_BACKEND=neo4j ESHU_LIVE_GRAPH_DATABASE=neo4j \
//	  go test ./internal/query/entity -tags live_nornicdb_answer_truth \
//	  -run TestLiveEntityContextUIDAnchor -count=1 -v
//
// The live-backend CI job runs it on both backends.
package entity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	uidAnchorRepoID       = "uid-anchor-entity:repo"
	uidAnchorFileUID      = "uid-anchor-entity:file"
	uidAnchorCanonicalID  = "uid-anchor-entity:fn-canonical"
	uidAnchorIDOnlyID     = "uid-anchor-entity:fn-id-only"
	uidAnchorCleanupByID  = `MATCH (n) WHERE n.id STARTS WITH 'uid-anchor-entity:' DETACH DELETE n`
	uidAnchorCleanupByUID = `MATCH (n) WHERE n.uid STARTS WITH 'uid-anchor-entity:' DETACH DELETE n`
)

// uidAnchorSeed mirrors the canonical writers: code entities carry id == uid
// and Files carry a uid but no id. The id-only Function stands for any node
// on a uid-constrained label written without a uid (older fixtures).
var uidAnchorSeed = []string{
	`CREATE (:Repository {id: '` + uidAnchorRepoID + `', name: 'uid-anchor-repo'})`,
	`CREATE (:File {uid: '` + uidAnchorFileUID + `', relative_path: 'b.go', language: 'go'})`,
	`CREATE (:Function {id: '` + uidAnchorCanonicalID + `', uid: '` + uidAnchorCanonicalID + `', name: 'Canonical', language: 'go'})`,
	`CREATE (:Function {id: '` + uidAnchorIDOnlyID + `', name: 'IDOnly', language: 'go'})`,
	`MATCH (r:Repository {id: '` + uidAnchorRepoID + `'}) MATCH (f:File {uid: '` + uidAnchorFileUID + `'}) CREATE (r)-[:REPO_CONTAINS]->(f)`,
	`MATCH (f:File {uid: '` + uidAnchorFileUID + `'}) MATCH (e:Function {uid: '` + uidAnchorCanonicalID + `'}) CREATE (f)-[:CONTAINS]->(e)`,
}

// countingLiveReader records every anchor-loop read GetEntityContext sends
// through the real live reader.
type countingLiveReader struct {
	entityLiveReader
	mu      sync.Mutex
	anchors []string
}

func (r *countingLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if strings.Contains(cypher, "OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)") {
		r.mu.Lock()
		r.anchors = append(r.anchors, anchorLine(cypher))
		r.mu.Unlock()
	}
	return r.entityLiveReader.RunSingle(ctx, cypher, params)
}

func (r *countingLiveReader) reset() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	got := r.anchors
	r.anchors = nil
	return got
}

func TestLiveEntityContextUIDAnchor(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	backend, database := liveGraphBackend()

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

	base := entityLiveReader{driver: driver, database: database}
	cleanup := func() {
		base.write(context.Background(), t, uidAnchorCleanupByID)
		base.write(context.Background(), t, uidAnchorCleanupByUID)
	}
	cleanup()
	for _, stmt := range uidAnchorSeed {
		base.write(ctx, t, stmt)
	}
	defer cleanup()

	reader := &countingLiveReader{entityLiveReader: base}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	get := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/"+id+"/context", nil)
		req.SetPathValue("entity_id", id)
		rec := httptest.NewRecorder()
		handler.GetEntityContext(rec, req)
		return rec
	}
	allReads := len(EntityContextAnchorLabels) + 1

	// (1) A canonical id == uid Function resolves on the first read, through
	// the uid-anchored Function statement, with its file and repository.
	rec := get(uidAnchorCanonicalID)
	anchors := reader.reset()
	if rec.Code != http.StatusOK {
		t.Fatalf("canonical entity: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		ID       string `json:"id"`
		FilePath string `json:"file_path"`
		RepoID   string `json:"repo_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode canonical entity: %v", err)
	}
	if body.ID != uidAnchorCanonicalID || body.FilePath != "b.go" || body.RepoID != uidAnchorRepoID {
		t.Fatalf("canonical entity = (%q, %q, %q), want (%q, b.go, %q)",
			body.ID, body.FilePath, body.RepoID, uidAnchorCanonicalID, uidAnchorRepoID)
	}
	if want := "MATCH (e:Function) WHERE e.uid = $entity_id AND e.id = $entity_id"; len(anchors) != 1 || anchors[0] != want {
		// Errorf, not Fatalf: the behavioral checks below must still run.
		t.Errorf("canonical entity anchor reads = %q, want exactly [%q]", anchors, want)
	}

	// (2) The File matches the uid equality alone but carries no id. The old
	// `e.id = $entity_id` read never returned it, so neither may the uid
	// anchor: every read misses and the request is a 404.
	rec = get(uidAnchorFileUID)
	anchors = reader.reset()
	if rec.Code != http.StatusNotFound {
		t.Fatalf("uid-only File: status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	if len(anchors) != allReads {
		t.Fatalf("uid-only File: anchor reads = %d, want %d (every label, then the fallback)", len(anchors), allReads)
	}

	// (3) An id-only Function misses the uid-anchored fast path and resolves
	// through the unlabeled fallback, as it did before #7089.
	rec = get(uidAnchorIDOnlyID)
	anchors = reader.reset()
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), uidAnchorIDOnlyID) {
		t.Fatalf("id-only Function: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(anchors) != allReads || anchors[allReads-1] != entityContextUnlabeledAnchor {
		t.Fatalf("id-only Function: anchor reads = %q, want %d ending with %q", anchors, allReads, entityContextUnlabeledAnchor)
	}

	if backend == graph.SchemaBackendNeo4j {
		assertUIDAnchorPlan(ctx, t, driver, database)
	}
}

// assertUIDAnchorPlan EXPLAINs the Function anchor on Neo4j: the shipped uid
// form must seek function_uid_unique, and the pre-#7089 id form must still
// plan as a label scan, so the check can tell the two apart.
func assertUIDAnchorPlan(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database string) {
	t.Helper()
	params := map[string]any{"entity_id": uidAnchorCanonicalID}
	access := querycontract.RepositoryAccessFilter{AllScopes: true}
	shipped := entityContextCypher(entityContextAnchors()[0], access)
	legacy := entityContextCypher("(e:Function) WHERE e.id = $entity_id", access)

	if ops := explainOperators(ctx, t, driver, database, shipped, params); !strings.Contains(ops, "NodeUniqueIndexSeek") || !strings.Contains(ops, "e:Function(uid)") || strings.Contains(ops, "NodeByLabelScan") {
		t.Fatalf("shipped Function anchor plan = %s, want a NodeUniqueIndexSeek on e:Function(uid) and no NodeByLabelScan", ops)
	}
	if ops := explainOperators(ctx, t, driver, database, legacy, params); !strings.Contains(ops, "NodeByLabelScan") {
		t.Fatalf("pre-#7089 Function anchor plan = %s, want a NodeByLabelScan (the check cannot tell the forms apart)", ops)
	}
}

// explainOperators returns the EXPLAIN plan of cypher as Neo4j renders it (the
// root plan's string-representation argument, the text cypher-shell prints),
// plus every operator name reachable through Children. The rendered text is
// the authoritative part: on the pinned Neo4j and driver, walking Children()
// under an Apply returned the right-hand branch twice and never reached the
// left-hand anchor leaf.
func explainOperators(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database, cypher string, params map[string]any) string {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, "EXPLAIN "+cypher, params)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		t.Fatalf("explain consume: %v", err)
	}
	var ops []string
	var walk func(neo4jdriver.Plan)
	walk = func(plan neo4jdriver.Plan) {
		if plan == nil {
			return
		}
		ops = append(ops, plan.Operator())
		for _, child := range plan.Children() {
			walk(child)
		}
	}
	root := summary.Plan()
	walk(root)
	if len(ops) == 0 {
		t.Fatalf("explain returned no plan for:\n%s", cypher)
	}
	rendered, _ := root.Arguments()["string-representation"].(string)
	if rendered == "" {
		t.Fatalf("explain plan carries no string-representation for:\n%s", cypher)
	}
	return strings.Join(ops, ",") + "\n" + rendered
}
