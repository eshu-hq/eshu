// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestServiceWorkloadAmbiguousErrorUsesAPINeutralSelectorGuidance(t *testing.T) {
	t.Parallel()

	err := serviceWorkloadAmbiguousError{Selector: "checkout"}

	message := err.Error()
	if strings.Contains(message, "--") {
		t.Fatalf("message = %q, want API-neutral selector names instead of CLI flags", message)
	}
	if !strings.Contains(message, "service_id, repo, or environment") {
		t.Fatalf("message = %q, want service_id, repo, or environment guidance", message)
	}
}

func TestCollectServiceWorkloadCandidatesHydratesRepositoryNames(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "w.name = $service_name"):
					return []map[string]any{
						{"id": "workload:checkout-api", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-api"},
						{"id": "workload:checkout-worker", "name": "checkout", "kind": "service", "repo_id": "repo-checkout-worker"},
					}, nil
				case strings.Contains(cypher, "r.id IN $repo_ids"):
					repoIDs, ok := params["repo_ids"].([]string)
					if !ok {
						t.Fatalf("params[repo_ids] = %T, want []string", params["repo_ids"])
					}
					for _, want := range []string{"repo-checkout-api", "repo-checkout-worker"} {
						if !slices.Contains(repoIDs, want) {
							t.Fatalf("repo_ids = %#v, missing %q", repoIDs, want)
						}
					}
					return []map[string]any{
						{"repo_id": "repo-checkout-api", "repo_name": "checkout-api"},
						{"repo_id": "repo-checkout-worker", "repo_name": "checkout-worker"},
					}, nil
				default:
					return nil, nil
				}
			},
		},
	}

	candidates, truncated, err := handler.collectServiceWorkloadCandidates(
		context.Background(),
		serviceWorkloadSelector{ServiceName: "checkout"},
		"",
	)
	if err != nil {
		t.Fatalf("collectServiceWorkloadCandidates() error = %v, want nil", err)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2: %#v", len(candidates), candidates)
	}
	for _, candidate := range candidates {
		if candidate.RepoName == "" {
			t.Fatalf("candidate missing repo name: %#v", candidate)
		}
	}
}
