// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

// AnswerTruthClass is the prompt-facing classification of an answer's truth.
//
// It folds the two existing truth axes — TruthLevel (exact, derived, fallback)
// and TruthBasis (authoritative_graph, semantic_facts, content_index, hybrid)
// — into a single label so prompt surfaces can choose presentation and caution
// without re-implementing the capability matrix. It does not introduce a new
// truth source; it is derived entirely from an existing TruthEnvelope. The
// mapping is documented in docs/public/reference/answer-packets.md.
type AnswerTruthClass string

const (
	// AnswerTruthDeterministic marks authoritative graph truth: an exact
	// TruthLevel with an authoritative_graph basis. Safe to present as fact.
	AnswerTruthDeterministic AnswerTruthClass = "deterministic"
	// AnswerTruthDerived marks a deterministic result computed from indexed
	// entities, content, or relational state rather than authoritative graph
	// topology.
	AnswerTruthDerived AnswerTruthClass = "derived"
	// AnswerTruthFallback marks an exploratory result that is useful but not
	// authoritative for the capability.
	AnswerTruthFallback AnswerTruthClass = "fallback"
	// AnswerTruthSemanticObservation marks durable semantic truth from facts
	// (an exact TruthLevel with a semantic_facts basis) rather than graph
	// topology.
	AnswerTruthSemanticObservation AnswerTruthClass = "semantic_observation"
	// AnswerTruthCodeHint marks a content-index or search signal: a hint, not a
	// verified relationship.
	AnswerTruthCodeHint AnswerTruthClass = "code_hint"
	// AnswerTruthUnsupported marks an answer with no truth to classify, built
	// from an ErrorEnvelope or missing required evidence.
	AnswerTruthUnsupported AnswerTruthClass = "unsupported"
)

// AnswerPacket is an evidence-backed, user-ready response plan composed from
// existing query truth. It is a view over the canonical ResponseEnvelope, not a
// replacement: ResultRef and Result point at the envelope data, and Truth is a
// copy of the envelope's TruthEnvelope. The packet exists so prompt surfaces
// (MCP tools, CLI summaries, console answers) can present a short human answer
// while keeping the machine-readable envelope as the source of truth and the
// evidence handles addressable.
//
// The packet never carries a confident Summary while Supported is false. That
// invariant is the core contract of this type and is enforced by NewAnswerPacket.
type AnswerPacket struct {
	// PromptFamily is the canonical prompt-family / capability identifier the
	// packet answers.
	PromptFamily string `json:"prompt_family,omitempty"`
	// Question is the canonical, normalized question the packet answers.
	Question string `json:"question,omitempty"`
	// PrimaryTool is the MCP tool or logical operation that produced the result.
	PrimaryTool string `json:"primary_tool,omitempty"`
	// PrimaryRoute is the HTTP route that produced the result.
	PrimaryRoute string `json:"primary_route,omitempty"`
	// TruthClass is the derived prompt-facing truth classification.
	TruthClass AnswerTruthClass `json:"truth_class"`
	// Summary is the human-readable answer. It is empty whenever Supported is
	// false or the answer is partial with no resolved evidence.
	Summary string `json:"summary,omitempty"`
	// Supported is false when required evidence is unavailable: the underlying
	// envelope carried an error, or no truth could be classified.
	Supported bool `json:"supported"`
	// Partial is true when the answer is usable but incomplete: truncated,
	// stale, or missing evidence.
	Partial bool `json:"partial"`
	// ResultRef references the canonical envelope payload (for example an
	// eshu:// URI). The referenced envelope remains canonical truth.
	ResultRef string `json:"result_ref,omitempty"`
	// Result is an optional compact embedded copy of the envelope Data. The
	// referenced envelope, not this copy, is canonical.
	Result any `json:"result,omitempty"`
	// Truth is a copy of the envelope's TruthEnvelope. It is the canonical truth
	// metadata for the answer and is nil only for unsupported answers built from
	// an error.
	Truth *TruthEnvelope `json:"truth,omitempty"`
	// Limitations carries bounded, human-readable caveats (limit caps, scope
	// bounds).
	Limitations []string `json:"limitations,omitempty"`
	// Truncated mirrors result-set truncation from the underlying query.
	Truncated bool `json:"truncated,omitempty"`
	// MissingEvidence lists evidence handles requested but not resolved.
	MissingEvidence []evidenceCitationHandle `json:"missing_evidence,omitempty"`
	// EvidenceHandles are addressable handles to the evidence behind the answer,
	// in the evidence_citation handle shape.
	EvidenceHandles []evidenceCitationHandle `json:"evidence_handles,omitempty"`
	// CitationRef references a citation packet that hydrates the handles.
	CitationRef string `json:"citation_ref,omitempty"`
	// RecommendedNextCalls lists bounded follow-up calls, in the same shape as
	// the evidence-citation recommended_next_calls.
	RecommendedNextCalls []map[string]any `json:"recommended_next_calls,omitempty"`
	// UnsupportedReasons explains why the answer is unsupported or partial. It
	// is non-empty whenever Supported is false or Partial is true.
	UnsupportedReasons []string `json:"unsupported_reasons,omitempty"`
}

// AnswerPacketInput carries the composition inputs for building an AnswerPacket
// from an existing ResponseEnvelope. The Envelope is required and supplies the
// canonical truth or error; the remaining fields describe how the answer was
// produced and what the caller would like to present.
type AnswerPacketInput struct {
	// PromptFamily is the canonical prompt-family / capability identifier.
	PromptFamily string
	// Question is the canonical question the answer addresses.
	Question string
	// PrimaryTool is the MCP tool or logical operation that produced the result.
	PrimaryTool string
	// PrimaryRoute is the HTTP route that produced the result.
	PrimaryRoute string
	// Summary is the proposed human-readable answer. The builder drops it when
	// the answer is unsupported or partial-with-no-evidence so an unanswerable
	// question never becomes a confident sentence.
	Summary string
	// ResultRef references the canonical envelope payload.
	ResultRef string
	// EmbedResult, when true, copies the envelope Data into the packet Result as
	// a compact embedded copy. The referenced envelope stays canonical.
	EmbedResult bool
	// Limitations carries bounded human-readable caveats to attach to the packet.
	Limitations []string
	// Truncated marks the underlying result as truncated.
	Truncated bool
	// NoEvidence signals an answerable capability that resolved no supporting
	// evidence. The zero value (false) means evidence is present or not tracked
	// for this capability. Set it true for evidence-centric answers that came
	// back empty; the builder then marks the packet partial and drops the
	// confident summary rather than presenting "no rows" as a definitive answer.
	NoEvidence bool
	// EvidenceHandles are addressable handles to the supporting evidence.
	EvidenceHandles []evidenceCitationHandle
	// MissingEvidence lists requested-but-unresolved evidence handles.
	MissingEvidence []evidenceCitationHandle
	// CitationRef references a citation packet that hydrates the handles.
	CitationRef string
	// RecommendedNextCalls lists bounded follow-up calls to surface.
	RecommendedNextCalls []map[string]any
	// Envelope is the canonical ResponseEnvelope. It supplies truth or error and
	// is required; a nil envelope yields an unsupported packet.
	Envelope *ResponseEnvelope
}

// NewAnswerPacket composes an AnswerPacket from an existing ResponseEnvelope.
//
// It takes one of two explicit paths. When the envelope carries an
// ErrorEnvelope (or is nil), the packet is unsupported: Supported is false,
// TruthClass is AnswerTruthUnsupported, the proposed Summary is dropped, and
// UnsupportedReasons records the error. When the envelope carries a
// TruthEnvelope and no error, the packet is supported and the truth is
// classified. If the underlying result is truncated, stale, or has no resolved
// evidence, the packet is marked Partial and, for the no-evidence case, the
// Summary is dropped so "no rows" is never presented as a confident answer.
func NewAnswerPacket(in AnswerPacketInput) AnswerPacket {
	packet := AnswerPacket{
		PromptFamily:         strings.TrimSpace(in.PromptFamily),
		Question:             strings.TrimSpace(in.Question),
		PrimaryTool:          strings.TrimSpace(in.PrimaryTool),
		PrimaryRoute:         strings.TrimSpace(in.PrimaryRoute),
		ResultRef:            strings.TrimSpace(in.ResultRef),
		Limitations:          in.Limitations,
		EvidenceHandles:      in.EvidenceHandles,
		MissingEvidence:      in.MissingEvidence,
		CitationRef:          strings.TrimSpace(in.CitationRef),
		RecommendedNextCalls: in.RecommendedNextCalls,
		Truncated:            in.Truncated,
	}

	if errEnv := envelopeError(in.Envelope); errEnv != nil {
		return finalizeUnsupported(packet, errEnv)
	}

	truth := envelopeTruth(in.Envelope)
	if truth == nil {
		return finalizeUnsupported(packet, &ErrorEnvelope{
			Code:    ErrorCodeInternalError,
			Message: "response envelope carried neither truth nor error",
		})
	}

	packet.Truth = cloneTruthEnvelope(truth)
	packet.TruthClass = classifyAnswerTruth(truth)
	packet.Supported = true
	if in.EmbedResult && in.Envelope != nil {
		packet.Result = in.Envelope.Data
	}

	hasEvidence := !in.NoEvidence || len(in.EvidenceHandles) > 0
	markPartial(&packet, truth, hasEvidence, len(in.MissingEvidence) > 0)

	// A confident summary survives only when the answer is fully usable, or
	// partial yet still backed by resolved evidence. A partial answer with no
	// resolved evidence must not present a confident summary.
	if !packet.Partial || hasEvidence {
		packet.Summary = strings.TrimSpace(in.Summary)
	}
	return packet
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

// ClassifyAnswerTruth is the exported wrapper over the canonical answer-truth
// classifier. It lets sibling packages (for example the answer-quality
// scorecard) re-derive the prompt-facing AnswerTruthClass a TruthEnvelope should
// map to, so they can detect a section whose serialized truth class was upgraded
// relative to its envelope without duplicating the mapping.
func ClassifyAnswerTruth(truth *TruthEnvelope) AnswerTruthClass {
	return classifyAnswerTruth(truth)
}

// classifyAnswerTruth folds an existing TruthEnvelope into a single prompt-facing
// AnswerTruthClass. A nil envelope means there is no truth to classify and maps
// to AnswerTruthUnsupported. The ordered rules are documented in
// docs/public/reference/answer-packets.md.
func classifyAnswerTruth(truth *TruthEnvelope) AnswerTruthClass {
	if truth == nil {
		return AnswerTruthUnsupported
	}
	switch {
	case truth.Basis == TruthBasisSemanticFacts && truth.Level == TruthLevelExact:
		return AnswerTruthSemanticObservation
	case truth.Basis == TruthBasisAuthoritativeGraph && truth.Level == TruthLevelExact:
		return AnswerTruthDeterministic
	case truth.Basis == TruthBasisContentIndex && truth.Level != TruthLevelExact:
		return AnswerTruthCodeHint
	case truth.Level == TruthLevelFallback:
		return AnswerTruthFallback
	default:
		return AnswerTruthDerived
	}
}

// finalizeUnsupported stamps an unsupported packet from an error envelope: it
// clears the proposed summary, sets the unsupported truth class, and records the
// error as an unsupported reason.
func finalizeUnsupported(packet AnswerPacket, errEnv *ErrorEnvelope) AnswerPacket {
	packet.Supported = false
	packet.Partial = false
	packet.Summary = ""
	packet.Truth = nil
	packet.TruthClass = AnswerTruthUnsupported
	packet.UnsupportedReasons = appendReason(packet.UnsupportedReasons, unsupportedReason(errEnv))
	return packet
}

// markPartial sets Partial and records the reasons when a supported answer is
// incomplete: stale or building freshness, an unavailable backend, truncation,
// unresolved evidence handles, or no resolved evidence. When the freshness
// carries a proven cause, the cause is folded into the partial reasons and its
// bounded next check is surfaced as a recommended next call, so the packet
// explains WHY the answer lags and WHERE to drill in.
func markPartial(packet *AnswerPacket, truth *TruthEnvelope, hasEvidence bool, missingEvidence bool) {
	if packet.Truncated {
		packet.Partial = true
		packet.UnsupportedReasons = appendReason(packet.UnsupportedReasons,
			"result truncated; not all evidence is included")
	}
	if missingEvidence {
		packet.Partial = true
		packet.UnsupportedReasons = appendReason(packet.UnsupportedReasons,
			"some requested evidence could not be resolved")
	}
	switch truth.Freshness.State {
	case FreshnessStale:
		packet.Partial = true
		packet.UnsupportedReasons = appendReason(packet.UnsupportedReasons,
			freshnessReason("underlying data is stale", truth.Freshness.Cause))
	case FreshnessBuilding:
		packet.Partial = true
		packet.UnsupportedReasons = appendReason(packet.UnsupportedReasons,
			freshnessReason("underlying index is still building", truth.Freshness.Cause))
	}
	surfaceFreshnessNextCheck(packet, truth.Freshness)
	if !hasEvidence {
		packet.Partial = true
		packet.UnsupportedReasons = appendReason(packet.UnsupportedReasons,
			"no supporting evidence resolved for this question")
	}
}

// freshnessReason augments a base freshness reason with a proven cause when one
// is present, keeping the base text intact when the cause is unset. It never
// invents a cause; an empty or invalid cause leaves the base reason unchanged.
func freshnessReason(base string, cause FreshnessCause) string {
	if !ValidFreshnessCause(cause) {
		return base
	}
	return fmt.Sprintf("%s (cause: %s)", base, cause)
}

// surfaceFreshnessNextCheck appends the freshness next check to the packet's
// recommended next calls when the freshness carries a proven cause and check. It
// de-duplicates against existing calls so a citation-derived call and a
// freshness drilldown do not collide.
func surfaceFreshnessNextCheck(packet *AnswerPacket, freshness TruthFreshness) {
	if !ValidFreshnessCause(freshness.Cause) || freshness.NextCheck == nil {
		return
	}
	call := freshness.NextCheck.asRecommendedNextCall()
	if len(call) == 0 {
		return
	}
	for _, existing := range packet.RecommendedNextCalls {
		if recommendedCallsEqual(existing, call) {
			return
		}
	}
	packet.RecommendedNextCalls = append(packet.RecommendedNextCalls, call)
}

// recommendedCallsEqual compares two recommended-next-call maps by their tool,
// route, and reason so a freshness drilldown is not appended twice.
func recommendedCallsEqual(a, b map[string]any) bool {
	for _, key := range []string{"tool", "route", "reason"} {
		if fmt.Sprintf("%v", a[key]) != fmt.Sprintf("%v", b[key]) {
			return false
		}
	}
	return true
}

func envelopeError(env *ResponseEnvelope) *ErrorEnvelope {
	if env == nil {
		return nil
	}
	return env.Error
}

func envelopeTruth(env *ResponseEnvelope) *TruthEnvelope {
	if env == nil {
		return nil
	}
	return env.Truth
}

func cloneTruthEnvelope(truth *TruthEnvelope) *TruthEnvelope {
	if truth == nil {
		return nil
	}
	cloned := *truth
	return &cloned
}

func unsupportedReason(errEnv *ErrorEnvelope) string {
	if errEnv == nil {
		return "answer unsupported"
	}
	msg := strings.TrimSpace(errEnv.Message)
	if msg == "" {
		return string(errEnv.Code)
	}
	if errEnv.Code == "" {
		return msg
	}
	return fmt.Sprintf("%s: %s", errEnv.Code, msg)
}

func appendReason(reasons []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
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
