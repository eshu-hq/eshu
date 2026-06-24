// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package truth

import (
	"math"
	"testing"
)

func TestCitationValidateRejectsNegativeByteRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cite    Citation
		wantErr bool
	}{
		{
			name: "line only citation is valid",
			cite: Citation{RepoID: "repo", RelativePath: "a.go", StartLine: 1, EndLine: 4},
		},
		{
			name: "byte offset citation is valid",
			cite: Citation{RepoID: "repo", RelativePath: "a.go", ByteOffset: 10, ByteLength: 5},
		},
		{
			name:    "negative byte offset is rejected",
			cite:    Citation{RepoID: "repo", RelativePath: "a.go", ByteOffset: -1},
			wantErr: true,
		},
		{
			name:    "negative byte length is rejected",
			cite:    Citation{RepoID: "repo", RelativePath: "a.go", ByteLength: -3},
			wantErr: true,
		},
		{
			name:    "end before start line is rejected",
			cite:    Citation{RepoID: "repo", RelativePath: "a.go", StartLine: 5, EndLine: 2},
			wantErr: true,
		},
		{
			name:    "missing locator is rejected",
			cite:    Citation{},
			wantErr: true,
		},
		{
			name: "entity locator is valid",
			cite: Citation{EntityID: "entity:x"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cite.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate() error = nil, want non-nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestEvidenceValidateBoundsConfidence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		confidence float64
		wantErr    bool
	}{
		{name: "zero confidence", confidence: 0},
		{name: "mid confidence", confidence: 0.5},
		{name: "full confidence", confidence: 1},
		{name: "above one", confidence: 1.0001, wantErr: true},
		{name: "below zero", confidence: -0.0001, wantErr: true},
		{name: "NaN", confidence: math.NaN(), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ev := Evidence{
				Kind:       "TERRAFORM_APP_REPO",
				Confidence: tc.confidence,
				Citation:   Citation{RepoID: "repo", RelativePath: "main.tf", StartLine: 1},
				Provenance: Provenance{Basis: ProvenanceBasisSourceContent},
			}
			err := ev.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("Validate() error = nil, want non-nil for confidence %v", tc.confidence)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil for confidence %v", err, tc.confidence)
			}
		})
	}
}

// TestEvidenceCarriesConfidenceAndByteCitation is the regression for #3489:
// the unified model must round-trip BOTH a confidence score AND byte-level
// citation, which none of the three former models did on its own.
func TestEvidenceCarriesConfidenceAndByteCitation(t *testing.T) {
	t.Parallel()

	ev := Evidence{
		Kind:       "HELM_CHART_REFERENCE",
		Confidence: 0.82,
		Citation: Citation{
			RepoID:       "repo-service",
			RelativePath: "charts/web/Chart.yaml",
			StartLine:    4,
			EndLine:      4,
			ByteOffset:   96,
			ByteLength:   18,
			ContentHash:  "sha256:abc",
			CommitSHA:    "deadbeef",
		},
		Provenance: Provenance{
			Basis:     ProvenanceBasisSourceContent,
			Rationale: "Helm chart metadata references the target repository",
		},
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if ev.Confidence != 0.82 {
		t.Fatalf("Confidence = %v, want 0.82", ev.Confidence)
	}
	if ev.Citation.ByteOffset != 96 || ev.Citation.ByteLength != 18 {
		t.Fatalf("byte citation = %d+%d, want 96+18", ev.Citation.ByteOffset, ev.Citation.ByteLength)
	}
	if ev.Citation.ContentHash != "sha256:abc" || ev.Citation.CommitSHA != "deadbeef" {
		t.Fatalf("hash/commit = %q/%q, want sha256:abc/deadbeef", ev.Citation.ContentHash, ev.Citation.CommitSHA)
	}
}

func TestProvenanceBasisValidate(t *testing.T) {
	t.Parallel()

	valid := []ProvenanceBasis{
		ProvenanceBasisSourceContent,
		ProvenanceBasisGraphProjection,
		ProvenanceBasisAssertion,
		ProvenanceBasisDerived,
	}
	for _, basis := range valid {
		if err := basis.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v, want nil", basis, err)
		}
	}
	if err := ProvenanceBasis("bogus").Validate(); err == nil {
		t.Fatal("Validate(bogus) error = nil, want non-nil")
	}
}
