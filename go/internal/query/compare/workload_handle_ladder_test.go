// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compare

import (
	"context"
	"strings"
	"testing"
)

// TestFetchWorkloadLaddersHandleToScopedRow is the #5385 flip-wave read-side
// retirement for POST /api/v0/compare/environments: the gate passes the
// public handle (workload:deployable-source), which matches no post-flip
// node id. fetchWorkload must keep the exact id rung first (scoped ids and
// any legacy handle-shaped node still resolve there) and fall back to the
// stripped-name rung for handles. The grant check stays with the caller,
// which renders an ungranted or missing workload identically.
func TestFetchWorkloadLaddersHandleToScopedRow(t *testing.T) {
	t.Parallel()

	const handle = "workload:deployable-source"
	const scoped = "workload:repository:r_1f68383d:deployable-source"
	handler := &Handler{
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "w.id = $workload_id") {
					if got := params["workload_id"]; got != handle {
						t.Fatalf("id probe param = %#v, want full selector %q (ladder rung 1)", got, handle)
					}
					return nil, nil
				}
				return nil, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = ") {
					return nil, nil
				}
				if got := params["workload_name"]; got != "deployable-source" {
					t.Fatalf("name probe param = %#v, want stripped bare name", got)
				}
				return []map[string]any{
					{"id": scoped, "name": "deployable-source", "kind": "service", "repo_id": "repository:r_1f68383d"},
				}, nil
			},
		},
	}

	workload, err := handler.fetchWorkload(context.Background(), handle)
	if err != nil {
		t.Fatalf("fetchWorkload() error = %v, want nil", err)
	}
	if workload == nil {
		t.Fatal("fetchWorkload() = nil, want the scoped deployable-source row")
	}
	if workload["id"] != scoped {
		t.Fatalf("workload id = %#v, want scoped %q", workload["id"], scoped)
	}
}
