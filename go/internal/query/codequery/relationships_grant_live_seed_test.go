// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_code_relationships_grant

// Extra two-tenant fixture for the #5167 POST /api/v0/code/relationships grant
// proof (relationships_grant_live_test.go). The shared clause fixture
// (grant_clause_attachment_live_seed_test.go) already carries a CALLS anchor
// with a granted, an ungranted and a repo-less neighbour in each direction,
// plus the bridged and clean call chains the transitive walk needs. This file
// adds what that fixture lacks:
//
//   - one anchor with a granted and an ungranted neighbour, in both directions,
//     for each of the five other relationship types the route filters on;
//   - a hub whose ungranted neighbours sort ahead of its granted ones and
//     outnumber the read's row ceiling, so a grant applied after LIMIT returns
//     a page of ungranted rows and none of the granted ones.
//
// Rows go in through UNWIND $rows with literal values only: on the pinned
// NornicDB an expression inside a property map can be stored as literal text.
package codequery

import (
	"context"
	"fmt"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	relLiveTypesAnchorUID  = "fn:rel-live-types-anchor"
	relLiveHubUID          = "fn:rel-live-hub"
	relLiveHubUngranted    = 1200
	relLiveHubGranted      = 40
	relLiveHubGrantedName  = "RelLiveHubGranted"
	relLiveHubOtherName    = "RelLiveHubOther"
	relLiveTypeGrantedName = "RelLiveTypeGranted"
	relLiveTypeOtherName   = "RelLiveTypeOther"
	// relLiveSharedName is held by one Function in each tenant, so a name
	// lookup without repo_id is ambiguous corpus-wide and unique in the grant.
	relLiveSharedName       = "RelLiveSharedName"
	relLiveSharedGrantedUID = "fn:rel-live-shared-granted"
)

// relLiveOtherTypes are the relationship types besides CALLS that
// NornicDBRelationshipPattern accepts.
var relLiveOtherTypes = []string{"REFERENCES", "IMPORTS", "INHERITS", "OVERRIDES", "USES_METACLASS"}

func relLiveTypeNodeName(base, relType, direction string) string {
	return base + "_" + relType + "_" + direction
}

// seedRelLiveGraph writes the shared clause fixture and then this file's
// additions into database.
func seedRelLiveGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, database string) {
	t.Helper()
	seedLiveClauseGraph(ctx, t, driver, database)

	type node struct{ uid, name, repo, path string }
	nodes := []node{
		{relLiveTypesAnchorUID, "RelLiveTypesAnchor", codeGrantGrantedRepo, "/granted/anchor.go"},
		{relLiveHubUID, "RelLiveHub", codeGrantGrantedRepo, "/granted/anchor.go"},
		{relLiveSharedGrantedUID, relLiveSharedName, codeGrantGrantedRepo, "/granted/neighbor.go"},
		{"fn:rel-live-shared-other", relLiveSharedName, codeGrantOtherRepo, "/other/neighbor.go"},
	}
	type edge struct{ from, to, relType string }
	var edges []edge
	for _, relType := range relLiveOtherTypes {
		for _, direction := range []string{"out", "in"} {
			grantedUID := fmt.Sprintf("fn:rel-live-type-%s-%s-granted", relType, direction)
			otherUID := fmt.Sprintf("fn:rel-live-type-%s-%s-other", relType, direction)
			nodes = append(nodes,
				node{grantedUID, relLiveTypeNodeName(relLiveTypeGrantedName, relType, direction), codeGrantGrantedRepo, "/granted/neighbor.go"},
				node{otherUID, relLiveTypeNodeName(relLiveTypeOtherName, relType, direction), codeGrantOtherRepo, "/other/neighbor.go"},
			)
			if direction == "out" {
				edges = append(edges, edge{relLiveTypesAnchorUID, grantedUID, relType}, edge{relLiveTypesAnchorUID, otherUID, relType})
			} else {
				edges = append(edges, edge{grantedUID, relLiveTypesAnchorUID, relType}, edge{otherUID, relLiveTypesAnchorUID, relType})
			}
		}
	}
	// Ungranted hub neighbours sort first by uid ("a" < "z"), so ORDER BY
	// target.uid LIMIT $row_limit fills the page with them.
	for i := 0; i < relLiveHubUngranted; i++ {
		uid := fmt.Sprintf("fn:rel-live-hub-a-other-%04d", i)
		nodes = append(nodes, node{uid, fmt.Sprintf("%s%04d", relLiveHubOtherName, i), codeGrantOtherRepo, "/other/neighbor.go"})
		edges = append(edges, edge{relLiveHubUID, uid, "CALLS"})
	}
	for i := 0; i < relLiveHubGranted; i++ {
		uid := fmt.Sprintf("fn:rel-live-hub-z-granted-%02d", i)
		nodes = append(nodes, node{uid, fmt.Sprintf("%s%02d", relLiveHubGrantedName, i), codeGrantGrantedRepo, "/granted/neighbor.go"})
		edges = append(edges, edge{relLiveHubUID, uid, "CALLS"})
	}

	nodeRows := make([]any, 0, len(nodes))
	for _, n := range nodes {
		nodeRows = append(nodeRows, map[string]any{"uid": n.uid, "name": n.name, "repo": n.repo, "path": n.path})
	}
	edgeRowsByType := map[string][]any{}
	for _, e := range edges {
		edgeRowsByType[e.relType] = append(edgeRowsByType[e.relType], map[string]any{"from": e.from, "to": e.to})
	}

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: database, AccessMode: neo4jdriver.AccessModeWrite})
	defer func() { _ = session.Close(ctx) }()
	run := func(stmt string, params map[string]any) {
		t.Helper()
		if _, err := session.Run(ctx, stmt, params); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
	run(`UNWIND $rows AS row
		MERGE (n:Function {uid: row.uid})
		SET n.id = row.uid, n.name = row.name, n.repo_id = row.repo, n.language = "go", n.lang = "go", n.start_line = 10, n.end_line = 40`,
		map[string]any{"rows": nodeRows})
	run(`UNWIND $rows AS row
		MATCH (f:File {path: row.path})
		MATCH (n:Function {uid: row.uid})
		MERGE (f)-[:CONTAINS]->(n)`,
		map[string]any{"rows": nodeRows})
	for relType, rows := range edgeRowsByType {
		run(`UNWIND $rows AS row
			MATCH (s:Function {uid: row.from})
			MATCH (t:Function {uid: row.to})
			MERGE (s)-[:`+relType+`]->(t)`,
			map[string]any{"rows": rows})
	}
}
