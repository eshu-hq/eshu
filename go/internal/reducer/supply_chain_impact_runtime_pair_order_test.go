// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestUniqueServiceWorkloadPairsCanonicalizesPermutationAndDecisionReason(t *testing.T) {
	t.Parallel()

	first := []SupplyChainServiceWorkloadPair{
		{ServiceID: " service-b ", WorkloadID: "workload-b"},
		{ServiceID: "service-a", WorkloadID: "workload-c"},
		{ServiceID: "service-a", WorkloadID: "workload-a"},
		{ServiceID: "service-a", WorkloadID: "workload-a"},
	}
	second := []SupplyChainServiceWorkloadPair{
		{ServiceID: "service-a", WorkloadID: "workload-a"},
		{ServiceID: "service-a", WorkloadID: "workload-c"},
		{ServiceID: "service-b", WorkloadID: "workload-b"},
	}
	want := []SupplyChainServiceWorkloadPair{
		{ServiceID: "service-a", WorkloadID: "workload-a"},
		{ServiceID: "service-a", WorkloadID: "workload-c"},
		{ServiceID: "service-b", WorkloadID: "workload-b"},
	}

	gotFirst := uniqueServiceWorkloadPairs(first)
	gotSecond := uniqueServiceWorkloadPairs(second)
	if !slices.Equal(gotFirst, want) {
		t.Fatalf("first permutation = %#v, want canonical %#v", gotFirst, want)
	}
	if !slices.Equal(gotSecond, want) {
		t.Fatalf("second permutation = %#v, want canonical %#v", gotSecond, want)
	}

	decisionFor := func(pairs []SupplyChainServiceWorkloadPair) SupplyChainSuppressionDecision {
		return EvaluateSupplyChainSuppression(
			SupplyChainImpactFinding{
				CVEID:                "CVE-2026-0597",
				WorkloadIDs:          []string{"workload-x"},
				ServiceIDs:           []string{"service-x"},
				ServiceWorkloadPairs: pairs,
			},
			[]vulnerabilitySuppression{{
				SuppressionID: "suppression-permutation",
				Source:        facts.VulnerabilitySuppressionSourcePolicy,
				Justification: facts.VulnerabilitySuppressionJustificationNotAffected,
				Scope: vulnerabilitySuppressionScope{
					CVEID:      "CVE-2026-0597",
					WorkloadID: "workload-x",
					ServiceID:  "service-x",
				},
			}},
			time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC),
		)
	}
	firstDecision := decisionFor(gotFirst)
	secondDecision := decisionFor(gotSecond)
	if firstDecision.State != SupplyChainSuppressionStateScopeMismatch {
		t.Fatalf("first decision state = %q, want %q", firstDecision.State, SupplyChainSuppressionStateScopeMismatch)
	}
	if firstDecision.Reason != secondDecision.Reason {
		t.Fatalf("permuted decision reasons differ:\nfirst:  %s\nsecond: %s", firstDecision.Reason, secondDecision.Reason)
	}
}
