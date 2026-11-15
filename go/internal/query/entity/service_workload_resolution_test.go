// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestServiceStoryAmbiguousEnvelopeCarriesSelectorInMessage pins the fact the
// report-bundle redactor is built against: an ambiguous service story does not
// merely put the caller's selector in details.selector, it composes that same
// string into the human-readable Message beside it.
//
// This matters outside this package. reportbundle.Capture writes the envelope
// into a share-safe bundle whose own guide tells reporters to attach it to a
// public GitHub issue, and its egress canary
// (reportbundle/redaction_egress_test.go) plants a credential in a selector and
// proves it does not survive into either field. That canary describes the
// message shape rather than calling this unexported path, so this test is the
// link: if the composition ever stops echoing the selector, the canary is
// guarding a shape the server no longer produces and should be revisited rather
// than trusted.
func TestServiceStoryAmbiguousEnvelopeCarriesSelectorInMessage(t *testing.T) {
	t.Parallel()

	const selector = "checkout?token=SELECTOR-ECHO-PROBE"

	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "w.name = $service_name") {
					return nil, nil
				}
				return []map[string]any{
					{"id": "workload:checkout-api", "name": selector, "kind": "service", "repo_id": "repo-a"},
					{"id": "workload:checkout-worker", "name": selector, "kind": "service", "repo_id": "repo-b"},
				}, nil
			},
		},
	}

	_, err := handler.resolveServiceWorkloadCandidate(
		context.Background(),
		ServiceWorkloadSelector{ServiceName: selector},
		"platform_impact.context_overview",
	)
	if err == nil {
		t.Fatal("resolveServiceWorkloadCandidate() error = nil, want an ambiguity error")
	}

	status, envelope := serviceStoryResolutionError(context.Background(), err)
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want %d", status, http.StatusConflict)
	}
	if envelope == nil {
		t.Fatal("envelope = nil, want an ambiguous error envelope")
	}
	if !strings.Contains(envelope.Message, selector) {
		t.Fatalf("envelope.Message = %q, want it to interpolate the selector %q", envelope.Message, selector)
	}
	if got, _ := envelope.Details["selector"].(string); got != selector {
		t.Fatalf("envelope.Details[selector] = %q, want %q", got, selector)
	}
}

// TestServiceStoryRepoAmbiguousCandidatesAreObjects pins the #10655 review
// follow-up: a repository-selector ambiguity on the story/investigation routes
// carries the same repo_id candidate objects as the shared
// selector.AmbiguousDetails writer, not the bare id strings that used to leak
// through repoAmbiguous.Matches. The service-workload branch keeps its richer
// map shape and is asserted elsewhere.
func TestServiceStoryRepoAmbiguousCandidatesAreObjects(t *testing.T) {
	t.Parallel()

	status, envelope := serviceStoryResolutionError(context.Background(), selector.AmbiguousError{
		Selector: "payments",
		Matches:  []string{"repository:r_b", "repository:r_a", "repository:r_b"},
	})
	if status != http.StatusConflict || envelope == nil {
		t.Fatalf("status, envelope = %d, %v, want 409 with an envelope", status, envelope)
	}
	if envelope.Code != querycontract.ErrorCodeAmbiguous {
		t.Fatalf("code = %q, want %q", envelope.Code, querycontract.ErrorCodeAmbiguous)
	}
	candidates, ok := envelope.Details["candidates"].([]selector.AmbiguousCandidate)
	if !ok {
		t.Fatalf("details.candidates = %#v (%T), want []selector.AmbiguousCandidate objects", envelope.Details["candidates"], envelope.Details["candidates"])
	}
	// AmbiguousCandidates drops the duplicate and sorts by id.
	if len(candidates) != 2 ||
		candidates[0].RepoID != "repository:r_a" ||
		candidates[1].RepoID != "repository:r_b" {
		t.Fatalf("candidates = %#v, want the two deduplicated, sorted repo_id objects", candidates)
	}
}

func TestServiceWorkloadAmbiguousErrorUsesAPINeutralSelectorGuidance(t *testing.T) {
	t.Parallel()

	err := serviceWorkloadAmbiguousError{Selector: "checkout"}

	message := err.Error()
	if strings.Contains(message, "--") {
		t.Fatalf("message = %q, want API-neutral selector names instead of CLI flags", message)
	}
	if !strings.Contains(message, "service_id") {
		t.Fatalf("message = %q, want service_id guidance", message)
	}
}

func TestCollectServiceWorkloadCandidatesHydratesRepositoryNames(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
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
		ServiceWorkloadSelector{ServiceName: "checkout"},
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
