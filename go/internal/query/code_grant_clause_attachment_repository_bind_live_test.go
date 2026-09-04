// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_relationship_story || live_nornicdb_call_chain

// Mechanism ladder for the #5167 batch-2b repository binding.
//
// The first run of the story probe turned up a second result nobody was
// looking for: every row came back with sourceRepo/targetRepo null, including
// the row whose repository IS in grant, and promoting those OPTIONAL MATCH
// clauses to required MATCHes emptied the read. So "the grant predicate does
// not filter" and "the Repository variable never binds" are two different
// findings, and a fix built on the second one would be built on sand.
//
// This ladder isolates which clause position still binds a Repository on the
// pinned build, so the proposed fix shape rests on a measured binding rather
// than on the Cypher the statement appears to say.
package query

import (
	"context"
	"testing"
	"time"
)

func TestLiveNornicDBRepositoryBindingLadder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveClauseDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveClauseGraph(ctx, t, driver)
	reader := NewNeo4jReader(driver, "nornic")

	for _, tc := range []struct {
		name   string
		cypher string
	}{
		{
			name:   "single_clause_repo_first",
			cypher: `MATCH (r:Repository)-[:REPO_CONTAINS]->(f:File {path:"/granted/neighbor.go"}) RETURN r.id AS repo_id`,
		},
		{
			name:   "single_clause_file_first",
			cypher: `MATCH (f:File {path:"/granted/neighbor.go"})<-[:REPO_CONTAINS]-(r:Repository) RETURN r.id AS repo_id`,
		},
		{
			name:   "two_required_matches_repo_pattern_first",
			cypher: `MATCH (f:File {path:"/granted/neighbor.go"}) MATCH (r:Repository)-[:REPO_CONTAINS]->(f) RETURN r.id AS repo_id`,
		},
		{
			name:   "two_required_matches_file_pattern_first",
			cypher: `MATCH (f:File {path:"/granted/neighbor.go"}) MATCH (f)<-[:REPO_CONTAINS]-(r:Repository) RETURN r.id AS repo_id`,
		},
		{
			name:   "optional_repo_pattern_first",
			cypher: `MATCH (f:File {path:"/granted/neighbor.go"}) OPTIONAL MATCH (r:Repository)-[:REPO_CONTAINS]->(f) RETURN r.id AS repo_id`,
		},
		{
			name:   "optional_file_pattern_first",
			cypher: `MATCH (f:File {path:"/granted/neighbor.go"}) OPTIONAL MATCH (f)<-[:REPO_CONTAINS]-(r:Repository) RETURN r.id AS repo_id`,
		},
		{
			name: "anchor_then_two_optional_hops",
			cypher: `MATCH (anchor:Function {uid:"` + liveClauseAnchorUID + `"})-[:CALLS]->(target)
				OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
				OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
				RETURN target.name AS name, targetFile.relative_path AS file_path, targetRepo.id AS repo_id`,
		},
		{
			name: "anchor_then_one_chained_optional_hop",
			cypher: `MATCH (anchor:Function {uid:"` + liveClauseAnchorUID + `"})-[:CALLS]->(target)
				OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)<-[:REPO_CONTAINS]-(targetRepo:Repository)
				RETURN target.name AS name, targetFile.relative_path AS file_path, targetRepo.id AS repo_id`,
		},
		{
			name: "anchor_then_required_chained_hop",
			cypher: `MATCH (anchor:Function {uid:"` + liveClauseAnchorUID + `"})-[:CALLS]->(target)
				MATCH (target)<-[:CONTAINS]-(targetFile:File)<-[:REPO_CONTAINS]-(targetRepo:Repository)
				RETURN target.name AS name, targetFile.relative_path AS file_path, targetRepo.id AS repo_id`,
		},
		{
			name: "single_clause_anchor_and_chained_hop",
			cypher: `MATCH (anchor:Function {uid:"` + liveClauseAnchorUID + `"})-[:CALLS]->(target)<-[:CONTAINS]-(targetFile:File)<-[:REPO_CONTAINS]-(targetRepo:Repository)
				RETURN target.name AS name, targetFile.relative_path AS file_path, targetRepo.id AS repo_id`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := reader.Run(ctx, tc.cypher, map[string]any{})
			if err != nil {
				t.Fatalf("run %s: %v", tc.name, err)
			}
			t.Logf("%s returned %d rows", tc.name, len(rows))
			for _, row := range rows {
				t.Logf("  name=%v file_path=%v repo_id=%v", row["name"], row["file_path"], row["repo_id"])
			}
		})
	}
}
