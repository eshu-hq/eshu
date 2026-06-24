// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

func TestCitationFromFileCarriesConfidenceAndByteCitation(t *testing.T) {
	t.Parallel()

	file := FileContent{
		RepoID:       "repo-service",
		RelativePath: "cmd/api/main.go",
		CommitSHA:    "abc123",
		Content:      "package main\n\nfunc main() {\n\tstartAPI()\n}\n",
		ContentHash:  "sha256:file",
		Language:     "go",
		ArtifactType: "source",
	}
	handle := evidenceCitationHandle{
		Kind:         "file",
		RepoID:       "repo-service",
		RelativePath: "cmd/api/main.go",
		StartLine:    3,
		EndLine:      4,
		Confidence:   0.9,
		Reason:       "entry point",
	}

	cite := citationFromFile(1, handle, file)
	if cite.Confidence != 0.9 {
		t.Fatalf("Confidence = %v, want 0.9", cite.Confidence)
	}
	// Byte window must point at the cited lines inside the content.
	if cite.ByteLength <= 0 {
		t.Fatalf("ByteLength = %d, want > 0", cite.ByteLength)
	}
	got := file.Content[cite.ByteOffset : cite.ByteOffset+cite.ByteLength]
	if got != cite.Excerpt {
		t.Fatalf("byte window %q does not match excerpt %q", got, cite.Excerpt)
	}
	if cite.Provenance.Basis != string(truth.ProvenanceBasisSourceContent) {
		t.Fatalf("Provenance.Basis = %q, want source_content", cite.Provenance.Basis)
	}
}

func TestEvidenceCitationRoundTripsCanonical(t *testing.T) {
	t.Parallel()

	cite := evidenceCitation{
		CitationID:     "citation:x",
		Kind:           "file",
		EvidenceFamily: "source",
		Confidence:     0.75,
		RepoID:         "repo-service",
		RelativePath:   "cmd/api/main.go",
		StartLine:      3,
		EndLine:        4,
		ByteOffset:     14,
		ByteLength:     24,
		ContentHash:    "sha256:file",
		CommitSHA:      "abc123",
		Provenance:     evidenceCitationProvenance{Basis: string(truth.ProvenanceBasisSourceContent), Rationale: "entry point"},
		Excerpt:        "func main() {\n\tstartAPI()",
	}

	ev := cite.toCanonical()
	if err := ev.Validate(); err != nil {
		t.Fatalf("toCanonical().Validate() error = %v, want nil", err)
	}
	if ev.Confidence != 0.75 {
		t.Fatalf("Confidence = %v, want 0.75", ev.Confidence)
	}
	if ev.Citation.RepoID != "repo-service" || ev.Citation.RelativePath != "cmd/api/main.go" {
		t.Fatalf("citation locator = %q/%q", ev.Citation.RepoID, ev.Citation.RelativePath)
	}
	if ev.Citation.ByteOffset != 14 || ev.Citation.ByteLength != 24 {
		t.Fatalf("byte window = %d+%d, want 14+24", ev.Citation.ByteOffset, ev.Citation.ByteLength)
	}
	if ev.Citation.ContentHash != "sha256:file" || ev.Citation.CommitSHA != "abc123" {
		t.Fatalf("hash/commit = %q/%q", ev.Citation.ContentHash, ev.Citation.CommitSHA)
	}
	if ev.Provenance.Rationale != "entry point" {
		t.Fatalf("Provenance.Rationale = %q, want 'entry point'", ev.Provenance.Rationale)
	}
}
