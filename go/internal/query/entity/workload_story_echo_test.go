// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestGetWorkloadStoryEchoesResolvedScopedID is the #5385 flip-wave tail of
// the read-side retirement: a handle request resolves to the scoped node,
// so the story envelope must echo the resolved node id, not the handle the
// caller typed. Echoing the raw path param re-emits the retired
// handle==id equation the flip removes.
func TestGetWorkloadStoryEchoesResolvedScopedID(t *testing.T) {
	t.Parallel()

	const handle = "workload:api-svc"
	const scoped = "workload:repository:r_d5318f0f:api-svc"
	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunByMatch: map[string][]map[string]any{
				"collect(DISTINCT dr.id) as defining": {
					{"id": scoped, "name": "api-svc", "kind": "service", "repo_id": "repository:r_d5318f0f", "defining": []string{"repository:r_d5318f0f"}},
				},
			},
			RunSingleByMatch: map[string]map[string]any{
				"MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)": {
					"repo_id": "repository:r_d5318f0f", "repo_name": "api",
				},
			},
		},
	}

	data, _ := getOutlierJSON(t, handler, "/api/v0/workloads/"+handle+"/story", "workload_id", handle)
	if got := data["workload_id"]; got != scoped {
		t.Fatalf("workload_id = %#v, want resolved scoped %q", got, scoped)
	}
	if got := data["name"]; got != "api-svc" {
		t.Fatalf("name = %#v, want %q", got, "api-svc")
	}
}
