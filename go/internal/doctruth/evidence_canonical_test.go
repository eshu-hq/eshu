// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

func TestVerificationFindingCanonicalCarriesConfidenceAndCitation(t *testing.T) {
	t.Parallel()

	finding := VerificationFinding{
		FindingID:        "finding:x",
		FindingType:      documentationClaimVerificationFindingType,
		Status:           VerificationStatusValid,
		TruthLevel:       string(TruthLevelExact),
		FreshnessState:   string(FreshnessFresh),
		SourceID:         "source:docs",
		DocumentID:       "doc:readme",
		SectionID:        "line:42",
		ClaimID:          "claim:abc",
		ClaimType:        ClaimTypeCLICommand,
		ClaimText:        "eshu docs verify",
		NormalizedClaim:  "docs verify",
		Summary:          "cli claim valid",
		EvidencePacketID: "doc-packet:x",
	}

	ev := finding.Canonical("sha256:excerpt")
	if err := ev.Validate(); err != nil {
		t.Fatalf("Canonical().Validate() error = %v, want nil", err)
	}
	if ev.Confidence <= 0 || ev.Confidence > 1 {
		t.Fatalf("Confidence = %v, want within (0,1]", ev.Confidence)
	}
	if ev.Citation.EntityID != "doc:readme" {
		t.Fatalf("Citation.EntityID = %q, want doc:readme", ev.Citation.EntityID)
	}
	if ev.Citation.ContentHash != "sha256:excerpt" {
		t.Fatalf("Citation.ContentHash = %q, want sha256:excerpt", ev.Citation.ContentHash)
	}
	if ev.Provenance.Basis != truth.ProvenanceBasisDerived {
		t.Fatalf("Provenance.Basis = %q, want derived", ev.Provenance.Basis)
	}
	if ev.Provenance.Rationale != finding.Summary {
		t.Fatalf("Provenance.Rationale = %q, want %q", ev.Provenance.Rationale, finding.Summary)
	}
}

func TestVerificationFindingCanonicalDerivedHasLowerConfidence(t *testing.T) {
	t.Parallel()

	base := VerificationFinding{DocumentID: "doc:a", Summary: "s"}
	exact := base
	exact.TruthLevel = string(TruthLevelExact)
	derived := base
	derived.TruthLevel = string(TruthLevelDerived)

	if got := exact.Canonical("").Confidence; got <= derived.Canonical("").Confidence {
		t.Fatalf("exact confidence %v must exceed derived confidence %v", got, derived.Canonical("").Confidence)
	}
}

func TestVerificationFindingCanonicalCarriesByteWindow(t *testing.T) {
	t.Parallel()

	finding := VerificationFinding{
		DocumentID:      "doc:readme",
		Summary:         "cli claim valid",
		TruthLevel:      string(TruthLevelExact),
		ClaimByteOffset: 42,
		ClaimByteLength: 16,
	}

	ev := finding.Canonical("sha256:excerpt")
	if ev.Citation.ByteOffset != 42 {
		t.Fatalf("Citation.ByteOffset = %d, want 42", ev.Citation.ByteOffset)
	}
	if ev.Citation.ByteLength != 16 {
		t.Fatalf("Citation.ByteLength = %d, want 16", ev.Citation.ByteLength)
	}
}

func TestVerificationFindingCanonicalZeroByteWindowIsAbsent(t *testing.T) {
	t.Parallel()

	// When byte offset/length are both zero the citation must still validate and
	// the byte window fields should remain zero (not fabricated).
	finding := VerificationFinding{
		DocumentID: "doc:readme",
		Summary:    "s",
		TruthLevel: string(TruthLevelExact),
	}

	ev := finding.Canonical("sha256:h")
	if ev.Citation.ByteOffset != 0 {
		t.Fatalf("Citation.ByteOffset = %d, want 0 when absent", ev.Citation.ByteOffset)
	}
	if ev.Citation.ByteLength != 0 {
		t.Fatalf("Citation.ByteLength = %d, want 0 when absent", ev.Citation.ByteLength)
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil for zero byte window", err)
	}
}
