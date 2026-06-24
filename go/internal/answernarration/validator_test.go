// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answernarration

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestValidateRejectsUncitedFactualSentence(t *testing.T) {
	packet := supportedPacket()
	response := Narration{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Sentences: []Sentence{
			{Text: "Checkout calls ChargeCard directly.", Kind: SentenceFactual},
		},
	}

	verdict := Validate(Input{
		Packet:      packet,
		Response:    response,
		CitationIDs: []string{"citation:checkout-chargecard"},
	})

	requireFinding(t, verdict, ReasonUncitedFactualSentence)
}

func TestValidateAcceptsCitedFactualSentence(t *testing.T) {
	packet := supportedPacket()
	response := Narration{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Sentences: []Sentence{
			{
				Text: "Checkout calls ChargeCard directly.",
				Kind: SentenceFactual,
				Provenance: []ProvenanceRef{
					{Kind: ProvenanceCitation, ID: "citation:checkout-chargecard"},
				},
			},
		},
	}

	verdict := Validate(Input{
		Packet:      packet,
		Response:    response,
		CitationIDs: []string{"citation:checkout-chargecard"},
	})

	if !verdict.Valid {
		t.Fatalf("Validate() valid = false, findings = %#v", verdict.Findings)
	}
}

func TestValidateRejectsUnsupportedConfidenceDrift(t *testing.T) {
	packet := query.AnswerPacket{
		TruthClass:         query.AnswerTruthUnsupported,
		Supported:          false,
		UnsupportedReasons: []string{"unsupported_capability: graph not available"},
	}
	response := Narration{
		TruthClass: query.AnswerTruthDerived,
		Supported:  true,
		Sentences: []Sentence{
			{
				Text: "The graph can answer this question.",
				Kind: SentenceUnsupportedReason,
				Provenance: []ProvenanceRef{
					{Kind: ProvenanceUnsupportedReason, ID: "unsupported_capability: graph not available"},
				},
			},
		},
	}

	verdict := Validate(Input{Packet: packet, Response: response})

	requireFinding(t, verdict, ReasonUnsupportedConfidenceDrift)
}

func TestValidateRejectsPartialSignalHiding(t *testing.T) {
	packet := supportedPacket()
	packet.Partial = true
	packet.Truncated = true
	packet.UnsupportedReasons = []string{"result truncated; not all evidence is included"}
	response := Narration{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Partial:    false,
		Sentences: []Sentence{
			{
				Text: "Checkout has 50 callers.",
				Kind: SentenceFactual,
				Provenance: []ProvenanceRef{
					{Kind: ProvenanceCitation, ID: "citation:checkout"},
				},
			},
		},
	}

	verdict := Validate(Input{
		Packet:      packet,
		Response:    response,
		CitationIDs: []string{"citation:checkout"},
	})

	requireFinding(t, verdict, ReasonPartialSignalHidden)
}

func TestValidateRejectsPartialSignalWithoutNarratedProvenance(t *testing.T) {
	packet := supportedPacket()
	packet.Partial = true
	packet.Truncated = true
	packet.UnsupportedReasons = []string{"result truncated; not all evidence is included"}
	response := Narration{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Partial:    true,
		Sentences: []Sentence{
			{
				Text: "Checkout has 50 callers.",
				Kind: SentenceFactual,
				Provenance: []ProvenanceRef{
					{Kind: ProvenanceCitation, ID: "citation:checkout"},
				},
			},
		},
	}

	verdict := Validate(Input{
		Packet:      packet,
		Response:    response,
		CitationIDs: []string{"citation:checkout"},
	})

	requireFinding(t, verdict, ReasonPartialSignalHidden)
}

func TestValidateRejectsEmptyLimitationProvenance(t *testing.T) {
	packet := supportedPacket()
	packet.Limitations = []string{"results bounded to 50 callers"}
	response := Narration{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Sentences: []Sentence{
			{
				Text: "The result is bounded.",
				Kind: SentenceLimitation,
				Provenance: []ProvenanceRef{
					{Kind: ProvenanceLimitation},
				},
			},
		},
	}

	verdict := Validate(Input{Packet: packet, Response: response})

	requireFinding(t, verdict, ReasonUnknownProvenance)
}

func TestValidateRejectsTruthClassPromotion(t *testing.T) {
	packet := supportedPacket()
	packet.TruthClass = query.AnswerTruthCodeHint
	response := Narration{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Sentences: []Sentence{
			{
				Text: "Checkout probably owns the payment flow.",
				Kind: SentenceFactual,
				Provenance: []ProvenanceRef{
					{Kind: ProvenanceCitation, ID: "citation:checkout"},
				},
			},
		},
	}

	verdict := Validate(Input{
		Packet:      packet,
		Response:    response,
		CitationIDs: []string{"citation:checkout"},
	})

	requireFinding(t, verdict, ReasonTruthClassPromotion)
}

func TestValidateRejectsUnsafeOutput(t *testing.T) {
	packet := supportedPacket()
	response := Narration{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Sentences: []Sentence{
			{
				Text: "The runbook lives at /Users/example/private/repo.",
				Kind: SentenceFactual,
				Provenance: []ProvenanceRef{
					{Kind: ProvenanceCitation, ID: "citation:runbook"},
				},
			},
		},
	}

	verdict := Validate(Input{
		Packet:      packet,
		Response:    response,
		CitationIDs: []string{"citation:runbook"},
	})

	requireFinding(t, verdict, ReasonUnsafeOutput)
}

func TestValidateRejectsTooManyRefsPerSentence(t *testing.T) {
	packet := supportedPacket()
	response := Narration{
		TruthClass: query.AnswerTruthDeterministic,
		Supported:  true,
		Sentences: []Sentence{
			{
				Text: "Checkout calls ChargeCard directly.",
				Kind: SentenceFactual,
				Provenance: []ProvenanceRef{
					{Kind: ProvenanceCitation, ID: "citation:checkout"},
					{Kind: ProvenanceCitation, ID: "citation:chargecard"},
				},
			},
		},
	}

	verdict := Validate(Input{
		Packet:             packet,
		Response:           response,
		CitationIDs:        []string{"citation:checkout", "citation:chargecard"},
		MaxRefsPerSentence: 1,
	})

	requireFinding(t, verdict, ReasonOverLimit)
}

func supportedPacket() query.AnswerPacket {
	return query.AnswerPacket{
		TruthClass:  query.AnswerTruthDeterministic,
		Supported:   true,
		Partial:     false,
		Summary:     "Checkout calls ChargeCard directly.",
		CitationRef: "citation:packet",
	}
}

func requireFinding(t *testing.T, verdict Verdict, reason Reason) {
	t.Helper()
	for _, finding := range verdict.Findings {
		if finding.Reason == reason {
			return
		}
	}
	t.Fatalf("Validate() findings missing %q: %#v", reason, verdict.Findings)
}
