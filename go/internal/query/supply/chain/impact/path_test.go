// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"
)

func impactPathTestRow() ExplanationRow {
	return ExplanationRow{
		Finding: FindingRow{
			FindingID:    "finding-1",
			RepositoryID: "repo-1",
			ImageRef:     "registry/app:1.0",
			WorkloadIDs:  []string{"workload-1"},
			EvidencePath: []string{"advisory"},
		},
		EvidenceFacts: []EvidenceFact{
			{
				FactID:   "fact-advisory",
				FactKind: "advisory",
				Payload:  map[string]any{"repository_id": "repo-1"},
			},
			{
				FactID:   "fact-image",
				FactKind: "image",
				Payload:  map[string]any{"digest": "sha256:abc"},
			},
		},
	}
}

// TestBuildSupplyChainImpactPath proves the impact path opens with the
// finding's evidence hops, carries the six semantic hops, and closes
// with one hop per missing reason.
func TestBuildSupplyChainImpactPath(t *testing.T) {
	t.Parallel()

	hops := buildSupplyChainImpactPath(impactPathTestRow(), []string{"workload evidence missing"})
	if len(hops) != 1+6+1 {
		t.Fatalf("len(hops) = %d, want 8 (evidence + semantic + missing)", len(hops))
	}
	if hops[0].Hop != "advisory" || hops[0].Status != "present" {
		t.Errorf("first hop = %+v, want the present advisory hop", hops[0])
	}
	if len(hops[0].EvidenceFactIDs) != 1 || hops[0].EvidenceFactIDs[0] != "fact-advisory" {
		t.Errorf("advisory hop evidence = %v", hops[0].EvidenceFactIDs)
	}
	last := hops[len(hops)-1]
	if last.Hop != "workload evidence missing" || last.Status != "missing_evidence" {
		t.Errorf("last hop = %+v, want the missing workload hop", last)
	}
	empty := buildSupplyChainImpactPath(ExplanationRow{}, nil)
	if len(empty) != 6 {
		t.Fatalf("empty rows must yield the 6 missing semantic hops, got %d", len(empty))
	}
	for _, hop := range empty {
		if hop.Status != "missing_evidence" {
			t.Errorf("empty hop = %+v, want missing_evidence", hop)
		}
	}
}

// TestSemanticSupplyChainImpactHops proves each semantic hop reports
// present only with finding or fact evidence, and otherwise names the
// matching missing reason.
func TestSemanticSupplyChainImpactHops(t *testing.T) {
	t.Parallel()

	hops := semanticSupplyChainImpactHops(impactPathTestRow(), []string{
		"repository dependency evidence missing",
		"deployment exposure evidence missing",
		"service evidence missing",
		"environment evidence missing",
		"image or SBOM attachment evidence missing",
	})
	byHop := make(map[string]PathHop, len(hops))
	for _, hop := range hops {
		byHop[hop.Hop] = hop
	}
	if len(byHop) != 6 {
		t.Fatalf("semantic hops = %d, want 6", len(byHop))
	}
	for _, hop := range []string{"repository", "image", "workload"} {
		if byHop[hop].Status != "present" {
			t.Errorf("%s hop = %+v, want present", hop, byHop[hop])
		}
	}
	if got := byHop["deployment"]; got.Status != "missing_evidence" || len(got.MissingEvidence) == 0 {
		t.Errorf("deployment hop = %+v, want missing with a reason", got)
	}
	if got := byHop["service"]; got.MissingEvidence[0] != "service evidence missing" {
		t.Errorf("service hop reason = %v", got.MissingEvidence)
	}
}

// TestSupplyChainImpactPathMissingEvidence proves only path-shaped
// reasons survive the path filter, deduplicated and sorted.
func TestSupplyChainImpactPathMissingEvidence(t *testing.T) {
	t.Parallel()

	filtered := supplyChainImpactPathMissingEvidence([]string{
		"workload evidence missing",
		"not a path reason",
		"workload evidence missing",
		"deployment evidence needs triage",
	})
	if len(filtered) != 2 {
		t.Fatalf("filtered = %v, want 2 path reasons", filtered)
	}
	if !supplyChainImpactPathMissingReason("package version evidence missing") {
		t.Error("package version gap must read as a path reason")
	}
	if supplyChainImpactPathMissingReason("not a path reason") {
		t.Error("foreign reasons must not read as path reasons")
	}
}

// TestMissingReasonForSemanticHop proves the hop matcher pairs each
// missing reason with its hop and falls back to the generic label.
func TestMissingReasonForSemanticHop(t *testing.T) {
	t.Parallel()

	if got := missingReasonForSemanticHop("image", []string{"image identity evidence stale"}); len(got) != 1 {
		t.Errorf("prefixed image reason must match: %v", got)
	}
	if got := missingReasonForSemanticHop("environment", []string{"unrelated"}); len(got) != 1 || got[0] != "environment evidence missing" {
		t.Errorf("unmatched hop must fall back to the generic label: %v", got)
	}
}
