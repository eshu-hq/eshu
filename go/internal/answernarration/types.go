// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answernarration

import "github.com/eshu-hq/eshu/go/internal/query"

// Reason identifies a low-cardinality narration validation outcome.
type Reason string

const (
	// ReasonUncitedFactualSentence marks factual text without approved packet
	// provenance.
	ReasonUncitedFactualSentence Reason = "uncited_factual_sentence"
	// ReasonUnsupportedConfidenceDrift marks narration that makes an unsupported
	// packet look supported.
	ReasonUnsupportedConfidenceDrift Reason = "unsupported_confidence_drift"
	// ReasonPartialSignalHidden marks narration that hides partial, truncated,
	// stale, or missing-evidence state from the source packet.
	ReasonPartialSignalHidden Reason = "partial_signal_hidden"
	// ReasonTruthClassPromotion marks narration that upgrades derived or hint
	// truth into authoritative graph truth.
	ReasonTruthClassPromotion Reason = "truth_class_promotion"
	// ReasonUnsafeOutput marks narrated text that contains publish-unsafe
	// material.
	ReasonUnsafeOutput Reason = "unsafe_output"
	// ReasonUnknownProvenance marks a provenance reference that is not allowed by
	// the source packet or citation set.
	ReasonUnknownProvenance Reason = "unknown_provenance"
	// ReasonOverLimit marks narration that exceeds configured sentence, byte, or
	// provenance-reference bounds.
	ReasonOverLimit Reason = "over_limit"
)

// SentenceKind classifies one narrated sentence so validation can apply the
// right provenance rule.
type SentenceKind string

const (
	// SentenceFactual states a fact from the answer packet or citations.
	SentenceFactual SentenceKind = "factual"
	// SentenceLimitation presents a packet limitation.
	SentenceLimitation SentenceKind = "limitation"
	// SentenceUnsupportedReason presents an unsupported or partial reason.
	SentenceUnsupportedReason SentenceKind = "unsupported_reason"
	// SentenceFreshness presents freshness state or cause.
	SentenceFreshness SentenceKind = "freshness"
	// SentenceTruthLabel presents the packet truth class or truth metadata.
	SentenceTruthLabel SentenceKind = "truth_label"
	// SentenceNextCall presents a bounded recommended next call.
	SentenceNextCall SentenceKind = "next_call"
)

// ProvenanceKind classifies the packet-owned input a narrated sentence cites.
type ProvenanceKind string

const (
	// ProvenanceCitation references an allowed hydrated citation id or the
	// packet citation reference.
	ProvenanceCitation ProvenanceKind = "citation"
	// ProvenanceLimitation references a source packet limitation.
	ProvenanceLimitation ProvenanceKind = "limitation"
	// ProvenanceUnsupportedReason references a packet unsupported or partial
	// reason.
	ProvenanceUnsupportedReason ProvenanceKind = "unsupported_reason"
	// ProvenanceFreshness references source packet freshness state.
	ProvenanceFreshness ProvenanceKind = "freshness"
	// ProvenanceTruth references the source packet truth class or truth envelope.
	ProvenanceTruth ProvenanceKind = "truth"
	// ProvenanceNextCall references a source packet recommended next call.
	ProvenanceNextCall ProvenanceKind = "next_call"
)

// ProvenanceRef points one narrated sentence back to packet-owned evidence.
type ProvenanceRef struct {
	Kind ProvenanceKind `json:"kind"`
	ID   string         `json:"id,omitempty"`
}

// Sentence is one candidate narrated sentence plus its provenance.
type Sentence struct {
	Text       string          `json:"text"`
	Kind       SentenceKind    `json:"kind"`
	Provenance []ProvenanceRef `json:"provenance,omitempty"`
}

// Narration is the candidate presentation text to validate.
type Narration struct {
	TruthClass query.AnswerTruthClass `json:"truth_class"`
	Supported  bool                   `json:"supported"`
	Partial    bool                   `json:"partial,omitempty"`
	Sentences  []Sentence             `json:"sentences"`
}

// Input carries the source answer packet and candidate narration.
type Input struct {
	Packet             query.AnswerPacket `json:"packet"`
	Response           Narration          `json:"response"`
	CitationIDs        []string           `json:"citation_ids,omitempty"`
	MaxSentences       int                `json:"max_sentences,omitempty"`
	MaxSentenceBytes   int                `json:"max_sentence_bytes,omitempty"`
	MaxRefsPerSentence int                `json:"max_refs_per_sentence,omitempty"`
}

// Finding describes one validation failure using an audit-safe reason code.
type Finding struct {
	Reason Reason `json:"reason"`
	Index  int    `json:"index,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Verdict is the pure validation result for candidate narration.
type Verdict struct {
	Valid    bool      `json:"valid"`
	Findings []Finding `json:"findings,omitempty"`
}
