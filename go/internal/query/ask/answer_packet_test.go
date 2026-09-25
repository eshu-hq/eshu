// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ask

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

func TestAnswerPacketFromExactGraphEnvelopeIsDeterministic(t *testing.T) {
	truth := &querycontract.TruthEnvelope{
		Level:      querycontract.TruthLevelExact,
		Capability: "call_graph.direct_callers",
		Profile:    querycontract.ProfileLocalAuthoritative,
		Basis:      querycontract.TruthBasisAuthoritativeGraph,
		Freshness:  querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
	}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "call_graph.direct_callers",
		Question:     "Who calls AdmitWorkload?",
		PrimaryTool:  "find_callers",
		PrimaryRoute: "POST /api/v0/code/call-graph/callers",
		Summary:      "12 direct callers across 3 repositories.",
		ResultRef:    "eshu://tool-result/envelope",
		Envelope:     &querycontract.ResponseEnvelope{Data: map[string]any{"callers": 12}, Truth: truth},
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
	if packet.Truth == nil || packet.Truth.Level != querycontract.TruthLevelExact {
		t.Fatalf("expected canonical truth preserved, got %+v", packet.Truth)
	}
	if len(packet.UnsupportedReasons) != 0 {
		t.Fatalf("did not expect unsupported reasons: %v", packet.UnsupportedReasons)
	}
}

func TestAnswerPacketTruthClassMapping(t *testing.T) {
	cases := []struct {
		name  string
		level querycontract.TruthLevel
		basis querycontract.TruthBasis
		want  AnswerTruthClass
	}{
		{"graph_exact", querycontract.TruthLevelExact, querycontract.TruthBasisAuthoritativeGraph, AnswerTruthDeterministic},
		{"semantic_exact", querycontract.TruthLevelExact, querycontract.TruthBasisSemanticFacts, AnswerTruthSemanticObservation},
		{"content_derived", querycontract.TruthLevelDerived, querycontract.TruthBasisContentIndex, AnswerTruthCodeHint},
		{"hybrid_derived", querycontract.TruthLevelDerived, querycontract.TruthBasisHybrid, AnswerTruthDerived},
		{"content_fallback_is_hint", querycontract.TruthLevelFallback, querycontract.TruthBasisContentIndex, AnswerTruthCodeHint},
		{"fallback", querycontract.TruthLevelFallback, querycontract.TruthBasisHybrid, AnswerTruthFallback},
		{"no_backend_read", querycontract.TruthLevelFallback, querycontract.TruthBasisNoBackendRead, AnswerTruthFallback},
		// The level here cannot occur through BuildTruthEnvelope (basisLevel
		// fixes a no-read basis at fallback), and that is the point: it proves
		// classifyAnswerTruth answers from its own no_backend_read case rather
		// than from the level. Without that case this row reaches the default
		// arm and a page that read nothing classifies as "derived".
		{"no_backend_read_never_upgrades", querycontract.TruthLevelExact, querycontract.TruthBasisNoBackendRead, AnswerTruthFallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyAnswerTruth(&querycontract.TruthEnvelope{Level: tc.level, Basis: tc.basis})
			if got != tc.want {
				t.Fatalf("ClassifyAnswerTruth(%s/%s)=%q want %q", tc.level, tc.basis, got, tc.want)
			}
		})
	}
	if got := ClassifyAnswerTruth(nil); got != AnswerTruthUnsupported {
		t.Fatalf("nil truth must classify as unsupported, got %q", got)
	}
}

func TestAnswerPacketFromErrorEnvelopeStaysNonConfident(t *testing.T) {
	for _, code := range []querycontract.ErrorCode{
		querycontract.ErrorCodeUnsupportedCapability,
		querycontract.ErrorCodeIndexBuilding,
		querycontract.ErrorCodeAmbiguous,
	} {
		t.Run(string(code), func(t *testing.T) {
			packet := NewAnswerPacket(AnswerPacketInput{
				PromptFamily: "call_graph.transitive_callers",
				Question:     "Who transitively calls AdmitWorkload?",
				// A confident summary is offered, but the builder MUST drop it
				// because the envelope carries an error.
				Summary: "Everything calls it.",
				Envelope: &querycontract.ResponseEnvelope{
					Error: &querycontract.ErrorEnvelope{Code: code, Message: "capability not available at this profile"},
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
	truth := &querycontract.TruthEnvelope{
		Level:      querycontract.TruthLevelDerived,
		Capability: "evidence_citation.packet",
		Basis:      querycontract.TruthBasisContentIndex,
		Freshness:  querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
	}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "evidence_citation.packet",
		Question:     "Cite the evidence for AdmitWorkload.",
		Summary:      "Here is the evidence.",
		Envelope:     &querycontract.ResponseEnvelope{Data: map[string]any{}, Truth: truth},
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
	truth := &querycontract.TruthEnvelope{
		Level:      querycontract.TruthLevelDerived,
		Capability: "evidence_citation.packet",
		Basis:      querycontract.TruthBasisContentIndex,
		Freshness:  querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
	}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily:    "evidence_citation.packet",
		Question:        "Cite the evidence for AdmitWorkload.",
		Summary:         "1 citation resolved.",
		EvidenceHandles: []evidence.EvidenceCitationHandle{{Kind: "entity", EntityID: "go:func:AdmitWorkload"}},
		MissingEvidence: []evidence.EvidenceCitationHandle{{Kind: "file", RepoID: "r1", RelativePath: "missing.go"}},
		Envelope:        &querycontract.ResponseEnvelope{Data: map[string]any{}, Truth: truth},
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
	truth := &querycontract.TruthEnvelope{
		Level:      querycontract.TruthLevelDerived,
		Capability: "evidence_citation.packet",
		Basis:      querycontract.TruthBasisContentIndex,
		Freshness:  querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
	}
	citation := evidence.EvidenceCitationResponse{
		Question:  "Cite the evidence for AdmitWorkload.",
		Citations: []evidence.EvidenceCitation{{CitationID: "citation:abc", Kind: "entity", EntityID: "go:func:AdmitWorkload"}},
		MissingHandles: []evidence.EvidenceCitationHandle{
			{Kind: "file", RepoID: "r1", RelativePath: "missing.go"},
		},
		Coverage:             evidence.EvidenceCitationCoverage{ResolvedCount: 1, MissingCount: 1, Truncated: true},
		RecommendedNextCalls: []map[string]any{{"tool": "search_file_content", "reason": "rediscover"}},
	}
	packet := NewAnswerPacketFromCitations(AnswerPacketInput{
		PromptFamily: "evidence_citation.packet",
		Question:     "Cite the evidence for AdmitWorkload.",
		Summary:      "1 citation resolved.",
		Envelope:     &querycontract.ResponseEnvelope{Data: citation, Truth: truth},
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
	truth := &querycontract.TruthEnvelope{
		Level:     querycontract.TruthLevelDerived,
		Basis:     querycontract.TruthBasisSemanticFacts,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessStale},
	}
	querycontract.WithFreshnessCause(truth, querycontract.FreshnessCauseReducerBacklog)

	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "platform_metrics.timeseries",
		Question:     "ingest rate trend",
		Summary:      "trend over 24h",
		Envelope:     &querycontract.ResponseEnvelope{Data: map[string]any{"points": 3}, Truth: truth},
	})

	if !packet.Supported || !packet.Partial {
		t.Fatalf("expected supported+partial stale packet, got %+v", packet)
	}
	foundReason := false
	for _, reason := range packet.UnsupportedReasons {
		if strings.Contains(reason, string(querycontract.FreshnessCauseReducerBacklog)) {
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
	truth := &querycontract.TruthEnvelope{
		Level:     querycontract.TruthLevelDerived,
		Basis:     querycontract.TruthBasisSemanticFacts,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessStale},
	}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "platform_metrics.timeseries",
		Question:     "ingest rate trend",
		Summary:      "trend over 24h",
		Envelope:     &querycontract.ResponseEnvelope{Data: map[string]any{"points": 3}, Truth: truth},
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
