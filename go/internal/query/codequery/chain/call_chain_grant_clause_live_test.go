// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_call_chain

// Live clause-attachment proof for POST /api/v0/code/call-chain
// (#5167 batch 2b, question 2).
//
// The shipped one-hop read writes its repository predicate --
// "coalesce(target.repo_id, targetRepo.id, ”) IN $traversal_repo_ids" -- after
// two OPTIONAL MATCH clauses. If a predicate in that position does not decide
// row membership, then the existing cross_repo traversal bound is already open
// on origin/main, and pushing a caller's grant into the same clause would be
// grant text that grants nothing.
//
// The ...MustNotLeak... test asserts the behaviour the traversal needs through
// the route: with the traversal restricted to the one granted repository, the
// response must carry the in-repository callee and neither out-of-bound
// target. The ...FixShape... test runs the candidate rewrite as a raw
// statement on the same seeded graph.
//
//	docker run -d --name nornic-5167-e1 -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17987:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
//	cd go && go test ./internal/query/codequery/chain -tags live_nornicdb_call_chain \
//	  -run TestLiveNornicDBCallChain -count=1 -v
package chain_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Entity names carried by the fixture. Every "Ungranted" name belongs to the
// repository the probe caller is NOT granted, so its appearance in a result set
// is the leak. Copy of grant_clause_attachment_live_seed_test.go, which stays
// in codequery: the live proofs seed the same graph, and a _test.go symbol
// cannot be imported here.
const (
	liveClauseAnchorFunction     = "LiveClauseAnchorFn"
	liveClauseGrantedCallee      = "LiveClauseGrantedCallee"
	liveClauseUngrantedCallee    = "LiveClauseUngrantedCallee"
	liveClauseOrphanCallee       = "LiveClauseOrphanCallee"
	liveClauseGrantedCaller      = "LiveClauseGrantedCaller"
	liveClauseUngrantedCaller    = "LiveClauseUngrantedCaller"
	liveClauseAnchorClass        = "LiveClauseAnchorClass"
	liveClauseUngrantedParent    = "LiveClauseUngrantedParentClass"
	liveClauseUngrantedOwner     = "LiveClauseUngrantedOwnerClass"
	liveClauseGrantedMethod      = "LiveClauseGrantedMethod"
	liveClauseUngrantedMethod    = "LiveClauseUngrantedMethod"
	liveClauseAnchorUID          = "fn:live-clause-anchor"
	liveClauseAnchorClassUID     = "class:live-clause-anchor"
	liveClauseUngrantedOwnerUID  = "class:live-clause-other-owner"
	liveClauseUngrantedCalleeUID = "fn:live-clause-other-callee"

	// The three-node chain used by the call-chain path-wide bound probe: two
	// in-repository endpoints joined only through a bridge in the other
	// repository, so a path bound that works excludes the whole chain.
	liveClauseChainStart    = "LiveClauseChainStart"
	liveClauseChainBridge   = "LiveClauseChainBridge"
	liveClauseChainEnd      = "LiveClauseChainEnd"
	liveClauseChainStartUID = "fn:live-clause-chain-start"
	liveClauseChainEndUID   = "fn:live-clause-chain-end"

	liveClauseCleanStart    = "LiveClauseCleanStart"
	liveClauseCleanMid      = "LiveClauseCleanMid"
	liveClauseCleanEnd      = "LiveClauseCleanEnd"
	liveClauseCleanStartUID = "fn:live-clause-clean-start"
	liveClauseCleanEndUID   = "fn:live-clause-clean-end"
)

// liveChainNornicDBReader is the test-only live-backend GraphQuery for this
// leaf's NornicDB compatibility proofs. Copy of codequery's liveNornicDBReader,
// which stays in codequery: it opens a read session per call and decodes rows
// with the same Keys/Values mapping.
type liveChainNornicDBReader struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func newLiveChainNornicDBReader(driver neo4jdriver.DriverWithContext, database string) *liveChainNornicDBReader {
	return &liveChainNornicDBReader{driver: driver, database: database}
}

func (r *liveChainNornicDBReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("live nornicdb query: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("live nornicdb collect: %w", err)
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for index, key := range record.Keys {
			row[key] = record.Values[index]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *liveChainNornicDBReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// openLiveClauseDriver dials the standalone proof container. ESHU_NEO4J_URI
// overrides the default so the proof never has to bind a default Bolt port.
// Copy of grant_clause_attachment_live_seed_test.go.
func openLiveClauseDriver(ctx context.Context, t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:17987"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	return driver
}

// seedLiveClauseGraph writes the fixture. MERGE keeps repeated runs against a
// retained store idempotent. Copy of grant_clause_attachment_live_seed_test.go.
func seedLiveClauseGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	granted := codeGrantGrantedRepo
	other := codeGrantOtherRepo

	statements := []string{
		`MERGE (r:Repository {id:"` + granted + `"}) SET r.name="granted-service"`,
		`MERGE (r:Repository {id:"` + other + `"}) SET r.name="other-service"`,

		`MERGE (f:File {path:"/granted/anchor.go"}) SET f.uid="file:granted-anchor", f.relative_path="internal/anchor.go", f.language="go", f.lang="go", f.repo_id="` + granted + `"`,
		`MERGE (f:File {path:"/granted/neighbor.go"}) SET f.uid="file:granted-neighbor", f.relative_path="internal/neighbor.go", f.language="go", f.lang="go", f.repo_id="` + granted + `"`,
		`MERGE (f:File {path:"/other/neighbor.go"}) SET f.uid="file:other-neighbor", f.relative_path="internal/neighbor.go", f.language="go", f.lang="go", f.repo_id="` + other + `"`,

		liveClauseFunctionMerge(liveClauseAnchorUID, liveClauseAnchorFunction, granted),
		liveClauseFunctionMerge("fn:live-clause-granted-callee", liveClauseGrantedCallee, granted),
		liveClauseFunctionMerge(liveClauseUngrantedCalleeUID, liveClauseUngrantedCallee, other),
		liveClauseFunctionMerge("fn:live-clause-granted-caller", liveClauseGrantedCaller, granted),
		liveClauseFunctionMerge("fn:live-clause-other-caller", liveClauseUngrantedCaller, other),
		liveClauseFunctionMerge("fn:live-clause-granted-method", liveClauseGrantedMethod, granted),
		liveClauseFunctionMerge("fn:live-clause-other-method", liveClauseUngrantedMethod, other),
		// No repo_id, and below it gets no File containment either.
		`MERGE (n:Function {uid:"fn:live-clause-orphan"}) SET n.id="fn:live-clause-orphan", n.name="` + liveClauseOrphanCallee + `", n.language="go", n.lang="go", n.start_line=1, n.end_line=4`,

		liveClauseClassMerge(liveClauseAnchorClassUID, liveClauseAnchorClass, granted),
		liveClauseClassMerge("class:live-clause-other-parent", liveClauseUngrantedParent, other),
		liveClauseClassMerge(liveClauseUngrantedOwnerUID, liveClauseUngrantedOwner, other),

		liveClauseRepoContains(granted, "/granted/anchor.go"),
		liveClauseRepoContains(granted, "/granted/neighbor.go"),
		liveClauseRepoContains(other, "/other/neighbor.go"),

		liveClauseContains("/granted/anchor.go", liveClauseAnchorUID),
		liveClauseContains("/granted/neighbor.go", "fn:live-clause-granted-callee"),
		liveClauseContains("/other/neighbor.go", liveClauseUngrantedCalleeUID),
		liveClauseContains("/granted/neighbor.go", "fn:live-clause-granted-caller"),
		liveClauseContains("/other/neighbor.go", "fn:live-clause-other-caller"),
		liveClauseContains("/granted/neighbor.go", "fn:live-clause-granted-method"),
		liveClauseContains("/other/neighbor.go", "fn:live-clause-other-method"),
		liveClauseContains("/granted/anchor.go", liveClauseAnchorClassUID),
		liveClauseContains("/other/neighbor.go", "class:live-clause-other-parent"),
		liveClauseContains("/other/neighbor.go", liveClauseUngrantedOwnerUID),

		liveClauseCalls(liveClauseAnchorUID, "fn:live-clause-granted-callee"),
		liveClauseCalls(liveClauseAnchorUID, liveClauseUngrantedCalleeUID),
		liveClauseCalls(liveClauseAnchorUID, "fn:live-clause-orphan"),
		liveClauseCalls("fn:live-clause-granted-caller", liveClauseAnchorUID),
		liveClauseCalls("fn:live-clause-other-caller", liveClauseAnchorUID),

		liveClauseFunctionMerge(liveClauseChainStartUID, liveClauseChainStart, granted),
		liveClauseFunctionMerge("fn:live-clause-chain-bridge", liveClauseChainBridge, other),
		liveClauseFunctionMerge(liveClauseChainEndUID, liveClauseChainEnd, granted),
		liveClauseContains("/granted/neighbor.go", liveClauseChainStartUID),
		liveClauseContains("/other/neighbor.go", "fn:live-clause-chain-bridge"),
		liveClauseContains("/granted/neighbor.go", liveClauseChainEndUID),
		liveClauseCalls(liveClauseChainStartUID, "fn:live-clause-chain-bridge"),
		liveClauseCalls("fn:live-clause-chain-bridge", liveClauseChainEndUID),

		// A second chain whose every node is granted. It exists so a path
		// predicate can be measured against a chain it MUST admit: a predicate
		// that returns nothing passes an out-of-grant fixture by accident,
		// which is how the single scalar equality was graded "right" before
		// #6548.
		liveClauseFunctionMerge(liveClauseCleanStartUID, liveClauseCleanStart, granted),
		liveClauseFunctionMerge("fn:live-clause-clean-mid", liveClauseCleanMid, granted),
		liveClauseFunctionMerge(liveClauseCleanEndUID, liveClauseCleanEnd, granted),
		liveClauseContains("/granted/neighbor.go", liveClauseCleanStartUID),
		liveClauseContains("/granted/neighbor.go", "fn:live-clause-clean-mid"),
		liveClauseContains("/granted/neighbor.go", liveClauseCleanEndUID),
		liveClauseCalls(liveClauseCleanStartUID, "fn:live-clause-clean-mid"),
		liveClauseCalls("fn:live-clause-clean-mid", liveClauseCleanEndUID),

		`MATCH (a {uid:"` + liveClauseAnchorClassUID + `"}), (b {uid:"class:live-clause-other-parent"}) MERGE (a)-[:INHERITS]->(b)`,
		`MATCH (c {uid:"` + liveClauseAnchorClassUID + `"}), (m {uid:"fn:live-clause-granted-method"}) MERGE (c)-[:CONTAINS]->(m)`,
		`MATCH (c {uid:"` + liveClauseUngrantedOwnerUID + `"}), (m {uid:"fn:live-clause-other-method"}) MERGE (c)-[:CONTAINS]->(m)`,
	}
	for _, stmt := range statements {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
}

func liveClauseFunctionMerge(uid, name, repoID string) string {
	return `MERGE (n:Function {uid:"` + uid + `"}) SET n.id="` + uid + `", n.name="` + name +
		`", n.repo_id="` + repoID + `", n.language="go", n.lang="go", n.start_line=10, n.end_line=40`
}

func liveClauseClassMerge(uid, name, repoID string) string {
	return `MERGE (n:Class {uid:"` + uid + `"}) SET n.id="` + uid + `", n.name="` + name +
		`", n.repo_id="` + repoID + `", n.language="go", n.lang="go", n.start_line=1, n.end_line=80`
}

func liveClauseRepoContains(repoID, filePath string) string {
	return `MATCH (r:Repository {id:"` + repoID + `"}), (f:File {path:"` + filePath + `"}) ` +
		`MERGE (r)-[:REPO_CONTAINS]->(f)`
}

func liveClauseContains(filePath, entityUID string) string {
	return `MATCH (f:File {path:"` + filePath + `"}), (n {uid:"` + entityUID + `"}) MERGE (f)-[:CONTAINS]->(n)`
}

func liveClauseCalls(sourceUID, targetUID string) string {
	return `MATCH (s {uid:"` + sourceUID + `"}), (t {uid:"` + targetUID + `"}) ` +
		`MERGE (s)-[rel:CALLS]->(t) SET rel.evidence_source="projector/canonical", rel.confidence=0.9`
}

// liveClauseRowNames collects a projected name column so a leak can be named in
// the failure message rather than only counted. Copy of
// grant_clause_attachment_live_seed_test.go.
func liveClauseRowNames(rows []map[string]any, keys ...string) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		for _, key := range keys {
			if value := strings.TrimSpace(querycontract.StringVal(row, key)); value != "" {
				names = append(names, value)
			}
		}
	}
	return names
}

func liveClauseContainsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func newLiveCallChainHandler(ctx context.Context, t *testing.T) (*codequery.CodeHandler, func()) {
	t.Helper()
	driver := openLiveClauseDriver(ctx, t)
	seedLiveClauseGraph(ctx, t, driver)
	handler := &codequery.CodeHandler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		GraphBackend: querycontract.GraphBackendNornicDB,
		Neo4j:        newLiveChainNornicDBReader(driver, "nornic"),
	}
	return handler, func() { _ = driver.Close(context.Background()) }
}

// TestLiveNornicDBCallChainOneHopMustNotLeakUngrantedTargets runs the shipped
// one-hop read through the route with the traversal restricted to the one
// granted repository. It used to call the handler method directly; the method
// is unexported and unreachable from this leaf's external test package, so the
// proof now asserts the same behaviour at the response level: the
// in-repository callee is present and neither out-of-bound target leaks.
func TestLiveNornicDBCallChainOneHopMustNotLeakUngrantedTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newChainRouteRequest(t, map[string]any{
		"start_entity_id": liveClauseAnchorUID,
		"end_entity_id":   "fn:live-clause-granted-callee",
		"repo_id":         codeGrantGrantedRepo,
		"max_depth":       5,
	}, nil))
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, liveClauseGrantedCallee) {
		t.Fatalf("the in-repository callee %q is missing; the probe seeded the wrong graph: %s",
			liveClauseGrantedCallee, body)
	}
	for _, leaked := range []string{liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if strings.Contains(body, leaked) {
			t.Fatalf("the bounded one-hop traversal returned the out-of-bound target %q: %s", leaked, body)
		}
	}
}

// TestLiveNornicDBCallChainOneHopFixShapeExcludesUngrantedTargets measures the
// candidate rewrite: the repository condition moves onto the anchoring MATCH's
// own WHERE and binds the target node's own repo_id, the property the canonical
// node writer already persists on every entity. The OPTIONAL MATCH file and
// repository hops stay optional, so the projection keeps its fallback columns.
//
// A target the graph cannot attribute to a repository is dropped, which is the
// fail-closed half batch 1 landed for complexityListAnchor.
func TestLiveNornicDBCallChainOneHopFixShapeExcludesUngrantedTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	rows, err := handler.Neo4j.Run(ctx, `
		MATCH (source:Function {uid: $source_id})-[:CALLS]->(target)
		WHERE coalesce(target.repo_id, '') IN $traversal_repo_ids
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN coalesce(target.id, target.uid) as id,
		       target.name as name,
		       labels(target) as labels,
		       coalesce(target.repo_id, targetRepo.id) as repo_id,
		       coalesce(target.language, target.lang) as language
	`, map[string]any{
		"source_id":          liveClauseAnchorUID,
		"traversal_repo_ids": []string{codeGrantGrantedRepo},
	})
	if err != nil {
		t.Fatalf("run candidate one-hop statement: %v", err)
	}
	names := liveClauseRowNames(rows, "name")
	t.Logf("candidate one-hop statement returned %d rows: %v", len(rows), names)
	for _, row := range rows {
		t.Logf("  row id=%v repo_id=%v", row["id"], row["repo_id"])
	}
	if !liveClauseContainsName(names, liveClauseGrantedCallee) {
		t.Fatalf("candidate statement dropped the in-repository callee %q: %v", liveClauseGrantedCallee, names)
	}
	for _, leaked := range []string{liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if liveClauseContainsName(names, leaked) {
			t.Fatalf("candidate statement returned the out-of-bound target %q: %v", leaked, names)
		}
	}
}

// TestLiveNornicDBCallChainOneHopUnscopedIsUnchanged pins the other direction:
// with no traversal bound the statement must still return every callee,
// including the orphan the fail-closed rewrite drops for a bounded caller. It
// used to call the handler method directly; the method is unexported here, so
// the proof runs the unbounded one-hop statement -- the candidate shape above
// without its WHERE -- as a raw read instead.
func TestLiveNornicDBCallChainOneHopUnscopedIsUnchanged(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	rows, err := handler.Neo4j.Run(ctx, `
		MATCH (source:Function {uid: $source_id})-[:CALLS]->(target)
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		RETURN coalesce(target.id, target.uid) as id,
		       target.name as name,
		       labels(target) as labels,
		       coalesce(target.repo_id, targetRepo.id) as repo_id,
		       coalesce(target.language, target.lang) as language
	`, map[string]any{
		"source_id": liveClauseAnchorUID,
	})
	if err != nil {
		t.Fatalf("run unbounded one-hop statement: %v", err)
	}
	names := liveClauseRowNames(rows, "name")
	t.Logf("unbounded one-hop statement returned %d rows: %v", len(rows), names)
	for _, want := range []string{liveClauseGrantedCallee, liveClauseUngrantedCallee, liveClauseOrphanCallee} {
		if !liveClauseContainsName(names, want) {
			t.Fatalf("the unbounded traversal lost %q: %v", want, names)
		}
	}
}

// TestLiveNornicDBRelationshipMetadataRowAnchors confirms the shared seed
// lookup routes 2, 4 and 5 all use. Its predicate sits on a plain anchoring
// MATCH pair, and both required MATCH clauses mean an entity the graph cannot
// attribute to a repository never resolves. The metadata builder moved to the
// relationships leaf, which this external test package imports directly.
func TestLiveNornicDBRelationshipMetadataRowAnchors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	handler, closeDriver := newLiveCallChainHandler(ctx, t)
	defer closeDriver()

	for _, tc := range []struct {
		name      string
		entity    string
		repoID    string
		wantNames []string
		wantCount int
	}{
		{
			name:      "in_repository_entity_resolves",
			entity:    liveClauseUngrantedCalleeUID,
			repoID:    codeGrantOtherRepo,
			wantNames: []string{liveClauseUngrantedCallee},
			wantCount: 1,
		},
		{
			name:      "wrong_repository_resolves_nothing",
			entity:    liveClauseUngrantedCalleeUID,
			repoID:    codeGrantGrantedRepo,
			wantCount: 0,
		},
		{
			name:      "entity_with_no_repository_path_resolves_nothing",
			entity:    "fn:live-clause-orphan",
			repoID:    "",
			wantCount: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			predicate, params := relationships.MetadataPredicate("", tc.repoID, querycontract.RepositoryAccessFilter{AllScopes: true})
			params["entity_id"] = tc.entity
			rows, err := handler.Neo4j.Run(
				ctx,
				relationships.MetadataCypher(predicate, "Function", "uid"),
				params,
			)
			if err != nil {
				t.Fatalf("run metadata statement: %v", err)
			}
			names := liveClauseRowNames(rows, "name")
			t.Logf("%s returned %d rows: %v", tc.name, len(rows), names)
			if len(rows) != tc.wantCount {
				t.Fatalf("row count = %d, want %d: %v", len(rows), tc.wantCount, names)
			}
			for _, want := range tc.wantNames {
				if !liveClauseContainsName(names, want) {
					t.Fatalf("metadata row lost %q: %v", want, names)
				}
			}
		})
	}
}
