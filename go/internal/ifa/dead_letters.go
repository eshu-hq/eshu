// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import "sort"

// DeadLetterRecord identifies one durable fact_work_items dead-letter row for
// Ifá P3 failure-path determinism (step 3a,
// docs/internal/design/4389-ifa-conformance-platform.md). It carries only the
// columns step 3a's cross-run comparison needs — the work item's identity and
// its triage classification — not the full fact_work_items row shape.
type DeadLetterRecord struct {
	// WorkItemID is the durable fact_work_items.work_item_id primary key.
	WorkItemID string `json:"work_item_id"`
	// Stage is fact_work_items.stage (for example "reducer" or "projector").
	Stage string `json:"stage"`
	// Domain is fact_work_items.domain (for example
	// "gcp_resource_materialization" or "source_local").
	Domain string `json:"domain"`
	// FailureClass is fact_work_items.failure_class. Do not assume a fixed
	// literal (e.g. "input_invalid") for a given mutation kind: which stage's
	// dead-letter path fires, and which operator-facing triage label it
	// assigns, depends on where the malformed fact is first rejected (see
	// go/internal/ifa/mutate.go's MutationKind doc comment for the two
	// empirically distinct paths a malformed gcp_cloud_resource fact can
	// take). Compare whole DeadLetterRecord sets with DeadLetterSetsEqual
	// rather than pattern-matching this field alone.
	FailureClass string `json:"failure_class"`
}

// SortDeadLetterRecords sorts records by WorkItemID so a caller renders or
// compares a dead-letter set in a deterministic order regardless of the order
// the source query or map iteration produced.
func SortDeadLetterRecords(records []DeadLetterRecord) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].WorkItemID < records[j].WorkItemID
	})
}

// DeadLetterSetsEqual reports whether a and b contain the identical set of
// DeadLetterRecord values, ignoring input order. Two sets are equal only when
// they have the same length and, after sorting both by WorkItemID, every
// record at each index matches exactly on every field: WorkItemID, Stage,
// Domain, and FailureClass all differing by even one field (for example the
// same work item dead-lettering with a different FailureClass on one run) is
// reported as a difference.
//
// This is the comparator step 3a's determinism matrix needs alongside the
// existing graph-identity comparison: it proves a malformed-fact Odù produces
// the identical dead-letter set across every worker count in the matrix, not
// just an identical graph.
func DeadLetterSetsEqual(a, b []DeadLetterRecord) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := append([]DeadLetterRecord(nil), a...)
	sortedB := append([]DeadLetterRecord(nil), b...)
	SortDeadLetterRecords(sortedA)
	SortDeadLetterRecords(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}
