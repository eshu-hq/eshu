// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_relationship_story || live_nornicdb_call_chain

// Shared two-tenant fixture for the #5167 batch-2b clause-attachment proofs.
//
// Both proofs ask the same question of a different builder: does a repository
// predicate written after one or more OPTIONAL MATCH clauses decide row
// membership, or does it only constrain the optional pattern and leave the
// out-of-grant row in the answer with its repository columns nulled? A
// text-capture test cannot tell those apart, because the predicate string is
// present either way. Only a real backend settles it.
//
// The graph mirrors what the canonical node writer actually persists
// (go/internal/storage/cypher/canonical_node_cypher.go): a Repository keyed on
// id, a File keyed on path carrying uid/relative_path/repo_id and reached by
// (:Repository)-[:REPO_CONTAINS]->(:File), and entities MERGEd on uid carrying
// their own id/name/repo_id/language and reached by (:File)-[:CONTAINS]->(n).
//
// Run against the pinned replay-tier proof image on a non-default port:
//
//	docker run -d --name nornic-5167-e1 -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17987:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
package query

import (
	"context"
	"os"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Entity names carried by the fixture. Every "Ungranted" name belongs to the
// repository the probe caller is NOT granted, so its appearance in a result set
// is the leak.
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
)

// openLiveClauseDriver dials the standalone proof container. ESHU_NEO4J_URI
// overrides the default so the proof never has to bind a default Bolt port.
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
// retained store idempotent.
//
// The orphan callee deliberately carries no repo_id and no File containment: it
// is the row an OPTIONAL MATCH-attached predicate keeps, and the row a
// fail-closed rewrite must drop.
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

// liveClauseGrantedAccess is the scoped filter every probe uses: tenant-a,
// granted exactly the one repository.
func liveClauseGrantedAccess() repositoryAccessFilter {
	return repositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
}

// liveClauseRowNames collects a projected name column so a leak can be named in
// the failure message rather than only counted.
func liveClauseRowNames(rows []map[string]any, keys ...string) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		for _, key := range keys {
			if value := strings.TrimSpace(StringVal(row, key)); value != "" {
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
