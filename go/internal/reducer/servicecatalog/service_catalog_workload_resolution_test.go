// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

type stubCatalogHandleLookup struct {
	rows []handle.Candidate
}

func (s *stubCatalogHandleLookup) ListWorkloadHandleCandidates(_ context.Context, _ []string) ([]handle.Candidate, error) {
	return s.rows, nil
}

type stubCatalogScopeRepos struct {
	repos []string
}

func (s *stubCatalogScopeRepos) ListScopeRepositoryIDs(_ context.Context, _, _ string) ([]string, error) {
	return s.repos, nil
}

func catalogResolutionCandidates() []handle.Candidate {
	return []handle.Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
		{ID: "workload:repository:r_alpha:billing", Name: "billing", RepoID: "repository:r_alpha"},
	}
}

func catalogResolver(repos []string) *handle.Resolver {
	return &handle.Resolver{
		ScopeRepos: &stubCatalogScopeRepos{repos: repos},
		Lookup:     &stubCatalogHandleLookup{rows: catalogResolutionCandidates()},
	}
}

func TestResolveServiceCatalogWorkloadIDsLadder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	decisions := []ServiceCatalogCorrelationDecision{
		{EntityRef: "full", WorkloadID: "workload:repository:r_alpha:checkout"},
		{EntityRef: "unique", WorkloadID: "workload:billing"},
		{EntityRef: "spanning", WorkloadID: "workload:checkout"},
		{EntityRef: "miss", WorkloadID: "workload:absent"},
		{EntityRef: "noworkload"},
	}

	got, err := ResolveServiceCatalogWorkloadIDs(ctx, "scope-1", "gen-1", decisions, catalogResolver(nil))
	if err != nil {
		t.Fatalf("ResolveServiceCatalogWorkloadIDs() error = %v", err)
	}
	byRef := map[string]ServiceCatalogCorrelationDecision{}
	for _, decision := range got {
		byRef[decision.EntityRef] = decision
	}
	if byRef["full"].WorkloadID != "workload:repository:r_alpha:checkout" {
		t.Fatalf("full id decision = %q, want the node linked", byRef["full"].WorkloadID)
	}
	if byRef["unique"].WorkloadID != "workload:repository:r_alpha:billing" {
		t.Fatalf("unique handle decision = %q, want the node linked", byRef["unique"].WorkloadID)
	}
	if byRef["spanning"].WorkloadID != "" {
		t.Fatalf("spanning handle decision = %q, want no link: applies to neither tenant", byRef["spanning"].WorkloadID)
	}
	if byRef["miss"].WorkloadID != "workload:absent" {
		t.Fatalf("miss decision = %q, want the raw selector kept", byRef["miss"].WorkloadID)
	}
	if byRef["noworkload"].WorkloadID != "" {
		t.Fatalf("empty decision = %q, want untouched", byRef["noworkload"].WorkloadID)
	}
}

func TestResolveServiceCatalogWorkloadIDsExplicitRepoHint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	decisions := []ServiceCatalogCorrelationDecision{
		{EntityRef: "hinted", WorkloadID: "workload:checkout", workloadRepositoryID: "repository:r_beta"},
	}

	got, err := ResolveServiceCatalogWorkloadIDs(ctx, "scope-1", "gen-1", decisions, catalogResolver([]string{"repository:r_alpha"}))
	if err != nil {
		t.Fatalf("ResolveServiceCatalogWorkloadIDs() error = %v", err)
	}
	if got[0].WorkloadID != "workload:repository:r_beta:checkout" {
		t.Fatalf("hinted decision = %q, want the explicit-repo node linked", got[0].WorkloadID)
	}
}

func TestResolveServiceCatalogWorkloadIDsNilResolverKeepsLegacy(t *testing.T) {
	t.Parallel()

	decisions := []ServiceCatalogCorrelationDecision{
		{EntityRef: "spanning", WorkloadID: "workload:checkout"},
	}
	got, err := ResolveServiceCatalogWorkloadIDs(context.Background(), "scope-1", "gen-1", decisions, nil)
	if err != nil {
		t.Fatalf("ResolveServiceCatalogWorkloadIDs() error = %v", err)
	}
	if got[0].WorkloadID != "workload:checkout" {
		t.Fatalf("decision = %q, want legacy pass-through when unwired", got[0].WorkloadID)
	}
}
