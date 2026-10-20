// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestBuildMaterializationMarksReprojectionOfActiveGeneration is the review r2
// F5 regression for #7978: an unchanged reconcile re-projects the ACTIVE
// generation, whose repository fact carries no reconciliation_generation
// marker, so its retractions went uncounted on
// eshu_dp_reconciliation_drift_retractions_total. The claim selects the
// generation's status: a re-projection arrives active, a first projection
// pending. An active full counts as a reconciliation; a pending generation and
// a delta do not.
func TestBuildMaterializationMarksReprojectionOfActiveGeneration(t *testing.T) {
	t.Parallel()
	repositoryFact := func(extra map[string]any) []facts.Envelope {
		payload := map[string]any{"repo_id": "repo-abc", "name": "my-project", "path": "/repos/my-project"}
		for key, value := range extra {
			payload[key] = value
		}
		// The fixture carries a live file fact on purpose: a non-first,
		// non-delta build with zero file facts of any kind is a loud
		// failure since #8657 (the file-less success would wipe prior File
		// nodes), and this test's subject is flag marking, not filelessness.
		return []facts.Envelope{
			{FactID: "r-1", ScopeID: "scope-1", FactKind: "repository", Payload: payload},
			{FactID: "f-1", ScopeID: "scope-1", FactKind: "file", Payload: map[string]any{
				"repo_id": "repo-abc", "relative_path": "src/main.go",
				"language": "go", "parsed_file_data": map[string]any{},
			}},
		}
	}
	cases := []struct {
		name   string
		status scope.GenerationStatus
		facts  []facts.Envelope
		want   bool
	}{
		{"re-projection of the active full", scope.GenerationStatusActive, repositoryFact(nil), true},
		{"first projection of a pending full", scope.GenerationStatusPending, repositoryFact(nil), false},
		{"re-projection of an active delta", scope.GenerationStatusActive, repositoryFact(map[string]any{
			"delta_generation": true, "delta_relative_paths": []string{"src/a.go"},
		}), false},
		{"stored reconciliation full, pending", scope.GenerationStatusPending, repositoryFact(map[string]any{
			"reconciliation_generation": true,
		}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sc := testScope()
			sc.PreviousGenerationExists = true
			gen := testGeneration()
			gen.Status = tc.status
			result, _, _ := BuildMaterialization(sc, gen, tc.facts)
			if result.ReconciliationProjection != tc.want {
				t.Fatalf("ReconciliationProjection = %v, want %v", result.ReconciliationProjection, tc.want)
			}
		})
	}
}
