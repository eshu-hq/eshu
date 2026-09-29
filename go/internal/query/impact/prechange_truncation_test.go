// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "testing"

func TestDeveloperChangePlanBlocksTruncatedEvidence(t *testing.T) {
	t.Parallel()

	impactData := map[string]any{
		"truncated":        true,
		"coverage":         map[string]any{"state": "partial", "truncated": true},
		"changed_files":    []map[string]any{{"path": "src/change.go", "status": "modified"}},
		"missing_evidence": []map[string]any{},
		"code_surface": map[string]any{
			"touched_symbols": []map[string]any{{
				"entity_type": "Function", "name": "Change", "relative_path": "src/change.go", "language": "go",
			}},
		},
	}
	data := developerChangePlanData(preChangeImpactRequest{}, impactData)
	if got := data["blocked"]; got != true {
		t.Fatalf("blocked = %#v, want true when impact evidence is truncated", got)
	}
	guidance := data["patch_guidance"].([]map[string]any)
	if len(guidance) != 1 || guidance[0]["safe"] != false {
		t.Fatalf("patch_guidance = %#v, want unsafe guidance for truncated evidence", guidance)
	}
	actions := data["actions"].([]map[string]any)
	foundBlock := false
	for _, action := range actions {
		if action["kind"] == "block_unsafe_recommendation" {
			foundBlock = true
		}
	}
	if !foundBlock {
		t.Fatalf("actions = %#v, want block_unsafe_recommendation", actions)
	}
}

func TestPreChangeCoverageMarksTruncatedEmptyDiffPartial(t *testing.T) {
	t.Parallel()

	coverage := preChangeCoverage(preChangeImpactRequest{}, map[string]any{
		"truncated": true,
		"coverage":  map[string]any{"truncated": true},
	})
	if got := coverage["state"]; got != "partial" {
		t.Fatalf("coverage.state = %#v, want partial for truncated evidence", got)
	}
}

func TestPreChangeMissingEvidenceDoesNotClaimAbsenceAfterPathCap(t *testing.T) {
	t.Parallel()

	req := preChangeImpactRequest{
		RepoID:  "repo-1",
		Changes: []preChangeFileChange{{Path: "src/change.go", Status: "modified"}},
	}
	missing := preChangeMissingEvidence(req, map[string]any{
		"coverage": map[string]any{"path_symbols_truncated": true},
	})
	if len(missing) != 1 || missing[0]["reason"] != "changed_path_lookup_truncated" {
		t.Fatalf("missing_evidence = %#v, want truncated lookup reason", missing)
	}
}

func TestPreChangeCoveragePreservesUnknownCandidatePoolStatus(t *testing.T) {
	t.Parallel()

	coverage := preChangeCoverage(preChangeImpactRequest{}, map[string]any{
		"coverage": map[string]any{"state": "partial", "candidate_pool_status": "unknown_empty_page"},
	})
	if got := coverage["state"]; got != "partial" {
		t.Fatalf("coverage.state = %#v, want partial", got)
	}
}

func TestDeveloperChangePlanBlocksUnknownCandidatePoolStatus(t *testing.T) {
	t.Parallel()

	data := developerChangePlanData(preChangeImpactRequest{}, map[string]any{
		"coverage": map[string]any{"state": "partial", "candidate_pool_status": "unknown_empty_page"},
	})
	if got := data["blocked"]; got != true {
		t.Fatalf("blocked = %#v, want true for unknown candidate-pool status", got)
	}
}
