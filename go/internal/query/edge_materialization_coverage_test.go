// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

// TestEdgeMaterializationCoverageReportsKnownWriters proves the registry
// reports materialized:true with a real reason for edge types this task wires
// (CONTAINS, QUERIES_TABLE, READS_FROM, TRIGGERS, INDEXES, MIGRATES) — the
// blast-radius SqlTable branches this task keeps live (#5330 Task 1/2,
// #5345 Task 4, #5346).
func TestEdgeMaterializationCoverageReportsKnownWriters(t *testing.T) {
	t.Parallel()

	for _, edgeType := range []string{"CONTAINS", "QUERIES_TABLE", "READS_FROM", "TRIGGERS", "INDEXES", "MIGRATES"} {
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
// (#5330 Task 2). REFERENCES_TABLE joined this list in #5345: the only prior
// producer was the SqlView/SqlFunction case, which now writes READS_FROM
// instead, so REFERENCES_TABLE is honestly fenced (reserved for a future
// table-level FK edge, not yet wired). MIGRATES left this list in #5346 (the
// SqlMigration writer landed); MAPS_TO_TABLE remains unwired.
func TestEdgeMaterializationCoverageReportsDeadBranches(t *testing.T) {
	t.Parallel()

	for _, edgeType := range []string{"REFERENCES_TABLE", "MAPS_TO_TABLE"} {
		got := EdgeMaterializationCoverage(edgeType)
		if got.Materialized {
			t.Errorf("EdgeMaterializationCoverage(%q).Materialized = true, want false (no writer exists)", edgeType)
		}
		if got.Reason != "no_writer" {
			t.Errorf("EdgeMaterializationCoverage(%q).Reason = %q, want %q", edgeType, got.Reason, "no_writer")
		}
	}
}

// TestEdgeMaterializationCoverageReportsSatisfiedByMaterialized proves the
// registry reports materialized:true for SATISFIED_BY (Crossplane claim ->
// XRD) now that cypher.CrossplaneSatisfiedByEdgeWriter MERGEs it (issue
// #5347): a K8sResource row resolved against exactly one CrossplaneXRD by
// (group, kind) == (spec.group, spec.claimNames.kind).
func TestEdgeMaterializationCoverageReportsSatisfiedByMaterialized(t *testing.T) {
	t.Parallel()

	got := EdgeMaterializationCoverage("SATISFIED_BY")
	if !got.Materialized {
		t.Error("EdgeMaterializationCoverage(\"SATISFIED_BY\").Materialized = false, want true (cypher.CrossplaneSatisfiedByEdgeWriter MERGEs it)")
	}
	if got.Reason == "" || got.Reason == "no_writer" {
		t.Errorf("EdgeMaterializationCoverage(\"SATISFIED_BY\").Reason = %q, want a real reason", got.Reason)
	}
}

// TestEdgeMaterializationCoverageReportsStructuralEdgeTypes proves the
// registry reports materialized:true for the core structural edges the
// #5335 edge-materialization gate needs for blast-radius queries with no
// per-query coverage/complete disclosure field (repository, terraform_module):
// DEPENDS_ON (reducer/code_import_repo_edge.go, reducer/package_consumption_repo_edge.go)
// and REPO_CONTAINS (cypher/canonical_node_cypher.go).
func TestEdgeMaterializationCoverageReportsStructuralEdgeTypes(t *testing.T) {
	t.Parallel()

	for _, edgeType := range []string{"DEPENDS_ON", "REPO_CONTAINS"} {
		got := EdgeMaterializationCoverage(edgeType)
		if !got.Materialized {
			t.Errorf("EdgeMaterializationCoverage(%q).Materialized = false, want true", edgeType)
		}
		if got.Reason == "" {
			t.Errorf("EdgeMaterializationCoverage(%q).Reason is empty, want a real reason", edgeType)
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
	for _, want := range []string{"CONTAINS", "QUERIES_TABLE", "READS_FROM", "HAS_COLUMN", "TRIGGERS", "EXECUTES", "INDEXES", "MIGRATES", "DEPENDS_ON", "REPO_CONTAINS"} {
		if _, ok := set[want]; !ok {
			t.Errorf("MaterializedEdgeTypeSet() missing %q", want)
		}
	}
	for _, notWant := range []string{"REFERENCES_TABLE", "MAPS_TO_TABLE", "TRIGGERS_ON"} {
		if _, ok := set[notWant]; ok {
			t.Errorf("MaterializedEdgeTypeSet() unexpectedly contains %q (no writer produces it)", notWant)
		}
	}
}
