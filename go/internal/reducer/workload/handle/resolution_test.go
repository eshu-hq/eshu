// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package handle

import (
	"reflect"
	"testing"
)

func TestResolveFullIDMatchesExactNode(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
	}

	got := Resolve("workload:repository:r_alpha:checkout", candidates, nil)
	if got.Outcome != OutcomeExact {
		t.Fatalf("Resolve() outcome = %q, want %q", got.Outcome, OutcomeExact)
	}
	if got.WorkloadID != "workload:repository:r_alpha:checkout" {
		t.Fatalf("Resolve() id = %q, want the exact node id", got.WorkloadID)
	}
	if len(got.CandidateRepoIDs) != 0 {
		t.Fatalf("Resolve() candidate repos = %v, want none on exact", got.CandidateRepoIDs)
	}
}

func TestResolveUniqueHandleResolvesByName(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_alpha:billing", Name: "billing", RepoID: "repository:r_alpha"},
	}

	got := Resolve("workload:checkout", candidates, nil)
	if got.Outcome != OutcomeUniqueByName {
		t.Fatalf("Resolve() outcome = %q, want %q", got.Outcome, OutcomeUniqueByName)
	}
	if got.WorkloadID != "workload:repository:r_alpha:checkout" {
		t.Fatalf("Resolve() id = %q, want the unique node id", got.WorkloadID)
	}
}

func TestResolveSpanningHandleFailsClosedWithCandidates(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
	}

	got := Resolve("workload:checkout", candidates, nil)
	if got.Outcome != OutcomeAmbiguous {
		t.Fatalf("Resolve() outcome = %q, want %q", got.Outcome, OutcomeAmbiguous)
	}
	if got.WorkloadID != "" {
		t.Fatalf("Resolve() id = %q, want empty on ambiguous: never attach to one tenant", got.WorkloadID)
	}
	wantRepos := []string{"repository:r_alpha", "repository:r_beta"}
	if !reflect.DeepEqual(got.CandidateRepoIDs, wantRepos) {
		t.Fatalf("Resolve() candidate repos = %v, want %v", got.CandidateRepoIDs, wantRepos)
	}
}

func TestResolveMissOnUnknownName(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
	}

	for _, selector := range []string{"workload:absent", "workload:repository:r_alpha:absent", "", "   "} {
		got := Resolve(selector, candidates, nil)
		if got.Outcome != OutcomeMiss {
			t.Fatalf("Resolve(%q) outcome = %q, want %q", selector, got.Outcome, OutcomeMiss)
		}
		if got.WorkloadID != "" {
			t.Fatalf("Resolve(%q) id = %q, want empty on miss", selector, got.WorkloadID)
		}
	}
}

func TestResolveRestrictsToAllowedRepositories(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
	}

	// A repository-bound scope sees only its own repositories: the same
	// handle that spans the graph resolves uniquely inside the scope.
	got := Resolve("workload:checkout", candidates, []string{"repository:r_beta"})
	if got.Outcome != OutcomeUniqueByName {
		t.Fatalf("Resolve() outcome = %q, want %q", got.Outcome, OutcomeUniqueByName)
	}
	if got.WorkloadID != "workload:repository:r_beta:checkout" {
		t.Fatalf("Resolve() id = %q, want the in-scope node id", got.WorkloadID)
	}

	// A full id outside the allowed set is a miss, not a cross-tenant read.
	got = Resolve("workload:repository:r_alpha:checkout", candidates, []string{"repository:r_beta"})
	if got.Outcome != OutcomeMiss {
		t.Fatalf("Resolve() outcome = %q, want %q", got.Outcome, OutcomeMiss)
	}
}

func TestResolveBareNameMatchesByName(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
	}

	got := Resolve("checkout", candidates, nil)
	if got.Outcome != OutcomeUniqueByName {
		t.Fatalf("Resolve() outcome = %q, want %q", got.Outcome, OutcomeUniqueByName)
	}
	if got.WorkloadID != "workload:repository:r_alpha:checkout" {
		t.Fatalf("Resolve() id = %q, want the unique node id", got.WorkloadID)
	}
}

func TestHandleNamesSkipsFullIDs(t *testing.T) {
	t.Parallel()

	got := HandleNames([]string{
		"workload:checkout",
		"workload:repository:r_alpha:checkout",
		"workload:billing",
		"workload:checkout",
		"  ",
	})
	want := []string{"billing", "checkout"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HandleNames() = %v, want %v", got, want)
	}
}

func TestResolveBatchDedupesSelectors(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
	}

	got := ResolveBatch(
		[]string{"workload:checkout", "workload:repository:r_alpha:checkout", "workload:absent"},
		candidates,
		[]string{"repository:r_alpha"},
	)
	if got["workload:checkout"].Outcome != OutcomeUniqueByName {
		t.Fatalf("batch handle outcome = %q, want unique_by_name inside the scope", got["workload:checkout"].Outcome)
	}
	if got["workload:repository:r_alpha:checkout"].Outcome != OutcomeExact {
		t.Fatalf("batch full id outcome = %q, want exact", got["workload:repository:r_alpha:checkout"].Outcome)
	}
	if got["workload:absent"].Outcome != OutcomeMiss {
		t.Fatalf("batch miss outcome = %q, want miss", got["workload:absent"].Outcome)
	}
}
