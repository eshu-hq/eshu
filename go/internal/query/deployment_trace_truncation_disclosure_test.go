// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

// TestQueryProvisioningRepositoryCandidatesDisclosesTruncation is the #5720
// round-2 P1-1 regression. Before this fix, queryProvisioningRepositoryCandidates
// requested exactly `limit` rows, so "the backend had exactly limit matching
// rows" and "the backend had more than limit and the rest were silently
// dropped" produced an identical return value -- every caller (dependents,
// consumer_repositories, provisioning_source_chains) reported truncated:
// false in both cases. This drives a seeded fake graph reader through the
// real function and proves it now tells the two apart: it probes limit+1
// rows, trims back to limit before grouping, and returns a truncated bool
// that agrees with the returned (bounded) candidate count.
func TestQueryProvisioningRepositoryCandidatesDisclosesTruncation(t *testing.T) {
	t.Parallel()

	const limit = 3

	t.Run("more rows than limit sets truncated and caps the count", func(t *testing.T) {
		t.Parallel()
		rows := make([]map[string]any, 0, limit+1)
		for i := 0; i < limit+1; i++ {
			rows = append(rows, map[string]any{
				"repo_id":             fmt.Sprintf("repository:candidate-%02d", i),
				"repo_name":           fmt.Sprintf("candidate-%02d", i),
				"relationship_type":   "USES_MODULE",
				"relationship_reason": "",
			})
		}
		reader := fakeGraphReader{run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			if got, want := params["limit"], limit+1; got != want {
				t.Fatalf("params[limit] = %#v, want %#v (must probe one row past the caller limit to detect truncation)", got, want)
			}
			return rows, nil
		}}

		candidates, truncated, err := queryProvisioningRepositoryCandidates(context.Background(), reader, "repository:orders", limit)
		if err != nil {
			t.Fatalf("queryProvisioningRepositoryCandidates() error = %v, want nil", err)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true (backend returned limit+1 = %d rows for a limit of %d)", len(rows), limit)
		}
		if got, want := len(candidates), limit; got != want {
			t.Fatalf("len(candidates) = %d, want %d (the disclosed truncated flag and the returned length must agree)", got, want)
		}
	})

	t.Run("exactly limit rows leaves truncated false", func(t *testing.T) {
		t.Parallel()
		rows := make([]map[string]any, 0, limit)
		for i := 0; i < limit; i++ {
			rows = append(rows, map[string]any{
				"repo_id":             fmt.Sprintf("repository:candidate-%02d", i),
				"repo_name":           fmt.Sprintf("candidate-%02d", i),
				"relationship_type":   "USES_MODULE",
				"relationship_reason": "",
			})
		}
		reader := fakeGraphReader{run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			if got, want := params["limit"], limit+1; got != want {
				t.Fatalf("params[limit] = %#v, want %#v", got, want)
			}
			return rows, nil
		}}

		candidates, truncated, err := queryProvisioningRepositoryCandidates(context.Background(), reader, "repository:orders", limit)
		if err != nil {
			t.Fatalf("queryProvisioningRepositoryCandidates() error = %v, want nil", err)
		}
		if truncated {
			t.Fatalf("truncated = true, want false (backend returned exactly limit = %d rows, nothing was dropped)", limit)
		}
		if got, want := len(candidates), limit; got != want {
			t.Fatalf("len(candidates) = %d, want %d", got, want)
		}
	})
}

// TestBuildServiceDownstreamConsumersDisclosesUpstreamTruncation proves
// buildServiceDownstreamConsumers (service_story_dossier.go) reports
// truncated: true when the workload context carries a truncated signal from
// the provisioning-candidate read, even though neither returned list is long
// enough to exceed serviceStoryItemLimit (50) on its own -- the #5720
// round-2 P1-1 gap: the default indirect-evidence search limit (25) sits
// below serviceStoryItemLimit, so a query-level truncation could never
// surface through the count-vs-limit comparison alone.
func TestBuildServiceDownstreamConsumersDisclosesUpstreamTruncation(t *testing.T) {
	t.Parallel()

	baseContext := func() map[string]any {
		return map[string]any{
			"dependents":            []map[string]any{{"repo_id": "repository:dependent-1"}},
			"consumer_repositories": []map[string]any{{"repo_id": "repository:consumer-1"}},
		}
	}

	t.Run("no truncation signal reports untruncated", func(t *testing.T) {
		t.Parallel()
		got := buildServiceDownstreamConsumers(baseContext())
		if got["truncated"] != false {
			t.Fatalf("truncated = %#v, want false", got["truncated"])
		}
	})

	t.Run("dependents_truncated propagates even under the row-count cap", func(t *testing.T) {
		t.Parallel()
		workloadContext := baseContext()
		workloadContext["dependents_truncated"] = true
		got := buildServiceDownstreamConsumers(workloadContext)
		if got["truncated"] != true {
			t.Fatalf("truncated = %#v, want true (dependents_truncated must propagate)", got["truncated"])
		}
	})

	t.Run("consumer_repositories_truncated propagates even under the row-count cap", func(t *testing.T) {
		t.Parallel()
		workloadContext := baseContext()
		workloadContext["consumer_repositories_truncated"] = true
		got := buildServiceDownstreamConsumers(workloadContext)
		if got["truncated"] != true {
			t.Fatalf("truncated = %#v, want true (consumer_repositories_truncated must propagate)", got["truncated"])
		}
	})
}

// TestBuildServiceResultLimitsWithContextDisclosesUpstreamTruncation is the
// same #5720 round-2 P1-1 gap proven against result_limits.truncated: a
// workload context whose dependents/consumer_repositories/
// provisioning_source_chains counts all sit under serviceStoryItemLimit must
// still report truncated: true once any of the three upstream truncation
// signals is set.
func TestBuildServiceResultLimitsWithContextDisclosesUpstreamTruncation(t *testing.T) {
	t.Parallel()

	newBuildCtx := func(extra map[string]any) serviceStoryBuildContext {
		workloadContext := map[string]any{
			"name": "orders-api",
		}
		for key, value := range extra {
			workloadContext[key] = value
		}
		return newServiceStoryBuildContext(workloadContext)
	}

	t.Run("no truncation signal reports untruncated", func(t *testing.T) {
		t.Parallel()
		got := buildServiceResultLimitsWithContext(newBuildCtx(nil))
		if got["truncated"] != false {
			t.Fatalf("truncated = %#v, want false", got["truncated"])
		}
	})

	for _, key := range []string{"dependents_truncated", "consumer_repositories_truncated", "provisioning_source_chains_truncated"} {
		key := key
		t.Run(key+" propagates", func(t *testing.T) {
			t.Parallel()
			got := buildServiceResultLimitsWithContext(newBuildCtx(map[string]any{key: true}))
			if got["truncated"] != true {
				t.Fatalf("truncated = %#v, want true (%s must propagate)", got["truncated"], key)
			}
		})
	}
}

// TestLoadConsumerRepositoryEnrichmentFromCandidatesDisclosesTruncation
// proves loadConsumerRepositoryEnrichmentFromCandidates
// (deployment_trace_candidate_enrichment.go) reports truncation from both of
// its two independent sources: the upstream candidatesTruncated flag passed
// in from queryProvisioningRepositoryCandidates, and its own final
// consumers[:limit] cap over the merged graph-candidate/content-evidence set
// (line ~146), which can trim rows even when the graph candidates themselves
// were not truncated.
func TestLoadConsumerRepositoryEnrichmentFromCandidatesDisclosesTruncation(t *testing.T) {
	t.Parallel()

	t.Run("propagates an upstream candidatesTruncated flag", func(t *testing.T) {
		t.Parallel()
		candidates := []provisioningRepositoryCandidate{
			{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
		}
		_, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, nil, "repository:orders", "orders-api", nil, 5, candidates, true,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true (must propagate the upstream candidatesTruncated flag)")
		}
	})

	t.Run("no truncation when candidates fit and the flag is false", func(t *testing.T) {
		t.Parallel()
		candidates := []provisioningRepositoryCandidate{
			{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
		}
		_, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, nil, "repository:orders", "orders-api", nil, 5, candidates, false,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if truncated {
			t.Fatalf("truncated = true, want false")
		}
	})

	t.Run("detects its own final-cap truncation independent of the upstream flag", func(t *testing.T) {
		t.Parallel()
		candidates := []provisioningRepositoryCandidate{
			{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
			{RepoID: "repository:consumer-2", RepoName: "consumer-2", RelationshipTypes: []string{"USES_MODULE"}},
			{RepoID: "repository:consumer-3", RepoName: "consumer-3", RelationshipTypes: []string{"USES_MODULE"}},
		}
		consumers, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, nil, "repository:orders", "orders-api", nil, 2, candidates, false,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true (3 candidates capped to a limit of 2 by the function's own final cap)")
		}
		if got, want := len(consumers), 2; got != want {
			t.Fatalf("len(consumers) = %d, want %d", got, want)
		}
	})
}
