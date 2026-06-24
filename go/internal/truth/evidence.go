// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package truth

import (
	"fmt"
	"math"
	"strings"
)

// Citation is a byte-level pointer into indexed source content. It carries the
// line range, the byte offset/length window, and the content/commit identity
// needed to fetch and re-verify the cited bytes. A Citation must locate at
// least one of: a repository file (RepoID plus RelativePath) or an entity
// (EntityID). Line and byte windows are optional refinements of that locator.
type Citation struct {
	// RepoID and RelativePath identify a file inside an indexed repository.
	RepoID       string `json:"repo_id,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	// EntityID identifies a parsed entity (function, resource) when the
	// citation points at an entity rather than a raw file span.
	EntityID string `json:"entity_id,omitempty"`
	// StartLine and EndLine are 1-based inclusive line bounds. Zero means the
	// span is expressed only by byte offset or covers the whole file.
	StartLine int `json:"start_line,omitempty"`
	EndLine   int `json:"end_line,omitempty"`
	// ByteOffset and ByteLength describe the cited window as a byte range into
	// the source content. ByteLength of zero with a non-zero offset means the
	// window extends to end of content; callers must treat it as "from offset".
	ByteOffset int `json:"byte_offset,omitempty"`
	ByteLength int `json:"byte_length,omitempty"`
	// ContentHash and CommitSHA pin the exact content version the byte window
	// was measured against so a consumer can detect drift.
	ContentHash string `json:"content_hash,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
}

// hasLocator reports whether the citation names a file or an entity.
func (c Citation) hasLocator() bool {
	if strings.TrimSpace(c.EntityID) != "" {
		return true
	}
	return strings.TrimSpace(c.RepoID) != "" && strings.TrimSpace(c.RelativePath) != ""
}

// Validate checks that the citation has a usable locator and non-negative,
// well-ordered line and byte windows. It does not fetch content; it only
// rejects internally inconsistent citations.
func (c Citation) Validate() error {
	if !c.hasLocator() {
		return fmt.Errorf("citation must locate a file (repo_id + relative_path) or an entity_id")
	}
	if c.StartLine < 0 || c.EndLine < 0 {
		return fmt.Errorf("citation line numbers must not be negative")
	}
	if c.StartLine > 0 && c.EndLine > 0 && c.EndLine < c.StartLine {
		return fmt.Errorf("citation end_line %d precedes start_line %d", c.EndLine, c.StartLine)
	}
	if c.ByteOffset < 0 || c.ByteLength < 0 {
		return fmt.Errorf("citation byte window must not be negative")
	}
	return nil
}

// ProvenanceBasis classifies where one piece of evidence ultimately came from.
type ProvenanceBasis string

const (
	// ProvenanceBasisSourceContent means the evidence was read directly from
	// indexed source or content bytes.
	ProvenanceBasisSourceContent ProvenanceBasis = "source_content"
	// ProvenanceBasisGraphProjection means the evidence was resolved from a
	// canonical graph projection rather than raw content.
	ProvenanceBasisGraphProjection ProvenanceBasis = "graph_projection"
	// ProvenanceBasisAssertion means a human or control-plane actor asserted
	// the evidence.
	ProvenanceBasisAssertion ProvenanceBasis = "assertion"
	// ProvenanceBasisDerived means the evidence was computed from other facts
	// rather than observed directly.
	ProvenanceBasisDerived ProvenanceBasis = "derived"
)

// Validate checks that the basis is one of the known provenance bases.
func (b ProvenanceBasis) Validate() error {
	switch b {
	case ProvenanceBasisSourceContent, ProvenanceBasisGraphProjection,
		ProvenanceBasisAssertion, ProvenanceBasisDerived:
		return nil
	default:
		return fmt.Errorf("unknown provenance basis %q", b)
	}
}

// Provenance records how a piece of evidence was obtained: its basis, the
// human-readable rationale, the originating actor (for assertions), and the
// collector or backend that produced it. It carries the explanation that the
// former free-form relationship Details map and the citation packet both
// lacked in a typed form.
type Provenance struct {
	// Basis classifies the origin (source content, graph, assertion, derived).
	Basis ProvenanceBasis `json:"basis"`
	// Rationale is the human-readable reason this evidence supports the claim.
	Rationale string `json:"rationale,omitempty"`
	// Actor names the human or control-plane principal for asserted evidence.
	Actor string `json:"actor,omitempty"`
	// Source names the collector, backend, or capability that produced it.
	Source string `json:"source,omitempty"`
}

// Validate checks that the provenance carries a known basis.
func (p Provenance) Validate() error {
	return p.Basis.Validate()
}

// Evidence is the single canonical evidence record for Eshu. It unifies the
// three former evidence shapes — relationship evidence (confidence but no byte
// citation), citation records (byte citation but no confidence), and versioned
// documentation packets — into one value that carries BOTH a bounded
// confidence score AND a byte-level Citation, plus typed Provenance. This is
// the contract issue #3489 requires: no evidence is dropped, confidence
// semantics are preserved, and citations stay accurate to the cited bytes.
type Evidence struct {
	// Kind is the evidence-kind discriminator (for example a relationship
	// EvidenceKind or a citation evidence_family). It is opaque to this package.
	Kind string `json:"kind,omitempty"`
	// Confidence is the bounded [0,1] score for this evidence.
	Confidence float64 `json:"confidence"`
	// Citation is the byte-level pointer to the cited source.
	Citation Citation `json:"citation"`
	// Provenance records how this evidence was obtained.
	Provenance Provenance `json:"provenance"`
}

// Validate checks that the evidence carries a bounded confidence, a consistent
// citation, and a known provenance basis.
func (e Evidence) Validate() error {
	if math.IsNaN(e.Confidence) || e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("evidence confidence %v must be within [0,1]", e.Confidence)
	}
	if err := e.Citation.Validate(); err != nil {
		return fmt.Errorf("evidence citation: %w", err)
	}
	if err := e.Provenance.Validate(); err != nil {
		return fmt.Errorf("evidence provenance: %w", err)
	}
	return nil
}
