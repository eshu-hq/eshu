// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestEdgeMaterializationCoverageReportsKnownWriters proves the registry
// reports materialized:true with a real reason for edge types this task wires
// (CONTAINS, QUERIES_TABLE, REFERENCES_TABLE, TRIGGERS, INDEXES) — the
// blast-radius SqlTable branches this task keeps live (#5330 Task 1/2).
func TestEdgeMaterializationCoverageReportsKnownWriters(t *testing.T) {
	t.Parallel()

	for _, edgeType := range []string{"CONTAINS", "QUERIES_TABLE", "REFERENCES_TABLE", "TRIGGERS", "INDEXES"} {
		got := EdgeMaterializationCoverage(edgeType)
		if !got.Materialized {
			t.Errorf("EdgeMaterializationCoverage(%q).Materialized = false, want true", edgeType)
		}
		if got.EdgeType != edgeType {
			t.Errorf("EdgeMaterializationCoverage(%q).EdgeType = %q, want %q", edgeType, got.EdgeType, edgeType)
		}
		if got.Reason == "" {
			t.Errorf("EdgeMaterializationCoverage(%q).Reason is empty, want a real reason", edgeType)
		}
	}
}

// TestEdgeMaterializationCoverageReportsDeadBranches proves the registry
// reports materialized:false with reason "no_writer" for the blast-radius
// SqlTable branches this task drops because no writer ever produces them
// (#5330 Task 2).
func TestEdgeMaterializationCoverageReportsDeadBranches(t *testing.T) {
	t.Parallel()

	for _, edgeType := range []string{"READS_FROM", "MIGRATES", "MAPS_TO_TABLE"} {
		got := EdgeMaterializationCoverage(edgeType)
		if got.Materialized {
			t.Errorf("EdgeMaterializationCoverage(%q).Materialized = true, want false (no writer exists)", edgeType)
		}
		if got.Reason != "no_writer" {
			t.Errorf("EdgeMaterializationCoverage(%q).Reason = %q, want %q", edgeType, got.Reason, "no_writer")
		}
	}
}

// TestMaterializedEdgeTypeSetIsRegistryDerived proves the full materialized
// set is derived from the shared writer registries (not a second
// hand-maintained list): it must include every SQL relationship write-reason
// key plus CONTAINS, and must exclude the known-dead branches.
func TestMaterializedEdgeTypeSetIsRegistryDerived(t *testing.T) {
	t.Parallel()

	set := MaterializedEdgeTypeSet()
	for _, want := range []string{"CONTAINS", "QUERIES_TABLE", "REFERENCES_TABLE", "HAS_COLUMN", "TRIGGERS", "EXECUTES", "INDEXES"} {
		if _, ok := set[want]; !ok {
			t.Errorf("MaterializedEdgeTypeSet() missing %q", want)
		}
	}
	for _, notWant := range []string{"READS_FROM", "MIGRATES", "MAPS_TO_TABLE", "TRIGGERS_ON"} {
		if _, ok := set[notWant]; ok {
			t.Errorf("MaterializedEdgeTypeSet() unexpectedly contains %q (no writer produces it)", notWant)
		}
	}
}
