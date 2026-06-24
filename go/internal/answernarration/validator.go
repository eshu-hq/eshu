// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answernarration

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	defaultMaxSentences       = 50
	defaultMaxSentenceBytes   = 4096
	defaultMaxRefsPerSentence = 8
)

var unsafePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(api[_-]?key|password|secret)\s*=`),
	regexp.MustCompile(`(?i)\b(raw prompt|provider response|provider error body)\b`),
	regexp.MustCompile(`(?i)(/Users/|/home/|C:\\Users\\)`),
	regexp.MustCompile(`(?i)\b[a-z0-9.-]+\.(internal|corp|local)\b`),
	regexp.MustCompile(`(?i)\bhttps?://(10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)`),
}

// Validate checks a candidate narrated answer against a source AnswerPacket.
func Validate(input Input) Verdict {
	check := validation{
		input:       input,
		citationIDs: stringSet(input.CitationIDs),
	}
	check.validateLimits()
	check.validatePacketState()
	check.validateSentences()
	return Verdict{
		Valid:    len(check.findings) == 0,
		Findings: check.findings,
	}
}

type validation struct {
	input       Input
	citationIDs map[string]struct{}
	findings    []Finding
}

func (v *validation) validateLimits() {
	maxSentences := v.input.MaxSentences
	if maxSentences <= 0 {
		maxSentences = defaultMaxSentences
	}
	maxSentenceBytes := v.input.MaxSentenceBytes
	if maxSentenceBytes <= 0 {
		maxSentenceBytes = defaultMaxSentenceBytes
	}
	maxRefsPerSentence := v.input.MaxRefsPerSentence
	if maxRefsPerSentence <= 0 {
		maxRefsPerSentence = defaultMaxRefsPerSentence
	}
	if len(v.input.Response.Sentences) > maxSentences {
		v.add(ReasonOverLimit, -1, fmt.Sprintf("sentences=%d max=%d",
			len(v.input.Response.Sentences), maxSentences))
	}
	for i, sentence := range v.input.Response.Sentences {
		if len(sentence.Text) > maxSentenceBytes {
			v.add(ReasonOverLimit, i, fmt.Sprintf("sentence_bytes=%d max=%d",
				len(sentence.Text), maxSentenceBytes))
		}
		if len(sentence.Provenance) > maxRefsPerSentence {
			v.add(ReasonOverLimit, i, fmt.Sprintf("refs=%d max=%d",
				len(sentence.Provenance), maxRefsPerSentence))
		}
	}
}

func (v *validation) validatePacketState() {
	packet := v.input.Packet
	response := v.input.Response
	if !packet.Supported && response.Supported {
		v.add(ReasonUnsupportedConfidenceDrift, -1, "unsupported packet narrated as supported")
	}
	if packet.Partial && !response.Partial {
		v.add(ReasonPartialSignalHidden, -1, "partial packet narrated as complete")
	}
	if packet.Truncated && !response.Partial {
		v.add(ReasonPartialSignalHidden, -1, "truncated packet narrated as complete")
	}
	if packetHasPartialSignal(packet) && !v.responseSurfacesPartialSignal() {
		v.add(ReasonPartialSignalHidden, -1, "partial packet lacks narrated provenance")
	}
	if promotesTruth(packet.TruthClass, response.TruthClass) {
		v.add(ReasonTruthClassPromotion, -1,
			fmt.Sprintf("source=%s response=%s", packet.TruthClass, response.TruthClass))
	}
}

func (v *validation) validateSentences() {
	for i, sentence := range v.input.Response.Sentences {
		if unsafeText(sentence.Text) {
			v.add(ReasonUnsafeOutput, i, "sentence contains publish-unsafe material")
		}
		if sentence.Kind == SentenceFactual && !v.hasAllowedProvenance(sentence) {
			v.add(ReasonUncitedFactualSentence, i, "factual sentence lacks allowed provenance")
		}
		for _, ref := range sentence.Provenance {
			if !v.provenanceAllowed(ref) {
				v.add(ReasonUnknownProvenance, i, string(ref.Kind))
			}
		}
	}
}

func (v *validation) hasAllowedProvenance(sentence Sentence) bool {
	for _, ref := range sentence.Provenance {
		if v.provenanceAllowed(ref) {
			return true
		}
	}
	return false
}

func (v *validation) provenanceAllowed(ref ProvenanceRef) bool {
	id := strings.TrimSpace(ref.ID)
	packet := v.input.Packet
	switch ref.Kind {
	case ProvenanceCitation:
		if id == "" {
			return false
		}
		if _, ok := v.citationIDs[id]; ok {
			return true
		}
		return id == strings.TrimSpace(packet.CitationRef)
	case ProvenanceLimitation:
		return matchesPacketText(id, packet.Limitations)
	case ProvenanceUnsupportedReason:
		return matchesPacketText(id, packet.UnsupportedReasons)
	case ProvenanceFreshness:
		return packet.Truth != nil || packet.Partial
	case ProvenanceTruth:
		return packet.TruthClass != ""
	case ProvenanceNextCall:
		return len(packet.RecommendedNextCalls) > 0
	default:
		return false
	}
}

func (v *validation) responseSurfacesPartialSignal() bool {
	for _, sentence := range v.input.Response.Sentences {
		for _, ref := range sentence.Provenance {
			if ref.Kind == ProvenanceLimitation ||
				ref.Kind == ProvenanceUnsupportedReason ||
				ref.Kind == ProvenanceFreshness {
				return v.provenanceAllowed(ref)
			}
		}
	}
	return false
}

func (v *validation) add(reason Reason, index int, detail string) {
	v.findings = append(v.findings, Finding{
		Reason: reason,
		Index:  index,
		Detail: detail,
	})
}

func promotesTruth(source, response query.AnswerTruthClass) bool {
	if response != query.AnswerTruthDeterministic {
		return false
	}
	return source == query.AnswerTruthDerived ||
		source == query.AnswerTruthFallback ||
		source == query.AnswerTruthCodeHint ||
		source == query.AnswerTruthSemanticObservation ||
		source == query.AnswerTruthUnsupported
}

func packetHasPartialSignal(packet query.AnswerPacket) bool {
	return packet.Partial ||
		packet.Truncated ||
		len(packet.Limitations) > 0 ||
		len(packet.MissingEvidence) > 0 ||
		len(packet.UnsupportedReasons) > 0
}

func unsafeText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, pattern := range unsafePatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func matchesPacketText(id string, values []string) bool {
	id = strings.TrimSpace(id)
	if len(values) == 0 || id == "" {
		return false
	}
	for _, value := range values {
		if id == strings.TrimSpace(value) {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}
