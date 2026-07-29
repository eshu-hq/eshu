// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestEvaluateSupplyChainSuppressionMultiAnchorScopeDoesNotMatchUnverifiedCombination
// is the #5466 round-7 review P1-A (codex) fix proof: a finding whose
// evidence aggregates TWO deployments -- (stage, workload-a) and
// (prod, workload-b) -- must NOT be hidden by a suppression scoped to
// environment=stage + workload_id=workload-b, a combination that never
// occurred in either real deployment. Before this fix,
// suppressionScopeMatchesFinding checked each anchor independently against
// its own flattened list (finding.Environments=[prod,stage],
// finding.WorkloadIDs=[workload-a,workload-b]), so "stage" and
// "workload-b" each matched SOMEWHERE in their own list even though they
// never co-occurred -- a Cartesian-product false positive that hides a
// real vulnerability (over-suppression), exactly what the #5466 fail-closed
// rule exists to prevent.
func TestEvaluateSupplyChainSuppressionMultiAnchorScopeDoesNotMatchUnverifiedCombination(t *testing.T) {
	t.Parallel()

	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-cartesian",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0600",
			SubjectDigest: "sha256:cartesian-digest",
			Environment:   "stage",
			WorkloadID:    "workload-b",
		},
	}
	// The finding aggregates two DISTINCT, independently sourced
	// deployments -- applySupplyChainRuntimeContext
	// (supply_chain_impact_runtime.go) would build exactly this shape from
	// a repository with two reducer_ci_cd_run_correlation deployments
	// (stage, prod) and two reducer_workload_identity workloads
	// (workload-a, workload-b), with no fact correlating which environment
	// paired with which workload.
	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0600",
		SubjectDigest: "sha256:cartesian-digest",
		Environments:  []string{"prod", "stage"},
		WorkloadIDs:   []string{"workload-a", "workload-b"},
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, now)
	if decision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("State = %q, want %q (the finding must stay visible -- the scoped combination \"stage\"+\"workload-b\" never occurred in any single real deployment)", decision.State, SupplyChainSuppressionStateScopeMismatch)
	}
	if decision.SuppressionID != "suppression-cartesian" {
		t.Fatalf("SuppressionID = %q, want the mismatched suppression preserved for audit", decision.SuppressionID)
	}
}

// TestEvaluateSupplyChainSuppressionMultiAnchorScopeMatchesTheOnlyPossibleCombination
// is the companion proof: when the finding has EXACTLY one distinct value
// per scope-referenced dimension, there is only one possible combination,
// so the multi-anchor scope must still match -- the ambiguity guard must
// not become a blanket regression against the legitimate, unambiguous
// common case.
func TestEvaluateSupplyChainSuppressionMultiAnchorScopeMatchesTheOnlyPossibleCombination(t *testing.T) {
	t.Parallel()

	suppression := vulnerabilitySuppression{
		SuppressionID: "suppression-unambiguous-combo",
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
		AuthoredAt:    time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Scope: vulnerabilitySuppressionScope{
			CVEID:         "CVE-2026-0601",
			SubjectDigest: "sha256:unambiguous-digest",
			Environment:   "stage",
			WorkloadID:    "workload-a",
			ServiceID:     "service-a",
		},
	}
	finding := SupplyChainImpactFinding{
		CVEID:         "CVE-2026-0601",
		SubjectDigest: "sha256:unambiguous-digest",
		Environments:  []string{"stage"},
		WorkloadIDs:   []string{"workload-a"},
		ServiceIDs:    []string{"service-a"},
	}
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	decision := EvaluateSupplyChainSuppression(finding, []vulnerabilitySuppression{suppression}, now)
	if decision.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("State = %q, want %q (a single distinct value per referenced dimension is unambiguous and must still match)", decision.State, SupplyChainSuppressionStateNotAffected)
	}
}
