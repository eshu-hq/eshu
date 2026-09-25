// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ask

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
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

// AnswerPacket aliases answer.AnswerPacket. The implementation moved to
// querycontract for #6060 and on to querycontract/answer for #6597; this alias
// keeps root callers and the packages outside go/internal/query unchanged.
type AnswerPacket = answer.AnswerPacket

// AnswerPacketInput aliases answer.AnswerPacketInput. The implementation
// moved to querycontract for #6060 and on to querycontract/answer for #6597;
// this alias keeps root callers unchanged.
type AnswerPacketInput = answer.AnswerPacketInput

// NewAnswerPacket forwards to answer.NewAnswerPacket. The implementation
// moved to querycontract for #6060 and on to querycontract/answer for #6597;
// this wrapper keeps root callers unchanged.
func NewAnswerPacket(in AnswerPacketInput) AnswerPacket {
	return answer.NewAnswerPacket(in)
}

// NewAnswerPacketFromCitations composes an AnswerPacket from an evidence
// citation response, mapping the citation packet's resolved citations, missing
// handles, truncation, and recommended next calls onto the packet. The
// evidence-citation shape is reused rather than duplicated. Explicit fields on
// the input still apply; the citation-derived fields fill the evidence slots.
func NewAnswerPacketFromCitations(in AnswerPacketInput, citation evidence.EvidenceCitationResponse) AnswerPacket {
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

// ClassifyAnswerTruth forwards to answer.ClassifyAnswerTruth. It lets
// sibling packages (for example the answer-quality scorecard) re-derive the
// prompt-facing AnswerTruthClass a TruthEnvelope should map to, so they can
// detect a section whose serialized truth class was upgraded relative to its
// envelope without duplicating the mapping. The implementation moved to
// querycontract for #6060 and on to querycontract/answer for #6597; this
// wrapper keeps root callers unchanged.
func ClassifyAnswerTruth(truth *querycontract.TruthEnvelope) AnswerTruthClass {
	return answer.ClassifyAnswerTruth(truth)
}

// AppendReason forwards to querycontract.AppendReason. The implementation
// moved to querycontract for #6060; this wrapper keeps root callers
// unchanged.
func AppendReason(reasons []string, reason string) []string {
	return querycontract.AppendReason(reasons, reason)
}

func handlesFromCitations(citations []evidence.EvidenceCitation) []evidence.EvidenceCitationHandle {
	if len(citations) == 0 {
		return nil
	}
	handles := make([]evidence.EvidenceCitationHandle, 0, len(citations))
	for _, citation := range citations {
		handles = append(handles, evidence.EvidenceCitationHandle{
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
