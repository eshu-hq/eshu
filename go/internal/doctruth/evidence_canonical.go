package doctruth

import (
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// Canonical projects one documentation VerificationFinding into the unified
// truth.Evidence record (issue #3489). Documentation evidence previously lived
// in a separate versioned packet model with no numeric confidence and no
// byte-level citation field; this bridge maps the finding's truth level to a
// bounded [0,1] confidence, cites the document via its identity plus the
// supplied excerpt content hash, and records derived provenance carrying the
// finding summary. The excerptHash should be the packet bounded_excerpt
// text_hash so the citation pins the exact cited bytes.
//
// When ClaimByteOffset and ClaimByteLength are both non-zero, Canonical threads
// them into truth.Citation so the citation carries a real document-absolute byte
// window (#3637). A zero ClaimByteLength means the byte position was not
// captured during extraction; in that case ByteOffset and ByteLength remain zero
// so the citation is valid via EntityID alone without fabricating a window.
//
// The VerificationFinding remains the durable documentation model. Canonical
// lets documentation evidence speak the same confidence + citation + provenance
// contract as relationship evidence and citation packets.
func (f VerificationFinding) Canonical(excerptHash string) truth.Evidence {
	citation := truth.Citation{
		ContentHash: excerptHash,
	}
	// Only set the byte window when length is non-zero. ByteLength==0 with a
	// non-zero ByteOffset is ambiguous ("from offset to end"), so we treat the
	// pair as a unit: either both are meaningful or neither is surfaced.
	if f.ClaimByteLength > 0 {
		citation.ByteOffset = f.ClaimByteOffset
		citation.ByteLength = f.ClaimByteLength
	}
	switch {
	case f.DocumentID != "":
		citation.EntityID = f.DocumentID
	case f.SourceID != "":
		citation.EntityID = f.SourceID
	case f.FindingID != "":
		citation.EntityID = f.FindingID
	}

	return truth.Evidence{
		Kind:       f.FindingType,
		Confidence: truthLevelConfidence(f.TruthLevel),
		Citation:   citation,
		Provenance: truth.Provenance{
			Basis:     truth.ProvenanceBasisDerived,
			Rationale: f.Summary,
			Source:    "documentation_verifier",
		},
	}
}

// truthLevelConfidence maps a documentation truth level to a bounded confidence
// score. Exact findings are backed by fresh comparable truth and rank highest;
// derived findings are bounded but not exact and rank lower; anything else is
// treated as weak corroboration.
func truthLevelConfidence(level string) float64 {
	switch TruthLevel(level) {
	case TruthLevelExact:
		return 1
	case TruthLevelDerived:
		return 0.5
	default:
		return 0.25
	}
}
