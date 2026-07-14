// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import "testing"

func TestValidateRepoDependencyIfaSnapshotRejectsWrongOrUnexpectedTruth(t *testing.T) {
	t.Parallel()
	want := []string{"repository:source-01\x00PROVISIONS_DEPENDENCY_FOR\x00repository:target-hub"}
	valid := repoDependencyIfaSnapshot{
		typedEdges: []string{want[0]},
		nodeCounts: map[string]int{
			"Repository":       11,
			"EvidenceArtifact": 1,
			"Environment":      1,
		},
		relationshipCounts: map[string]int{
			"PROVISIONS_DEPENDENCY_FOR":         1,
			"HAS_DEPLOYMENT_EVIDENCE":           1,
			"EVIDENCES_REPOSITORY_RELATIONSHIP": 1,
			"TARGETS_ENVIRONMENT":               1,
		},
	}
	if err := validateRepoDependencyIfaSnapshot(valid, want); err != nil {
		t.Fatalf("valid snapshot: %v", err)
	}

	wrong := valid
	wrong.typedEdges = []string{"repository:source-01\x00PROVISIONS_DEPENDENCY_FOR\x00repository:wrong"}
	if err := validateRepoDependencyIfaSnapshot(wrong, want); err == nil {
		t.Fatal("wrong target mapping passed exact snapshot validation")
	}

	unexpected := valid
	unexpected.relationshipCounts = map[string]int{
		"PROVISIONS_DEPENDENCY_FOR":         1,
		"HAS_DEPLOYMENT_EVIDENCE":           1,
		"EVIDENCES_REPOSITORY_RELATIONSHIP": 1,
		"TARGETS_ENVIRONMENT":               1,
		"UNEXPECTED_FIXTURE_EDGE":           1,
	}
	if err := validateRepoDependencyIfaSnapshot(unexpected, want); err == nil {
		t.Fatal("unexpected fixture relationship passed exact snapshot validation")
	}

	orphan := valid
	orphan.nodeCounts = map[string]int{
		"Repository":       11,
		"EvidenceArtifact": 2,
		"Environment":      1,
	}
	if err := validateRepoDependencyIfaSnapshot(orphan, want); err == nil {
		t.Fatal("orphan fixture artifact passed exact snapshot validation")
	}
}
