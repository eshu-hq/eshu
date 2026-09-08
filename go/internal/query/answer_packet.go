// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// AnswerTruthClass is the prompt-facing classification of an answer's truth.
//
// It folds the two existing truth axes — TruthLevel (exact, derived, fallback)
// and TruthBasis (authoritative_graph, semantic_facts, content_index, hybrid,
// runtime_state, no_backend_read)
// — into a single label so prompt surfaces can choose presentation and caution
// without re-implementing the capability matrix. It does not introduce a new
// truth source; it is derived entirely from an existing TruthEnvelope. The
// mapping is documented in docs/public/reference/answer-packets.md.
type AnswerTruthClass = querycontract.AnswerTruthClass

// Answer truth-class aliases preserve the root package's wire values.
const (
	AnswerTruthDeterministic       = querycontract.AnswerTruthDeterministic
	AnswerTruthDerived             = querycontract.AnswerTruthDerived
	AnswerTruthFallback            = querycontract.AnswerTruthFallback
	AnswerTruthSemanticObservation = querycontract.AnswerTruthSemanticObservation
	AnswerTruthCodeHint            = querycontract.AnswerTruthCodeHint
	AnswerTruthUnsupported         = querycontract.AnswerTruthUnsupported
)

// AnswerPacket aliases querycontract.AnswerPacket. The implementation moved
// to querycontract for #6060; this alias keeps root callers unchanged.
type AnswerPacket = querycontract.AnswerPacket

// AnswerPacketInput aliases querycontract.AnswerPacketInput. The
// implementation moved to querycontract for #6060; this alias keeps root
// callers unchanged.
type AnswerPacketInput = querycontract.AnswerPacketInput

// NewAnswerPacket forwards to querycontract.NewAnswerPacket. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func NewAnswerPacket(in AnswerPacketInput) AnswerPacket {
	return querycontract.NewAnswerPacket(in)
}

// NewAnswerPacketFromCitations composes an AnswerPacket from an evidence
// citation response, mapping the citation packet's resolved citations, missing
// handles, truncation, and recommended next calls onto the packet. The
// evidence-citation shape is reused rather than duplicated. Explicit fields on
// the input still apply; the citation-derived fields fill the evidence slots.
func NewAnswerPacketFromCitations(in AnswerPacketInput, citation evidenceCitationResponse) AnswerPacket {
	if len(in.EvidenceHandles) == 0 {
		in.EvidenceHandles = handlesFromCitations(citation.Citations)
	}
	if len(in.MissingEvidence) == 0 {
		in.MissingEvidence = citation.MissingHandles
	}
	if len(in.RecommendedNextCalls) == 0 {
		in.RecommendedNextCalls = citation.RecommendedNextCalls
	}
	in.Truncated = in.Truncated || citation.Coverage.Truncated
	resolvedEvidence := citation.Coverage.ResolvedCount > 0 || len(citation.Citations) > 0
	if !resolvedEvidence {
		in.NoEvidence = true
	}
	return NewAnswerPacket(in)
}

// ClassifyAnswerTruth forwards to querycontract.ClassifyAnswerTruth. It lets
// sibling packages (for example the answer-quality scorecard) re-derive the
// prompt-facing AnswerTruthClass a TruthEnvelope should map to, so they can
// detect a section whose serialized truth class was upgraded relative to its
// envelope without duplicating the mapping. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func ClassifyAnswerTruth(truth *TruthEnvelope) AnswerTruthClass {
	return querycontract.ClassifyAnswerTruth(truth)
}

// cloneTruthEnvelope forwards to querycontract.CloneTruthEnvelope. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers (investigation_packet_build.go) unchanged.
func cloneTruthEnvelope(truth *TruthEnvelope) *TruthEnvelope {
	return querycontract.CloneTruthEnvelope(truth)
}

// freshnessReason forwards to querycontract.FreshnessReason. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers (investigation_packet_build.go) unchanged.
func freshnessReason(base string, cause FreshnessCause) string {
	return querycontract.FreshnessReason(base, cause)
}

// appendReason forwards to querycontract.AppendReason. The implementation
// moved to querycontract for #6060; this wrapper keeps root callers
// unchanged.
func appendReason(reasons []string, reason string) []string {
	return querycontract.AppendReason(reasons, reason)
}

func handlesFromCitations(citations []evidenceCitation) []evidenceCitationHandle {
	if len(citations) == 0 {
		return nil
	}
	handles := make([]evidenceCitationHandle, 0, len(citations))
	for _, citation := range citations {
		handles = append(handles, evidenceCitationHandle{
			Kind:           citation.Kind,
			RepoID:         citation.RepoID,
			RelativePath:   citation.RelativePath,
			EntityID:       citation.EntityID,
			EvidenceFamily: citation.EvidenceFamily,
			Reason:         citation.Reason,
			StartLine:      citation.StartLine,
			EndLine:        citation.EndLine,
		})
	}
	return handles
}
