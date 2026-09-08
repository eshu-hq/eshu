// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"fmt"
	"strings"
)

// AnswerPacket is an evidence-backed, user-ready response plan composed from
// existing query truth. It is a view over the canonical ResponseEnvelope, not
// a replacement: ResultRef and Result point at the envelope data, and Truth
// is a copy of the envelope's TruthEnvelope. The packet exists so prompt
// surfaces (MCP tools, CLI summaries, console answers) can present a short
// human answer while keeping the machine-readable envelope as the source of
// truth and the evidence handles addressable.
//
// The packet never carries a confident Summary while Supported is false.
// That invariant is the core contract of this type and is enforced by
// NewAnswerPacket. The implementation moved from root's answer_packet.go for
// #6060 so a handler-family subpackage can build the same packet without
// importing root.
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
	MissingEvidence []EvidenceCitationHandle `json:"missing_evidence,omitempty"`
	// EvidenceHandles are addressable handles to the evidence behind the answer,
	// in the evidence_citation handle shape.
	EvidenceHandles []EvidenceCitationHandle `json:"evidence_handles,omitempty"`
	// CitationRef references a citation packet that hydrates the handles.
	CitationRef string `json:"citation_ref,omitempty"`
	// RecommendedNextCalls lists bounded follow-up calls, in the same shape as
	// the evidence-citation recommended_next_calls.
	RecommendedNextCalls []map[string]any `json:"recommended_next_calls,omitempty"`
	// UnsupportedReasons explains why the answer is unsupported or partial. It
	// is non-empty whenever Supported is false or Partial is true.
	UnsupportedReasons []string `json:"unsupported_reasons,omitempty"`
}

// AnswerPacketInput carries the composition inputs for building an
// AnswerPacket from an existing ResponseEnvelope. The Envelope is required
// and supplies the canonical truth or error; the remaining fields describe
// how the answer was produced and what the caller would like to present.
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
	EvidenceHandles []EvidenceCitationHandle
	// MissingEvidence lists requested-but-unresolved evidence handles.
	MissingEvidence []EvidenceCitationHandle
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
// classified. If the underlying result is truncated, stale, or has no
// resolved evidence, the packet is marked Partial and, for the no-evidence
// case, the Summary is dropped so "no rows" is never presented as a
// confident answer.
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

	if errEnv := answerPacketEnvelopeError(in.Envelope); errEnv != nil {
		return finalizeUnsupportedAnswerPacket(packet, errEnv)
	}

	truth := answerPacketEnvelopeTruth(in.Envelope)
	if truth == nil {
		return finalizeUnsupportedAnswerPacket(packet, &ErrorEnvelope{
			Code:    ErrorCodeInternalError,
			Message: "response envelope carried neither truth nor error",
		})
	}

	packet.Truth = CloneTruthEnvelope(truth)
	packet.TruthClass = ClassifyAnswerTruth(truth)
	packet.Supported = true
	if in.EmbedResult && in.Envelope != nil {
		packet.Result = in.Envelope.Data
	}

	hasEvidence := !in.NoEvidence || len(in.EvidenceHandles) > 0
	markAnswerPacketPartial(&packet, truth, hasEvidence, len(in.MissingEvidence) > 0)

	// A confident summary survives only when the answer is fully usable, or
	// partial yet still backed by resolved evidence. A partial answer with no
	// resolved evidence must not present a confident summary.
	if !packet.Partial || hasEvidence {
		packet.Summary = strings.TrimSpace(in.Summary)
	}
	return packet
}

// ClassifyAnswerTruth folds an existing TruthEnvelope into a single
// prompt-facing AnswerTruthClass. A nil envelope means there is no truth to
// classify and maps to AnswerTruthUnsupported. The ordered rules are
// documented in docs/public/reference/answer-packets.md. The implementation
// moved from root's answer_packet.go for #6060; root's own ClassifyAnswerTruth
// (which sibling packages such as the answer-quality scorecard use to
// re-derive a truth class) now forwards here.
func ClassifyAnswerTruth(truth *TruthEnvelope) AnswerTruthClass {
	if truth == nil {
		return AnswerTruthUnsupported
	}
	switch {
	case truth.Basis == TruthBasisSemanticFacts && truth.Level == TruthLevelExact:
		return AnswerTruthSemanticObservation
	case truth.Basis == TruthBasisAuthoritativeGraph && truth.Level == TruthLevelExact:
		return AnswerTruthDeterministic
	case truth.Basis == TruthBasisNoBackendRead:
		// Stated ahead of the level rule below rather than left to it. The
		// level is already fallback (basisLevel fixes it there), so this
		// changes no outcome today -- it pins the outcome so a future level
		// rule cannot promote a page that read nothing.
		return AnswerTruthFallback
	case truth.Basis == TruthBasisContentIndex && truth.Level != TruthLevelExact:
		return AnswerTruthCodeHint
	case truth.Level == TruthLevelFallback:
		return AnswerTruthFallback
	default:
		return AnswerTruthDerived
	}
}

// finalizeUnsupportedAnswerPacket stamps an unsupported packet from an error
// envelope: it clears the proposed summary, sets the unsupported truth
// class, and records the error as an unsupported reason.
func finalizeUnsupportedAnswerPacket(packet AnswerPacket, errEnv *ErrorEnvelope) AnswerPacket {
	packet.Supported = false
	packet.Partial = false
	packet.Summary = ""
	packet.Truth = nil
	packet.TruthClass = AnswerTruthUnsupported
	packet.UnsupportedReasons = AppendReason(packet.UnsupportedReasons, unsupportedAnswerPacketReason(errEnv))
	return packet
}

// markAnswerPacketPartial sets Partial and records the reasons when a
// supported answer is incomplete: stale or building freshness, an
// unavailable backend, truncation, unresolved evidence handles, or no
// resolved evidence. When the freshness carries a proven cause, the cause is
// folded into the partial reasons and its bounded next check is surfaced as
// a recommended next call, so the packet explains WHY the answer lags and
// WHERE to drill in.
func markAnswerPacketPartial(packet *AnswerPacket, truth *TruthEnvelope, hasEvidence bool, missingEvidence bool) {
	if packet.Truncated {
		packet.Partial = true
		packet.UnsupportedReasons = AppendReason(packet.UnsupportedReasons,
			"result truncated; not all evidence is included")
	}
	if missingEvidence {
		packet.Partial = true
		packet.UnsupportedReasons = AppendReason(packet.UnsupportedReasons,
			"some requested evidence could not be resolved")
	}
	switch truth.Freshness.State {
	case FreshnessStale:
		packet.Partial = true
		packet.UnsupportedReasons = AppendReason(packet.UnsupportedReasons,
			FreshnessReason("underlying data is stale", truth.Freshness.Cause))
	case FreshnessBuilding:
		packet.Partial = true
		packet.UnsupportedReasons = AppendReason(packet.UnsupportedReasons,
			FreshnessReason("underlying index is still building", truth.Freshness.Cause))
	case FreshnessFresh, FreshnessUnavailable:
		// Neither adds a packet limitation. Fresh needs no caveat, and an
		// unavailable freshness signal is already reported by the truth
		// envelope itself rather than as an answer-packet reason. Listed
		// explicitly rather than left to a default so a new state added to
		// the enum fails this switch instead of silently answering as if
		// the data were fresh.
	}
	surfaceAnswerPacketFreshnessNextCheck(packet, truth.Freshness)
	if !hasEvidence {
		packet.Partial = true
		packet.UnsupportedReasons = AppendReason(packet.UnsupportedReasons,
			"no supporting evidence resolved for this question")
	}
}

// FreshnessReason augments a base freshness reason with a proven cause when
// one is present, keeping the base text intact when the cause is unset. It
// never invents a cause; an empty or invalid cause leaves the base reason
// unchanged. The implementation moved from root's answer_packet.go for
// #6060; root's own freshnessReason (used directly by
// investigation_packet_build.go) now forwards here.
func FreshnessReason(base string, cause FreshnessCause) string {
	if !ValidFreshnessCause(cause) {
		return base
	}
	return fmt.Sprintf("%s (cause: %s)", base, cause)
}

// surfaceAnswerPacketFreshnessNextCheck appends the freshness next check to
// the packet's recommended next calls when the freshness carries a proven
// cause and check. It de-duplicates against existing calls so a
// citation-derived call and a freshness drilldown do not collide.
func surfaceAnswerPacketFreshnessNextCheck(packet *AnswerPacket, freshness TruthFreshness) {
	if !ValidFreshnessCause(freshness.Cause) || freshness.NextCheck == nil {
		return
	}
	call := freshnessNextCheckAsRecommendedCall(*freshness.NextCheck)
	if len(call) == 0 {
		return
	}
	for _, existing := range packet.RecommendedNextCalls {
		if answerPacketRecommendedCallsEqual(existing, call) {
			return
		}
	}
	packet.RecommendedNextCalls = append(packet.RecommendedNextCalls, call)
}

// answerPacketRecommendedCallsEqual compares two recommended-next-call maps
// by their tool, route, and reason so a freshness drilldown is not appended
// twice.
func answerPacketRecommendedCallsEqual(a, b map[string]any) bool {
	for _, key := range []string{"tool", "route", "reason"} {
		if fmt.Sprintf("%v", a[key]) != fmt.Sprintf("%v", b[key]) {
			return false
		}
	}
	return true
}

// freshnessNextCheckAsRecommendedCall renders a bounded freshness follow-up
// call as a recommended-next-call map. The implementation moved from root's
// freshness_causality.go for #6060 so a handler-family subpackage can build
// the same call without importing root.
func freshnessNextCheckAsRecommendedCall(next FreshnessNextCheck) map[string]any {
	call := map[string]any{}
	if tool := strings.TrimSpace(next.Tool); tool != "" {
		call["tool"] = tool
	}
	if route := strings.TrimSpace(next.Route); route != "" {
		call["route"] = route
	}
	if reason := strings.TrimSpace(next.Reason); reason != "" {
		call["reason"] = reason
	}
	if len(next.Params) > 0 {
		params := make(map[string]any, len(next.Params))
		for key, value := range next.Params {
			params[key] = value
		}
		call["params"] = params
	}
	return call
}

func answerPacketEnvelopeError(env *ResponseEnvelope) *ErrorEnvelope {
	if env == nil {
		return nil
	}
	return env.Error
}

func answerPacketEnvelopeTruth(env *ResponseEnvelope) *TruthEnvelope {
	if env == nil {
		return nil
	}
	return env.Truth
}

// CloneTruthEnvelope returns a shallow copy of truth, or nil when truth is
// nil. The implementation moved from root's answer_packet.go for #6060;
// root's own cloneTruthEnvelope (used directly by
// investigation_packet_build.go) now forwards here.
func CloneTruthEnvelope(truth *TruthEnvelope) *TruthEnvelope {
	if truth == nil {
		return nil
	}
	cloned := *truth
	return &cloned
}

func unsupportedAnswerPacketReason(errEnv *ErrorEnvelope) string {
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
