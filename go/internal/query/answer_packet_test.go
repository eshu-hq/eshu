// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestAnswerPacketFromExactGraphEnvelopeIsDeterministic(t *testing.T) {
	truth := &TruthEnvelope{
		Level:      TruthLevelExact,
		Capability: "call_graph.direct_callers",
		Profile:    ProfileLocalAuthoritative,
		Basis:      TruthBasisAuthoritativeGraph,
		Freshness:  TruthFreshness{State: FreshnessFresh},
	}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "call_graph.direct_callers",
		Question:     "Who calls AdmitWorkload?",
		PrimaryTool:  "find_callers",
		PrimaryRoute: "POST /api/v0/code/call-graph/callers",
		Summary:      "12 direct callers across 3 repositories.",
		ResultRef:    "eshu://tool-result/envelope",
		Envelope:     &ResponseEnvelope{Data: map[string]any{"callers": 12}, Truth: truth},
	})

	if !packet.Supported {
		t.Fatalf("expected supported packet, got unsupported: %+v", packet)
	}
	if packet.Partial {
		t.Fatalf("did not expect partial packet: %+v", packet)
	}
	if packet.TruthClass != AnswerTruthDeterministic {
		t.Fatalf("expected deterministic truth class, got %q", packet.TruthClass)
	}
	if packet.Summary == "" {
		t.Fatalf("expected a confident summary on a supported answer")
	}
	if packet.Truth == nil || packet.Truth.Level != TruthLevelExact {
		t.Fatalf("expected canonical truth preserved, got %+v", packet.Truth)
	}
	if len(packet.UnsupportedReasons) != 0 {
		t.Fatalf("did not expect unsupported reasons: %v", packet.UnsupportedReasons)
	}
}

func TestAnswerPacketTruthClassMapping(t *testing.T) {
	cases := []struct {
		name  string
		level TruthLevel
		basis TruthBasis
		want  AnswerTruthClass
	}{
		{"graph_exact", TruthLevelExact, TruthBasisAuthoritativeGraph, AnswerTruthDeterministic},
		{"semantic_exact", TruthLevelExact, TruthBasisSemanticFacts, AnswerTruthSemanticObservation},
		{"content_derived", TruthLevelDerived, TruthBasisContentIndex, AnswerTruthCodeHint},
		{"hybrid_derived", TruthLevelDerived, TruthBasisHybrid, AnswerTruthDerived},
		{"content_fallback_is_hint", TruthLevelFallback, TruthBasisContentIndex, AnswerTruthCodeHint},
		{"fallback", TruthLevelFallback, TruthBasisHybrid, AnswerTruthFallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyAnswerTruth(&TruthEnvelope{Level: tc.level, Basis: tc.basis})
			if got != tc.want {
				t.Fatalf("classifyAnswerTruth(%s/%s)=%q want %q", tc.level, tc.basis, got, tc.want)
			}
		})
	}
	if got := classifyAnswerTruth(nil); got != AnswerTruthUnsupported {
		t.Fatalf("nil truth must classify as unsupported, got %q", got)
	}
}

func TestAnswerPacketFromErrorEnvelopeStaysNonConfident(t *testing.T) {
	for _, code := range []ErrorCode{
		ErrorCodeUnsupportedCapability,
		ErrorCodeIndexBuilding,
		ErrorCodeAmbiguous,
	} {
		t.Run(string(code), func(t *testing.T) {
			packet := NewAnswerPacket(AnswerPacketInput{
				PromptFamily: "call_graph.transitive_callers",
				Question:     "Who transitively calls AdmitWorkload?",
				// A confident summary is offered, but the builder MUST drop it
				// because the envelope carries an error.
				Summary: "Everything calls it.",
				Envelope: &ResponseEnvelope{
					Error: &ErrorEnvelope{Code: code, Message: "capability not available at this profile"},
				},
			})

			if packet.Supported {
				t.Fatalf("error envelope %q must not produce a supported packet", code)
			}
			if packet.TruthClass != AnswerTruthUnsupported {
				t.Fatalf("error envelope %q must classify as unsupported, got %q", code, packet.TruthClass)
			}
			if strings.TrimSpace(packet.Summary) != "" {
				t.Fatalf("error envelope %q must not carry a confident summary, got %q", code, packet.Summary)
			}
			if len(packet.UnsupportedReasons) == 0 {
				t.Fatalf("error envelope %q must record an unsupported reason", code)
			}
		})
	}
}

func TestAnswerPacketEmptyEvidenceIsPartialNotConfident(t *testing.T) {
	truth := &TruthEnvelope{
		Level:      TruthLevelDerived,
		Capability: "evidence_citation.packet",
		Basis:      TruthBasisContentIndex,
		Freshness:  TruthFreshness{State: FreshnessFresh},
	}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "evidence_citation.packet",
		Question:     "Cite the evidence for AdmitWorkload.",
		Summary:      "Here is the evidence.",
		Envelope:     &ResponseEnvelope{Data: map[string]any{}, Truth: truth},
		// No resolved evidence handles: the question is answerable but nothing
		// resolved, so the packet must be partial, never a confident "no".
		NoEvidence: true,
	})

	if !packet.Supported {
		t.Fatalf("a derived capability with no evidence is still supported, just partial")
	}
	if !packet.Partial {
		t.Fatalf("empty evidence must mark the packet partial: %+v", packet)
	}
	if strings.TrimSpace(packet.Summary) != "" {
		t.Fatalf("partial answer with no evidence must not carry a confident summary, got %q", packet.Summary)
	}
	if len(packet.UnsupportedReasons) == 0 {
		t.Fatalf("partial answer must record why it is partial")
	}
}

func TestAnswerPacketMissingEvidenceIsPartialWithSummary(t *testing.T) {
	truth := &TruthEnvelope{
		Level:      TruthLevelDerived,
		Capability: "evidence_citation.packet",
		Basis:      TruthBasisContentIndex,
		Freshness:  TruthFreshness{State: FreshnessFresh},
	}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily:    "evidence_citation.packet",
		Question:        "Cite the evidence for AdmitWorkload.",
		Summary:         "1 citation resolved.",
		EvidenceHandles: []evidenceCitationHandle{{Kind: "entity", EntityID: "go:func:AdmitWorkload"}},
		MissingEvidence: []evidenceCitationHandle{{Kind: "file", RepoID: "r1", RelativePath: "missing.go"}},
		Envelope:        &ResponseEnvelope{Data: map[string]any{}, Truth: truth},
	})

	if !packet.Supported {
		t.Fatalf("missing evidence on a supported answer must remain supported")
	}
	if !packet.Partial {
		t.Fatalf("missing evidence must mark the packet partial: %+v", packet)
	}
	if strings.TrimSpace(packet.Summary) == "" {
		t.Fatalf("partial answer with resolved evidence should keep its summary")
	}
	if len(packet.UnsupportedReasons) == 0 {
		t.Fatalf("missing evidence must record why the packet is partial")
	}
}

func TestAnswerPacketFromCitationResponseMapsEvidence(t *testing.T) {
	truth := &TruthEnvelope{
		Level:      TruthLevelDerived,
		Capability: "evidence_citation.packet",
		Basis:      TruthBasisContentIndex,
		Freshness:  TruthFreshness{State: FreshnessFresh},
	}
	citation := evidenceCitationResponse{
		Question:  "Cite the evidence for AdmitWorkload.",
		Citations: []evidenceCitation{{CitationID: "citation:abc", Kind: "entity", EntityID: "go:func:AdmitWorkload"}},
		MissingHandles: []evidenceCitationHandle{
			{Kind: "file", RepoID: "r1", RelativePath: "missing.go"},
		},
		Coverage:             evidenceCitationCoverage{ResolvedCount: 1, MissingCount: 1, Truncated: true},
		RecommendedNextCalls: []map[string]any{{"tool": "search_file_content", "reason": "rediscover"}},
	}
	packet := NewAnswerPacketFromCitations(AnswerPacketInput{
		PromptFamily: "evidence_citation.packet",
		Question:     "Cite the evidence for AdmitWorkload.",
		Summary:      "1 citation resolved.",
		Envelope:     &ResponseEnvelope{Data: citation, Truth: truth},
	}, citation)

	if !packet.Supported {
		t.Fatalf("resolved citation packet must be supported")
	}
	if !packet.Truncated {
		t.Fatalf("expected truncated mirrored from coverage")
	}
	if len(packet.EvidenceHandles) != 1 || packet.EvidenceHandles[0].EntityID != "go:func:AdmitWorkload" {
		t.Fatalf("expected one evidence handle mapped from citations, got %+v", packet.EvidenceHandles)
	}
	if len(packet.MissingEvidence) != 1 {
		t.Fatalf("expected one missing-evidence handle, got %+v", packet.MissingEvidence)
	}
	if len(packet.RecommendedNextCalls) != 1 {
		t.Fatalf("expected recommended next calls passed through, got %+v", packet.RecommendedNextCalls)
	}
	// Truncation makes it partial, but evidence resolved so summary survives.
	if !packet.Partial {
		t.Fatalf("truncated citation packet must be partial")
	}
	if packet.Summary == "" {
		t.Fatalf("a partial-but-resolved answer keeps its summary")
	}
}

// TestAnswerPacketSurfacesStaleFreshnessCause proves a stale envelope carrying a
// proven cause folds the cause into the partial reasons and surfaces its bounded
// next check, while keeping the answer usable (still supported, still partial).
func TestAnswerPacketSurfacesStaleFreshnessCause(t *testing.T) {
	truth := &TruthEnvelope{
		Level:     TruthLevelDerived,
		Basis:     TruthBasisSemanticFacts,
		Freshness: TruthFreshness{State: FreshnessStale},
	}
	WithFreshnessCause(truth, FreshnessCauseReducerBacklog)

	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "platform_metrics.timeseries",
		Question:     "ingest rate trend",
		Summary:      "trend over 24h",
		Envelope:     &ResponseEnvelope{Data: map[string]any{"points": 3}, Truth: truth},
	})

	if !packet.Supported || !packet.Partial {
		t.Fatalf("expected supported+partial stale packet, got %+v", packet)
	}
	foundReason := false
	for _, reason := range packet.UnsupportedReasons {
		if strings.Contains(reason, string(FreshnessCauseReducerBacklog)) {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("expected the reducer_backlog cause in partial reasons, got %v", packet.UnsupportedReasons)
	}
	foundCall := false
	for _, call := range packet.RecommendedNextCalls {
		if _, ok := call["reason"]; ok {
			foundCall = true
		}
	}
	if !foundCall {
		t.Fatalf("expected a freshness next-check recommended call, got %+v", packet.RecommendedNextCalls)
	}
}

// TestAnswerPacketWithoutFreshnessCauseStaysGeneric proves that when no cause is
// proven, the packet keeps the generic stale reason and adds no freshness next
// call: the packet never invents a cause.
func TestAnswerPacketWithoutFreshnessCauseStaysGeneric(t *testing.T) {
	truth := &TruthEnvelope{
		Level:     TruthLevelDerived,
		Basis:     TruthBasisSemanticFacts,
		Freshness: TruthFreshness{State: FreshnessStale},
	}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "platform_metrics.timeseries",
		Question:     "ingest rate trend",
		Summary:      "trend over 24h",
		Envelope:     &ResponseEnvelope{Data: map[string]any{"points": 3}, Truth: truth},
	})
	for _, reason := range packet.UnsupportedReasons {
		if strings.Contains(reason, "cause:") {
			t.Fatalf("did not expect an invented cause, got %v", packet.UnsupportedReasons)
		}
	}
	if len(packet.RecommendedNextCalls) != 0 {
		t.Fatalf("did not expect a freshness next call without a cause, got %+v", packet.RecommendedNextCalls)
	}
}
