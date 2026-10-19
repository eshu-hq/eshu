// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

type stubHandleLookup struct {
	rows []handle.Candidate
	err  error
}

func (s *stubHandleLookup) ListWorkloadHandleCandidates(_ context.Context, _ []string) ([]handle.Candidate, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

type stubHandleScopeRepos struct {
	repos []string
}

func (s *stubHandleScopeRepos) ListScopeRepositoryIDs(_ context.Context, _, _ string) ([]string, error) {
	return s.repos, nil
}

func cloudResolutionCandidates() []handle.Candidate {
	return []handle.Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
		{ID: "workload:repository:r_alpha:billing", Name: "billing", RepoID: "repository:r_alpha"},
	}
}

func cloudResolutionRow(workloadID, repoID string) map[string]any {
	row := map[string]any{
		"workload_id":        workloadID,
		"cloud_resource_uid": "arn:aws:ec2:us-east-1:123:instance/i-1",
		"environment":        "prod",
	}
	if repoID != "" {
		row["workload_repository_id"] = repoID
	}
	return row
}

func TestResolveWorkloadCloudRelationshipRowsLadder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	resolver := &handle.Resolver{
		ScopeRepos: &stubHandleScopeRepos{},
		Lookup:     &stubHandleLookup{rows: cloudResolutionCandidates()},
	}
	rows := []map[string]any{
		cloudResolutionRow("workload:repository:r_alpha:checkout", ""),
		cloudResolutionRow("workload:billing", ""),
		cloudResolutionRow("workload:checkout", ""),
		cloudResolutionRow("workload:absent", ""),
	}
	tally := newWorkloadCloudRelationshipTally()

	kept, err := ResolveWorkloadCloudRelationshipRows(ctx, rows, "scope-1", "gen-1", &tally, resolver)
	if err != nil {
		t.Fatalf("ResolveWorkloadCloudRelationshipRows() error = %v", err)
	}
	// Full id passes rung 1, unique handle rewrites to its node, miss stays
	// for the writer MATCH no-op; only the spanning handle drops.
	if len(kept) != 3 {
		t.Fatalf("kept rows = %d, want 3 (spanning handle drops)", len(kept))
	}
	byWorkload := map[string]string{}
	for _, row := range kept {
		byWorkload[anyToString(row["workload_id"])] = anyToString(row["cloud_resource_uid"])
	}
	if _, ok := byWorkload["workload:repository:r_alpha:checkout"]; !ok {
		t.Fatalf("kept workload ids = %v, want the exact full id kept", byWorkload)
	}
	if _, ok := byWorkload["workload:repository:r_alpha:billing"]; !ok {
		t.Fatalf("kept workload ids = %v, want the unique handle rewritten to its node id", byWorkload)
	}
	if _, ok := byWorkload["workload:absent"]; !ok {
		t.Fatalf("kept workload ids = %v, want the miss kept for the writer no-op", byWorkload)
	}
	if got := tally.skipped[handle.SkipReasonHandleAmbiguous]; got != 1 {
		t.Fatalf("tally[%q] = %d, want 1 for the spanning handle", handle.SkipReasonHandleAmbiguous, got)
	}
	if got := tally.skipped[workloadCloudRelationshipSkipAmbiguousAnchor]; got != 0 {
		t.Fatalf("tally[%q] = %d, want 0: the handle reason never overloads ambiguous_anchor", workloadCloudRelationshipSkipAmbiguousAnchor, got)
	}
}

func TestResolveWorkloadCloudRelationshipRowsScopeAndRepoRestriction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	newResolver := func(repos []string) *handle.Resolver {
		return &handle.Resolver{
			ScopeRepos: &stubHandleScopeRepos{repos: repos},
			Lookup:     &stubHandleLookup{rows: cloudResolutionCandidates()},
		}
	}

	// Repository-bound scope: the graph-spanning handle resolves inside the scope.
	tally := newWorkloadCloudRelationshipTally()
	kept, err := ResolveWorkloadCloudRelationshipRows(ctx,
		[]map[string]any{cloudResolutionRow("workload:checkout", "")},
		"scope-1", "gen-1", &tally, newResolver([]string{"repository:r_beta"}))
	if err != nil {
		t.Fatalf("ResolveWorkloadCloudRelationshipRows() error = %v", err)
	}
	if len(kept) != 1 || anyToString(kept[0]["workload_id"]) != "workload:repository:r_beta:checkout" {
		t.Fatalf("kept = %v, want the in-scope node id", kept)
	}

	// Explicit workload_repository_id wins over the scope set.
	tally = newWorkloadCloudRelationshipTally()
	kept, err = ResolveWorkloadCloudRelationshipRows(ctx,
		[]map[string]any{cloudResolutionRow("workload:checkout", "repository:r_beta")},
		"scope-1", "gen-1", &tally, newResolver([]string{"repository:r_alpha"}))
	if err != nil {
		t.Fatalf("ResolveWorkloadCloudRelationshipRows() error = %v", err)
	}
	if len(kept) != 1 || anyToString(kept[0]["workload_id"]) != "workload:repository:r_beta:checkout" {
		t.Fatalf("kept = %v, want the explicit-repo node id", kept)
	}
}

func TestResolveWorkloadCloudRelationshipRowsNilResolverKeepsLegacy(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{cloudResolutionRow("workload:checkout", "")}
	tally := newWorkloadCloudRelationshipTally()
	kept, err := ResolveWorkloadCloudRelationshipRows(context.Background(), rows, "scope-1", "gen-1", &tally, nil)
	if err != nil {
		t.Fatalf("ResolveWorkloadCloudRelationshipRows() error = %v", err)
	}
	if len(kept) != 1 || anyToString(kept[0]["workload_id"]) != "workload:checkout" {
		t.Fatalf("kept = %v, want legacy pass-through when unwired", kept)
	}
}
