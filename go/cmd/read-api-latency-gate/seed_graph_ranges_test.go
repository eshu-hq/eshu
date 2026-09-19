// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestBulkNodeRangesCoverEveryIndexOnce guards the fix for a real gate bug
// (issue #6797): the bulk infra-label seed used
// `UNWIND range(0, $count - 1) ... CREATE`, and NornicDB evaluates a
// parameter expression as the range bound to a single element, so every
// label held ONE node instead of the intended 150,000. The bound must be
// precomputed here in Go, and split into bounded inclusive ranges because one
// 150k-row statement fails with "Txn is too big to fit into one request".
func TestBulkNodeRangesCoverEveryIndexOnce(t *testing.T) {
	ranges := bulkNodeRanges(25000, 10000)

	want := []idRange{{0, 9999}, {10000, 19999}, {20000, 24999}}
	if len(ranges) != len(want) {
		t.Fatalf("len(ranges) = %d, want %d: %v", len(ranges), len(want), ranges)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Errorf("ranges[%d] = %v, want %v", i, ranges[i], want[i])
		}
	}
}

func TestBulkNodeRangesTotalsExactlyTheRequestedCount(t *testing.T) {
	for _, total := range []int{1, 9999, 10000, 10001, 150000} {
		count := 0
		next := 0
		for _, r := range bulkNodeRanges(total, 10000) {
			if r.First != next {
				t.Fatalf("total %d: range starts at %d, want contiguous %d", total, r.First, next)
			}
			if r.Last < r.First {
				t.Fatalf("total %d: empty range %v", total, r)
			}
			count += r.Last - r.First + 1
			next = r.Last + 1
		}
		if count != total {
			t.Errorf("total %d: ranges cover %d indexes", total, count)
		}
	}
}

func TestBulkNodeRangesEmptyForNonPositiveTotal(t *testing.T) {
	for _, total := range []int{0, -5} {
		if got := bulkNodeRanges(total, 10000); len(got) != 0 {
			t.Errorf("total %d produced %d ranges, want 0", total, len(got))
		}
	}
}

// TestExpectedGraphNodeCountsAddsCorrelatedNodesPerLabel pins the count the
// post-seed verification compares against: every infra label carries
// nodesPerLabel anonymous nodes, and each IaC entity type additionally carries
// one correlated node per SeedIaCFact of that type.
func TestExpectedGraphNodeCountsAddsCorrelatedNodesPerLabel(t *testing.T) {
	facts := BuildIaCFacts("s", "g", 30) // 10 of each of the three entity types
	got := expectedGraphNodeCounts(100, facts)

	want := map[string]int{
		"TerraformResource":      110,
		"TerraformModule":        10,
		"TerraformDataSource":    10,
		"TerraformStateResource": 100,
		"K8sResource":            100,
	}
	for label, n := range want {
		if got[label] != n {
			t.Errorf("expected count for %s = %d, want %d", label, got[label], n)
		}
	}
	for _, label := range infraLabels {
		if _, ok := got[label]; !ok {
			t.Errorf("infra label %s missing from expected counts", label)
		}
	}
}

func TestGraphCountMismatchesReportsEveryShortLabel(t *testing.T) {
	expected := map[string]int{"K8sResource": 100, "HelmChart": 100, "CloudResource": 100}
	actual := map[string]int{"K8sResource": 1, "HelmChart": 100, "CloudResource": 99}

	mismatches := graphCountMismatches(expected, actual)

	if len(mismatches) != 2 {
		t.Fatalf("mismatches = %v, want exactly K8sResource and CloudResource", mismatches)
	}
}

// TestDimensionMismatchesCatchesUniquePerNodeValues is the check that would
// have caught the CASE-as-literal-text seed bug (issue #6797): the node COUNT was
// right while every node held a unique provider string, so the distinct counts
// are what has to be read back.
func TestDimensionMismatchesCatchesUniquePerNodeValues(t *testing.T) {
	if got := dimensionMismatches("K8sResource", 3, 2); len(got) != 0 {
		t.Fatalf("correct dimensions reported as mismatched: %v", got)
	}
	got := dimensionMismatches("CloudResource", 150000, 150000)
	if len(got) != 2 {
		t.Fatalf("mismatches = %v, want one for providers and one for environments", got)
	}
}
