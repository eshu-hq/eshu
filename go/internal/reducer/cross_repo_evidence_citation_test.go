// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestResolvedRelationshipEvidenceArtifactsCarriesCitationFields proves that
// resolvedRelationshipEvidenceArtifacts propagates the byte-level citation
// fields (start_line, end_line, commit_sha) from the evidence preview details
// into the artifact maps that are stored as graph node properties.
//
// This is the TDD anchor for the reducer side of issue #3636. It MUST FAIL
// before the citation projection is added to resolvedRelationshipEvidenceArtifacts.
func TestResolvedRelationshipEvidenceArtifactsCarriesCitationFields(t *testing.T) {
	t.Parallel()

	r := relationships.ResolvedRelationship{
		SourceRepoID: "repo-infra",
		TargetRepoID: "repo-payments",
		Details: map[string]any{
			"evidence_preview": []map[string]any{
				{
					"kind":       "TERRAFORM_APP_REPO",
					"confidence": 0.99,
					"details": map[string]any{
						"path":          "main.tf",
						"matched_value": "payments-service",
						"matched_alias": "payments-service",
						"extractor":     "terraform",
						"start_line":    float64(3),
						"end_line":      float64(3),
						"byte_offset":   float64(42),
						"byte_length":   float64(28),
						"commit_sha":    "abc123def456",
					},
				},
			},
		},
	}

	artifacts := resolvedRelationshipEvidenceArtifacts(r)
	if len(artifacts) == 0 {
		t.Fatalf("resolvedRelationshipEvidenceArtifacts() returned no artifacts")
	}
	art := artifacts[0]

	// start_line and end_line must be projected from the preview details.
	if got, _ := art["start_line"].(int); got != 3 {
		t.Errorf("artifact[start_line] = %v (%T), want 3 (int)", art["start_line"], art["start_line"])
	}
	if got, _ := art["end_line"].(int); got != 3 {
		t.Errorf("artifact[end_line] = %v (%T), want 3 (int)", art["end_line"], art["end_line"])
	}

	// commit_sha must be projected from the preview details.
	if got, _ := art["commit_sha"].(string); got != "abc123def456" {
		t.Errorf("artifact[commit_sha] = %q, want abc123def456", got)
	}
}

// TestResolvedRelationshipEvidenceArtifactsCitationAbsentWhenMissing proves
// that when evidence details lack citation fields, the artifact map does not
// fabricate them (safe degradation).
func TestResolvedRelationshipEvidenceArtifactsCitationAbsentWhenMissing(t *testing.T) {
	t.Parallel()

	r := relationships.ResolvedRelationship{
		SourceRepoID: "repo-infra",
		TargetRepoID: "repo-payments",
		Details: map[string]any{
			"evidence_preview": []map[string]any{
				{
					"kind":       "TERRAFORM_APP_REPO",
					"confidence": 0.99,
					"details": map[string]any{
						"path":          "main.tf",
						"matched_value": "payments-service",
						"extractor":     "terraform",
						// No citation fields.
					},
				},
			},
		},
	}

	artifacts := resolvedRelationshipEvidenceArtifacts(r)
	if len(artifacts) == 0 {
		t.Fatalf("resolvedRelationshipEvidenceArtifacts() returned no artifacts")
	}
	art := artifacts[0]

	// start_line must be absent or zero — no fabrication.
	if v, ok := art["start_line"]; ok {
		if asInt, _ := v.(int); asInt != 0 {
			t.Errorf("artifact[start_line] = %v, want absent or 0 (no fabrication)", v)
		}
	}
	// commit_sha must be absent or empty — no fabrication.
	if v, ok := art["commit_sha"]; ok {
		if s, _ := v.(string); s != "" {
			t.Errorf("artifact[commit_sha] = %q, want absent or empty (no fabrication)", s)
		}
	}
}
