// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"
)

// Issue #5720: two of the three ordering paths #5644 did not cover --
// queryProvisioningRepositoryCandidates, loadProvisioningSourceChainsFromCandidates
// (feeding provisioning_source_chains), and buildGraphDependents (feeding
// dependents) -- sorted only by repository display name. Two distinct
// repositories can legitimately share a display name (the same scenario
// #5644 modeled for consumer repositories), so a comparator that leaves that
// tie unresolved lets repeated calls over unchanged data return those tied
// entries in a different relative order. repo_id is unique, so it is the
// final tiebreaker that makes each comparator a total order.

// TestBuildGraphDependentsBreaksTiesByRepoID proves buildGraphDependents
// (service_contract_helpers.go), which feeds the workload-context/service-story
// "dependents" field, orders same-named candidates by repo_id when the
// display name ties, regardless of input slice order.
func TestBuildGraphDependentsBreaksTiesByRepoID(t *testing.T) {
	t.Parallel()

	candidateA := provisioningRepositoryCandidate{RepoID: "repository:dependent-a", RepoName: "shared-name", RelationshipTypes: []string{"USES_MODULE"}}
	candidateB := provisioningRepositoryCandidate{RepoID: "repository:dependent-b", RepoName: "shared-name", RelationshipTypes: []string{"USES_MODULE"}}

	ascending := []provisioningRepositoryCandidate{candidateA, candidateB}
	reversed := []provisioningRepositoryCandidate{candidateB, candidateA}

	wantOrder := []string{"repository:dependent-a", "repository:dependent-b"}

	for name, candidates := range map[string][]provisioningRepositoryCandidate{"ascending": ascending, "reversed": reversed} {
		candidates := candidates
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dependents := buildGraphDependents(candidates)
			got := make([]string, 0, len(dependents))
			for _, dependent := range dependents {
				got = append(got, StringVal(dependent, "repo_id"))
			}
			if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
				t.Fatalf("dependent order (%s) = %v, want %v", name, got, wantOrder)
			}
		})
	}
}

// TestLoadProvisioningSourceChainsFromCandidatesBreaksTiesByRepoID proves
// loadProvisioningSourceChainsFromCandidates (deployment_trace_candidate_enrichment.go),
// which feeds provisioning_source_chains, orders same-named candidates by
// repo_id when the display name ties, regardless of input slice order.
func TestLoadProvisioningSourceChainsFromCandidatesBreaksTiesByRepoID(t *testing.T) {
	t.Parallel()

	candidateA := provisioningRepositoryCandidate{RepoID: "repository:provisioner-a", RepoName: "shared-provisioner", RelationshipTypes: []string{"USES_MODULE"}}
	candidateB := provisioningRepositoryCandidate{RepoID: "repository:provisioner-b", RepoName: "shared-provisioner", RelationshipTypes: []string{"USES_MODULE"}}

	ascending := []provisioningRepositoryCandidate{candidateA, candidateB}
	reversed := []provisioningRepositoryCandidate{candidateB, candidateA}

	wantOrder := []string{"repository:provisioner-a", "repository:provisioner-b"}

	for name, candidates := range map[string][]provisioningRepositoryCandidate{"ascending": ascending, "reversed": reversed} {
		candidates := candidates
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			chains, err := loadProvisioningSourceChainsFromCandidates(context.Background(), fakePortContentStore{}, candidates)
			if err != nil {
				t.Fatalf("loadProvisioningSourceChainsFromCandidates() error = %v", err)
			}
			got := make([]string, 0, len(chains))
			for _, chain := range chains {
				got = append(got, StringVal(chain, "repo_id"))
			}
			if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
				t.Fatalf("chain order (%s) = %v, want %v", name, got, wantOrder)
			}
		})
	}
}

// TestQueryProvisioningRepositoryCandidatesSortIsStableAcrossMapIterationOrder
// proves queryProvisioningRepositoryCandidates (deployment_trace_support_helpers.go)
// orders same-named candidates by repo_id regardless of the randomized
// iteration order of the internal `grouped` Go map it builds candidates from.
//
// The map-iteration source of the non-determinism does NOT give a broken
// (name-only) comparator even odds of passing by luck, and the odds are a
// closed form, not something to measure. This fixture's two entries never
// grow the map past one bucket, so they occupy slots 0 and 1 of an 8-slot
// group in insertion order; range starts at a start offset drawn uniformly
// from [0,8) and walks the group with wraparound, and only a start offset of
// 1 visits slot 1 before slot 0 -- so slot 0's occupant is visited first for
// 7 of the 8 possible start offsets: P(slot 0 first) = 7/8 = 0.875 exactly.
//
// The fixture below inserts candidate-b before candidate-a (`rows` lists b
// first), so b takes slot 0 and a takes slot 1. A broken (name-only)
// comparator relies entirely on range order to place a before b, which needs
// the disfavored slot-1-first draw: P(pass per call) = 1/8 exactly, so
// P(all 40 calls pass) = (1/8)^40 = 8^-40 = 1 in 1,329,227,995,784,915,872,
// 903,807,060,280,344,576 (~1 in 1.33e36). An earlier version of this
// fixture inserted candidate-a first instead, putting the broken comparator
// on the *favored* side: P(pass per call) = 7/8, so P(all 40) = (7/8)^40 ~=
// 1 in 208.8 -- weak enough that #5720 round 2 replaced that direction with
// this one. The ~1-in-205/1-in-209/1-in-211 figures that circulated across
// earlier review rounds for that discarded direction are trial-based point
// estimates of the same (7/8)^40 closed form, differing by sampling error
// rather than disagreeing about the underlying probability -- not
// independent measurements needing reconciliation; this comment states the
// closed form directly instead of re-estimating it by trial.
func TestQueryProvisioningRepositoryCandidatesSortIsStableAcrossMapIterationOrder(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"repo_id": "repository:candidate-b", "repo_name": "shared-candidate", "relationship_type": "DEPLOYS_FROM", "relationship_reason": ""},
		{"repo_id": "repository:candidate-a", "repo_name": "shared-candidate", "relationship_type": "DEPLOYS_FROM", "relationship_reason": ""},
	}
	reader := fakeGraphReader{run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return rows, nil
	}}

	wantOrder := []string{"repository:candidate-a", "repository:candidate-b"}

	const iterations = 40
	for i := 0; i < iterations; i++ {
		candidates, _, err := queryProvisioningRepositoryCandidates(context.Background(), reader, "repository:orders", 0)
		if err != nil {
			t.Fatalf("queryProvisioningRepositoryCandidates() iteration %d error = %v", i, err)
		}
		got := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			got = append(got, candidate.RepoID)
		}
		if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
			t.Fatalf("candidate order on iteration %d = %v, want %v (map-iteration order leaked through an unresolved name tie)", i, got, wantOrder)
		}
	}
}

// TestQueryProvisioningRepositoryCandidatesClampsLimitToPackageCap pins both
// branches of the self-clamp queryProvisioningRepositoryCandidates applies
// before the graph read (deployment_trace_support_helpers.go): a
// caller-supplied limit of 0 or less, and a limit above the package cap, both
// resolve to maxIndirectEvidenceSearchLimit; an in-range limit passes through
// unchanged. It also asserts the emitted Cypher always carries a literal
// `LIMIT $limit` clause, since the clamp is only meaningful if the query
// actually bounds on the clamped value. #5720 round-2 P1-1: it also pins the
// full `ORDER BY repo.name, repo.id, relationship_type, relationship_reason`
// shape -- without repo.id, LIMIT truncates two same-named repositories in
// backend-chosen (not data-determined) order, and without relationship_type
// or relationship_reason, LIMIT can silently drop one row's reason for a
// same-repo/same-type pair. #5720 round-4 P3-3: relocated here from
// deployment_trace_support_helpers_test.go, which asserts the same
// ORDER BY shape this file's other tests exist to cover.
func TestQueryProvisioningRepositoryCandidatesClampsLimitToPackageCap(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		requestedLimit int
		wantLimit      int
	}{
		{name: "zero clamps up to the package cap", requestedLimit: 0, wantLimit: maxIndirectEvidenceSearchLimit},
		{name: "negative clamps up to the package cap", requestedLimit: -5, wantLimit: maxIndirectEvidenceSearchLimit},
		{name: "above cap clamps down to the package cap", requestedLimit: maxIndirectEvidenceSearchLimit + 400, wantLimit: maxIndirectEvidenceSearchLimit},
		{name: "within cap passes through unchanged", requestedLimit: 40, wantLimit: 40},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotCypher string
			var gotLimit any
			reader := fakeGraphReader{run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				gotCypher = cypher
				gotLimit = params["limit"]
				return nil, nil
			}}

			if _, _, err := queryProvisioningRepositoryCandidates(context.Background(), reader, "repo-sample-service-api", tc.requestedLimit); err != nil {
				t.Fatalf("queryProvisioningRepositoryCandidates() error = %v, want nil", err)
			}
			if !strings.Contains(gotCypher, "LIMIT $limit") {
				t.Fatalf("cypher = %q, want a literal LIMIT $limit clause", gotCypher)
			}
			if !strings.Contains(gotCypher, "ORDER BY repo.name, repo.id, relationship_type, relationship_reason") {
				t.Fatalf("cypher = %q, want ORDER BY repo.name, repo.id, relationship_type, relationship_reason so LIMIT truncates deterministically for same-named/same-type/same-reason rows", gotCypher)
			}
			// #5720 round-2 P1-1: the query now probes one row past the
			// disclosed limit to detect truncation without a second count
			// query, so the literal LIMIT bound sent to the backend is
			// wantLimit+1; the disclosed/returned bound stays wantLimit.
			if got, want := gotLimit, tc.wantLimit+1; got != want {
				t.Fatalf("params[limit] = %#v, want %#v (wantLimit+1, since the read probes one row past the disclosed bound)", got, want)
			}
		})
	}
}
