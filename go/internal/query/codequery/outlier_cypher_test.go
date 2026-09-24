// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func outlierBackends() []querycontract.GraphBackend {
	return []querycontract.GraphBackend{querycontract.GraphBackendNeo4j, querycontract.GraphBackendNornicDB}
}

func unscopedOutlierAccess() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{AllScopes: true}
}

func scopedOutlierAccess() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}}
}

// TestBuildOutlierCohortsCypherShapes pins the three enumeration reads on
// both backends: a Function anchor with repo/grant scope, exactly one hop
// (CONTAINS+IMPLEMENTS, HANDLES_ROUTE, CONTAINS+File), and the columns the
// grouper reads. The NornicDB and Neo4j texts are identical by design: a
// repo-wide enumeration has no uid anchor to dialect-split on.
func TestBuildOutlierCohortsCypherShapes(t *testing.T) {
	t.Parallel()

	shapes := map[codedivergence.CohortSource][]string{
		codedivergence.CohortInterface: {"CONTAINS", "IMPLEMENTS", "iface_id", "member_id"},
		codedivergence.CohortRouter:    {"HANDLES_ROUTE", "endpoint_path", "member_id"},
		codedivergence.CohortPackage:   {"CONTAINS", "file_path", "member_id"},
	}
	for source, wants := range shapes {
		for _, backend := range outlierBackends() {
			cypher, params := BuildOutlierCohortsCypher(source, "repo-a", backend, scopedOutlierAccess())
			for _, want := range append(wants, "member:Function", "$repo_id") {
				if !strings.Contains(cypher, want) {
					t.Errorf("source %q backend %v cypher missing %q:\n%s", source, backend, want, cypher)
				}
			}
			if params["repo_id"] != "repo-a" {
				t.Errorf("source %q backend %v repo param = %v, want repo-a", source, backend, params["repo_id"])
			}
		}
	}
	nornic, _ := BuildOutlierCohortsCypher(codedivergence.CohortRouter, "repo-a", querycontract.GraphBackendNornicDB, unscopedOutlierAccess())
	neo4j, _ := BuildOutlierCohortsCypher(codedivergence.CohortRouter, "repo-a", querycontract.GraphBackendNeo4j, unscopedOutlierAccess())
	if nornic != neo4j {
		t.Errorf("router enumeration dialects differ:\nNornicDB:%s\nNeo4j:%s", nornic, neo4j)
	}
}

// TestBuildOutlierCalleeEdgesCypherBatches pins the batched outgoing-CALLS
// read on both backends: a UNWIND id anchor, one CALLS hop, the callee
// columns with edge provenance, and repo/grant scope on the callee.
func TestBuildOutlierCalleeEdgesCypherBatches(t *testing.T) {
	t.Parallel()

	for _, backend := range outlierBackends() {
		cypher, params := BuildOutlierCalleeEdgesCypher([]string{"h-1", "h-2"}, "repo-a", backend, scopedOutlierAccess())
		for _, want := range []string{"UNWIND $member_ids AS mid", "CALLS", "callee_id", "edge_method", "edge_confidence", "$repo_id"} {
			if !strings.Contains(cypher, want) {
				t.Errorf("backend %v callee-edges cypher missing %q:\n%s", backend, want, cypher)
			}
		}
		ids, _ := params["member_ids"].([]string)
		if len(ids) != 2 || ids[0] != "h-1" || ids[1] != "h-2" {
			t.Errorf("backend %v member ids = %v, want [h-1 h-2]", backend, ids)
		}
	}
	nornic, _ := BuildOutlierCalleeEdgesCypher([]string{"h-1"}, "repo-a", querycontract.GraphBackendNornicDB, unscopedOutlierAccess())
	if !strings.Contains(nornic, "(member:Function {uid: mid})") {
		t.Errorf("NornicDB callee-edges anchor misses the uid node pattern:\n%s", nornic)
	}
	neo4j, _ := BuildOutlierCalleeEdgesCypher([]string{"h-1"}, "repo-a", querycontract.GraphBackendNeo4j, unscopedOutlierAccess())
	if !strings.Contains(neo4j, "(member:Function {uid: mid})") {
		t.Errorf("Neo4j callee-edges anchor misses the indexed uid node pattern:\n%s", neo4j)
	}
	if strings.Contains(neo4j, "member.id = mid OR member.uid = mid") || strings.Contains(neo4j, "member.id = mid") {
		t.Errorf("Neo4j callee-edges anchor must not use the unindexed id-OR-uid scan:\n%s", neo4j)
	}
}
