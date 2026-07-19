// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

func TestFetchWorkloadContextSelectsRepositoryFromActualDefinesCandidates(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		storedRepoID string
		wantRepoID   string
	}{
		{
			name: "scoped authorized stored repository is stale",
			ctx: ContextWithAuthContext(t.Context(), AuthContext{
				Mode:                 AuthModeScoped,
				AllowedRepositoryIDs: []string{"repo-team-a", "repo-team-b", "repo-team-stale"},
			}),
			storedRepoID: "repo-team-stale",
			wantRepoID:   "repo-team-a",
		},
		{
			name:         "unscoped stored repository is stale",
			ctx:          t.Context(),
			storedRepoID: "repo-team-stale",
			wantRepoID:   "repo-team-a",
		},
		{
			name:         "stored repository is an actual defining candidate",
			ctx:          t.Context(),
			storedRepoID: "repo-team-b",
			wantRepoID:   "repo-team-b",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateLookupCalled := false
			reader := fakeWorkloadGraphReader{
				runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
					if strings.Contains(cypher, "MATCH (r:Repository)-[:DEFINES]->(w)") {
						candidateLookupCalled = true
						if strings.Contains(cypher, "CASE WHEN r.id = $preferred_repo_id") &&
							StringVal(params, "preferred_repo_id") == "repo-team-b" {
							return map[string]any{"repo_id": "repo-team-b", "repo_name": "payments-b"}, nil
						}
						return map[string]any{"repo_id": "repo-team-a", "repo_name": "payments-a"}, nil
					}
					if strings.Contains(cypher, "MATCH (w:Workload)") {
						return map[string]any{
							"id": "workload:payments", "name": "payments", "kind": "service",
							"repo_id": test.storedRepoID,
						}, nil
					}
					if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
						return map[string]any{"repo_name": "stale"}, nil
					}
					return nil, nil
				},
				run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
					switch {
					case strings.Contains(cypher, "[instanceOf:INSTANCE_OF]"):
						repoID := StringVal(params, "repo_id")
						if repoID != "repo-team-a" && repoID != "repo-team-b" {
							return nil, nil
						}
						return []map[string]any{{
							"repo_id": repoID, "repo_name": "payments", "workload_id": "workload:payments",
							"instance_id": "instance:payments:prod", "environment": "prod",
						}}, nil
					default:
						return nil, nil
					}
				},
			}

			got, err := (&EntityHandler{Neo4j: reader}).fetchWorkloadContextForOperation(
				test.ctx,
				"w.id = $workload_id",
				map[string]any{"workload_id": "workload:payments"},
				"workload_context",
			)
			if err != nil {
				t.Fatalf("fetchWorkloadContextForOperation() error = %v", err)
			}
			if !candidateLookupCalled {
				t.Fatal("repository selection did not derive from DEFINES candidates")
			}
			if gotRepoID := StringVal(got, "repo_id"); gotRepoID != test.wantRepoID {
				t.Fatalf("repo_id = %q, want actual defining repository %q", gotRepoID, test.wantRepoID)
			}
			instances := mapSliceValue(got, "instances")
			if len(instances) != 1 || StringVal(instances[0], "instance_id") != "instance:payments:prod" {
				t.Fatalf("instances = %#v, want topology for selected defining repository", instances)
			}
		})
	}
}
