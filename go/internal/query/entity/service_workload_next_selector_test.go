// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const sameRepoSelectorRepo = "repository:r_sample"

// sameRepoHandler returns a handler whose graph holds two workloads with the
// same name in one repository, the shape that repo cannot separate.
func sameRepoHandler(t *testing.T) *Handler {
	t.Helper()
	return &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				if got, _ := params["repo_id"].(string); got != sameRepoSelectorRepo {
					t.Errorf("repo_id param = %q, want %q so the repo filter is applied", got, sameRepoSelectorRepo)
				}
				return []map[string]any{
					{"id": "workload:repository:r_sample:checkout-service", "name": "checkout-service", "kind": "service", "repo_id": sameRepoSelectorRepo},
					{"id": "workload:repository:r_sample:checkout-service-legacy", "name": "checkout-service", "kind": "service", "repo_id": sameRepoSelectorRepo},
				}, nil
			},
		},
	}
}

// TestSameRepositoryCandidatesStayAmbiguousAndNameTheNextSelector pins the
// issue's resolver case: service_name plus repo with two same-repository
// workloads still refuses to guess, and each candidate carries the exact
// argument to pass next.
func TestSameRepositoryCandidatesStayAmbiguousAndNameTheNextSelector(t *testing.T) {
	t.Parallel()

	handler := sameRepoHandler(t)
	_, err := handler.resolveServiceWorkloadCandidate(
		context.Background(),
		ServiceWorkloadSelector{ServiceName: "checkout-service", Repository: sameRepoSelectorRepo},
		"service_story",
	)
	if err == nil {
		t.Fatal("resolveServiceWorkloadCandidate() error = nil, want an ambiguity error")
	}

	status, envelope := serviceStoryResolutionError(context.Background(), err)
	if status != http.StatusConflict || envelope == nil {
		t.Fatalf("status, envelope = %d, %v, want 409 with an envelope", status, envelope)
	}
	candidates, ok := envelope.Details["candidates"].([]map[string]any)
	if !ok || len(candidates) != 2 {
		t.Fatalf("details.candidates = %#v, want two candidates", envelope.Details["candidates"])
	}
	for _, candidate := range candidates {
		next, ok := candidate["next_selector"].(map[string]any)
		if !ok {
			t.Fatalf("candidate %#v has no next_selector object", candidate)
		}
		if got := next["argument"]; got != "service_id" {
			t.Errorf("next_selector.argument = %v, want service_id", got)
		}
		if got, want := next["value"], candidate["service_id"]; got != want {
			t.Errorf("next_selector.value = %v, want the candidate id %v", got, want)
		}
		// The existing fields stay, so current readers keep working.
		for _, key := range []string{"service_id", "service_name", "kind", "repo_id", "repo_name", "environment", "match_basis"} {
			if _, ok := candidate[key]; !ok {
				t.Errorf("candidate lost field %q", key)
			}
		}
	}
}

// TestAmbiguousMessageSaysRepoOnlySeparatesDifferentRepositories keeps the
// error text honest: it names service_id as the working argument and says what
// repo can and cannot do.
func TestAmbiguousMessageSaysRepoOnlySeparatesDifferentRepositories(t *testing.T) {
	t.Parallel()

	message := serviceWorkloadAmbiguousError{Selector: "checkout-service"}.Error()
	for _, want := range []string{"checkout-service", "service_id", "repo separates only services in different repositories"} {
		if !strings.Contains(message, want) {
			t.Errorf("message = %q, want it to contain %q", message, want)
		}
	}
}
