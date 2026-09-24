// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestNeo4jWrapperAndCompareAnchorsSeekUID pins the Neo4j anchor of every
// wrapper-bypass builder and the compare-paths hop to the indexed
// (alias:Function|Class|File {uid: ...}) node pattern (issue #7057). The old
// WHERE (alias.id = x OR alias.uid = x) shape cannot use the uid uniqueness
// constraints and planned as a NodeByLabelScan over all three labels per id,
// so each case also rejects any ".id =" predicate.
func TestNeo4jWrapperAndCompareAnchorsSeekUID(t *testing.T) {
	t.Parallel()

	neo4j := querycontract.GraphBackendNeo4j
	ids := []string{"fn-a", "fn-b"}
	cases := []struct {
		name   string
		build  func(access querycontract.RepositoryAccessFilter) string
		anchor string
	}{
		{
			name: "BuildWrapperCallersCypher",
			build: func(a querycontract.RepositoryAccessFilter) string {
				c, _ := BuildWrapperCallersCypher("fn-a", "repo-a", neo4j, a)
				return c
			},
			anchor: "(target:Function|Class|File {uid: $target_entity_id})",
		},
		{
			name: "BuildWrapperFanInCypher",
			build: func(a querycontract.RepositoryAccessFilter) string {
				c, _ := BuildWrapperFanInCypher("fn-a", "repo-a", neo4j, a)
				return c
			},
			anchor: "(target:Function|Class|File {uid: $entity_id})",
		},
		{
			name: "BuildWrapperCalleesCypher",
			build: func(a querycontract.RepositoryAccessFilter) string {
				c, _ := BuildWrapperCalleesCypher("fn-a", "fn-b", "repo-a", neo4j, a)
				return c
			},
			anchor: "(source:Function|Class|File {uid: $entity_id})",
		},
		{
			name: "BuildWrapperFamilyCallersCypher",
			build: func(a querycontract.RepositoryAccessFilter) string {
				c, _ := BuildWrapperFamilyCallersCypher(ids, "repo-a", neo4j, a)
				return c
			},
			anchor: "(target:Function|Class|File {uid: tid})",
		},
		{
			name: "BuildWrapperFamilyFanInCypher",
			build: func(a querycontract.RepositoryAccessFilter) string {
				c, _ := BuildWrapperFamilyFanInCypher(ids, "repo-a", neo4j, a)
				return c
			},
			anchor: "(target:Function|Class|File {uid: eid})",
		},
		{
			name: "BuildWrapperFamilyCalleesCypher",
			build: func(a querycontract.RepositoryAccessFilter) string {
				c, _ := BuildWrapperFamilyCalleesCypher(ids, "repo-a", neo4j, a)
				return c
			},
			anchor: "(source:Function|Class|File {uid: sid})",
		},
		{
			name: "BuildComparePathsHopCypher",
			build: func(a querycontract.RepositoryAccessFilter) string {
				c, _ := BuildComparePathsHopCypher("fn-a", "repo-a", neo4j, a)
				return c
			},
			anchor: "(source:Function|Class|File {uid: $source_entity_id})",
		},
	}
	accesses := map[string]querycontract.RepositoryAccessFilter{
		"unscoped": {AllScopes: true},
		"scoped":   {AllowedRepositoryIDs: []string{"repo-a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for accessName, access := range accesses {
				cypher := tc.build(access)
				if !strings.Contains(cypher, tc.anchor) {
					t.Errorf("%s Neo4j anchor missing %q:\n%s", accessName, tc.anchor, cypher)
				}
				if strings.Contains(cypher, ".id =") {
					t.Errorf("%s Neo4j anchor still uses the unindexed id predicate:\n%s", accessName, cypher)
				}
			}
		})
	}
}
