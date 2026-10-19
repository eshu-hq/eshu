// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

type stubSuppressionHandleLookup struct {
	rows []handle.Candidate
}

func (s *stubSuppressionHandleLookup) ListWorkloadHandleCandidates(_ context.Context, _ []string) ([]handle.Candidate, error) {
	return s.rows, nil
}

type stubSuppressionScopeRepos struct {
	repos []string
}

func (s *stubSuppressionScopeRepos) ListScopeRepositoryIDs(_ context.Context, _, _ string) ([]string, error) {
	return s.repos, nil
}

func suppressionResolutionCandidates() []handle.Candidate {
	return []handle.Candidate{
		{ID: "workload:repository:r_alpha:checkout", Name: "checkout", RepoID: "repository:r_alpha"},
		{ID: "workload:repository:r_beta:checkout", Name: "checkout", RepoID: "repository:r_beta"},
		{ID: "workload:repository:r_alpha:billing", Name: "billing", RepoID: "repository:r_alpha"},
	}
}

func suppressionResolver() *handle.Resolver {
	return &handle.Resolver{
		ScopeRepos: &stubSuppressionScopeRepos{},
		Lookup:     &stubSuppressionHandleLookup{rows: suppressionResolutionCandidates()},
	}
}

func workloadScopedSuppression(id, workloadID string) vulnerabilitySuppression {
	return vulnerabilitySuppression{
		SuppressionID: id,
		Source:        chain.VulnerabilitySuppressionSourcePolicy,
		Justification: chain.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:      "CVE-2026-0200",
			WorkloadID: workloadID,
		},
	}
}

func workloadFinding(workloadIDs ...string) SupplyChainImpactFinding {
	return SupplyChainImpactFinding{
		CVEID:       "CVE-2026-0200",
		PackageID:   "pkg:npm/example",
		WorkloadIDs: workloadIDs,
	}
}

// TestResolveSuppressionWorkloadScopesLadder proves the migration T3 row
// for vulnerability.suppression: a full id applies to that node's findings,
// a unique handle applies, and a handle spanning two repositories applies
// to neither tenant.
func TestResolveSuppressionWorkloadScopesLadder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	alphaFinding := workloadFinding("workload:repository:r_alpha:checkout")
	betaFinding := workloadFinding("workload:repository:r_beta:checkout")
	billingFinding := workloadFinding("workload:repository:r_alpha:billing")

	suppressions := []vulnerabilitySuppression{
		workloadScopedSuppression("sup-full", "workload:repository:r_alpha:checkout"),
		workloadScopedSuppression("sup-unique", "workload:billing"),
		workloadScopedSuppression("sup-spanning", "workload:checkout"),
	}
	resolved, err := ResolveSuppressionWorkloadScopes(ctx, suppressions, "scope-1", "gen-1", suppressionResolver())
	if err != nil {
		t.Fatalf("ResolveSuppressionWorkloadScopes() error = %v", err)
	}
	byID := map[string]vulnerabilitySuppression{}
	for _, suppression := range resolved {
		byID[suppression.SuppressionID] = suppression
	}
	if byID["sup-unique"].Scope.WorkloadID != "workload:repository:r_alpha:billing" {
		t.Fatalf("unique handle scope = %q, want the resolved node id", byID["sup-unique"].Scope.WorkloadID)
	}
	if !byID["sup-spanning"].Scope.WorkloadUnresolved {
		t.Fatal("spanning handle scope WorkloadUnresolved = false, want fail-closed against both tenants")
	}

	fullDecision := EvaluateSupplyChainSuppression(alphaFinding, []vulnerabilitySuppression{byID["sup-full"]}, now)
	if fullDecision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("full id on alpha = %q, want not_affected", fullDecision.State)
	}
	betaFullDecision := EvaluateSupplyChainSuppression(betaFinding, []vulnerabilitySuppression{byID["sup-full"]}, now)
	// Adjacent identity (same CVE) with a mismatched workload narrows to a
	// visible scope_mismatch for audit: the finding stays visible either
	// way, which is what "never silences another tenant" means here.
	if betaFullDecision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("full id on beta = %q, want scope_mismatch: a full id never silences another tenant", betaFullDecision.State)
	}
	uniqueDecision := EvaluateSupplyChainSuppression(billingFinding, []vulnerabilitySuppression{byID["sup-unique"]}, now)
	if uniqueDecision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("unique handle on billing = %q, want not_affected", uniqueDecision.State)
	}
	for name, finding := range map[string]SupplyChainImpactFinding{"alpha": alphaFinding, "beta": betaFinding} {
		decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{byID["sup-spanning"]}, now)
		// Applies to neither tenant: the finding stays visible (active or
		// the visible scope_mismatch audit state), never hidden.
		if SupplyChainSuppressionStateIsHidden(decision.State) {
			t.Fatalf("spanning handle on %s = %q, want a visible state: applies to neither tenant", name, decision.State)
		}
	}
}

func TestResolveSuppressionWorkloadScopesNilResolverKeepsLegacy(t *testing.T) {
	t.Parallel()

	suppressions := []vulnerabilitySuppression{workloadScopedSuppression("sup-legacy", "workload:checkout")}
	got, err := ResolveSuppressionWorkloadScopes(context.Background(), suppressions, "scope-1", "gen-1", nil)
	if err != nil {
		t.Fatalf("ResolveSuppressionWorkloadScopes() error = %v", err)
	}
	if got[0].Scope.WorkloadID != "workload:checkout" || got[0].Scope.WorkloadUnresolved {
		t.Fatalf("scope = %+v, want legacy pass-through when unwired", got[0].Scope)
	}
	// Legacy string equality still hides the legacy-handle finding, exactly
	// as before the ladder: no behavior change on the unwired path.
	legacyFinding := workloadFinding("workload:checkout")
	decision := EvaluateSupplyChainSuppression(legacyFinding, got, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("legacy state = %q, want not_affected", decision.State)
	}
}
