// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_relationship_story

// Live proof for #6548: the NornicDB inheritance walk must not report a depth
// measured through a class the caller cannot read.
//
// Seed: grantedA -INHERITS-> ungrantedC -INHERITS-> grantedB, beside a wholly
// granted grantedA -INHERITS-> grantedD control. Both endpoints of the A->B
// chain are in grant, so the statement's endpoint predicates admit it; only the
// interior is out of grant, and only the Go-side filter can drop it.
//
//	docker run -d --name nornic-5167-m1b -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17995:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
//	cd go && ESHU_NEO4J_URI=bolt://localhost:17995 go test ./internal/query \
//	  -tags live_nornicdb_relationship_story -run TestLiveNornicDBInheritance -count=1 -v
package query

import (
	"context"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	liveInteriorAnchorUID   = "class:live-interior-granted-a"
	liveInteriorBridgeUID   = "class:live-interior-ungranted-c"
	liveInteriorFarUID      = "class:live-interior-granted-b"
	liveInteriorControlUID  = "class:live-interior-granted-d"
	liveInteriorFarName     = "LiveInteriorGrantedB"
	liveInteriorControlName = "LiveInteriorGrantedD"
	liveInteriorBridgeName  = "LiveInteriorUngrantedC"
)

func seedLiveInheritanceInterior(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic", AccessMode: neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()
	for _, stmt := range []string{
		liveClauseClassMerge(liveInteriorAnchorUID, "LiveInteriorGrantedA", codeGrantGrantedRepo),
		liveClauseClassMerge(liveInteriorBridgeUID, liveInteriorBridgeName, codeGrantOtherRepo),
		liveClauseClassMerge(liveInteriorFarUID, liveInteriorFarName, codeGrantGrantedRepo),
		liveClauseClassMerge(liveInteriorControlUID, liveInteriorControlName, codeGrantGrantedRepo),
		`MATCH (a {uid:"` + liveInteriorAnchorUID + `"}), (c {uid:"` + liveInteriorBridgeUID + `"}) MERGE (a)-[:INHERITS]->(c)`,
		`MATCH (c {uid:"` + liveInteriorBridgeUID + `"}), (b {uid:"` + liveInteriorFarUID + `"}) MERGE (c)-[:INHERITS]->(b)`,
		`MATCH (a {uid:"` + liveInteriorAnchorUID + `"}), (d {uid:"` + liveInteriorControlUID + `"}) MERGE (a)-[:INHERITS]->(d)`,
	} {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
}

// TestLiveNornicDBInheritanceWalkDropsAnOutOfGrantInteriorClass carries its own
// negative control. The raw statement is run first and MUST still return the
// A->B row: that is what the endpoint-only bound leaves behind, and it is what
// this fix exists to remove. The shipped read must then not return it, while
// keeping the wholly granted A->D control.
func TestLiveNornicDBInheritanceWalkDropsAnOutOfGrantInteriorClass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveClauseDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveInheritanceInterior(ctx, t, driver)
	reader := NewNeo4jReader(driver, "nornic")

	access := liveClauseGrantedAccess()
	req := storyProbeRequest()
	cypher, params := nornicDBRelationshipStoryInheritanceDepthCypher(
		req, liveInteriorAnchorUID, "outgoing", "uid", access)

	rawRows, err := reader.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("run shipped inheritance statement: %v", err)
	}
	rawNames := liveClauseRowNames(rawRows, "target_name")
	t.Logf("raw statement returned %d rows: %v", len(rawRows), rawNames)
	if !liveClauseContainsName(rawNames, liveInteriorFarName) {
		t.Fatalf("negative control failed: the endpoint-bound statement no longer returns the interior-crossing chain %q, so this test proves nothing: %v",
			liveInteriorFarName, rawNames)
	}
	if !liveClauseContainsName(rawNames, liveInteriorControlName) {
		t.Fatalf("the wholly granted control chain %q is missing from the raw rows: %v",
			liveInteriorControlName, rawNames)
	}

	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j:        reader,
	}
	scoped := ContextWithAuthContext(ctx, codeGrantScopedAuthContext([]string{codeGrantGrantedRepo}))
	rows, err := handler.nornicDBRelationshipStoryInheritanceDepthRows(scoped, req, liveInteriorAnchorUID, "outgoing")
	if err != nil {
		t.Fatalf("shipped inheritance read: %v", err)
	}
	names := liveClauseRowNames(rows, "target_name")
	t.Logf("shipped read returned %d rows: %v", len(rows), names)
	if liveClauseContainsName(names, liveInteriorFarName) {
		t.Fatalf("the walk still reports a depth measured through the out-of-grant class %q: %v",
			liveInteriorBridgeName, names)
	}
	if !liveClauseContainsName(names, liveInteriorControlName) {
		t.Fatalf("the wholly granted chain %q was dropped, so the filter over-filters: %v",
			liveInteriorControlName, names)
	}
	for _, row := range rows {
		if _, leaked := row["path_nodes"]; leaked {
			t.Fatalf("path_nodes reached the caller: %#v", row)
		}
	}
}
