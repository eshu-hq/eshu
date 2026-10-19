// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestEntityMapWorkloadHandleLadder is the T5 handle-ladder table for the
// entity-map workload resolver (compatibility doc section 1.4, rung order
// shared with the change-surface resolver): an exact scoped id resolves at
// rung 1, a unique handle resolves at rung 2 by its stripped bare name, a
// handle naming workloads in two repositories fails closed as ambiguous
// carrying both candidate repo_ids, a repo-scoped handle resolves through
// the stripped repo-scoped probe, and an unknown name misses. The fake
// backend answers id probes only for full ids and name probes only for bare
// names, which is the post-re-key graph shape.
func TestEntityMapWorkloadHandleLadder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		from           string
		repoID         string
		idRows         map[string][]map[string]any
		nameRows       map[string][]map[string]any
		repoRows       map[string][]map[string]any
		wantStatus     string
		wantSelectedID string
		wantCandRepos  []string
	}{
		{
			name: "exact id resolves",
			from: "workload:repository:r_a:checkout",
			idRows: map[string][]map[string]any{
				"workload:repository:r_a:checkout": {
					{"id": "workload:repository:r_a:checkout", "name": "checkout", "repo_id": "repository:r_a"},
				},
			},
			wantStatus:     "resolved",
			wantSelectedID: "workload:repository:r_a:checkout",
		},
		{
			name:   "unique handle resolves by stripped bare name",
			from:   "workload:checkout",
			idRows: map[string][]map[string]any{},
			nameRows: map[string][]map[string]any{
				"checkout": {
					{"id": "workload:repository:r_a:checkout", "name": "checkout", "repo_id": "repository:r_a"},
				},
			},
			wantStatus:     "resolved",
			wantSelectedID: "workload:repository:r_a:checkout",
		},
		{
			name:   "ambiguous handle fails closed with candidate repos",
			from:   "workload:checkout",
			idRows: map[string][]map[string]any{},
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
			name:     "repo-scoped handle resolves through stripped probe",
			from:     "workload:checkout",
			repoID:   "repository:r_a",
			idRows:   map[string][]map[string]any{},
			nameRows: map[string][]map[string]any{},
			repoRows: map[string][]map[string]any{
				"checkout": {
					{"id": "workload:repository:r_a:checkout", "name": "checkout", "repo_id": "repository:r_a"},
				},
			},
			wantStatus:     "resolved",
			wantSelectedID: "workload:repository:r_a:checkout",
		},
		{
			name:           "miss resolves nothing",
			from:           "workload:checkout",
			idRows:         map[string][]map[string]any{},
			nameRows:       map[string][]map[string]any{},
			repoRows:       map[string][]map[string]any{},
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
				from, _ := params["from"].(string)
				switch {
				case strings.Contains(cypher, "MATCH (n:Workload {id: $from})"):
					return tc.idRows[from], nil
				case strings.Contains(cypher, "MATCH (n:Workload {name: $from})"):
					nameProbeTargets = append(nameProbeTargets, from)
					return tc.nameRows[from], nil
				case strings.Contains(cypher, "MATCH (n:Workload {repo_id: $repo_id, name: $from})"):
					nameProbeTargets = append(nameProbeTargets, from)
					return tc.repoRows[from], nil
				default:
					return nil, nil
				}
			}}}
			req := entityMapRequest{From: tc.from, FromType: "workload", RepoID: tc.repoID, Limit: 10}

			selected, resolution, err := handler.resolveEntityMapStart(context.Background(), req)
			if err != nil {
				t.Fatalf("resolveEntityMapStart() error = %v, want nil", err)
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
			for _, probed := range nameProbeTargets {
				if probed != "checkout" {
					t.Fatalf("name probe target = %q, want stripped bare name %q", probed, "checkout")
				}
			}
			if tc.from == "workload:checkout" && len(nameProbeTargets) == 0 {
				t.Fatal("no workload name probe ran for a handle input")
			}
			if tc.wantSelectedID == "workload:repository:r_a:checkout" && tc.from == "workload:repository:r_a:checkout" && len(nameProbeTargets) != 0 {
				t.Fatalf("name probes ran for a full id: %v, want rung-1 exact match only", nameProbeTargets)
			}
		})
	}
}
