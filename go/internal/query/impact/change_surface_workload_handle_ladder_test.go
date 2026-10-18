// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestChangeSurfaceWorkloadHandleLadder is the T5 handle-ladder table for the
// change-surface workload resolver (compatibility doc section 1.4): an exact
// scoped id resolves at rung 1, a unique handle resolves at rung 2 by its
// stripped bare name, a handle naming workloads in two granted repositories
// fails closed as ambiguous carrying both candidate repo_ids, and an unknown
// name misses. The fake backend answers id probes only for full ids and name
// probes only for bare names, which is the post-re-key graph shape.
func TestChangeSurfaceWorkloadHandleLadder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		target         string
		grantedRepos   []string
		idRows         map[string][]map[string]any
		nameRows       map[string][]map[string]any
		wantStatus     string
		wantSelectedID string
		wantCandRepos  []string
	}{
		{
			name:         "exact id resolves",
			target:       "workload:repository:r_a:checkout",
			grantedRepos: []string{"repository:r_a"},
			idRows: map[string][]map[string]any{
				"workload:repository:r_a:checkout": {
					{"id": "workload:repository:r_a:checkout", "name": "checkout", "repo_id": "repository:r_a"},
				},
			},
			nameRows:       map[string][]map[string]any{},
			wantStatus:     "resolved",
			wantSelectedID: "workload:repository:r_a:checkout",
		},
		{
			name:         "unique handle resolves by stripped bare name",
			target:       "workload:checkout",
			grantedRepos: []string{"repository:r_a"},
			idRows:       map[string][]map[string]any{},
			nameRows: map[string][]map[string]any{
				"checkout": {
					{"id": "workload:repository:r_a:checkout", "name": "checkout", "repo_id": "repository:r_a"},
				},
			},
			wantStatus:     "resolved",
			wantSelectedID: "workload:repository:r_a:checkout",
		},
		{
			name:         "ambiguous handle fails closed with candidate repos",
			target:       "workload:checkout",
			grantedRepos: []string{"repository:r_a", "repository:r_b"},
			idRows:       map[string][]map[string]any{},
			nameRows: map[string][]map[string]any{
				"checkout": {
					{"id": "workload:repository:r_a:checkout", "name": "checkout", "repo_id": "repository:r_a"},
					{"id": "workload:repository:r_b:checkout", "name": "checkout", "repo_id": "repository:r_b"},
				},
			},
			wantStatus:    "ambiguous",
			wantCandRepos: []string{"repository:r_a", "repository:r_b"},
		},
		{
			name:           "miss resolves nothing",
			target:         "workload:checkout",
			grantedRepos:   []string{"repository:r_a"},
			idRows:         map[string][]map[string]any{},
			nameRows:       map[string][]map[string]any{},
			wantStatus:     "no_match",
			wantSelectedID: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var nameProbeTargets []string
			handler := &Handler{Neo4j: graph.FakeGraphReader{RunFn: func(
				_ context.Context,
				cypher string,
				params map[string]any,
			) ([]map[string]any, error) {
				target, _ := params["target"].(string)
				switch {
				case strings.Contains(cypher, "MATCH (n:Workload {id: $target})"):
					return tc.idRows[target], nil
				case strings.Contains(cypher, "MATCH (n:Workload {name: $target})"):
					nameProbeTargets = append(nameProbeTargets, target)
					return tc.nameRows[target], nil
				default:
					return nil, nil
				}
			}}}
			ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
				Mode:                 auth.AuthModeScoped,
				AllowedRepositoryIDs: tc.grantedRepos,
			})
			req := ChangeSurfaceInvestigationRequest{Target: tc.target, TargetType: "workload", Limit: 10}

			selected, resolution, err := handler.resolveChangeSurfaceTarget(ctx, req)
			if err != nil {
				t.Fatalf("resolveChangeSurfaceTarget() error = %v, want nil", err)
			}
			if got := resolution["status"]; got != tc.wantStatus {
				t.Fatalf("status = %v, want %v", got, tc.wantStatus)
			}
			switch tc.wantStatus {
			case "resolved":
				if selected == nil {
					t.Fatal("selected = nil, want a resolved candidate")
				}
				if selected.ID != tc.wantSelectedID {
					t.Fatalf("selected.ID = %q, want %q", selected.ID, tc.wantSelectedID)
				}
			case "ambiguous":
				if selected != nil {
					t.Fatalf("selected = %+v, want nil on ambiguity", selected)
				}
				candidates, _ := resolution["candidates"].([]map[string]any)
				var repos []string
				for _, candidate := range candidates {
					repo, _ := candidate["repo_id"].(string)
					repos = append(repos, repo)
				}
				if len(repos) != len(tc.wantCandRepos) {
					t.Fatalf("candidate repos = %v, want %v", repos, tc.wantCandRepos)
				}
				for i := range repos {
					if repos[i] != tc.wantCandRepos[i] {
						t.Fatalf("candidate repos = %v, want %v", repos, tc.wantCandRepos)
					}
				}
			default:
				if selected != nil {
					t.Fatalf("selected = %+v, want nil on miss", selected)
				}
			}
			// A handle input must reach a rung-2 name probe carrying the
			// stripped bare name, never the prefixed handle. A full scoped
			// id resolves at rung 1 and runs no name probe by design.
			if tc.target == "workload:checkout" {
				for _, probed := range nameProbeTargets {
					if probed != "checkout" {
						t.Fatalf("name probe target = %q, want stripped bare name %q", probed, "checkout")
					}
				}
				if len(nameProbeTargets) == 0 {
					t.Fatal("no workload name probe ran for a handle input")
				}
			}
		})
	}
}
