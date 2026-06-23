package query

import (
	"math"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/truth"
)

// evidenceCitationProvenance is the wire shape of the canonical truth.Provenance
// carried on every citation. It records where the cited bytes came from so a
// citation now carries provenance alongside confidence and the byte window,
// closing the gap called out in issue #3489.
type evidenceCitationProvenance struct {
	Basis     string `json:"basis"`
	Rationale string `json:"rationale,omitempty"`
	Source    string `json:"source,omitempty"`
}

// excerptByteWindow locates the byte offset and length of an excerpt inside the
// original content. boundedLineExcerpt drops a single trailing newline before
// splitting, so the excerpt is a verbatim substring of content trimmed of that
// newline. Returning the real byte window keeps the citation accurate to the
// bytes rather than inventing an offset. A startLine <= 1 anchors at the first
// byte; otherwise the offset is the sum of the preceding lines' byte lengths.
func excerptByteWindow(content string, startLine int, excerpt string) (int, int) {
	if excerpt == "" {
		return 0, 0
	}
	if startLine <= 1 {
		return 0, len(excerpt)
	}
	offset := 0
	line := 1
	for offset < len(content) && line < startLine {
		next := strings.IndexByte(content[offset:], '\n')
		if next < 0 {
			return 0, len(excerpt)
		}
		offset += next + 1
		line++
	}
	if offset > len(content) {
		return 0, len(excerpt)
	}
	return offset, len(excerpt)
}

// normalizeCitationConfidence clamps a caller-supplied confidence into the
// canonical [0,1] range used by truth.Evidence.
func normalizeCitationConfidence(c float64) float64 {
	if math.IsNaN(c) || c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

// citationProvenance builds the typed provenance for a content-hydrated
// citation. Citations always read indexed source content, so the basis is
// source_content; the rationale carries the handle reason.
func citationProvenance(reason string) evidenceCitationProvenance {
	return evidenceCitationProvenance{
		Basis:     string(truth.ProvenanceBasisSourceContent),
		Rationale: strings.TrimSpace(reason),
		Source:    "postgres_content_store",
	}
}

// toCanonical projects one wire evidenceCitation into the unified truth.Evidence
// record, proving the citation packet carries BOTH confidence and a byte-level
// citation under one contract (issue #3489).
func (c evidenceCitation) toCanonical() truth.Evidence {
	basis := truth.ProvenanceBasis(c.Provenance.Basis)
	if basis.Validate() != nil {
		basis = truth.ProvenanceBasisSourceContent
	}
	return truth.Evidence{
		Kind:       c.EvidenceFamily,
		Confidence: normalizeCitationConfidence(c.Confidence),
		Citation: truth.Citation{
			RepoID:       c.RepoID,
			RelativePath: c.RelativePath,
			EntityID:     c.EntityID,
			StartLine:    c.StartLine,
			EndLine:      c.EndLine,
			ByteOffset:   c.ByteOffset,
			ByteLength:   c.ByteLength,
			ContentHash:  c.ContentHash,
			CommitSHA:    c.CommitSHA,
		},
		Provenance: truth.Provenance{
			Basis:     basis,
			Rationale: c.Provenance.Rationale,
			Source:    c.Provenance.Source,
		},
	}
}
