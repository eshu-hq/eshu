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
