// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestCollectServiceWorkloadCandidatesStripsHandleBeforeNameProbe is the T5
// handle-ladder table for service-workload resolution (compatibility doc
// section 1.4): rung 1 probes the full selector string as a node id, rung 2
// probes the stripped bare name, and a handle shared by two granted workloads
// still fails closed with serviceWorkloadAmbiguousError carrying both
// candidates.
func TestCollectServiceWorkloadCandidatesStripsHandleBeforeNameProbe(t *testing.T) {
	t.Parallel()

	const handle = "workload:checkout"
	var nameProbeParam string
	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "w.id = $service_name"):
					if got := params["service_name"]; got != handle {
						t.Fatalf("id probe param = %#v, want full selector %q (ladder rung 1)", got, handle)
					}
					return nil, nil
				case strings.Contains(cypher, "w.name = $service_name"):
					nameProbeParam, _ = params["service_name"].(string)
					return []map[string]any{
						{"id": "workload:repository:r_a:checkout", "name": "checkout", "kind": "service", "repo_id": "repository:r_a"},
						{"id": "workload:repository:r_b:checkout", "name": "checkout", "kind": "service", "repo_id": "repository:r_b"},
					}, nil
				case strings.Contains(cypher, "r.id IN $repo_ids"):
					return []map[string]any{
						{"repo_id": "repository:r_a", "repo_name": "alpha"},
						{"repo_id": "repository:r_b", "repo_name": "beta"},
					}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	_, err := handler.resolveServiceWorkloadCandidate(
		context.Background(),
		ServiceWorkloadSelector{ServiceName: handle},
		"platform_impact.context_overview",
	)
	if err == nil {
		t.Fatal("resolveServiceWorkloadCandidate() error = nil, want an ambiguity error for a shared handle")
	}
	var ambiguous serviceWorkloadAmbiguousError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("resolveServiceWorkloadCandidate() error = %#v, want serviceWorkloadAmbiguousError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Fatalf("ambiguous candidates = %d, want 2 (fail closed, never pick a tenant)", len(ambiguous.Candidates))
	}
	seen := map[string]bool{}
	for _, candidate := range ambiguous.Candidates {
		seen[candidate.RepoID] = true
	}
	if !seen["repository:r_a"] || !seen["repository:r_b"] {
		t.Fatalf("ambiguous candidates = %#v, want both repo_ids", ambiguous.Candidates)
	}
	if nameProbeParam != "checkout" {
		t.Fatalf("name probe param = %q, want stripped bare name %q", nameProbeParam, "checkout")
	}
}
