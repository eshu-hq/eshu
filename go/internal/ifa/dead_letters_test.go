// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import "testing"

func TestDeadLetterSetsEqualPassesOnIdenticalSets(t *testing.T) {
	a := []DeadLetterRecord{
		{WorkItemID: "wi-2", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
		{WorkItemID: "wi-1", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
	}
	// Same records, different input order and a fresh slice — order and
	// slice identity must not matter.
	b := []DeadLetterRecord{
		{WorkItemID: "wi-1", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
		{WorkItemID: "wi-2", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
	}

	if !DeadLetterSetsEqual(a, b) {
		t.Fatalf("DeadLetterSetsEqual() = false for identical sets in different order, want true")
	}
}

func TestDeadLetterSetsEqualFailsOnFailureClassDivergence(t *testing.T) {
	a := []DeadLetterRecord{
		{WorkItemID: "wi-1", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
	}
	// Same work item, same stage, same domain — but a divergent failure_class,
	// the exact shape a racy dead-letter path (the ADR's "teeth test") would
	// produce on one worker count and not another.
	b := []DeadLetterRecord{
		{WorkItemID: "wi-1", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "projection_bug"},
	}

	if DeadLetterSetsEqual(a, b) {
		t.Fatalf("DeadLetterSetsEqual() = true for sets differing by one failure_class, want false")
	}
}

func TestDeadLetterSetsEqualFailsOnMissingWorkItem(t *testing.T) {
	a := []DeadLetterRecord{
		{WorkItemID: "wi-1", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
		{WorkItemID: "wi-2", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
	}
	b := []DeadLetterRecord{
		{WorkItemID: "wi-1", Stage: "reducer", Domain: "gcp_resource_materialization", FailureClass: "input_invalid"},
	}

	if DeadLetterSetsEqual(a, b) {
		t.Fatalf("DeadLetterSetsEqual() = true for sets of different length, want false")
	}
}

func TestDeadLetterSetsEqualPassesOnBothEmpty(t *testing.T) {
	if !DeadLetterSetsEqual(nil, []DeadLetterRecord{}) {
		t.Fatalf("DeadLetterSetsEqual(nil, []) = false, want true (both denote a drained, dead-letter-free queue)")
	}
}

func TestSortDeadLetterRecordsIsDeterministic(t *testing.T) {
	records := []DeadLetterRecord{
		{WorkItemID: "wi-3"},
		{WorkItemID: "wi-1"},
		{WorkItemID: "wi-2"},
	}
	SortDeadLetterRecords(records)
	want := []string{"wi-1", "wi-2", "wi-3"}
	for i, w := range want {
		if records[i].WorkItemID != w {
			t.Fatalf("records[%d].WorkItemID = %q, want %q", i, records[i].WorkItemID, w)
		}
	}
}
