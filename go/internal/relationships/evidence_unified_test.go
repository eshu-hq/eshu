// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

func TestEvidenceFactToCanonicalCarriesConfidenceAndCitation(t *testing.T) {
	t.Parallel()

	fact := EvidenceFact{
		EvidenceKind:     EvidenceKindHelmChart,
		RelationshipType: RelDeploysFrom,
		SourceRepoID:     "repo-platform",
		TargetRepoID:     "repo-service",
		Confidence:       0.8,
		Rationale:        "Helm chart metadata references the target repository",
		Details: map[string]any{
			"path":          "charts/web/Chart.yaml",
			"matched_value": "repo-service",
			"start_line":    float64(4),
			"end_line":      float64(4),
			"content_hash":  "sha256:abc",
			"commit_sha":    "deadbeef",
		},
	}

	ev := fact.Canonical()
	if err := ev.Validate(); err != nil {
		t.Fatalf("Canonical().Validate() error = %v, want nil", err)
	}
	if ev.Kind != string(EvidenceKindHelmChart) {
		t.Fatalf("Kind = %q, want %q", ev.Kind, EvidenceKindHelmChart)
	}
	if ev.Confidence != 0.8 {
		t.Fatalf("Confidence = %v, want 0.8", ev.Confidence)
	}
	if ev.Citation.RepoID != "repo-platform" {
		t.Fatalf("Citation.RepoID = %q, want repo-platform", ev.Citation.RepoID)
	}
	if ev.Citation.RelativePath != "charts/web/Chart.yaml" {
		t.Fatalf("Citation.RelativePath = %q, want charts/web/Chart.yaml", ev.Citation.RelativePath)
	}
	if ev.Citation.StartLine != 4 || ev.Citation.EndLine != 4 {
		t.Fatalf("Citation line range = %d-%d, want 4-4", ev.Citation.StartLine, ev.Citation.EndLine)
	}
	if ev.Citation.ContentHash != "sha256:abc" || ev.Citation.CommitSHA != "deadbeef" {
		t.Fatalf("hash/commit = %q/%q, want sha256:abc/deadbeef", ev.Citation.ContentHash, ev.Citation.CommitSHA)
	}
	if ev.Provenance.Basis != truth.ProvenanceBasisSourceContent {
		t.Fatalf("Provenance.Basis = %q, want source_content", ev.Provenance.Basis)
	}
	if ev.Provenance.Rationale != fact.Rationale {
		t.Fatalf("Provenance.Rationale = %q, want %q", ev.Provenance.Rationale, fact.Rationale)
	}
}

func TestEvidenceFactToCanonicalWithoutPathUsesEntityLocator(t *testing.T) {
	t.Parallel()

	fact := EvidenceFact{
		EvidenceKind:   EvidenceKindGCPCloudRelationship,
		SourceRepoID:   "repo-a",
		TargetRepoID:   "repo-b",
		SourceEntityID: "entity:resource:bucket",
		Confidence:     0.6,
		Rationale:      "GCP cloud relationship",
	}

	ev := fact.Canonical()
	if err := ev.Validate(); err != nil {
		t.Fatalf("Canonical().Validate() error = %v, want nil", err)
	}
	if ev.Citation.EntityID != "entity:resource:bucket" {
		t.Fatalf("Citation.EntityID = %q, want entity:resource:bucket", ev.Citation.EntityID)
	}
}

func TestEvidenceFactCanonicalClampsConfidence(t *testing.T) {
	t.Parallel()

	fact := EvidenceFact{
		EvidenceKind: EvidenceKindTerraformAppRepo,
		SourceRepoID: "repo-a",
		Confidence:   1.7,
		Details:      map[string]any{"path": "main.tf"},
	}
	ev := fact.Canonical()
	if ev.Confidence != 1 {
		t.Fatalf("Confidence = %v, want clamped to 1", ev.Confidence)
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
