// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_neo4j

package query

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestServiceStoryTargetSupportDefinesGateNeo4jLive runs the #7138 service
// gate's DEFINES read on a real Neo4j graph through the production loader and
// checks the verdict it hands the content store. The read is bounded at three
// rows, so the case that matters is a repository defining more workloads than
// the bound with the target sorting last: the target must still come back and
// the story must read as ambiguous, not as "the repository does not define
// the target".
//
// Run with ESHU_INFRA_SCOPE_NEO4J_LIVE=1 and ESHU_NEO4J_URI (plus
// ESHU_NEO4J_USERNAME/ESHU_NEO4J_PASSWORD when auth is on):
//
//	go test -tags live_infra_scope_neo4j ./internal/query -run DefinesGateNeo4jLive -count=1
func TestServiceStoryTargetSupportDefinesGateNeo4jLive(t *testing.T) {
	live := openLiveNeo4jScope(t)
	nonce := fmt.Sprintf("7138-%d", time.Now().UnixNano())
	workload := func(name string) string { return "workload:" + nonce + "-" + name }
	params := map[string]any{"repo_prefix": "repository:" + nonce, "workload_prefix": "workload:" + nonce}
	t.Cleanup(func() {
		live.raw(t, 30*time.Second, `MATCH (n) WHERE (n:Repository AND n.id STARTS WITH $repo_prefix)
   OR (n:Workload AND n.id STARTS WITH $workload_prefix)
DETACH DELETE n`, params)
	})

	target := workload("zz-target")
	tests := []struct {
		name        string
		defines     []string
		wantCount   int
		wantDefines bool
	}{
		{name: "sole", defines: []string{target}, wantCount: 1, wantDefines: true},
		{name: "two", defines: []string{workload("aa"), target}, wantCount: 2, wantDefines: true},
		{name: "target sorts last among four", defines: []string{workload("aa"), workload("bb"), workload("cc"), target}, wantCount: 3, wantDefines: true},
		{name: "four without the target", defines: []string{workload("aa"), workload("bb"), workload("cc"), workload("dd")}, wantCount: 3, wantDefines: false},
		{name: "none", defines: nil, wantCount: 0, wantDefines: false},
	}
	for i, tt := range tests {
		repoID := fmt.Sprintf("repository:%s-%d", nonce, i)
		live.raw(t, 30*time.Second, `CREATE (r:Repository {id: $repo_id})
WITH r
UNWIND $ids AS id
MERGE (w:Workload {id: id})
ON CREATE SET w.name = id
MERGE (r)-[:DEFINES]->(w)`, map[string]any{"repo_id": repoID, "ids": tt.defines})
		if len(tt.defines) == 0 {
			live.raw(t, 30*time.Second, "MERGE (:Repository {id: $repo_id})", map[string]any{"repo_id": repoID})
		}

		var captured serviceStoryTargetSupportFilter
		store := fakePortContentStore{targetSupportFilter: &captured}
		if _, err := loadServiceStoryTargetSupport(context.Background(), live.reader, store, map[string]any{
			"id":      target,
			"repo_id": repoID,
		}); err != nil {
			t.Fatalf("%s: loadServiceStoryTargetSupport() error = %v", tt.name, err)
		}
		if captured.RepositoryWorkloadCount != tt.wantCount || captured.RepositoryDefinesTarget != tt.wantDefines {
			t.Fatalf("%s: gate (count, defines) = (%d, %v), want (%d, %v)",
				tt.name, captured.RepositoryWorkloadCount, captured.RepositoryDefinesTarget, tt.wantCount, tt.wantDefines)
		}
	}
}
